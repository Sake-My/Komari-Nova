package tasks

import (
	"errors"
	"testing"

	"github.com/Sake-My/Komari-Nova/cmd/flags"
	"github.com/Sake-My/Komari-Nova/database/dbcore"
	"github.com/Sake-My/Komari-Nova/database/models"
	"github.com/Sake-My/Komari-Nova/utils"
	"gorm.io/gorm"
)

func setupPingTaskTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	flags.DatabaseType = flags.DatabaseTypeSQLite
	flags.DatabaseFile = "file:ping_tasks?mode=memory&cache=shared"
	db := dbcore.GetDBInstance()
	// dbcore 使用进程级单例，清理数据而不是通过切换 DSN 假定测试已隔离。
	clear := func() {
		if err := utils.ReloadPingSchedule(nil); err != nil {
			t.Errorf("clear ping schedule: %v", err)
		}
		for _, model := range []any{&models.PingTask{}, &models.Client{}} {
			if err := db.Where("1 = 1").Delete(model).Error; err != nil {
				t.Errorf("clear ping task fixture: %v", err)
			}
		}
	}
	clear()
	t.Cleanup(clear)
	return db
}

func createPingClients(t *testing.T, db *gorm.DB) {
	t.Helper()
	for _, uuid := range []string{"source", "target", "other"} {
		if err := db.Create(&models.Client{UUID: uuid, Token: uuid + "-token"}).Error; err != nil {
			t.Fatalf("create client %q: %v", uuid, err)
		}
	}
}

func createEntryPingTask(t *testing.T, db *gorm.DB) models.PingTask {
	t.Helper()
	id, err := AddPingTask([]string{"source"}, false, "entry", "192.0.2.10", "icmp", 60, "target")
	if err != nil {
		t.Fatalf("add entry task: %v", err)
	}
	var task models.PingTask
	if err := db.First(&task, id).Error; err != nil {
		t.Fatalf("load entry task: %v", err)
	}
	return task
}

func TestAddEntryPingTaskValidation(t *testing.T) {
	db := setupPingTaskTestDB(t)
	createPingClients(t, db)
	for _, tt := range []struct {
		name         string
		clients      []string
		defaultOn    bool
		targetClient string
		target       string
		kind         string
		interval     int
		valid        bool
	}{
		{"icmp IPv4", []string{"source"}, false, "target", "192.0.2.10", "icmp", 60, true},
		{"icmp IPv6", []string{"source"}, false, "target", "2001:db8::10", "icmp", 60, true},
		{"tcp IPv4", []string{"source"}, false, "target", "192.0.2.10:443", "tcp", 60, true},
		{"tcp IPv6", []string{"source"}, false, "target", "[2001:db8::10]:443", "tcp", 60, true},
		{"no source", nil, false, "target", "192.0.2.10", "icmp", 60, false},
		{"multiple sources", []string{"source", "other"}, false, "target", "192.0.2.10", "icmp", 60, false},
		{"same source and target", []string{"target"}, false, "target", "192.0.2.10", "icmp", 60, false},
		{"unknown source", []string{"missing"}, false, "target", "192.0.2.10", "icmp", 60, false},
		{"unknown target", []string{"source"}, false, "missing", "192.0.2.10", "icmp", 60, false},
		{"default on", []string{"source"}, true, "target", "192.0.2.10", "icmp", 60, false},
		{"http", []string{"source"}, false, "target", "http://192.0.2.10", "http", 60, false},
		{"hostname", []string{"source"}, false, "target", "example.com", "icmp", 60, false},
		{"invalid IP", []string{"source"}, false, "target", "256.0.2.10", "icmp", 60, false},
		{"icmp port", []string{"source"}, false, "target", "192.0.2.10:443", "icmp", 60, false},
		{"tcp hostname", []string{"source"}, false, "target", "example.com:443", "tcp", 60, false},
		{"tcp missing port", []string{"source"}, false, "target", "192.0.2.10", "tcp", 60, false},
		{"tcp IPv6 missing brackets", []string{"source"}, false, "target", "2001:db8::10:443", "tcp", 60, false},
		{"tcp zero port", []string{"source"}, false, "target", "192.0.2.10:0", "tcp", 60, false},
		{"tcp oversized port", []string{"source"}, false, "target", "192.0.2.10:65536", "tcp", 60, false},
		{"tcp service name", []string{"source"}, false, "target", "192.0.2.10:https", "tcp", 60, false},
		{"zero interval", []string{"source"}, false, "target", "192.0.2.10", "icmp", 0, false},
		{"negative interval", []string{"source"}, false, "target", "192.0.2.10", "icmp", -1, false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			id, err := AddPingTask(tt.clients, tt.defaultOn, tt.name, tt.target, tt.kind, tt.interval, tt.targetClient)
			if tt.valid {
				if err != nil || id == 0 {
					t.Fatalf("add valid entry task: id=%d, err=%v", id, err)
				}
				return
			}
			if !errors.Is(err, ErrInvalidPingTask) || id != 0 {
				t.Fatalf("invalid entry task: id=%d, err=%v", id, err)
			}
		})
	}
	var count int64
	if err := db.Model(&models.PingTask{}).Count(&count).Error; err != nil || count != 4 {
		t.Fatalf("persisted tasks = %d, err=%v, want 4", count, err)
	}
}

func TestResolvePingRecordClientChecksSourceAndNodeLifetime(t *testing.T) {
	db := setupPingTaskTestDB(t)
	createPingClients(t, db)
	entry := createEntryPingTask(t, db)
	outboundID, err := AddPingTask([]string{"source"}, false, "outbound", "example.com", "icmp", 60, "")
	if err != nil {
		t.Fatal(err)
	}
	for _, tt := range []struct {
		name   string
		taskID uint
		source string
		want   string
	}{
		{"entry belongs to target", entry.Id, "source", "target"},
		{"entry target cannot report", entry.Id, "target", ""},
		{"unassigned cannot report", entry.Id, "other", ""},
		{"outbound belongs to source", outboundID, "source", "source"},
		{"outbound rejects unassigned", outboundID, "other", ""},
		{"missing task", outboundID + 1000, "source", ""},
		{"zero task", 0, "source", ""},
		{"empty source", entry.Id, "", ""},
	} {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ResolvePingRecordClient(tt.taskID, tt.source)
			if got != tt.want || (err == nil) != (tt.want != "") {
				t.Fatalf("resolved=%q, err=%v, want=%q", got, err, tt.want)
			}
		})
	}
	if err := db.Where("uuid = ?", "target").Delete(&models.Client{}).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := ResolvePingRecordClient(entry.Id, "source"); !errors.Is(err, ErrInvalidPingTask) {
		t.Fatalf("deleted target must reject late result: %v", err)
	}
	if err := db.Create(&models.Client{UUID: "target", Token: "target-token"}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Where("uuid = ?", "source").Delete(&models.Client{}).Error; err != nil {
		t.Fatal(err)
	}
	for _, id := range []uint{entry.Id, outboundID} {
		if _, err := ResolvePingRecordClient(id, "source"); !errors.Is(err, ErrInvalidPingTask) {
			t.Fatalf("deleted source must reject result for task %d: %v", id, err)
		}
	}
}

func TestEditEntryPingTaskPreservesMeasurementPath(t *testing.T) {
	db := setupPingTaskTestDB(t)
	createPingClients(t, db)
	entry := createEntryPingTask(t, db)
	for _, tt := range []struct {
		name   string
		change func(*models.PingTask)
	}{
		{"source", func(task *models.PingTask) { task.Clients = models.StringArray{"other"} }},
		{"target client", func(task *models.PingTask) { task.TargetClient = "other" }},
		{"outbound conversion", func(task *models.PingTask) { task.TargetClient = "" }},
		{"target address", func(task *models.PingTask) { task.Target = "192.0.2.20" }},
		{"type", func(task *models.PingTask) {
			task.Type = "tcp"
			task.Target = "192.0.2.10:443"
		}},
		{"default on", func(task *models.PingTask) { task.DefaultOn = true }},
	} {
		t.Run(tt.name, func(t *testing.T) {
			candidate := entry
			tt.change(&candidate)
			if err := EditPingTask([]*models.PingTask{&candidate}); !errors.Is(err, ErrInvalidPingTask) {
				t.Fatalf("changing entry %s must fail: %v", tt.name, err)
			}
		})
	}
	entry.Name = "renamed entry"
	entry.Interval = 120
	if err := EditPingTask([]*models.PingTask{&entry}); err != nil {
		t.Fatalf("edit name and interval: %v", err)
	}
	if err := EditPingTask([]*models.PingTask{&entry}); err != nil {
		t.Fatalf("no-op edit must succeed: %v", err)
	}
	var got models.PingTask
	if err := db.First(&got, entry.Id).Error; err != nil || got.Name != entry.Name || got.Interval != 120 || got.TargetClient != "target" {
		t.Fatalf("edited entry=%+v, err=%v", got, err)
	}
}

func TestEditPingTaskBatchIsAtomic(t *testing.T) {
	db := setupPingTaskTestDB(t)
	createPingClients(t, db)
	entry := createEntryPingTask(t, db)
	id, err := AddPingTask([]string{"source"}, false, "original", "example.com", "icmp", 60, "")
	if err != nil {
		t.Fatal(err)
	}
	var outbound models.PingTask
	if err := db.First(&outbound, id).Error; err != nil {
		t.Fatal(err)
	}
	converted := outbound
	converted.TargetClient = "target"
	converted.Target = "192.0.2.10"
	if err := EditPingTask([]*models.PingTask{&converted}); !errors.Is(err, ErrInvalidPingTask) {
		t.Fatalf("outbound to entry conversion must fail: %v", err)
	}
	outbound.Name = "must roll back"
	entry.Target = "192.0.2.20"
	if err := EditPingTask([]*models.PingTask{&outbound, &entry}); !errors.Is(err, ErrInvalidPingTask) {
		t.Fatalf("invalid second task must fail batch: %v", err)
	}
	var got models.PingTask
	if err := db.First(&got, id).Error; err != nil || got.Name != "original" || got.TargetClient != "" {
		t.Fatalf("batch partially updated first task: %+v, err=%v", got, err)
	}
}

func TestDefaultOnDoesNotAddEntryProbeSources(t *testing.T) {
	db := setupPingTaskTestDB(t)
	createPingClients(t, db)
	entry := createEntryPingTask(t, db)
	// 即使旧数据或手工写库留下 default_on，注册新节点也不能扩大入口探测来源。
	if err := db.Model(&models.PingTask{}).Where("id = ?", entry.Id).Update("all_clients", true).Error; err != nil {
		t.Fatal(err)
	}
	id, err := AddPingTask(nil, true, "default outbound", "example.com", "icmp", 60, "")
	if err != nil {
		t.Fatal(err)
	}
	if err := AddDefaultOnClientUUID("other"); err != nil {
		t.Fatal(err)
	}
	var gotEntry, gotOutbound models.PingTask
	if err := db.First(&gotEntry, entry.Id).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.First(&gotOutbound, id).Error; err != nil {
		t.Fatal(err)
	}
	if len(gotEntry.Clients) != 1 || gotEntry.Clients[0] != "source" || !gotOutbound.ExecutesOnClient("other") {
		t.Fatalf("default registration: entry=%v, outbound=%v", gotEntry.Clients, gotOutbound.Clients)
	}
}

func TestEntryPingIntervalDurationBoundary(t *testing.T) {
	// MaxInt64 / (3 秒)，为三个探测周期的新鲜度窗口保留完整的 Duration 范围。
	boundary := int64(3_074_457_345)
	if int64(int(boundary+1)) != boundary+1 {
		t.Skip("32-bit int cannot represent an overflowing entry interval")
	}
	db := setupPingTaskTestDB(t)
	createPingClients(t, db)
	id, err := AddPingTask([]string{"source"}, false, "long entry", "192.0.2.10", "icmp", int(boundary), "target")
	if err != nil || id == 0 {
		t.Fatalf("maximum safe entry interval rejected: id=%d, err=%v", id, err)
	}
	if id, err := AddPingTask([]string{"source"}, false, "overflow entry", "192.0.2.10", "icmp", int(boundary+1), "target"); id != 0 || !errors.Is(err, ErrInvalidPingTask) {
		t.Fatalf("overflowing entry interval accepted: id=%d, err=%v", id, err)
	}
	var entry models.PingTask
	if err := db.First(&entry, id).Error; err != nil {
		t.Fatal(err)
	}
	entry.Interval = int(boundary + 1)
	if err := EditPingTask([]*models.PingTask{&entry}); !errors.Is(err, ErrInvalidPingTask) {
		t.Fatalf("editing entry to overflowing interval must fail: %v", err)
	}
	if err := db.First(&entry, id).Error; err != nil || int64(entry.Interval) != boundary {
		t.Fatalf("failed edit changed interval: interval=%d, err=%v", entry.Interval, err)
	}
}

func TestScheduledPingTasksExcludeDeletedEntryNodesAndKeepConfiguration(t *testing.T) {
	for _, deleted := range []string{"source", "target"} {
		t.Run(deleted, func(t *testing.T) {
			db := setupPingTaskTestDB(t)
			createPingClients(t, db)
			entry := createEntryPingTask(t, db)
			outboundID, err := AddPingTask([]string{"source"}, false, "outbound", "example.com", "icmp", 60, "")
			if err != nil {
				t.Fatal(err)
			}
			before, err := getScheduledPingTasks()
			if err != nil || len(before) != 2 {
				t.Fatalf("initial scheduled tasks=%+v, err=%v", before, err)
			}
			if err := db.Where("uuid = ?", deleted).Delete(&models.Client{}).Error; err != nil {
				t.Fatal(err)
			}
			after, err := getScheduledPingTasks()
			if err != nil || len(after) != 1 || after[0].Id != outboundID {
				t.Fatalf("scheduled tasks after deleting %s=%+v, err=%v", deleted, after, err)
			}
			configured, err := GetAllPingTasks()
			if err != nil || len(configured) != 2 {
				t.Fatalf("node deletion changed task configuration: tasks=%+v, err=%v", configured, err)
			}
			var retained models.PingTask
			if err := db.First(&retained, entry.Id).Error; err != nil || retained.TargetClient != "target" || retained.Target != entry.Target {
				t.Fatalf("entry configuration was not retained: %+v, err=%v", retained, err)
			}
		})
	}
}

func TestReloadPingScheduleReturnsClientLookupError(t *testing.T) {
	db := setupPingTaskTestDB(t)
	createPingClients(t, db)
	createEntryPingTask(t, db)
	want := errors.New("client lookup failed")
	const callback = "test:entry_ping_client_lookup_failure"
	if err := db.Callback().Query().Before("gorm:query").Register(callback, func(tx *gorm.DB) {
		if tx.Statement.Table == "clients" {
			tx.AddError(want)
		}
	}); err != nil {
		t.Fatalf("register database failure: %v", err)
	}
	t.Cleanup(func() {
		if err := db.Callback().Query().Remove(callback); err != nil {
			t.Errorf("remove database failure: %v", err)
		}
	})
	if err := ReloadPingSchedule(); !errors.Is(err, want) {
		t.Fatalf("client lookup error was not returned: %v", err)
	}
}
