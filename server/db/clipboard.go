package db

import (
	"time"
)

// ClipboardItem 服务器存储的剪贴板条目
type ClipboardItem struct {
	Id        string     `gorm:"primaryKey;type:text"`
	GroupId   string     `gorm:"index;type:text;not null"` // PBKDF2(syncPassword) 作为分组标识
	DevId     string     `gorm:"type:text;not null"`       // 来源设备 ID
	Type      string     `gorm:"type:text;not null"`       // "text" 或 "image"
	Content   string     `gorm:"type:text"`                // 加密的文本内容（Base64），图片类型时为空
	FileId    string     `gorm:"type:text"`                // 图片文件 ID，文本类型时为空
	CreatedAt time.Time  `gorm:"not null"`
	ExpireAt  *time.Time `gorm:"index"` // 图片到期时间（30天后），文本为 nil
}

func AddClipboardItem(item ClipboardItem) error {
	checkDb()
	return AppDb.Create(&item).Error
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
