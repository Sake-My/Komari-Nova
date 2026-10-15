package jsonrpc

import (
	"context"
	"net/http"
	"testing"

	"github.com/Sake-My/Komari-Nova/database/dbcore"
	"github.com/Sake-My/Komari-Nova/database/models"
	"github.com/Sake-My/Komari-Nova/pkg/rpc"
	"github.com/Sake-My/Komari-Nova/utils/geoip"
)

func TestAdminEditClientRejectsInvalidRegionWithoutPartialUpdate(t *testing.T) {
	db := dbcore.GetDBInstance()
	const clientUUID = "rpc-invalid-region"
	original := models.Client{
		UUID:           clientUUID,
		Token:          "rpc-invalid-region-token",
		Name:           "original-name",
		Region:         geoip.GetRegionUnicodeEmoji("SG"),
		RegionOverride: "JP",
	}
	if err := db.Create(&original).Error; err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := db.Delete(&models.Client{}, "uuid = ?", clientUUID).Error; err != nil {
			t.Errorf("delete test client: %v", err)
		}
	})

	for _, test := range []struct {
		name   string
		fields map[string]any
	}{
		{name: "null", fields: map[string]any{"region_override": nil}},
		{name: "number", fields: map[string]any{"region_override": 1}},
		{name: "three-letters", fields: map[string]any{"region_override": "USA"}},
		{name: "unknown-code", fields: map[string]any{"region_override": "ZZ"}},
		{name: "emoji", fields: map[string]any{"region_override": "🇯🇵"}},
		{name: "go-field-alias", fields: map[string]any{"RegionOverride": "US"}},
		{name: "sql-case-alias", fields: map[string]any{"REGION_OVERRIDE": "US"}},
		{name: "mixed-case-alias", fields: map[string]any{"rEgIoNoVeRrIdE": "US"}},
		{name: "conflicting-alias", fields: map[string]any{"region_override": "DE", "RegionOverride": "US"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			params := map[string]any{"uuid": clientUUID, "name": "must-not-be-saved"}
			for key, value := range test.fields {
				params[key] = value
			}
			_, rpcErr := adminEditClient(context.Background(), rpc.NewRequest(1, "admin:editClient", params))
			if rpcErr == nil || rpcErr.Code != rpc.InvalidParams {
				t.Fatalf("invalid region returned %v, want InvalidParams", rpcErr)
			}
			if status := rpcErrorHTTPStatus(rpcErr.Code); status != http.StatusBadRequest {
				t.Fatalf("REST status = %d, want 400", status)
			}
			var stored models.Client
			if err := db.First(&stored, "uuid = ?", clientUUID).Error; err != nil {
				t.Fatal(err)
			}
			if stored.Name != original.Name || stored.Region != original.Region || stored.RegionOverride != original.RegionOverride || !stored.UpdatedAt.Equal(original.UpdatedAt) {
				t.Fatalf("invalid update changed stored client: name=%q region=%q override=%q updated_at=%v", stored.Name, stored.Region, stored.RegionOverride, stored.UpdatedAt)
			}
		})
	}
}

func TestAdminRegionOverrideRoundTripThroughPublicReads(t *testing.T) {
	db := dbcore.GetDBInstance()
	const clientUUID = "rpc-visible-region"
	automaticRegion := geoip.GetRegionUnicodeEmoji("SG")
	if err := db.Create(&models.Client{
		UUID: clientUUID, Token: "rpc-visible-region-token", Name: "visible-region", Region: automaticRegion,
	}).Error; err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := db.Delete(&models.Client{}, "uuid = ?", clientUUID).Error; err != nil {
			t.Errorf("delete test client: %v", err)
		}
	})

	for _, test := range []struct {
		name     string
		input    string
		wantCode string
		wantFlag string
	}{
		{name: "manual", input: " jp ", wantCode: "JP", wantFlag: geoip.GetRegionUnicodeEmoji("JP")},
		{name: "automatic", input: "", wantCode: "", wantFlag: automaticRegion},
	} {
		t.Run(test.name, func(t *testing.T) {
			params := map[string]any{"uuid": clientUUID, "region_override": test.input}
			if _, rpcErr := adminEditClient(context.Background(), rpc.NewRequest(1, "admin:editClient", params)); rpcErr != nil {
				t.Fatalf("edit client: %v", rpcErr)
			}
			var stored models.Client
			if err := db.First(&stored, "uuid = ?", clientUUID).Error; err != nil {
				t.Fatal(err)
			}
			if stored.Region != automaticRegion || stored.RegionOverride != test.wantCode {
				t.Fatalf("stored region=%q override=%q", stored.Region, stored.RegionOverride)
			}

			result, rpcErr := publicGetNodesInformation(context.Background(), rpc.NewRequest(2, "public:getNodesInformation", nil))
			if rpcErr != nil {
				t.Fatal(rpcErr)
			}
			visible, ok := result.([]models.Client)
			if !ok {
				t.Fatalf("unexpected public node result %T", result)
			}
			found := false
			for _, node := range visible {
				if node.UUID == clientUUID {
					found = true
					if node.Region != test.wantFlag || node.Token != "" {
						t.Fatalf("public region=%q token=%q", node.Region, node.Token)
					}
				}
			}
			if !found {
				t.Fatal("test client missing from public nodes")
			}

			ctx := rpc.NewContextWithMeta(context.Background(), &rpc.ContextMeta{Principal: rpc.NewAnonymousPrincipal()})
			result, rpcErr = getNodes(ctx, rpc.NewRequest(3, "common:getNodes", map[string]any{"uuid": clientUUID}))
			if rpcErr != nil {
				t.Fatal(rpcErr)
			}
			node, ok := result.(models.Client)
			if !ok || node.Region != test.wantFlag || node.RegionOverride != test.wantCode {
				t.Fatalf("common:getNodes returned %#v", result)
			}
		})
	}
}
