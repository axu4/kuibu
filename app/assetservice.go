package main

import (
	"encoding/base64"
	"fmt"
	"mime"
	"path/filepath"
	"strings"
)

// noteAssetsDroppedEvent 是文件拖进笔记编辑器、存进 assets/ 后发给前端的事件名。
const noteAssetsDroppedEvent = "note:assets-dropped"

// DroppedAsset 是拖进笔记的一个文件。
type DroppedAsset struct {
	// Name 是原文件名。
	Name string `json:"name"`
	// Rel 是正文里用的相对路径（../assets/xxx）。
	Rel string `json:"rel"`
	// Image 表示是否是图片（插入 ![] 还是 []）。
	Image bool `json:"image"`
}

// SaveAsset 把编辑器粘贴 / 上传的文件存进 assets/（内容相同复用已有文件），返回相对路径。
// data 是 base64 编码的文件内容。
func (s *WorkspaceService) SaveAsset(name, data string) (string, error) {
	ws := s.workspace()
	if ws == nil {
		return "", errNotOpen()
	}
	raw, err := base64.StdEncoding.DecodeString(data)
	if err != nil {
		return "", fmt.Errorf("图片数据不合法: %w", err)
	}
	if len(raw) == 0 {
		return "", fmt.Errorf("文件为空")
	}
	if strings.TrimSpace(name) == "" || filepath.Ext(name) == "" {
		name = "image.png"
	}
	return ws.AddAssetBytes(name, raw)
}

// HandleNoteDroppedFiles 处理拖进笔记编辑器的文件：存进 assets/ 并通知前端在光标处插入。
// 拖放事件由 main.go 的 WindowFilesDropped 监听器按 data-note-drop 属性转发到这里。
func (s *WorkspaceService) HandleNoteDroppedFiles(files []string) ([]DroppedAsset, error) {
	ws := s.workspace()
	if ws == nil {
		return nil, errNotOpen()
	}
	out := make([]DroppedAsset, 0, len(files))
	for _, f := range files {
		rel, err := ws.AddAsset(f)
		if err != nil {
			return out, err
		}
		out = append(out, DroppedAsset{Name: filepath.Base(f), Rel: rel, Image: isImageFile(f)})
	}
	if len(out) > 0 {
		s.emit(noteAssetsDroppedEvent, out)
	}
	return out, nil
}

// isImageFile 按扩展名判断是否是图片。
func isImageFile(path string) bool {
	return strings.HasPrefix(mime.TypeByExtension(strings.ToLower(filepath.Ext(path))), "image/")
}
