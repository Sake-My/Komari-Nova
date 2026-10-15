package client

import (
	"net"
	"testing"
	"time"

	"github.com/Sake-My/Komari-Nova/cmd/flags"
	"github.com/Sake-My/Komari-Nova/database/clients"
	"github.com/Sake-My/Komari-Nova/database/dbcore"
	"github.com/Sake-My/Komari-Nova/database/models"
	"github.com/Sake-My/Komari-Nova/internal/config"
	v2 "github.com/Sake-My/Komari-Nova/protocol/v2"
	"github.com/Sake-My/Komari-Nova/utils/geoip"
)

type staticGeoIPProvider struct {
	name string
	iso  string
}

func (p staticGeoIPProvider) Name() string {
	return p.name
}

func (p staticGeoIPProvider) GetGeoInfo(ip net.IP) (*geoip.GeoInfo, error) {
	return &geoip.GeoInfo{ISOCode: p.iso, Name: p.iso}, nil
}

func (p staticGeoIPProvider) UpdateDatabase() error {
	return nil
}

func (p staticGeoIPProvider) Close() error {
	return nil
}

func TestV2BasicInfoFillsRegionFromGeoIP(t *testing.T) {
	flags.DatabaseType = "sqlite"
	flags.DatabaseFile = "file:v2_basic_info_geoip?mode=memory&cache=shared"

	db := dbcore.GetDBInstance()
	if err := config.Set(config.GeoIpEnabledKey, true); err != nil {
		t.Fatalf("enable geoip: %v", err)
	}

	oldProvider := geoip.CurrentProvider
	geoip.CurrentProvider = staticGeoIPProvider{name: t.Name(), iso: "SG"}
	t.Cleanup(func() {
		geoip.CurrentProvider = oldProvider
	})

	clientUUID := "client-v2-geoip"
	now := time.Now().UTC()
	if err := db.Create(&models.Client{
		UUID:      clientUUID,
		Token:     "token-v2-geoip",
		Name:      "client_v2_geoip",
		CreatedAt: now,
		UpdatedAt: now,
	}).Error; err != nil {
		t.Fatalf("create client: %v", err)
	}

	resp := handleV2RPC(clientUUID, v2.Request{
		JSONRPC: v2.Version,
		Method:  v2.MethodAgentBasicInfo,
		Params: map[string]interface{}{
			"info": map[string]interface{}{
				"ipv4": "8.8.8.8",
			},
		},
		ID: "basic-info",
	}, false)
	if resp.Error != nil {
		t.Fatalf("v2 basic info failed: %+v", resp.Error)
	}

	var got models.Client
	if err := db.First(&got, "uuid = ?", clientUUID).Error; err != nil {
		t.Fatalf("load client: %v", err)
	}
	want := geoip.GetRegionUnicodeEmoji("SG")
	if got.Region != want {
		t.Fatalf("expected GeoIP region to be saved, got %q", got.Region)
	}
}

func TestV2BasicInfoPreservesManualRegionAndRefreshesAutomaticRegion(t *testing.T) {
	flags.DatabaseType = flags.DatabaseTypeSQLite
	flags.DatabaseFile = "file:v2_basic_info_region_override?mode=memory&cache=shared"
	db := dbcore.GetDBInstance()
	oldEnabled, err := config.GetAs[bool](config.GeoIpEnabledKey, true)
	if err != nil {
		t.Fatal(err)
	}
	if err := config.Set(config.GeoIpEnabledKey, true); err != nil {
		t.Fatal(err)
	}
	oldProvider := geoip.CurrentProvider
	t.Cleanup(func() {
		geoip.CurrentProvider = oldProvider
		if err := config.Set(config.GeoIpEnabledKey, oldEnabled); err != nil {
			t.Errorf("restore GeoIP configuration: %v", err)
		}
	})

	const clientUUID = "client-v2-region-override"
	if err := db.Create(&models.Client{
		UUID:           clientUUID,
		Token:          "token-v2-region-override",
		Name:           "manual-region",
		Region:         geoip.GetRegionUnicodeEmoji("US"),
		RegionOverride: "JP",
	}).Error; err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := db.Delete(&models.Client{}, "uuid = ?", clientUUID).Error; err != nil {
			t.Errorf("delete test client: %v", err)
		}
	})

	for _, transport := range []struct {
		name      string
		allowWait bool
		iso       string
	}{
		{name: "websocket", allowWait: false, iso: "SG"},
		{name: "http", allowWait: true, iso: "DE"},
	} {
		t.Run(transport.name, func(t *testing.T) {
			// 提供器名称独立，避免 GeoIP 缓存掩盖第二次上报的新自动地区。
			geoip.CurrentProvider = staticGeoIPProvider{name: t.Name(), iso: transport.iso}
			response := handleV2RPC(clientUUID, v2.Request{
				JSONRPC: v2.Version,
				Method:  v2.MethodAgentBasicInfo,
				Params: map[string]interface{}{
					"info": map[string]interface{}{
						"ipv4":            "8.8.8.8",
						"region_override": "",
						"RegionOverride":  "US",
						"REGION_OVERRIDE": "FR",
						"regionOverride":  "GB",
						"ReGiOn_OvErRiDe": nil,
					},
				},
				ID: "manual-region-basic-info",
			}, transport.allowWait)
			if response.Error != nil {
				t.Fatalf("basic info failed: %+v", response.Error)
			}

			var stored models.Client
			if err := db.First(&stored, "uuid = ?", clientUUID).Error; err != nil {
				t.Fatal(err)
			}
			if stored.RegionOverride != "JP" || stored.Region != geoip.GetRegionUnicodeEmoji(transport.iso) {
				t.Fatalf("report changed override or lost automatic region: override=%q region=%q", stored.RegionOverride, stored.Region)
			}
			visible, err := clients.GetClientByUUID(clientUUID)
			if err != nil {
				t.Fatal(err)
			}
			if visible.Region != geoip.GetRegionUnicodeEmoji("JP") {
				t.Fatalf("manual region not applied to reads: %q", visible.Region)
			}
		})
	}

	if err := clients.SaveClient(map[string]interface{}{"uuid": clientUUID, "region_override": ""}); err != nil {
		t.Fatal(err)
	}
	automatic, err := clients.GetClientByUUID(clientUUID)
	if err != nil {
		t.Fatal(err)
	}
	if automatic.RegionOverride != "" || automatic.Region != geoip.GetRegionUnicodeEmoji("DE") {
		t.Fatalf("clearing override did not restore latest automatic region: override=%q region=%q", automatic.RegionOverride, automatic.Region)
	}
}
