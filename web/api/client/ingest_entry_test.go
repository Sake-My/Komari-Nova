package client

import (
	"context"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/Sake-My/Komari-Nova/cmd/flags"
	"github.com/Sake-My/Komari-Nova/database/dbcore"
	"github.com/Sake-My/Komari-Nova/database/models"
	"github.com/Sake-My/Komari-Nova/internal/config"
	"github.com/Sake-My/Komari-Nova/internal/metricstore"
	"github.com/Sake-My/Komari-Nova/pkg/metric"
	v2 "github.com/Sake-My/Komari-Nova/protocol/v2"
	"gorm.io/gorm"
)

func setupEntryPingIngestStore(t *testing.T) (*gorm.DB, *metric.Store) {
	t.Helper()
	oldDatabaseType, oldDatabaseFile := flags.DatabaseType, flags.DatabaseFile
	flags.DatabaseType = flags.DatabaseTypeSQLite
	flags.DatabaseFile = "file:entry_ping_ingest?mode=memory&cache=shared"
	t.Cleanup(func() {
		flags.DatabaseType, flags.DatabaseFile = oldDatabaseType, oldDatabaseFile
	})
	db := dbcore.GetDBInstance()
	if metricstore.GetStore() != nil {
		t.Fatal("entry ping test requires an uninitialized metric store")
	}
	previous, err := config.GetMany(map[string]any{
		metricstore.MetricDBDriverKey:    "sqlite",
		metricstore.MetricDBDSNKey:       "./data/metrics.db",
		metricstore.MetricTablePrefixKey: "metric_",
	})
	if err != nil {
		t.Fatalf("read previous metric configuration: %v", err)
	}
	metricPath := filepath.ToSlash(filepath.Join(t.TempDir(), "entry-ping-metrics.db"))
	t.Cleanup(func() {
		if err := metricstore.CloseStoreContext(context.Background()); err != nil {
			t.Errorf("close entry ping metric store: %v", err)
		}
		if err := config.SetMany(previous); err != nil {
			t.Errorf("restore metric configuration: %v", err)
		}
	})
	if err := config.SetMany(map[string]any{
		metricstore.MetricDBDriverKey:    "sqlite",
		metricstore.MetricDBDSNKey:       metricPath,
		metricstore.MetricTablePrefixKey: "metric_",
	}); err != nil {
		t.Fatalf("configure entry ping metric store: %v", err)
	}
	if err := metricstore.InitializeStore(); err != nil {
		t.Fatalf("initialize entry ping metric store: %v", err)
	}
	return db, metricstore.GetStore()
}

func TestV2EntryPingResultsUseAuthenticatedSourceAndTargetSeries(t *testing.T) {
	db, store := setupEntryPingIngestStore(t)
	const (
		source = "entry-rpc-source"
		target = "entry-rpc-target"
		other  = "entry-rpc-other"
	)
	clientIDs := []string{source, target, other}
	var taskIDs []uint
	t.Cleanup(func() {
		if len(taskIDs) > 0 {
			if err := db.Where("id IN ?", taskIDs).Delete(&models.PingTask{}).Error; err != nil {
				t.Errorf("delete entry ping test tasks: %v", err)
			}
		}
		if err := db.Where("uuid IN ?", clientIDs).Delete(&models.Client{}).Error; err != nil {
			t.Errorf("delete entry ping test clients: %v", err)
		}
	})
	for _, uuid := range clientIDs {
		if err := db.Create(&models.Client{UUID: uuid, Token: uuid + "-token"}).Error; err != nil {
			t.Fatalf("create ping client %q: %v", uuid, err)
		}
	}
	// 用不同 task_id 验证成功、丢包及普通出站的序列隔离，不依赖上报时钟间隔。
	taskConfigs := []models.PingTask{
		{Name: "entry ICMP", Clients: models.StringArray{source}, TargetClient: target, Type: "icmp", Target: "192.0.2.10", Interval: 60},
		{Name: "entry TCP", Clients: models.StringArray{source}, TargetClient: target, Type: "tcp", Target: "[2001:db8::10]:443", Interval: 60},
		{Name: "outbound", Clients: models.StringArray{source}, Type: "icmp", Target: "198.51.100.10", Interval: 60},
	}
	for i := range taskConfigs {
		if err := db.Create(&taskConfigs[i]).Error; err != nil {
			t.Fatalf("create ping task %q: %v", taskConfigs[i].Name, err)
		}
		taskIDs = append(taskIDs, taskConfigs[i].Id)
	}
	start := time.Now().UTC().Add(-time.Minute)
	for i, tt := range []struct {
		name      string
		value     int
		allowWait bool
	}{
		{name: "entry success without long-poll allowance", value: 24, allowWait: false},
		{name: "entry loss with long-poll allowance", value: -1, allowWait: true},
		{name: "outbound success", value: 37, allowWait: false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			response := handleV2RPC(source, v2.Request{
				JSONRPC: v2.Version,
				Method:  v2.MethodAgentPingResult,
				Params: map[string]any{
					"task_id":       taskConfigs[i].Id,
					"ping_type":     taskConfigs[i].Type,
					"value":         tt.value,
					"finished_at":   time.Now().UTC(),
					"client":        other,
					"target_client": other,
				},
				ID: tt.name,
			}, tt.allowWait)
			if response.Error != nil {
				t.Fatalf("ping result rejected: %+v", response.Error)
			}
		})
	}
	for _, reporter := range []string{other, target} {
		for _, task := range taskConfigs[:2] {
			for _, allowWait := range []bool{false, true} {
				response := handleV2RPC(reporter, v2.Request{
					JSONRPC: v2.Version,
					Method:  v2.MethodAgentPingResult,
					Params: map[string]any{
						"task_id":       task.Id,
						"ping_type":     task.Type,
						"value":         999,
						"source":        source,
						"client":        source,
						"target_client": target,
					},
					ID: "forged-entry-result",
				}, allowWait)
				if response.Error == nil {
					t.Fatalf("unassigned reporter %q was accepted for task %d (HTTP=%v)", reporter, task.Id, allowWait)
				}
			}
		}
	}

	// 直接读真实 metric store 的原始点，同时验证丢包指标与 task_id 标签。
	end := time.Now().UTC().Add(time.Minute)
	for i, want := range []struct {
		client  string
		latency float64
		loss    float64
	}{
		{client: target, latency: 24, loss: 0},
		{client: target, latency: -1, loss: 1},
		{client: source, latency: 37, loss: 0},
	} {
		taskTag := strconv.FormatUint(uint64(taskConfigs[i].Id), 10)
		for _, metricWant := range []struct {
			name  string
			value float64
		}{
			{name: metricstore.MetricPingLatency, value: want.latency},
			{name: metricstore.MetricPingLoss, value: want.loss},
		} {
			for _, clientID := range clientIDs {
				points, err := store.Query(context.Background(), metric.Query{
					MetricName: metricWant.name,
					EntityID:   clientID,
					Tags:       map[string]string{"task_id": taskTag},
					Start:      start,
					End:        end,
				})
				if err != nil {
					t.Fatalf("query %s client=%s task=%s: %v", metricWant.name, clientID, taskTag, err)
				}
				if clientID != want.client {
					if len(points) != 0 {
						t.Fatalf("ping result leaked to client=%s task=%s metric=%s: %+v", clientID, taskTag, metricWant.name, points)
					}
					continue
				}
				if len(points) != 1 || points[0].EntityID != want.client || points[0].Tags["task_id"] != taskTag || points[0].Value != metricWant.value {
					t.Fatalf("unexpected %s series client=%s task=%s: %+v, want one value=%v", metricWant.name, want.client, taskTag, points, metricWant.value)
				}
			}
		}
	}
}
