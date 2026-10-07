package client

import (
	"testing"

	v2 "github.com/Sake-My/Komari-Nova/protocol/v2"
)

func TestV2RemovedRemoteControlResultsNoLongerSupported(t *testing.T) {
	for _, transport := range []struct {
		name      string
		allowWait bool
	}{
		{name: "http", allowWait: true},
		{name: "websocket", allowWait: false},
	} {
		for _, method := range []string{"agent.taskResult", "agent.file.result"} {
			t.Run(transport.name+"/"+method, func(t *testing.T) {
				// 老 Agent 仍可能回传已经移除功能的结果，不能接受或写入数据库。
				response := handleV2RPC("legacy-agent", v2.Request{
					JSONRPC: v2.Version,
					ID:      "legacy-result",
					Method:  method,
					Params: map[string]any{
						"task_id":    "legacy-task",
						"request_id": "legacy-file-request",
						"result":     "completed",
						"exit_code":  0,
					},
				}, transport.allowWait)
				if response.Error == nil || response.Error.Code != -32601 {
					t.Fatalf("legacy remote control result returned %#v, want method not found", response)
				}
				if response.ID != "legacy-result" || response.Error.Data != method {
					t.Fatalf("unexpected method-not-found response: %#v", response)
				}
			})
		}
	}
}
