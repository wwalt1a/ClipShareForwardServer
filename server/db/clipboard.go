package db

import (
	"time"

	"gorm.io/gorm/clause"
)

// ClipboardItem 服务器存储的剪贴板条目
type ClipboardItem struct {
	Id        string     `gorm:"primaryKey;type:text"`
	GroupId   string     `gorm:"index;type:text;not null"` // PBKDF2(syncPassword) 作为分组标识
	DevId     string     `gorm:"type:text;not null"`       // 来源设备 ID
	Type      string     `gorm:"type:text;not null"`       // "text" 或 "image"
	Content   string     `gorm:"type:text"`                // 加密的文本内容（Base64），图片类型时为空
	FileId    string     `gorm:"type:text"`                // 图片文件 ID，文本类型时为空
	Tags      string     `gorm:"type:text"`                // 加密的标签列表（JSON数组，Base64）
	CreatedAt time.Time  `gorm:"not null"`
	UpdatedAt time.Time  `gorm:"not null"`                 // 最后修改时间
	ExpireAt  *time.Time `gorm:"index"`                    // 图片到期时间（30天后），文本为 nil
}

// ClipboardTag 标签关联表
type ClipboardTag struct {
	Id        string    `gorm:"primaryKey;type:text"`
	ItemId    string    `gorm:"index;type:text;not null"` // 关联的 ClipboardItem.Id
	TagName   string    `gorm:"type:text;not null"`       // 加密的标签名（Base64）
	CreatedAt time.Time `gorm:"not null"`
}

// OperationLog 操作日志表（用于细粒度同步）
type OperationLog struct {
	Id        string     `gorm:"primaryKey;type:text"`
	GroupId   string     `gorm:"index:idx_group_created;type:text;not null"` // 复合索引
	DevId     string     `gorm:"type:text;not null"`                          // 操作来源设备
	Type      string     `gorm:"type:text;not null"`                          // "addItem", "deleteItem", "addTag", "removeTag"
	ItemId    string     `gorm:"index;type:text"`                             // 关联的 ClipboardItem.Id
	TagName   string     `gorm:"type:text"`                                   // 标签操作时的加密标签名（Base64）
	Content   string     `gorm:"type:text"`                                   // addItem 时的加密内容（Base64）
	FileId    string     `gorm:"type:text"`                                   // addItem 图片时的文件ID
	ItemType  string     `gorm:"type:text"`                                   // addItem 时的类型 "text" 或 "image"
	CreatedAt time.Time  `gorm:"index:idx_group_created;not null"`           // 复合索引
	ExpireAt  *time.Time `gorm:"index"`                                       // 操作日志过期时间
}

// DeviceState 设备状态表
type DeviceState struct {
	DevId         string    `gorm:"primaryKey;type:text"`
	GroupId       string    `gorm:"index;type:text;not null"`
	LastSyncAt    time.Time `gorm:"not null"`          // 最后同步时间
	StorageLimit  int       `gorm:"default:30"`        // 存储限制（天数）
	FirstSyncDone bool      `gorm:"default:false"`     // 是否完成首次全量同步
	UpdatedAt     time.Time `gorm:"not null"`
}

// ServerConfig 服务器配置表
type ServerConfig struct {
	Key       string    `gorm:"primaryKey;type:text"`
	Value     string    `gorm:"type:text;not null"`
	UpdatedAt time.Time `gorm:"not null"`
}

func AddClipboardItem(item ClipboardItem) error {
	checkDb()
	// Use ON CONFLICT DO NOTHING to gracefully handle duplicate inserts
	// (e.g. when the same item is pushed via both /api/clip/push and /api/sync/push)
	return AppDb.Clauses(clause.OnConflict{DoNothing: true}).Create(&item).Error
}

func GetClipboardItemsSince(groupId string, since time.Time) ([]ClipboardItem, error) {
	checkDb()
	var items []ClipboardItem
	err := AppDb.Where("group_id = ? AND created_at > ?", groupId, since).
		Order("created_at ASC").
		Find(&items).Error
	return items, err
}

func DeleteClipboardItems(groupId string, ids []string) error {
	checkDb()
	return AppDb.Where("group_id = ? AND id IN ?", groupId, ids).
		Delete(&ClipboardItem{}).Error
}

func DeleteExpiredImages() (int64, error) {
	checkDb()
	result := AppDb.Where("type = 'image' AND expire_at IS NOT NULL AND expire_at <= ?", time.Now()).
		Delete(&ClipboardItem{})
	return result.RowsAffected, result.Error
}

func GetClipboardImageItem(groupId string, fileId string) (*ClipboardItem, error) {
	checkDb()
	var item ClipboardItem
	err := AppDb.Where("group_id = ? AND file_id = ? AND type = 'image'", groupId, fileId).
		First(&item).Error
	if err != nil {
		return nil, err
	}
	return &item, nil
}

func GetExpiredImageItems() ([]ClipboardItem, error) {
	checkDb()
	var items []ClipboardItem
	err := AppDb.Where("type = 'image' AND expire_at IS NOT NULL AND expire_at <= ?", time.Now()).
		Find(&items).Error
	return items, err
}

// OperationLog 相关函数

func AddOperationLog(log OperationLog) error {
	checkDb()
	return AppDb.Create(&log).Error
}

func AddOperationLogs(logs []OperationLog) error {
	checkDb()
	if len(logs) == 0 {
		return nil
	}
	return AppDb.Create(&logs).Error
}

func GetOperationLogsSince(groupId string, since time.Time) ([]OperationLog, error) {
	checkDb()
	var logs []OperationLog
	err := AppDb.Where("group_id = ? AND created_at >= ?", groupId, since).
		Order("created_at ASC").
		Find(&logs).Error
	return logs, err
}

func DeleteExpiredOperationLogs() (int64, error) {
	checkDb()
	result := AppDb.Where("expire_at IS NOT NULL AND expire_at <= ?", time.Now()).
		Delete(&OperationLog{})
	return result.RowsAffected, result.Error
}

// ClipboardTag 相关函数

func AddClipboardTag(tag ClipboardTag) error {
	checkDb()
	return AppDb.Create(&tag).Error
}

func GetClipboardTags(itemId string) ([]ClipboardTag, error) {
	checkDb()
	var tags []ClipboardTag
	err := AppDb.Where("item_id = ?", itemId).Find(&tags).Error
	return tags, err
}

func DeleteClipboardTag(itemId string, tagName string) error {
	checkDb()
	return AppDb.Where("item_id = ? AND tag_name = ?", itemId, tagName).
		Delete(&ClipboardTag{}).Error
}

func DeleteClipboardTagsByItemId(itemId string) error {
	checkDb()
	return AppDb.Where("item_id = ?", itemId).Delete(&ClipboardTag{}).Error
}

// DeviceState 相关函数

func UpsertDeviceState(state DeviceState) error {
	checkDb()
	state.UpdatedAt = time.Now()
	return AppDb.Save(&state).Error
}

func GetDeviceState(devId string) (*DeviceState, error) {
	checkDb()
	var state DeviceState
	err := AppDb.Where("dev_id = ?", devId).First(&state).Error
	if err != nil {
		return nil, err
	}
	return &state, nil
}

func GetDeviceStatesByGroup(groupId string) ([]DeviceState, error) {
	checkDb()
	var states []DeviceState
	err := AppDb.Where("group_id = ?", groupId).Find(&states).Error
	return states, err
}

// ServerConfig 相关函数

func GetServerConfig(key string, defaultValue string) (string, error) {
	checkDb()
	var config ServerConfig
	err := AppDb.Where("key = ?", key).First(&config).Error
	if err != nil {
		return defaultValue, nil
	}
	return config.Value, nil
}

func SetServerConfig(key string, value string) error {
	checkDb()
	config := ServerConfig{
		Key:       key,
		Value:     value,
		UpdatedAt: time.Now(),
	}
	return AppDb.Save(&config).Error
}
