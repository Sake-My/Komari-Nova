package jsonrpc

import (
	"context"
	"testing"

	"github.com/Sake-My/Komari-Nova/pkg/rpc"
)

func TestRemovedRemoteControlMethodsUnavailableToAdmin(t *testing.T) {
	methods := []string{
		"admin:switchAgentVersion",
		"admin:exec",
		"admin:fileList",
		"admin:fileListRoots",
		"admin:fileStat",
		"admin:fileMkdir",
		"admin:fileDelete",
		"admin:fileMove",
		"admin:fileCopy",
		"admin:fileChmod",
		"admin:fileChown",
		"admin:fileSearch",
		"admin:getTasks",
		"admin:getTaskById",
		"admin:getTasksByClientId",
		"admin:getSpecificTaskResult",
		"admin:getTaskResultsByTaskId",
		"admin:getXtermjsSettings",
		"admin:setXtermjsSettings",
	}
	for _, method := range methods {
		t.Run(method, func(t *testing.T) {
			// 即使已通过管理员认证，旧入口也必须不可调用。
			response := Dispatch(context.Background(), &rpc.ContextMeta{Permission: rpc.RoleAdmin}, &rpc.JsonRpcRequest{
				Version: rpc.RPC_VERSION,
				ID:      "removed-feature",
				Method:  method,
			})
			if response.Error == nil || response.Error.Code != rpc.MethodNotFound {
				t.Fatalf("removed method %q returned %#v, want method not found", method, response)
			}
			if response.ID != "removed-feature" {
				t.Fatalf("response ID = %#v, want original request ID", response.ID)
			}

			help := rpc.Call(1, "rpc.help", map[string]any{"method": method})
			if help.Error == nil {
				t.Fatalf("removed method %q is still advertised by rpc.help", method)
			}
		})
	}
}
