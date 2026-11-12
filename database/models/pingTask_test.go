package models

import "testing"

func TestPingTaskSeparatesExecutionAndResultClients(t *testing.T) {
	for _, tt := range []struct {
		name     string
		task     PingTask
		client   string
		applies  bool
		executes bool
	}{
		{"outbound source", PingTask{Clients: StringArray{"source"}}, "source", true, true},
		{"outbound other", PingTask{Clients: StringArray{"source"}}, "other", false, false},
		{"entry source", PingTask{Clients: StringArray{"source"}, TargetClient: "target"}, "source", false, true},
		{"entry target", PingTask{Clients: StringArray{"source"}, TargetClient: "target"}, "target", true, false},
		{"entry other", PingTask{Clients: StringArray{"source"}, TargetClient: "target"}, "other", false, false},
		{"empty client", PingTask{Clients: StringArray{""}}, "", false, false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.task.AppliesToClient(tt.client); got != tt.applies {
				t.Errorf("AppliesToClient(%q) = %v, want %v", tt.client, got, tt.applies)
			}
			if got := tt.task.ExecutesOnClient(tt.client); got != tt.executes {
				t.Errorf("ExecutesOnClient(%q) = %v, want %v", tt.client, got, tt.executes)
			}
		})
	}
}
