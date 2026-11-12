package utils

import (
	"reflect"
	"testing"
	"time"

	"github.com/Sake-My/Komari-Nova/database/models"
	agent_runtime "github.com/Sake-My/Komari-Nova/web/agent"
)

func TestEntryPingScheduleUsesLiveSourceWithoutTargetPresence(t *testing.T) {
	const source = "entry-schedule-source"
	const target = "entry-schedule-target"
	agent_runtime.MarkV2Client(source)
	t.Cleanup(func() {
		agent_runtime.SetPresence(source, 991, false)
		agent_runtime.DeleteConnectedClients(source)
	})
	task := models.PingTask{Clients: models.StringArray{source}, TargetClient: target}
	if got := targetPingClientUUIDs(task); len(got) != 0 {
		t.Fatalf("V2 capability alone dispatched an offline probe: %v", got)
	}
	agent_runtime.KeepAlivePresence(source, 991, time.Minute)
	if got := targetPingClientUUIDs(task); !reflect.DeepEqual(got, []string{source}) {
		t.Fatalf("live source = %v, want only %s", got, source)
	}
	agent_runtime.KeepAlivePresence(source, 991, -time.Second)
	if got := targetPingClientUUIDs(task); len(got) != 0 {
		t.Fatalf("expired source dispatched: %v", got)
	}
	task.TargetClient = ""
	if got := targetPingClientUUIDs(task); !reflect.DeepEqual(got, []string{source}) {
		t.Fatalf("ordinary task scheduling changed: %v", got)
	}
}
