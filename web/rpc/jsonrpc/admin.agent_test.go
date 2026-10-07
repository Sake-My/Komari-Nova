package jsonrpc

import (
	"context"
	"testing"

	"github.com/Sake-My/Komari-Nova/pkg/rpc"
)

func TestAgentStartupConfigRequiresAdmin(t *testing.T) {
	for _, role := range []string{rpc.RoleGuest, rpc.RoleClient} {
		response := Dispatch(context.Background(), &rpc.ContextMeta{Permission: role}, &rpc.JsonRpcRequest{
			Version: rpc.RPC_VERSION, ID: 1, Method: "admin:getAgentStartupConfig",
			Params: map[string]any{"uuid": "offline"},
		})
		if response.Error == nil || response.Error.Code != rpc.PermissionDenied {
			t.Fatalf("startup configuration allowed role %s: %#v", role, response)
		}
	}
	if !rpc.IsSensitive("admin:getAgentStartupConfig") {
		t.Fatal("credential-bearing startup configuration must require sensitive-operation verification")
	}
}

func TestAgentStartupConfigRejectsInvalidAndOfflineRequests(t *testing.T) {
	for _, test := range []struct {
		params any
		code   int
	}{
		{map[string]any{}, rpc.InvalidParams},
		{map[string]any{"uuid": " "}, rpc.InvalidParams},
		{map[string]any{"uuid": "offline"}, rpc.Unavailable},
	} {
		response := Dispatch(context.Background(), &rpc.ContextMeta{Permission: rpc.RoleAdmin}, &rpc.JsonRpcRequest{
			Version: rpc.RPC_VERSION, ID: 1, Method: "admin:getAgentStartupConfig", Params: test.params,
		})
		if response.Error == nil || response.Error.Code != test.code {
			t.Fatalf("startup configuration returned %#v, want error %d", response, test.code)
		}
	}
}
