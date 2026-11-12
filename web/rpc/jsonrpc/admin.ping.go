package jsonrpc

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/Sake-My/Komari-Nova/database/dbcore"
	"github.com/Sake-My/Komari-Nova/database/models"
	"github.com/Sake-My/Komari-Nova/database/tasks"
	"github.com/Sake-My/Komari-Nova/pkg/rpc"
)

// admin.ping.go
// 延迟监测任务（ping task）的 RPC2 方法（admin 命名空间）。

func init() {
	RegisterWithGroupAndMeta("addPingTask", rpc.RoleAdmin, adminAddPingTask, &rpc.MethodMeta{
		Name:    "admin:addPingTask",
		Summary: "Create a ping task",
		Returns: "{ task_id: uint }",
	})
	RegisterWithGroupAndMeta("deletePingTask", rpc.RoleAdmin, adminDeletePingTask, &rpc.MethodMeta{
		Name:    "admin:deletePingTask",
		Summary: "Delete ping tasks by ids",
		Returns: "null",
	})
	RegisterWithGroupAndMeta("editPingTask", rpc.RoleAdmin, adminEditPingTask, &rpc.MethodMeta{
		Name:    "admin:editPingTask",
		Summary: "Edit ping tasks",
		Returns: "null",
	})
	RegisterWithGroupAndMeta("getAllPingTasks", rpc.RoleAdmin, adminGetAllPingTasks, &rpc.MethodMeta{
		Name:    "admin:getAllPingTasks",
		Summary: "List all ping tasks",
		Returns: "PingTask[]",
	})
	RegisterWithGroupAndMeta("orderPingTask", rpc.RoleAdmin, adminOrderPingTask, &rpc.MethodMeta{
		Name:    "admin:orderPingTask",
		Summary: "Reorder ping tasks (map of id->weight)",
		Returns: "null",
	})
}

func adminAddPingTask(_ context.Context, req *rpc.JsonRpcRequest) (any, *rpc.JsonRpcError) {
	var params struct {
		Clients      []string `json:"clients"`
		DefaultOn    bool     `json:"default_on"`
		Name         string   `json:"name"`
		Target       string   `json:"target"`
		TaskType     string   `json:"type"`
		Interval     int      `json:"interval"`
		TargetClient string   `json:"target_client"`
	}
	if err := req.BindParams(&params); err != nil {
		return nil, rpc.MakeError(rpc.InvalidParams, "Invalid request data", nil)
	}
	if params.Name == "" || params.Target == "" || params.TaskType == "" || params.Interval == 0 {
		return nil, rpc.MakeError(rpc.InvalidParams, "name, target, type and interval are required", nil)
	}
	if !params.DefaultOn && len(params.Clients) == 0 {
		return nil, rpc.MakeError(rpc.InvalidParams, "clients is required when default_on is false", nil)
	}
	taskID, err := tasks.AddPingTask(params.Clients, params.DefaultOn, params.Name, params.Target, params.TaskType, params.Interval, params.TargetClient)
	if err != nil {
		if errors.Is(err, tasks.ErrInvalidPingTask) {
			return nil, rpc.MakeError(rpc.InvalidParams, err.Error(), nil)
		}
		return nil, rpc.MakeError(rpc.InternalError, err.Error(), nil)
	}
	return map[string]any{"task_id": taskID}, nil
}

func adminDeletePingTask(_ context.Context, req *rpc.JsonRpcRequest) (any, *rpc.JsonRpcError) {
	var params struct {
		ID []uint `json:"id"`
	}
	req.BindParams(&params)
	if len(params.ID) == 0 {
		return nil, rpc.MakeError(rpc.InvalidParams, "id is required", nil)
	}
	if err := tasks.DeletePingTask(params.ID); err != nil {
		return nil, rpc.MakeError(rpc.InternalError, err.Error(), nil)
	}
	return nil, nil
}

func adminEditPingTask(_ context.Context, req *rpc.JsonRpcRequest) (any, *rpc.JsonRpcError) {
	var params struct {
		Tasks []json.RawMessage `json:"tasks"`
	}
	if err := req.BindParams(&params); err != nil {
		return nil, rpc.MakeError(rpc.InvalidParams, "Invalid request data", nil)
	}
	if len(params.Tasks) == 0 {
		return nil, rpc.MakeError(rpc.InvalidParams, "Invalid request data", nil)
	}
	updates := make([]*models.PingTask, 0, len(params.Tasks))
	for _, raw := range params.Tasks {
		var task *models.PingTask
		if err := json.Unmarshal(raw, &task); err != nil || task == nil || task.Id == 0 {
			return nil, rpc.MakeError(rpc.InvalidParams, "Invalid request data", nil)
		}
		var fields map[string]json.RawMessage
		if err := json.Unmarshal(raw, &fields); err != nil {
			return nil, rpc.MakeError(rpc.InvalidParams, "Invalid request data", nil)
		}
		// 兼容尚未发送新字段的编辑界面，避免清除入口关联。
		if _, supplied := fields["target_client"]; !supplied {
			var existing models.PingTask
			if err := dbcore.GetDBInstance().Select("id", "target_client").First(&existing, task.Id).Error; err != nil {
				return nil, rpc.MakeError(rpc.InvalidParams, "Ping task not found", nil)
			}
			task.TargetClient = existing.TargetClient
		}
		updates = append(updates, task)
	}
	if err := tasks.EditPingTask(updates); err != nil {
		if errors.Is(err, tasks.ErrInvalidPingTask) {
			return nil, rpc.MakeError(rpc.InvalidParams, err.Error(), nil)
		}
		return nil, rpc.MakeError(rpc.InternalError, err.Error(), nil)
	}
	return nil, nil
}

func adminGetAllPingTasks(_ context.Context, _ *rpc.JsonRpcRequest) (any, *rpc.JsonRpcError) {
	list, err := tasks.GetAllPingTasks()
	if err != nil {
		return nil, rpc.MakeError(rpc.InternalError, err.Error(), nil)
	}
	return list, nil
}

func adminOrderPingTask(_ context.Context, req *rpc.JsonRpcRequest) (any, *rpc.JsonRpcError) {
	// 参数为 { idStr: weight } 映射。
	order := map[uint]int{}
	var raw map[string]int
	if err := req.BindParams(&raw); err != nil {
		return nil, rpc.MakeError(rpc.InvalidParams, "Invalid or missing request body: "+err.Error(), nil)
	}
	for idStr, weight := range raw {
		id, err := parseUintKey(idStr)
		if err != nil {
			return nil, rpc.MakeError(rpc.InvalidParams, "Invalid task id: "+idStr, nil)
		}
		order[id] = weight
	}
	if err := tasks.UpdatePingTaskOrder(order); err != nil {
		return nil, rpc.MakeError(rpc.InternalError, err.Error(), nil)
	}
	return nil, nil
}
