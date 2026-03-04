package storage

import (
	"fmt"
	"os"
	"path/filepath"
)

const imageDir = "./data/images"

func init() {
	_ = os.MkdirAll(imageDir, os.ModePerm)
}

// SaveImage 保存加密图片到文件系统，返回存储路径
func SaveImage(fileId string, data []byte) error {
	path := imagePath(fileId)
	return os.WriteFile(path, data, 0644)
}

// ReadImage 读取加密图片
func ReadImage(fileId string) ([]byte, error) {
	return os.ReadFile(imagePath(fileId))
}

// DeleteImage 删除图片文件
func DeleteImage(fileId string) error {
	path := imagePath(fileId)
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return nil
	}
	return os.Remove(path)
}

func imagePath(fileId string) string {
	return filepath.Join(imageDir, fmt.Sprintf("%s.bin", fileId))
}
