package tasks

import (
	"context"
	"errors"
	"fmt"
	"net"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/Sake-My/Komari-Nova/database/dbcore"
	"github.com/Sake-My/Komari-Nova/database/models"
	"github.com/Sake-My/Komari-Nova/internal/metricstore"
	"github.com/Sake-My/Komari-Nova/utils"
	"gorm.io/gorm"
)

// ErrInvalidPingTask 表示任务参数或结果来源不符合探测约定。
var ErrInvalidPingTask = errors.New("invalid ping task")

// 入口状态以三个探测周期判断新鲜度，乘法及调度间隔都必须能安全转为 Duration。
const maxEntryPingIntervalSeconds = int64((1<<63 - 1) / (3 * time.Second))

// AddPingTask 创建延迟监测任务。defaultOn 表示新加入的服务器是否自动开启此监测。
func AddPingTask(clients []string, defaultOn bool, name string, target, task_type string, interval int, targetClient string) (uint, error) {
	db := dbcore.GetDBInstance()
	normalizedClients := normalizePingClients(models.StringArray(clients))
	task := models.PingTask{
		Clients:      normalizedClients,
		TargetClient: targetClient,
		DefaultOn:    defaultOn,
		Name:         name,
		Type:         task_type,
		Target:       target,
		Interval:     interval,
	}
	err := db.Transaction(func(tx *gorm.DB) error {
		if err := validatePingTask(tx, &task); err != nil {
			return err
		}
		if err := tx.Create(&task).Error; err != nil {
			return err
		}

		// Append by id to avoid races between concurrent create requests.
		result := tx.Model(&models.PingTask{}).Where("id = ?", task.Id).Update("weight", int(task.Id))
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return gorm.ErrRecordNotFound
		}

		return nil
	})
	if err != nil {
		return 0, err
	}
	return task.Id, ReloadPingSchedule()
}

func DeletePingTask(id []uint) error {
	// The metric store is independent from the main database, so clean it first
	// to avoid leaving history that can no longer be addressed through the task.
	if err := DeletePingRecords(id); err != nil {
		return err
	}

	db := dbcore.GetDBInstance()
	result := db.Where("id IN ?", id).Delete(&models.PingTask{})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	return ReloadPingSchedule()
}

// EditPingTask 批量更新延迟监测任务配置。
func EditPingTask(tasks []*models.PingTask) error {
	db := dbcore.GetDBInstance()
	err := db.Transaction(func(tx *gorm.DB) error {
		for _, task := range tasks {
			if task == nil || task.Id == 0 {
				return fmt.Errorf("%w: task id is required", ErrInvalidPingTask)
			}
			var existing models.PingTask
			if err := tx.First(&existing, task.Id).Error; err != nil {
				return err
			}
			task.Clients = normalizePingClients(task.Clients)
			// 固定入口任务的测量路径，防止历史或在途回包被归到另一节点。
			if task.TargetClient != existing.TargetClient {
				return fmt.Errorf("%w: target_client cannot be changed; create a new task", ErrInvalidPingTask)
			}
			if existing.TargetClient != "" && (task.Type != existing.Type || task.Target != existing.Target || !slices.Equal(task.Clients, existing.Clients)) {
				return fmt.Errorf("%w: entry probe source, target and type cannot be changed; create a new task", ErrInvalidPingTask)
			}
			if err := validatePingTask(tx, task); err != nil {
				return err
			}
			// 使用 map 显式更新，避免 GORM 跳过 false/0/空切片等零值。
			updates := map[string]interface{}{
				"name":        task.Name,
				"clients":     task.Clients,
				"all_clients": task.DefaultOn,
				"type":        task.Type,
				"target":      task.Target,
				"interval":    task.Interval,
			}
			if err := tx.Model(&models.PingTask{}).Where("id = ?", task.Id).Updates(updates).Error; err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return err
	}
	return ReloadPingSchedule()
}

func validatePingTask(db *gorm.DB, task *models.PingTask) error {
	if strings.TrimSpace(task.Name) == "" || strings.TrimSpace(task.Target) == "" || task.Interval <= 0 {
		return fmt.Errorf("%w: name, target and a positive interval are required", ErrInvalidPingTask)
	}
	if task.Type != "icmp" && task.Type != "tcp" && task.Type != "http" {
		return fmt.Errorf("%w: unsupported ping type", ErrInvalidPingTask)
	}
	if task.TargetClient == "" {
		return nil
	}
	if int64(task.Interval) > maxEntryPingIntervalSeconds {
		return fmt.Errorf("%w: entry interval must not exceed %d seconds", ErrInvalidPingTask, maxEntryPingIntervalSeconds)
	}
	if task.DefaultOn || len(task.Clients) != 1 || task.Clients[0] == "" || task.Clients[0] == task.TargetClient {
		return fmt.Errorf("%w: entry probes require exactly one other source client and default_on=false", ErrInvalidPingTask)
	}
	switch task.Type {
	case "icmp":
		if net.ParseIP(task.Target) == nil {
			return fmt.Errorf("%w: entry ICMP target must be an IP address", ErrInvalidPingTask)
		}
	case "tcp":
		host, port, err := net.SplitHostPort(task.Target)
		if err != nil || net.ParseIP(host) == nil {
			return fmt.Errorf("%w: entry TCP target must be IP:port (use brackets for IPv6)", ErrInvalidPingTask)
		}
		portNumber, err := strconv.ParseUint(port, 10, 16)
		if err != nil || portNumber == 0 {
			return fmt.Errorf("%w: entry TCP port must be between 1 and 65535", ErrInvalidPingTask)
		}
	default:
		return fmt.Errorf("%w: entry probes support only icmp and tcp", ErrInvalidPingTask)
	}
	return requirePingClients(db, []string{task.Clients[0], task.TargetClient})
}

func requirePingClients(db *gorm.DB, clients []string) error {
	var count int64
	if err := db.Model(&models.Client{}).Where("uuid IN ?", clients).Count(&count).Error; err != nil {
		return err
	}
	if count != int64(len(clients)) {
		return fmt.Errorf("%w: probe source or target client does not exist", ErrInvalidPingTask)
	}
	return nil
}

// ResolvePingRecordClient 校验回包来源，并把入口探测结果归到目标节点。
func ResolvePingRecordClient(taskID uint, source string) (string, error) {
	if taskID == 0 || source == "" {
		return "", fmt.Errorf("%w: task id and source are required", ErrInvalidPingTask)
	}
	db := dbcore.GetDBInstance()
	var task models.PingTask
	if err := db.First(&task, taskID).Error; err != nil {
		return "", err
	}
	if !task.ExecutesOnClient(source) {
		return "", fmt.Errorf("%w: result source is not assigned to this task", ErrInvalidPingTask)
	}
	if task.TargetClient != "" {
		if err := requirePingClients(db, []string{source, task.TargetClient}); err != nil {
			return "", err
		}
		return task.TargetClient, nil
	}
	if err := requirePingClients(db, []string{source}); err != nil {
		return "", err
	}
	return source, nil
}

// normalizePingClients 保持 clients 字段序列化为 JSON 数组，避免空值变成 null。
func normalizePingClients(clients models.StringArray) models.StringArray {
	if clients == nil {
		return models.StringArray{}
	}
	return clients
}

func GetAllPingTasks() ([]models.PingTask, error) {
	db := dbcore.GetDBInstance()
	var tasks []models.PingTask
	if err := db.Order("weight ASC").Order("id ASC").Find(&tasks).Error; err != nil {
		return nil, err
	}
	return tasks, nil
}

// GetPingTasksByClient 获取指定服务器需要执行的延迟监测任务。
func GetPingTasksByClient(uuid string) []models.PingTask {
	db := dbcore.GetDBInstance()
	var tasks []models.PingTask
	if err := db.Where("clients LIKE ?", `%"`+uuid+`"%`).Order("weight ASC").Order("id ASC").Find(&tasks).Error; err != nil {
		return nil
	}
	return tasks
}

func UpdatePingTaskOrder(order map[uint]int) error {
	if len(order) == 0 {
		return nil
	}

	db := dbcore.GetDBInstance()
	err := db.Transaction(func(tx *gorm.DB) error {
		// Validate all ids before changing any weights. The update response is
		// not a reliable existence check because some drivers report zero rows
		// when the new value equals the current value.
		ids := make([]uint, 0, len(order))
		for id := range order {
			ids = append(ids, id)
		}
		sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })

		var existing int64
		if err := tx.Model(&models.PingTask{}).Where("id IN ?", ids).Count(&existing).Error; err != nil {
			return err
		}
		if existing != int64(len(ids)) {
			return gorm.ErrRecordNotFound
		}

		for _, id := range ids {
			weight := order[id]
			result := tx.Model(&models.PingTask{}).Where("id = ?", id).Update("weight", weight)
			if result.Error != nil {
				return result.Error
			}
		}
		return nil
	})
	if err != nil {
		return err
	}
	return ReloadPingSchedule()
}

// ping 记录已完全迁移到 metric store（指标 ping.latency_ms），运行期读写全部走
// metric store，旧 ping_records 表不再参与。

func SavePingRecord(record models.PingRecord) error {
	return metricstore.WritePingRecord(context.Background(), record)
}

func DeletePingRecords(id []uint) error {
	return metricstore.DeletePingRecordsByTask(context.Background(), id)
}

func DeleteAllPingRecords() error {
	return metricstore.DeleteAllPingRecords(context.Background())
}

func ReloadPingSchedule() error {
	pingTasks, err := getScheduledPingTasks()
	if err != nil {
		return err
	}
	return utils.ReloadPingSchedule(pingTasks)
}

// getScheduledPingTasks 保留失去节点的入口配置供管理员查看，但不再向旧地址派发探测。
func getScheduledPingTasks() ([]models.PingTask, error) {
	pingTasks, err := GetAllPingTasks()
	if err != nil {
		return nil, err
	}
	clientIDs := make(map[string]struct{})
	for _, task := range pingTasks {
		if task.TargetClient != "" {
			clientIDs[task.TargetClient] = struct{}{}
			for _, source := range task.Clients {
				clientIDs[source] = struct{}{}
			}
		}
	}
	if len(clientIDs) == 0 {
		return pingTasks, nil
	}
	ids := make([]string, 0, len(clientIDs))
	for id := range clientIDs {
		ids = append(ids, id)
	}
	var existing []string
	if err := dbcore.GetDBInstance().Model(&models.Client{}).Where("uuid IN ?", ids).Pluck("uuid", &existing).Error; err != nil {
		return nil, err
	}
	alive := make(map[string]bool, len(existing))
	for _, id := range existing {
		alive[id] = true
	}
	scheduled := make([]models.PingTask, 0, len(pingTasks))
	for _, task := range pingTasks {
		if task.TargetClient != "" && (len(task.Clients) != 1 || !alive[task.TargetClient] || !alive[task.Clients[0]]) {
			continue
		}
		scheduled = append(scheduled, task)
	}
	return scheduled, nil
}

// AddDefaultOnClientUUID 在新客户端注册后，把该 UUID 追加到所有 default_on=true 的任务的 clients 中（去重）。
func AddDefaultOnClientUUID(uuid string) error {
	if uuid == "" {
		return nil
	}
	db := dbcore.GetDBInstance()
	var tasks []models.PingTask
	if err := db.Where("all_clients = ? AND (target_client = ? OR target_client IS NULL)", true, "").Find(&tasks).Error; err != nil {
		return err
	}
	if len(tasks) == 0 {
		return nil
	}
	changed := false
	for _, task := range tasks {
		exists := false
		for _, c := range task.Clients {
			if c == uuid {
				exists = true
				break
			}
		}
		if exists {
			continue
		}
		next := append(models.StringArray{}, task.Clients...)
		next = append(next, uuid)
		if err := db.Model(&models.PingTask{}).Where("id = ?", task.Id).Update("clients", next).Error; err != nil {
			return err
		}
		changed = true
	}
	if changed {
		return ReloadPingSchedule()
	}
	return nil
}

func GetPingRecords(uuid string, taskId int, start, end time.Time) ([]models.PingRecord, error) {
	return metricstore.GetPingRecords(context.Background(), uuid, taskId, start, end)
}
