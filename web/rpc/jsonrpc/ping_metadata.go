package jsonrpc

import (
	"context"

	"github.com/Sake-My/Komari-Nova/database/dbcore"
	"github.com/Sake-My/Komari-Nova/database/models"
	agent_runtime "github.com/Sake-My/Komari-Nova/web/agent"
)

// publicEntryProbeInfo 不包含入口地址；隐藏探测节点的身份也不会向访客公开。
type publicEntryProbeInfo struct {
	TargetClient      string `json:"target_client,omitempty"`
	ProbeSource       string `json:"probe_source,omitempty"`
	ProbeSourceName    string `json:"probe_source_name,omitempty"`
	ProbeSourceOnline *bool  `json:"probe_source_online,omitempty"`
}

type pingTaskVisibility struct {
	admin  bool
	nodes  map[string]models.Client
	online map[string]bool
}

func loadPingTaskVisibility(ctx context.Context, tasks []models.PingTask) (pingTaskVisibility, error) {
	view := pingTaskVisibility{
		admin: isLoginFromCtx(ctx), nodes: make(map[string]models.Client), online: make(map[string]bool),
	}
	ids := make([]string, 0)
	for _, task := range tasks {
		if task.TargetClient != "" {
			ids = append(ids, task.TargetClient)
			ids = append(ids, task.Clients...)
		}
	}
	if len(ids) == 0 {
		return view, nil
	}
	var nodes []models.Client
	if err := dbcore.GetDBInstance().Select("uuid", "name", "hidden").Where("uuid IN ?", ids).Find(&nodes).Error; err != nil {
		return view, err
	}
	for _, node := range nodes {
		view.nodes[node.UUID] = node
	}
	// V2 能力标记不代表在线，POST Agent 必须按有期限的 presence 判断。
	for _, id := range agent_runtime.GetAllOnlineUUIDs() {
		view.online[id] = true
	}
	return view, nil
}

func (view pingTaskVisibility) allows(task models.PingTask) bool {
	if task.TargetClient == "" {
		return true
	}
	node, exists := view.nodes[task.TargetClient]
	return exists && (view.admin || !node.Hidden)
}

func (view pingTaskVisibility) entryInfo(task models.PingTask) publicEntryProbeInfo {
	if task.TargetClient == "" {
		return publicEntryProbeInfo{}
	}
	online := false
	info := publicEntryProbeInfo{TargetClient: task.TargetClient, ProbeSourceOnline: &online}
	if len(task.Clients) != 1 {
		return info
	}
	source, exists := view.nodes[task.Clients[0]]
	if !exists {
		return info
	}
	online = view.online[source.UUID]
	if view.admin || !source.Hidden {
		info.ProbeSource = source.UUID
		info.ProbeSourceName = source.Name
	}
	return info
}

func pingResultClients(task models.PingTask) []string {
	if task.TargetClient != "" {
		return []string{task.TargetClient}
	}
	return task.Clients
}

func addEntryProbeMetadata(info map[string]any, entry publicEntryProbeInfo) {
	if entry.TargetClient == "" {
		return
	}
	info["target_client"] = entry.TargetClient
	info["probe_source_online"] = *entry.ProbeSourceOnline
	if entry.ProbeSource != "" {
		info["probe_source"] = entry.ProbeSource
		info["probe_source_name"] = entry.ProbeSourceName
	}
}
