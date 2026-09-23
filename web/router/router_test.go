package router

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestRemovedRemoteControlEndpointsRejectRequests(t *testing.T) {
	gin.SetMode(gin.TestMode)
	t.Chdir(t.TempDir())
	engine := gin.New()
	Register(engine)

	// 同时覆盖浏览器入口、旧 REST 客户端以及 WebSocket 重连请求。
	requests := []struct {
		method string
		path   string
	}{
		{http.MethodPost, "/api/admin/task/exec"},
		{http.MethodGet, "/api/admin/task/all"},
		{http.MethodGet, "/api/admin/task/example"},
		{http.MethodGet, "/api/admin/task/example/result"},
		{http.MethodGet, "/api/admin/task/example/result/node"},
		{http.MethodGet, "/api/admin/task/client/node"},
		{http.MethodGet, "/api/admin/client/node/terminal?request_id=old-session"},
		{http.MethodGet, "/api/clients/terminal?id=old-session"},
		{http.MethodGet, "/api/admin/settings/xtermjs"},
		{http.MethodPost, "/api/admin/settings/xtermjs"},
		{http.MethodGet, "/terminal?uuid=node"},
		{http.MethodGet, "/terminal/"},
		{http.MethodGet, "/admin/exec"},
		{http.MethodGet, "/admin/settings/xtermjs"},
	}
	for _, test := range requests {
		t.Run(test.method+" "+test.path, func(t *testing.T) {
			request := httptest.NewRequest(test.method, test.path, strings.NewReader("{}"))
			request.Header.Set("Content-Type", "application/json")
			request.Header.Set("Connection", "Upgrade")
			request.Header.Set("Upgrade", "websocket")
			request.Header.Set("Sec-WebSocket-Version", "13")
			request.Header.Set("Sec-WebSocket-Key", "dGhlIHNhbXBsZSBub25jZQ==")
			response := httptest.NewRecorder()
			engine.ServeHTTP(response, request)
			if response.Code != http.StatusGone {
				t.Fatalf("status = %d, want %d; body=%s", response.Code, http.StatusGone, response.Body.String())
			}
			if response.Header().Get("Sec-WebSocket-Accept") != "" {
				t.Fatal("removed endpoint accepted a WebSocket connection")
			}
		})
	}
}

func TestMonitoringAndFileTransferRoutesRemainRegistered(t *testing.T) {
	gin.SetMode(gin.TestMode)
	t.Chdir(t.TempDir())
	engine := gin.New()
	Register(engine)
	routes := make(map[string]bool)
	for _, route := range engine.Routes() {
		routes[route.Method+" "+route.Path] = true
	}
	for _, route := range []string{
		"GET /api/clients/v2/rpc",
		"POST /api/clients/v2/rpc",
		"GET /api/clients/transfer/:id",
		"POST /api/clients/transfer/:id",
		"GET /api/admin/client/:uuid/file/download",
		"POST /api/admin/client/:uuid/file/upload",
		"GET /api/task/ping",
		"GET /api/admin/ping/",
		"POST /api/rpc2",
	} {
		if !routes[route] {
			t.Errorf("unrelated route was removed: %s", route)
		}
	}
	for route := range routes {
		if strings.Contains(route, "/terminal") || strings.Contains(route, "/xtermjs") ||
			strings.Contains(route, "/api/admin/task/") {
			t.Errorf("removed remote control route is still registered: %s", route)
		}
	}
}
