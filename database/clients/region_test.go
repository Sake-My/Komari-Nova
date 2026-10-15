package clients

import (
	"database/sql"
	"fmt"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/Sake-My/Komari-Nova/cmd/flags"
	"github.com/Sake-My/Komari-Nova/database/dbcore"
	"github.com/Sake-My/Komari-Nova/database/models"
	"github.com/Sake-My/Komari-Nova/utils/geoip"
	"github.com/google/uuid"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

const regionTestDSN = "file:client_region_override_test?mode=memory&cache=shared"

var (
	regionTestOnce sync.Once
	regionTestDB   *gorm.DB
)

func regionDatabase(t *testing.T) *gorm.DB {
	t.Helper()
	regionTestOnce.Do(func() {
		previousType, previousFile := flags.DatabaseType, flags.DatabaseFile
		flags.DatabaseType = "sqlite"
		flags.DatabaseFile = regionTestDSN
		regionTestDB = dbcore.GetDBInstance()
		flags.DatabaseType, flags.DatabaseFile = previousType, previousFile
	})
	if regionTestDB == nil {
		t.Fatal("initialize region test database")
	}
	// dbcore 的单例由整个测试进程共用，不能在单个测试结束时关闭。
	return regionTestDB
}

func regionFixture(t *testing.T, automaticRegion, override string) (*gorm.DB, models.Client) {
	t.Helper()
	db := regionDatabase(t)
	stamp := time.Date(2026, 10, 15, 1, 2, 3, 0, time.UTC)
	client := models.Client{
		UUID:           uuid.NewString(),
		Token:          uuid.NewString(),
		Name:           "original name",
		IPv4:           "192.0.2.1",
		Region:         automaticRegion,
		RegionOverride: override,
		CreatedAt:      stamp,
		UpdatedAt:      stamp,
	}
	if err := db.Create(&client).Error; err != nil {
		t.Fatalf("create region test client: %v", err)
	}
	t.Cleanup(func() {
		if err := db.Delete(&models.Client{}, "uuid = ?", client.UUID).Error; err != nil {
			t.Errorf("clean up region test client: %v", err)
		}
	})

	// 另开连接直接读取数据库，避免把读取接口的展示值误当成持久化值。
	observer, err := gorm.Open(sqlite.Open(regionTestDSN), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatalf("open independent region database connection: %v", err)
	}
	sqlDB, err := observer.DB()
	if err != nil {
		t.Fatalf("get independent region database connection: %v", err)
	}
	t.Cleanup(func() {
		if err := sqlDB.Close(); err != nil {
			t.Errorf("close independent region database connection: %v", err)
		}
	})
	return observer, client
}

func storedRegionClient(t *testing.T, db *gorm.DB, clientUUID string) models.Client {
	t.Helper()
	var client models.Client
	if err := db.First(&client, "uuid = ?", clientUUID).Error; err != nil {
		t.Fatalf("read stored region client: %v", err)
	}
	return client
}

func requireEffectiveRegion(t *testing.T, clientUUID, region, override string) {
	t.Helper()
	client, err := GetClientByUUID(clientUUID)
	if err != nil {
		t.Fatalf("get region client: %v", err)
	}
	if client.Region != region || client.RegionOverride != override {
		t.Fatalf("single client region = (%q, %q), want (%q, %q)", client.Region, client.RegionOverride, region, override)
	}
	all, err := GetAllClientBasicInfo()
	if err != nil {
		t.Fatalf("get all region clients: %v", err)
	}
	for _, client := range all {
		if client.UUID == clientUUID {
			if client.Region != region || client.RegionOverride != override {
				t.Fatalf("listed client region = (%q, %q), want (%q, %q)", client.Region, client.RegionOverride, region, override)
			}
			return
		}
	}
	t.Fatalf("client %s missing from client list", clientUUID)
}

func TestRegionOverrideLifecycle(t *testing.T) {
	japan := geoip.GetRegionUnicodeEmoji("JP")
	singapore := geoip.GetRegionUnicodeEmoji("SG")
	unitedStates := geoip.GetRegionUnicodeEmoji("US")
	observer, client := regionFixture(t, japan, "")

	if err := SaveClient(map[string]interface{}{
		"uuid": client.UUID, "region_override": "  sg\t", "name": "manual region",
	}); err != nil {
		t.Fatalf("set region override: %v", err)
	}
	requireEffectiveRegion(t, client.UUID, singapore, "SG")
	stored := storedRegionClient(t, observer, client.UUID)
	if stored.Region != japan || stored.RegionOverride != "SG" || stored.Name != "manual region" {
		t.Fatalf("stored manual region = (%q, %q, %q)", stored.Region, stored.RegionOverride, stored.Name)
	}

	if err := SaveClient(map[string]interface{}{"uuid": client.UUID, "name": "ordinary edit"}); err != nil {
		t.Fatalf("edit client without region override: %v", err)
	}
	stored = storedRegionClient(t, observer, client.UUID)
	if stored.RegionOverride != "SG" || stored.Region != japan || stored.Name != "ordinary edit" {
		t.Fatalf("ordinary edit changed region: (%q, %q, %q)", stored.Region, stored.RegionOverride, stored.Name)
	}

	if err := SaveClientInfo(map[string]interface{}{
		"uuid": client.UUID, "region": unitedStates, "ipv4": "203.0.113.8",
	}); err != nil {
		t.Fatalf("update automatic region: %v", err)
	}
	requireEffectiveRegion(t, client.UUID, singapore, "SG")
	stored = storedRegionClient(t, observer, client.UUID)
	if stored.Region != unitedStates || stored.RegionOverride != "SG" || stored.IPv4 != "203.0.113.8" {
		t.Fatalf("agent update lost stored region/IP: (%q, %q, %q)", stored.Region, stored.RegionOverride, stored.IPv4)
	}

	if err := SaveClient(map[string]interface{}{"uuid": client.UUID, "region_override": " \n\t "}); err != nil {
		t.Fatalf("restore automatic region: %v", err)
	}
	requireEffectiveRegion(t, client.UUID, unitedStates, "")
	stored = storedRegionClient(t, observer, client.UUID)
	if stored.Region != unitedStates || stored.RegionOverride != "" {
		t.Fatalf("clearing override lost latest automatic region: (%q, %q)", stored.Region, stored.RegionOverride)
	}
}

func TestRegionOverrideAcceptsCountriesAndTerritories(t *testing.T) {
	for _, code := range []string{"CN", "US", "HK", "TW"} {
		t.Run(code, func(t *testing.T) {
			observer, client := regionFixture(t, "", "")
			if err := SaveClient(map[string]interface{}{"uuid": client.UUID, "region_override": code}); err != nil {
				t.Fatalf("set valid region %s: %v", code, err)
			}
			requireEffectiveRegion(t, client.UUID, geoip.GetRegionUnicodeEmoji(code), code)
			stored := storedRegionClient(t, observer, client.UUID)
			if stored.Region != "" || stored.RegionOverride != code {
				t.Fatalf("valid override changed automatic region: (%q, %q)", stored.Region, stored.RegionOverride)
			}
		})
	}
}

func TestRegionOverrideRejectsInvalidInputWithoutPartialUpdate(t *testing.T) {
	cases := []struct {
		name  string
		value interface{}
	}{
		{"null", nil},
		{"integer", 42},
		{"json_number", float64(42)},
		{"boolean", true},
		{"object", map[string]interface{}{"code": "SG"}},
		{"array", []string{"SG"}},
		{"one_letter", "S"},
		{"three_letters", "SGP"},
		{"embedded_space", "S G"},
		{"numeric", "01"},
		{"mixed_number", "S1"},
		{"flag_emoji", "🇸🇬"},
		{"non_ascii_letters", "ſg"},
		{"non_ascii_single_letter", "ſ"},
		{"unknown", "ZZ"},
		{"supranational", "EU"},
		{"world_region", "001"},
		{"multiple_regions", "SG-US"},
		{"control_character", "S\x00"},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			observer, client := regionFixture(t, geoip.GetRegionUnicodeEmoji("JP"), "SG")
			before := storedRegionClient(t, observer, client.UUID)
			err := SaveClient(map[string]interface{}{
				"uuid": client.UUID, "region_override": test.value,
				"name": "must not persist", "ipv4": "203.0.113.99",
			})
			if err == nil {
				t.Fatalf("accepted invalid region override %#v", test.value)
			}
			after := storedRegionClient(t, observer, client.UUID)
			if after.RegionOverride != before.RegionOverride || after.Region != before.Region ||
				after.Name != before.Name || after.IPv4 != before.IPv4 || !after.UpdatedAt.Equal(before.UpdatedAt) {
				t.Fatalf("invalid override partially updated client: before=%+v after=%+v", before, after)
			}
		})
	}
}

func TestRegionOverrideCannotBeChangedByAgent(t *testing.T) {
	keys := []string{"region_override", "REGION_OVERRIDE", "Region_Override", "rEgIoN_oVeRrIdE", "RegionOverride", "REGIONOVERRIDE", "rEgIoNoVeRrIdE"}
	for _, key := range keys {
		for _, initialOverride := range []string{"", "SG"} {
			t.Run(fmt.Sprintf("%s/initial_%s", key, initialOverride), func(t *testing.T) {
				automatic := geoip.GetRegionUnicodeEmoji("JP")
				observer, client := regionFixture(t, "", initialOverride)
				for _, attempt := range []interface{}{"DE", "", nil, 42} {
					if err := SaveClientInfo(map[string]interface{}{
						"uuid": client.UUID, key: attempt, "region": automatic, "ipv4": "203.0.113.7",
					}); err != nil {
						t.Fatalf("ignore agent override %s=%#v: %v", key, attempt, err)
					}
					stored := storedRegionClient(t, observer, client.UUID)
					if stored.RegionOverride != initialOverride || stored.Region != automatic || stored.IPv4 != "203.0.113.7" {
						t.Fatalf("agent override %s=%#v affected stored values: (%q, %q, %q)", key, attempt, stored.Region, stored.RegionOverride, stored.IPv4)
					}
				}
				effective := automatic
				if initialOverride != "" {
					effective = geoip.GetRegionUnicodeEmoji(initialOverride)
				}
				requireEffectiveRegion(t, client.UUID, effective, initialOverride)
			})
		}
	}
}

func TestRegionOverrideLegacyClientUsesAutomaticRegion(t *testing.T) {
	db := regionDatabase(t)
	clientUUID, token := uuid.NewString(), uuid.NewString()
	automatic := geoip.GetRegionUnicodeEmoji("JP")
	// 旧写入方不提供新列，仍应默认使用自动地区。
	if err := db.Exec("INSERT INTO clients (uuid, token, region) VALUES (?, ?, ?)", clientUUID, token, automatic).Error; err != nil {
		t.Fatalf("insert legacy client: %v", err)
	}
	t.Cleanup(func() {
		if err := db.Delete(&models.Client{}, "uuid = ?", clientUUID).Error; err != nil {
			t.Errorf("clean up legacy client: %v", err)
		}
	})
	requireEffectiveRegion(t, clientUUID, automatic, "")
	if err := SaveClient(map[string]interface{}{"uuid": clientUUID, "name": "legacy edit"}); err != nil {
		t.Fatalf("edit legacy client: %v", err)
	}
	requireEffectiveRegion(t, clientUUID, automatic, "")
}

func TestRegionOverrideMigrationPreservesAutomaticRegion(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "region-migration.db")), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatalf("open migration database: %v", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("get migration database connection: %v", err)
	}
	t.Cleanup(func() {
		if err := sqlDB.Close(); err != nil {
			t.Errorf("close migration database: %v", err)
		}
	})
	if err := db.AutoMigrate(&models.Client{}); err != nil {
		t.Fatalf("create client schema: %v", err)
	}
	// 移除本次新列，保留其余真实模型字段，模拟升级前的 clients 表。
	if err := db.Migrator().DropColumn(&models.Client{}, "region_override"); err != nil {
		t.Fatalf("create legacy client schema: %v", err)
	}
	automatic := geoip.GetRegionUnicodeEmoji("JP")
	if err := db.Exec("INSERT INTO clients (uuid, token, name, region) VALUES (?, ?, ?, ?)", "legacy", "legacy-token", "legacy name", automatic).Error; err != nil {
		t.Fatalf("insert pre-migration client: %v", err)
	}
	if err := db.AutoMigrate(&models.Client{}); err != nil {
		t.Fatalf("migrate client region override: %v", err)
	}
	if !db.Migrator().HasColumn(&models.Client{}, "region_override") {
		t.Fatal("region_override column missing after migration")
	}
	var override sql.NullString
	if err := db.Raw("SELECT region_override FROM clients WHERE uuid = ?", "legacy").Row().Scan(&override); err != nil {
		t.Fatalf("read migrated region override: %v", err)
	}
	if !override.Valid || override.String != "" {
		t.Fatalf("migrated region override = %+v, want non-null empty default", override)
	}
	stored := storedRegionClient(t, db, "legacy")
	if stored.Region != automatic || stored.RegionOverride != "" || stored.Name != "legacy name" {
		t.Fatalf("migration changed legacy client: (%q, %q, %q)", stored.Region, stored.RegionOverride, stored.Name)
	}
}
