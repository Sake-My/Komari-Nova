package client

import (
	"context"
	"testing"
	"time"

	v2 "github.com/Sake-My/Komari-Nova/protocol/v2"
	agent_runtime "github.com/Sake-My/Komari-Nova/web/agent"
)

func TestV2StartupConfigResponseUsesAuthenticatedAgent(t *testing.T) {
	for _, allowWait := range []bool{false, true} {
		name := "websocket"
		if allowWait {
			name = "http"
		}
		t.Run(name, func(t *testing.T) {
			uuid := t.Name()
			agent_runtime.KeepAlivePresence(uuid, 1, time.Minute)
			agent_runtime.MarkV2Client(uuid)
			t.Cleanup(func() {
				agent_runtime.SetPresence(uuid, 1, false)
				agent_runtime.DeleteConnectedClients(uuid)
			})
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			type result struct {
				config map[string]any
				err    error
			}
			done := make(chan result, 1)
			go func() {
				config, err := agent_runtime.GetStartupConfig(ctx, uuid)
				done <- result{config, err}
			}()
			events := agent_runtime.WaitV2Events(uuid, nil, 2*time.Second)
			if len(events) != 1 {
				t.Fatalf("startup events = %#v", events)
			}
			t.Cleanup(func() { agent_runtime.AckV2Events(uuid, []string{events[0].ID}) })
			params, ok := events[0].Params.(v2.StartupConfigParams)
			if !ok {
				t.Fatalf("startup event params = %#v", events[0].Params)
			}
			req := v2.Request{
				JSONRPC: v2.Version, ID: "reply", Method: v2.MethodAgentStartupConfigResult,
				Params: v2.StartupConfigResult{RequestID: params.RequestID, Config: map[string]any{"token": "test-credential"}},
			}
			wrongAgent := handleV2RPC("another-agent", req, allowWait)
			if wrongAgent.Error == nil || wrongAgent.Error.Code != -32004 || wrongAgent.Error.Data != nil {
				t.Fatalf("wrong agent response = %#v", wrongAgent)
			}
			response := handleV2RPC(uuid, req, allowWait)
			if response.Error != nil {
				t.Fatalf("matching agent response = %#v", response)
			}
			select {
			case got := <-done:
				if got.err != nil || got.config["token"] != "test-credential" {
					t.Fatalf("configuration was not delivered to requester: %#v, %v", got.config, got.err)
				}
			case <-ctx.Done():
				t.Fatal("startup configuration request timed out")
			}
			duplicate := handleV2RPC(uuid, req, allowWait)
			if duplicate.Error == nil || duplicate.Error.Code != -32004 {
				t.Fatalf("duplicate response = %#v", duplicate)
			}
			req.Params = map[string]any{"request_id": 123, "config": map[string]any{"token": "test-credential"}}
			invalid := handleV2RPC(uuid, req, allowWait)
			if invalid.Error == nil || invalid.Error.Code != -32602 || invalid.Error.Data != nil {
				t.Fatalf("invalid result response = %#v", invalid)
			}
		})
	}
}
