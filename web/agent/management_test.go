package agent

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	v2 "github.com/Sake-My/Komari-Nova/protocol/v2"
)

func useManagementTestAgent(t *testing.T, v2Enabled bool) string {
	t.Helper()
	uuid := t.Name()
	KeepAlivePresence(uuid, 1, time.Minute)
	if v2Enabled {
		MarkV2Client(uuid)
	}
	t.Cleanup(func() {
		SetPresence(uuid, 1, false)
		DeleteConnectedClients(uuid)
		v2EventMu.Lock()
		delete(v2EventQueues, uuid)
		v2EventMu.Unlock()
	})
	return uuid
}

func requestTestStartupConfig(t *testing.T, uuid string) (v2.StartupConfigParams, context.CancelFunc, <-chan startupConfigTestResult) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	t.Cleanup(cancel)
	done := make(chan startupConfigTestResult, 1)
	go func() {
		config, err := GetStartupConfig(ctx, uuid)
		done <- startupConfigTestResult{config, err}
	}()
	events := WaitV2Events(uuid, nil, 2*time.Second)
	if len(events) != 1 || events[0].Method != v2.MethodAgentStartupConfig {
		t.Fatalf("unexpected startup configuration events: %#v", events)
	}
	params, ok := events[0].Params.(v2.StartupConfigParams)
	if !ok || params.RequestID == "" {
		t.Fatalf("invalid startup configuration request: %#v", events[0].Params)
	}
	return params, cancel, done
}

type startupConfigTestResult struct {
	config map[string]any
	err    error
}

func awaitTestStartupConfig(t *testing.T, done <-chan startupConfigTestResult) startupConfigTestResult {
	t.Helper()
	select {
	case result := <-done:
		return result
	case <-time.After(4 * time.Second):
		t.Fatal("startup configuration request did not finish")
		return startupConfigTestResult{}
	}
}

func TestStartupConfigRequiresMatchingAuthenticatedAgent(t *testing.T) {
	uuid := useManagementTestAgent(t, true)
	params, _, done := requestTestStartupConfig(t, uuid)
	want := map[string]any{"token": "test-credential", "interval": float64(3)}
	response := v2.StartupConfigResult{RequestID: params.RequestID, Config: want}
	if ResolveStartupConfig("another-agent", response) {
		t.Fatal("another authenticated agent was allowed to answer the request")
	}
	if !ResolveStartupConfig(uuid, response) {
		t.Fatal("matching agent response was rejected")
	}
	if ResolveStartupConfig(uuid, response) {
		t.Fatal("duplicate response was accepted")
	}
	got := awaitTestStartupConfig(t, done)
	if got.err != nil || !reflect.DeepEqual(got.config, want) {
		t.Fatalf("startup configuration result = %#v, %v", got.config, got.err)
	}
}

func TestStartupConfigCancellationRejectsLateResponses(t *testing.T) {
	uuid := useManagementTestAgent(t, true)
	params, cancel, done := requestTestStartupConfig(t, uuid)
	cancel()
	result := awaitTestStartupConfig(t, done)
	if !errors.Is(result.err, context.Canceled) || result.config != nil {
		t.Fatalf("canceled request returned %#v, %v", result.config, result.err)
	}
	if ResolveStartupConfig(uuid, v2.StartupConfigResult{RequestID: params.RequestID, Config: map[string]any{"token": "late"}}) {
		t.Fatal("canceled request accepted a late response")
	}
}

func TestStartupConfigDoesNotExposeAgentErrors(t *testing.T) {
	for _, agentError := range []string{"", "secret=test-credential"} {
		t.Run(agentError, func(t *testing.T) {
			uuid := useManagementTestAgent(t, true)
			params, _, done := requestTestStartupConfig(t, uuid)
			if !ResolveStartupConfig(uuid, v2.StartupConfigResult{RequestID: params.RequestID, Error: agentError}) {
				t.Fatal("response was not matched")
			}
			result := awaitTestStartupConfig(t, done)
			if result.err == nil || result.config != nil || strings.Contains(result.err.Error(), "test-credential") {
				t.Fatalf("invalid or unsafe error response: %#v, %v", result.config, result.err)
			}
		})
	}
}

func TestStartupConfigOfflineAndExpiredContext(t *testing.T) {
	if _, err := GetStartupConfig(context.Background(), t.Name()); !errors.Is(err, ErrStartupConfigOffline) {
		t.Fatalf("offline request error = %v", err)
	}
	ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer cancel()
	if _, err := GetStartupConfig(ctx, t.Name()); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expired request error = %v", err)
	}
}
