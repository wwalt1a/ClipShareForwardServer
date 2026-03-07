package web

import (
	"clipshare/db"
	"clipshare/storage"
	"clipshare/utils"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

const imageTTLDays = 30

// ClipPushTextDto 推送文本剪贴板条目
type ClipPushTextDto struct {
	GroupId string `json:"groupId" binding:"required"`
	DevId   string `json:"devId" binding:"required"`
	Content string `json:"content" binding:"required"` // AES 加密后 Base64
}

// ClipDeleteDto 删除条目
type ClipDeleteDto struct {
	GroupId string   `json:"groupId" binding:"required"`
	Ids     []string `json:"ids" binding:"required"`
}

// pushClipText 推送加密文本剪贴板条目
func pushClipText(c *gin.Context) {
	var dto ClipPushTextDto
	if err := c.ShouldBindJSON(&dto); err != nil {
		errorResult(c, http.StatusBadRequest, err.Error(), nil)
		return
	}
	item := db.ClipboardItem{
		Id:        uuid.NewString(),
		GroupId:   dto.GroupId,
		DevId:     dto.DevId,
		Type:      "text",
		Content:   dto.Content,
		CreatedAt: time.Now(),
	}
	if err := db.AddClipboardItem(item); err != nil {
		errorResult(c, http.StatusInternalServerError, err.Error(), nil)
		return
	}
	successResult(c, gin.H{"id": item.Id})
}

// pushClipImage 推送加密图片（multipart/form-data）
// 表单字段：groupId, devId, fileId(可选), data(文件)
func pushClipImage(c *gin.Context) {
	groupId := c.PostForm("groupId")
	devId := c.PostForm("devId")
	if groupId == "" || devId == "" {
		errorResult(c, http.StatusBadRequest, "groupId and devId required", nil)
		return
	}

	file, err := c.FormFile("data")
	if err != nil {
		errorResult(c, http.StatusBadRequest, "missing file data", nil)
		return
	}

	fileId := uuid.NewString()
	src, err := file.Open()
	if err != nil {
		errorResult(c, http.StatusInternalServerError, err.Error(), nil)
		return
	}
	defer src.Close()

	buf := make([]byte, file.Size)
	if _, err = src.Read(buf); err != nil {
		errorResult(c, http.StatusInternalServerError, err.Error(), nil)
		return
	}
	if err = storage.SaveImage(fileId, buf); err != nil {
		errorResult(c, http.StatusInternalServerError, err.Error(), nil)
		return
	}

	expireAt := time.Now().AddDate(0, 0, imageTTLDays)
	item := db.ClipboardItem{
		Id:        uuid.NewString(),
		GroupId:   groupId,
		DevId:     devId,
		Type:      "image",
		FileId:    fileId,
		CreatedAt: time.Now(),
		ExpireAt:  &expireAt,
	}
	if err = db.AddClipboardItem(item); err != nil {
		// 回滚文件
		_ = storage.DeleteImage(fileId)
		errorResult(c, http.StatusInternalServerError, err.Error(), nil)
		return
	}
	successResult(c, gin.H{"id": item.Id, "fileId": fileId, "expireAt": expireAt.Unix()})
}

// pullClipItems 拉取指定 groupId 在 since 时间戳之后的所有条目
// Query: groupId, since (unix 秒，0 表示拉取全部)
func pullClipItems(c *gin.Context) {
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
	items, err := db.GetClipboardItemsSince(groupId, since)
	if err != nil {
		errorResult(c, http.StatusInternalServerError, err.Error(), nil)
		return
	}
	successResult(c, items)
}

// deleteClipItems 删除指定条目（需同 groupId 匹配，防止越权）
func deleteClipItems(c *gin.Context) {
	var dto ClipDeleteDto
	if err := c.ShouldBindJSON(&dto); err != nil {
		errorResult(c, http.StatusBadRequest, err.Error(), nil)
		return
	}
	// 先查图片条目，删文件
	items, err := db.GetClipboardItemsSince(dto.GroupId, time.Time{})
	if err != nil {
		errorResult(c, http.StatusInternalServerError, err.Error(), nil)
		return
	}
	idSet := make(map[string]bool, len(dto.Ids))
	for _, id := range dto.Ids {
		idSet[id] = true
	}
	for _, item := range items {
		if idSet[item.Id] && item.Type == "image" && item.FileId != "" {
			_ = storage.DeleteImage(item.FileId)
		}
	}
	if err = db.DeleteClipboardItems(dto.GroupId, dto.Ids); err != nil {
		errorResult(c, http.StatusInternalServerError, err.Error(), nil)
		return
	}
	successResult(c, true)
}

// getClipImage 下载加密图片文件
// Query: groupId, fileId
func getClipImage(c *gin.Context) {
	groupId := c.Query("groupId")
	fileId := c.Query("fileId")
	if groupId == "" || fileId == "" {
		errorResult(c, http.StatusBadRequest, "groupId and fileId required", nil)
		return
	}
	// 验证该图片确实属于此 groupId
	item, err := db.GetClipboardImageItem(groupId, fileId)
	if err != nil || item == nil {
		errorResult(c, http.StatusNotFound, "not found", nil)
		return
	}
	data, err := storage.ReadImage(fileId)
	if err != nil {
		errorResult(c, http.StatusNotFound, "file not found", nil)
		return
	}
	c.Data(http.StatusOK, "application/octet-stream", data)
}

// StartImageCleanup 启动定时任务，每天清理过期图片
func StartImageCleanup() {
	go func() {
		for {
			time.Sleep(24 * time.Hour)
			cleanExpiredImages()
		}
	}()
	// 启动时也执行一次
	go cleanExpiredImages()
}

func cleanExpiredImages() {
	defer func() {
		if r := recover(); r != nil {
			utils.LogUtil.Error("cleanExpiredImages", r)
		}
	}()
	// 先拿到要删除的图片条目
	items, err := db.GetExpiredImageItems()
	if err != nil {
		utils.LogUtil.Error("cleanExpiredImages query", err)
		return
	}
	for _, item := range items {
		if item.FileId != "" {
			_ = storage.DeleteImage(item.FileId)
		}
	}
	cnt, err := db.DeleteExpiredImages()
	if err != nil {
		utils.LogUtil.Error("cleanExpiredImages delete", err)
		return
	}
	if cnt > 0 {
		utils.LogUtil.Info("cleanExpiredImages: deleted", cnt, "expired image items")
	}
}

// uploadSyncImage 上传图片文件用于同步（只存文件，返回 fileId，不创建剪贴板条目）
// 由 /api/sync/push 的 addItem 操作引用此 fileId
func uploadSyncImage(c *gin.Context) {
	groupId := c.PostForm("groupId")
	if groupId == "" {
		errorResult(c, http.StatusBadRequest, "groupId required", nil)
		return
	}
	file, err := c.FormFile("data")
	if err != nil {
		errorResult(c, http.StatusBadRequest, "missing file data", nil)
		return
	}
	src, err := file.Open()
	if err != nil {
		errorResult(c, http.StatusInternalServerError, err.Error(), nil)
		return
	}
	defer src.Close()
	buf := make([]byte, file.Size)
	if _, err = src.Read(buf); err != nil {
		errorResult(c, http.StatusInternalServerError, err.Error(), nil)
		return
	}
	fileId := uuid.NewString()
	if err = storage.SaveImage(fileId, buf); err != nil {
		errorResult(c, http.StatusInternalServerError, err.Error(), nil)
		return
	}
	successResult(c, gin.H{"fileId": fileId})
}

// getSyncImage 下载同步图片文件
// Query: groupId, fileId
func getSyncImage(c *gin.Context) {
	groupId := c.Query("groupId")
	fileId := c.Query("fileId")
	if groupId == "" || fileId == "" {
		errorResult(c, http.StatusBadRequest, "groupId and fileId required", nil)
		return
	}
	data, err := storage.ReadImage(fileId)
	if err != nil {
		errorResult(c, http.StatusNotFound, "file not found", nil)
		return
	}
	c.Data(http.StatusOK, "application/octet-stream", data)
}
