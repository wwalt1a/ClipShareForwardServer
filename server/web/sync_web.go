package web

import (
	"clipshare/db"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// SyncInitDto 首次全量同步请求
type SyncInitDto struct {
	GroupId string         `json:"groupId" binding:"required"`
	DevId   string         `json:"devId" binding:"required"`
	Items   []SyncInitItem `json:"items" binding:"required"`
}

type SyncInitItem struct {
	ItemId    string   `json:"itemId" binding:"required"`
	Type      string   `json:"type" binding:"required"` // "text" 或 "image"
	Content   string   `json:"content"`                 // 加密内容（text类型）
	FileId    string   `json:"fileId"`                  // 图片文件ID（image类型）
	Tags      []string `json:"tags"`                    // 加密的标签列表
	CreatedAt string   `json:"createdAt" binding:"required"` // ISO8601格式
}

// SyncPushDto 推送操作日志
type SyncPushDto struct {
	GroupId    string          `json:"groupId" binding:"required"`
	DevId      string          `json:"devId" binding:"required"`
	Operations []SyncOperation `json:"operations" binding:"required"`
}

type SyncOperation struct {
	Type      string `json:"type" binding:"required"` // "addItem", "deleteItem", "addTag", "removeTag"
	ItemId    string `json:"itemId" binding:"required"`
	Content   string `json:"content"`   // addItem 时的加密内容
	FileId    string `json:"fileId"`    // addItem 图片时的文件ID
	ItemType  string `json:"itemType"`  // addItem 时的类型
	TagName   string `json:"tagName"`   // 标签操作时的加密标签名
	CreatedAt string `json:"createdAt" binding:"required"` // ISO8601格式
}

// SyncPullResponse 拉取操作日志响应
type SyncPullResponse struct {
	Operations []SyncOperationResponse `json:"operations"`
}

type SyncOperationResponse struct {
	Id        string `json:"id"`
	Type      string `json:"type"`
	ItemId    string `json:"itemId"`
	Content   string `json:"content,omitempty"`
	FileId    string `json:"fileId,omitempty"`
	ItemType  string `json:"itemType,omitempty"`
	TagName   string `json:"tagName,omitempty"`
	DevId     string `json:"devId"`
	CreatedAt string `json:"createdAt"`
}

// DeviceStateDto 更新设备状态
type DeviceStateDto struct {
	GroupId      string `json:"groupId" binding:"required"`
	DevId        string `json:"devId" binding:"required"`
	StorageLimit int    `json:"storageLimit"`
}

// syncInit 首次全量同步
func syncInit(c *gin.Context) {
	var dto SyncInitDto
	if err := c.ShouldBindJSON(&dto); err != nil {
		errorResult(c, http.StatusBadRequest, err.Error(), nil)
		return
	}

	// 获取操作日志TTL配置（默认7天）
	ttlDaysStr, _ := db.GetServerConfig("operation_log_ttl_days", "7")
	ttlDays, _ := strconv.Atoi(ttlDaysStr)

	for _, item := range dto.Items {
		createdAt, err := time.Parse(time.RFC3339, item.CreatedAt)
		if err != nil {
			errorResult(c, http.StatusBadRequest, "invalid createdAt format", nil)
			return
		}

		// 添加到 ClipboardItem 表
		clipItem := db.ClipboardItem{
			Id:        item.ItemId,
			GroupId:   dto.GroupId,
			DevId:     dto.DevId,
			Type:      item.Type,
			Content:   item.Content,
			FileId:    item.FileId,
			CreatedAt: createdAt,
			UpdatedAt: createdAt,
		}
		if item.Type == "image" {
			expireAt := createdAt.AddDate(0, 0, imageTTLDays)
			clipItem.ExpireAt = &expireAt
		}
		if err := db.AddClipboardItem(clipItem); err != nil {
			// 忽略重复插入错误
			continue
		}

		// 添加标签
		for _, tagName := range item.Tags {
			tag := db.ClipboardTag{
				Id:        uuid.NewString(),
				ItemId:    item.ItemId,
				TagName:   tagName,
				CreatedAt: createdAt,
			}
			_ = db.AddClipboardTag(tag)
		}

		// 记录操作日志
		expireAt := time.Now().AddDate(0, 0, ttlDays)
		opLog := db.OperationLog{
			Id:        uuid.NewString(),
			GroupId:   dto.GroupId,
			DevId:     dto.DevId,
			Type:      "addItem",
			ItemId:    item.ItemId,
			Content:   item.Content,
			FileId:    item.FileId,
			ItemType:  item.Type,
			CreatedAt: createdAt,
			ExpireAt:  &expireAt,
		}
		_ = db.AddOperationLog(opLog)

		// 记录标签操作日志
		for _, tagName := range item.Tags {
			tagLog := db.OperationLog{
				Id:        uuid.NewString(),
				GroupId:   dto.GroupId,
				DevId:     dto.DevId,
				Type:      "addTag",
				ItemId:    item.ItemId,
				TagName:   tagName,
				CreatedAt: createdAt,
				ExpireAt:  &expireAt,
			}
			_ = db.AddOperationLog(tagLog)
		}
	}

	// 更新设备状态
	state := db.DeviceState{
		DevId:         dto.DevId,
		GroupId:       dto.GroupId,
		LastSyncAt:    time.Now(),
		FirstSyncDone: true,
	}
	_ = db.UpsertDeviceState(state)

	successResult(c, gin.H{"synced": len(dto.Items)})
}

// syncPush 推送操作日志
func syncPush(c *gin.Context) {
	var dto SyncPushDto
	if err := c.ShouldBindJSON(&dto); err != nil {
		errorResult(c, http.StatusBadRequest, err.Error(), nil)
		return
	}

	// 获取操作日志TTL配置
	ttlDaysStr, _ := db.GetServerConfig("operation_log_ttl_days", "7")
	ttlDays, _ := strconv.Atoi(ttlDaysStr)

	var logs []db.OperationLog
	for _, op := range dto.Operations {
		createdAt, err := time.Parse(time.RFC3339, op.CreatedAt)
		if err != nil {
			errorResult(c, http.StatusBadRequest, "invalid createdAt format", nil)
			return
		}

		expireAt := time.Now().AddDate(0, 0, ttlDays)
		log := db.OperationLog{
			Id:        uuid.NewString(),
			GroupId:   dto.GroupId,
			DevId:     dto.DevId,
			Type:      op.Type,
			ItemId:    op.ItemId,
			Content:   op.Content,
			FileId:    op.FileId,
			ItemType:  op.ItemType,
			TagName:   op.TagName,
			CreatedAt: createdAt,
			ExpireAt:  &expireAt,
		}
		logs = append(logs, log)

		// 根据操作类型更新 ClipboardItem 和 ClipboardTag
		switch op.Type {
		case "addItem":
			clipItem := db.ClipboardItem{
				Id:        op.ItemId,
				GroupId:   dto.GroupId,
				DevId:     dto.DevId,
				Type:      op.ItemType,
				Content:   op.Content,
				FileId:    op.FileId,
				CreatedAt: createdAt,
				UpdatedAt: createdAt,
			}
			if op.ItemType == "image" {
				imgExpireAt := createdAt.AddDate(0, 0, imageTTLDays)
				clipItem.ExpireAt = &imgExpireAt
			}
			_ = db.AddClipboardItem(clipItem)

		case "deleteItem":
			// 删除关联的标签
			_ = db.DeleteClipboardTagsByItemId(op.ItemId)
			// 删除记录
			_ = db.DeleteClipboardItems(dto.GroupId, []string{op.ItemId})

		case "addTag":
			tag := db.ClipboardTag{
				Id:        uuid.NewString(),
				ItemId:    op.ItemId,
				TagName:   op.TagName,
				CreatedAt: createdAt,
			}
			_ = db.AddClipboardTag(tag)

		case "removeTag":
			_ = db.DeleteClipboardTag(op.ItemId, op.TagName)
		}
	}

	// 批量插入操作日志
	if err := db.AddOperationLogs(logs); err != nil {
		errorResult(c, http.StatusInternalServerError, err.Error(), nil)
		return
	}

	// 更新设备状态
	state := db.DeviceState{
		DevId:      dto.DevId,
		GroupId:    dto.GroupId,
		LastSyncAt: time.Now(),
	}
	_ = db.UpsertDeviceState(state)

	successResult(c, gin.H{"pushed": len(dto.Operations)})
}

// syncPull 拉取操作日志
func syncPull(c *gin.Context) {
	groupId := c.Query("groupId")
	if groupId == "" {
		errorResult(c, http.StatusBadRequest, "groupId required", nil)
		return
	}
	sinceStr := c.Query("since")
	var since time.Time
	if sinceStr != "" {
		if ts, err := time.Parse(time.RFC3339, sinceStr); err == nil {
			since = ts
		}
	}

	logs, err := db.GetOperationLogsSince(groupId, since)
	if err != nil {
		errorResult(c, http.StatusInternalServerError, err.Error(), nil)
		return
	}

	var operations []SyncOperationResponse
	for _, log := range logs {
		op := SyncOperationResponse{
			Id:        log.Id,
			Type:      log.Type,
			ItemId:    log.ItemId,
			Content:   log.Content,
			FileId:    log.FileId,
			ItemType:  log.ItemType,
			TagName:   log.TagName,
			DevId:     log.DevId,
			CreatedAt: log.CreatedAt.Format(time.RFC3339),
		}
		operations = append(operations, op)
	}

	successResult(c, SyncPullResponse{Operations: operations})
}

// updateDeviceState 更新设备状态
func updateDeviceState(c *gin.Context) {
	var dto DeviceStateDto
	if err := c.ShouldBindJSON(&dto); err != nil {
		errorResult(c, http.StatusBadRequest, err.Error(), nil)
		return
	}

	state := db.DeviceState{
		DevId:        dto.DevId,
		GroupId:      dto.GroupId,
		LastSyncAt:   time.Now(),
		StorageLimit: dto.StorageLimit,
	}
	if err := db.UpsertDeviceState(state); err != nil {
		errorResult(c, http.StatusInternalServerError, err.Error(), nil)
		return
	}

	successResult(c, true)
}

// StartOperationLogCleanup 启动定时任务，清理过期操作日志
func StartOperationLogCleanup() {
	go func() {
		for {
			time.Sleep(24 * time.Hour)
			cleanExpiredOperationLogs()
		}
	}()
	// 启动时也执行一次
	go cleanExpiredOperationLogs()
}

func cleanExpiredOperationLogs() {
	cnt, err := db.DeleteExpiredOperationLogs()
	if err != nil {
		return
	}
	if cnt > 0 {
		// 日志记录
	}
}

