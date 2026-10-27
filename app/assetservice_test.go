package main

import (
	"encoding/base64"
	"os"
	"path/filepath"
	"testing"
)

func TestSaveAssetAndNoteDrop(t *testing.T) {
	ws := newWorkspaceService()
	openTempWorkspace(t, ws)
	var events []string
	ws.emitHook = func(name string, _ any) { events = append(events, name) }

	png := []byte("\x89PNG\r\n\x1a\nfake")
	rel, err := ws.SaveAsset("截图.png", base64.StdEncoding.EncodeToString(png))
	if err != nil || filepath.Dir(rel) != "../assets" {
		t.Fatalf("SaveAsset = %q, %v", rel, err)
	}
	again, _ := ws.SaveAsset("另一个名字.png", base64.StdEncoding.EncodeToString(png))
	if again != rel {
		t.Fatalf("同内容应复用已有文件: %q vs %q", again, rel)
	}
	if _, err := ws.SaveAsset("x.png", "不是base64"); err == nil {
		t.Fatal("非法数据应报错")
	}

	dir := t.TempDir()
	img, doc := filepath.Join(dir, "a.jpg"), filepath.Join(dir, "说明.pdf")
	_ = os.WriteFile(img, []byte("jpg"), 0o644)
	_ = os.WriteFile(doc, []byte("pdf"), 0o644)
	got, err := ws.HandleNoteDroppedFiles([]string{img, doc})
	if err != nil || len(got) != 2 || !got[0].Image || got[1].Image || got[1].Name != "说明.pdf" {
		t.Fatalf("HandleNoteDroppedFiles = %+v, %v", got, err)
	}
	if len(events) != 1 || events[0] != noteAssetsDroppedEvent {
		t.Fatalf("应发 note:assets-dropped: %v", events)
	}
}
