package agent

import (
	"context"
	"errors"
	"slices"
	"sync"
	"time"

	v2 "github.com/Sake-My/Komari-Nova/protocol/v2"
)

var (
	ErrStartupConfigOffline = errors.New("agent is not connected")
	ErrStartupConfigTimeout = errors.New("agent startup configuration timed out; the agent may need an update")
	startupConfigMu         sync.Mutex
	startupConfigPending    = make(map[string]startupConfigCall)
)

type startupConfigCall struct {
	uuid     string
	response chan v2.StartupConfigResult
}

// GetStartupConfig 仅在本次请求期间保存启动配置凭据，调用方必须校验管理员权限。
func GetStartupConfig(ctx context.Context, uuid string) (map[string]any, error) {
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if !slices.Contains(GetAllOnlineUUIDs(), uuid) {
		return nil, ErrStartupConfigOffline
	}
	requestID := newV2EventID()
	response := make(chan v2.StartupConfigResult, 1)
	startupConfigMu.Lock()
	startupConfigPending[requestID] = startupConfigCall{uuid: uuid, response: response}
	startupConfigMu.Unlock()
	defer func() {
		startupConfigMu.Lock()
		delete(startupConfigPending, requestID)
		startupConfigMu.Unlock()
	}()
	if !DispatchV2Event(uuid, v2.MethodAgentStartupConfig, v2.StartupConfigParams{RequestID: requestID}) {
		return nil, ErrStartupConfigOffline
	}
	select {
	case result := <-response:
		if result.Error != "" {
			// 不回显 Agent 的原始错误，避免将凭据包含在错误响应中。
			return nil, errors.New("agent could not provide startup configuration")
		}
		if result.Config == nil {
			return nil, errors.New("agent returned an empty startup configuration")
		}
		return result.Config, nil
	case <-ctx.Done():
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return nil, ErrStartupConfigTimeout
		}
		return nil, ctx.Err()
	}
}

// ResolveStartupConfig 以已认证的 Agent 身份匹配请求，拒绝其他节点冒领响应。
func ResolveStartupConfig(uuid string, result v2.StartupConfigResult) bool {
	startupConfigMu.Lock()
	defer startupConfigMu.Unlock()
	wait, ok := startupConfigPending[result.RequestID]
	if !ok || wait.uuid != uuid {
		return false
	}
	delete(startupConfigPending, result.RequestID)
	wait.response <- result
	return true
}
