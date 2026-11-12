package clients

import (
	"errors"
	"testing"

	"github.com/Sake-My/Komari-Nova/database/models"
	"github.com/Sake-My/Komari-Nova/database/tasks"
	"github.com/Sake-My/Komari-Nova/utils"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

func TestDeleteClientKeepsEntryPingConfiguration(t *testing.T) {
	db := regionDatabase(t)
	source, target := uuid.NewString(), uuid.NewString()
	task := models.PingTask{
		Name: "retained entry", Clients: models.StringArray{source}, TargetClient: target,
		Type: "icmp", Target: "192.0.2.10", Interval: 3600,
	}
	t.Cleanup(func() {
		if err := utils.ReloadPingSchedule(nil); err != nil {
			t.Errorf("clear ping schedule: %v", err)
		}
		if task.Id != 0 {
			if err := db.Delete(&models.PingTask{}, task.Id).Error; err != nil {
				t.Errorf("delete test task: %v", err)
			}
		}
		if err := db.Where("uuid IN ?", []string{source, target}).Delete(&models.Client{}).Error; err != nil {
			t.Errorf("delete test clients: %v", err)
		}
	})
	for _, id := range []string{source, target} {
		if err := db.Create(&models.Client{UUID: id, Token: uuid.NewString()}).Error; err != nil {
			t.Fatal(err)
		}
	}
	if err := db.Create(&task).Error; err != nil {
		t.Fatal(err)
	}
	if err := tasks.ReloadPingSchedule(); err != nil {
		t.Fatal(err)
	}
	if err := DeleteClient(target); err != nil {
		t.Fatalf("delete entry target: %v", err)
	}
	var retained models.PingTask
	if err := db.First(&retained, task.Id).Error; err != nil || retained.TargetClient != target || retained.Target != task.Target {
		t.Fatalf("entry task was removed or rewritten: %+v, err=%v", retained, err)
	}
	var count int64
	if err := db.Model(&models.Client{}).Where("uuid = ?", target).Count(&count).Error; err != nil || count != 0 {
		t.Fatalf("target deletion failed: count=%d, err=%v", count, err)
	}
}

func TestDeleteClientReturnsPingScheduleReloadError(t *testing.T) {
	db := regionDatabase(t)
	clientUUID := uuid.NewString()
	if err := db.Create(&models.Client{UUID: clientUUID, Token: uuid.NewString()}).Error; err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := db.Where("uuid = ?", clientUUID).Delete(&models.Client{}).Error; err != nil {
			t.Errorf("delete test client: %v", err)
		}
	})
	want := errors.New("ping task query failed after client deletion")
	const callback = "test:delete_client_ping_reload_failure"
	if err := db.Callback().Query().Before("gorm:query").Register(callback, func(tx *gorm.DB) {
		if tx.Statement.Table == "ping_tasks" {
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
	if err := DeleteClient(clientUUID); !errors.Is(err, want) {
		t.Fatalf("schedule reload was skipped or its error was swallowed: %v", err)
	}
	var count int64
	if err := db.Model(&models.Client{}).Where("uuid = ?", clientUUID).Count(&count).Error; err != nil || count != 0 {
		t.Fatalf("delete must finish before schedule reload: count=%d, err=%v", count, err)
	}
}
