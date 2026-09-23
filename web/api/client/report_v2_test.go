package client

import (
	"testing"

	v2 "github.com/Sake-My/Komari-Nova/protocol/v2"
)

func TestV2TaskResultNoLongerSupported(t *testing.T) {
	for _, transport := range []struct {
		name      string
		allowWait bool
	}{
		{name: "http", allowWait: true},
		{name: "websocket", allowWait: false},
	} {
		t.Run(transport.name, func(t *testing.T) {
			// 老 Agent 仍可能回传已经移除功能的结果，不能接受或写入数据库。
			response := handleV2RPC("legacy-agent", v2.Request{
				JSONRPC: v2.Version,
				ID:      "legacy-result",
				Method:  "agent.taskResult",
				Params: map[string]any{
					"task_id":   "legacy-task",
					"result":    "completed",
					"exit_code": 0,
				},
			}, transport.allowWait)
			if response.Error == nil || response.Error.Code != -32601 {
				t.Fatalf("legacy task result returned %#v, want method not found", response)
			}
			if response.ID != "legacy-result" || response.Error.Data != "agent.taskResult" {
				t.Fatalf("unexpected method-not-found response: %#v", response)
			}
		})
	}
}
