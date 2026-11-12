package jsonrpc

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/Sake-My/Komari-Nova/database/dbcore"
	"github.com/Sake-My/Komari-Nova/database/models"
	"github.com/Sake-My/Komari-Nova/database/tasks"
	"github.com/Sake-My/Komari-Nova/internal/metricstore"
	"github.com/Sake-My/Komari-Nova/pkg/metric"
	"github.com/Sake-My/Komari-Nova/pkg/rpc"
	agent_runtime "github.com/Sake-My/Komari-Nova/web/agent"
)

func TestEntryPingPublicProjectionAndLegacyEdit(t *testing.T) {
	ctx := context.Background()
	db := dbcore.GetDBInstance()
	const source, target = "rpc-entry-source", "rpc-entry-target"
	for _, node := range []models.Client{
		{UUID: source, Name: "private-source-name", Token: "rpc-entry-source-token", Hidden: true},
		{UUID: target, Name: "entry-target", Token: "rpc-entry-target-token"},
	} {
		if err := db.Create(&node).Error; err != nil {
			t.Fatal(err)
		}
	}
	taskID := uint(0)
	t.Cleanup(func() {
		agent_runtime.SetPresence(source, 992, false)
		agent_runtime.DeleteConnectedClients(source)
		if err := db.Delete(&models.PingTask{}, "id = ?", taskID).Error; err != nil {
			t.Error(err)
		}
		if err := db.Delete(&models.Client{}, "uuid IN ?", []string{source, target}).Error; err != nil {
			t.Error(err)
		}
		if err := tasks.ReloadPingSchedule(); err != nil {
			t.Error(err)
		}
	})
	params := map[string]any{
		"name": "entry-route", "clients": []string{source}, "target_client": target,
		"target": "198.51.100.8:443", "type": "tcp", "interval": 60, "default_on": false,
	}
	result, rpcErr := adminAddPingTask(ctx, rpc.NewRequest(1, "admin:addPingTask", params))
	if rpcErr != nil {
		t.Fatal(rpcErr)
	}
	taskID = result.(map[string]any)["task_id"].(uint)
	readEntry := func() map[string]any {
		t.Helper()
		result, rpcErr := publicGetPublicPingTasks(ctx, nil)
		if rpcErr != nil {
			t.Fatal(rpcErr)
		}
		data, err := json.Marshal(result)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(data), "198.51.100.8") || strings.Contains(string(data), "private-source-name") {
			t.Fatalf("public list leaked private probe details: %s", data)
		}
		var rows []map[string]any
		if err := json.Unmarshal(data, &rows); err != nil {
			t.Fatal(err)
		}
		for _, row := range rows {
			if row["id"] == float64(taskID) {
				return row
			}
		}
		return nil
	}
	agent_runtime.MarkV2Client(source)
	row := readEntry()
	if row == nil || row["target_client"] != target || row["probe_source_online"] != false || row["probe_source"] != nil {
		t.Fatalf("unexpected entry projection: %#v", row)
	}
	assigned := row["clients"].([]any)
	if len(assigned) != 1 || assigned[0] != target {
		t.Fatalf("public clients must refer to result owner: %v", assigned)
	}
	agent_runtime.KeepAlivePresence(source, 992, time.Minute)
	if row := readEntry(); row["probe_source_online"] != true {
		t.Fatalf("live POST source not recognized: %#v", row)
	}
	delete(params, "target_client")
	params["id"], params["name"] = taskID, "renamed-entry"
	_, rpcErr = adminEditPingTask(ctx, rpc.NewRequest(2, "admin:editPingTask", map[string]any{"tasks": []any{params}}))
	if rpcErr != nil {
		t.Fatalf("legacy edit lost target association: %v", rpcErr)
	}
	var stored models.PingTask
	if err := db.First(&stored, taskID).Error; err != nil {
		t.Fatal(err)
	}
	if stored.TargetClient != target || stored.Name != "renamed-entry" {
		t.Fatalf("legacy edit = %#v", stored)
	}
	params["target_client"] = ""
	if _, rpcErr := adminEditPingTask(ctx, rpc.NewRequest(3, "admin:editPingTask", map[string]any{"tasks": []any{params}})); rpcErr == nil || rpcErr.Code != rpc.InvalidParams {
		t.Fatalf("entry conversion should return InvalidParams: %v", rpcErr)
	}
	if err := db.Model(&models.Client{}).Where("uuid = ?", target).Update("hidden", true).Error; err != nil {
		t.Fatal(err)
	}
	if row := readEntry(); row != nil {
		t.Fatalf("hidden target task was exposed: %#v", row)
	}
}

func TestEntryPingCurrentStateUsesRealLatestSample(t *testing.T) {
	ctx := context.Background()
	store, err := metric.Open(ctx, metric.SQLite(":memory:", metric.WithMaxOpenConns(1),
		metric.WithRollupPolicy(metric.RollupPolicy{
			RawRetention: metricstore.DefaultRollupRawRetention,
			Tiers: []metric.RollupTier{{Interval: time.Minute, Retention: 10 * time.Hour}}, Compression: 30,
		})))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := store.CreateMetric(ctx, metric.Definition{Name: metricstore.MetricPingLatency, Type: metric.TypeGauge, RetentionDays: 1}); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Truncate(time.Millisecond)
	failureTime := now.Add(-10 * time.Second)
	if err := store.WriteBatch(ctx, []metric.Point{
		{MetricName: metricstore.MetricPingLatency, EntityID: "target", Timestamp: now.Add(-20 * time.Second), Value: 21, Tags: map[string]string{"task_id": "7"}},
		{MetricName: metricstore.MetricPingLatency, EntityID: "target", Timestamp: failureTime, Value: -1, Tags: map[string]string{"task_id": "7"}},
	}); err != nil {
		t.Fatal(err)
	}
	task := models.PingTask{Id: 7, Name: "entry", TargetClient: "target", Clients: models.StringArray{"source"}, Type: "icmp", Interval: 60}
	view := pingTaskVisibility{nodes: map[string]models.Client{
		"target": {UUID: "target"}, "source": {UUID: "source", Name: "source-name"},
	}, online: map[string]bool{"source": true}}
	read := func(at time.Time) publicPingMetricTaskStats {
		t.Helper()
		got, err := enrichEntryPingStats(ctx, store, nil, []models.PingTask{task}, []string{"target"}, nil, view, at)
		if err != nil || len(got) != 1 {
			t.Fatalf("entry stats = %v, %v", got, err)
		}
		return got[0]
	}
	stat := read(now)
	if stat.CurrentValue == nil || *stat.CurrentValue != -1 || stat.CurrentTime == nil || !stat.CurrentTime.Equal(failureTime) {
		t.Fatalf("failure replaced by older success or bucket time: %#v", stat)
	}
	if stat := read(now.Add(4 * time.Minute)); stat.CurrentValue != nil || stat.CurrentTime != nil {
		t.Fatalf("stale sample shown as current: %#v", stat)
	}
	view.online["source"] = false
	if stat := read(now); stat.CurrentValue != nil || stat.CurrentTime != nil || *stat.ProbeSourceOnline {
		t.Fatalf("offline source must show no data: %#v", stat)
	}
	view.online["source"] = true
	task.Id = 8
	if stat := read(now); stat.CurrentValue != nil || stat.Total != 0 {
		t.Fatalf("task without samples must not fabricate a failure: %#v", stat)
	}
}

func TestEntryPingStatsDropInaccessibleOwners(t *testing.T) {
	task := models.PingTask{Id: 7, TargetClient: "target", Clients: models.StringArray{"source"}}
	for _, test := range []struct {
		name   string
		entity string
		nodes  map[string]models.Client
	}{
		{name: "deleted target", entity: "target", nodes: map[string]models.Client{}},
		{name: "hidden target", entity: "target", nodes: map[string]models.Client{"target": {UUID: "target", Hidden: true}}},
		{name: "wrong owner", entity: "source", nodes: map[string]models.Client{"target": {UUID: "target"}}},
	} {
		t.Run(test.name, func(t *testing.T) {
			stats, err := enrichEntryPingStats(context.Background(), nil,
				[]publicPingMetricTaskStats{{TaskID: "7", EntityID: test.entity, Name: "private-route"}},
				[]models.PingTask{task}, []string{test.entity}, nil,
				pingTaskVisibility{nodes: test.nodes, online: map[string]bool{}}, time.Now().UTC())
			if err != nil || len(stats) != 0 {
				t.Fatalf("inaccessible entry statistics exposed: %v, %v", stats, err)
			}
		})
	}
}
