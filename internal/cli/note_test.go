package cli

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestNoteFolder(t *testing.T) {
	e := newTestEnv(t)
	e.initWorkspace()

	// --folder 建目录：纯文本模式输出 ID，JSON 输出 type=folder。
	dirID := idOf(t, e.mustRun("", "note", "new", "--workspace", e.workspace, "--title", "资料", "--folder"))
	dir := e.mustRun("", "note", "show", dirID, "--json", "--workspace", e.workspace).jsonOut(t)
	if dir["type"] != "folder" {
		t.Errorf("--folder 建的节点 type = %v，期望 folder", dir["type"])
	}

	// -d 简写与 --type folder 等价（-d 与 --folder 都走同一分支）。
	shortID := idOf(t, e.mustRun("", "note", "new", "--workspace", e.workspace, "--title", "短选项目录", "-d"))
	if typ := e.mustRun("", "note", "show", shortID, "--json", "--workspace", e.workspace).jsonOut(t)["type"]; typ != "folder" {
		t.Errorf("-d 建的节点 type = %v，期望 folder", typ)
	}
	typedID := idOf(t, e.mustRun("", "note", "new", "--workspace", e.workspace, "--title", "类型目录", "--type", "folder"))
	if typ := e.mustRun("", "note", "show", typedID, "--json", "--workspace", e.workspace).jsonOut(t)["type"]; typ != "folder" {
		t.Errorf("--type folder 建的节点 type = %v，期望 folder", typ)
	}

	// --folder 与 --type folder 同时给不冲突。
	e.mustRun("", "note", "new", "--workspace", e.workspace, "--title", "双写目录", "--folder", "--type", "folder")

	// --folder 与非 folder 的 --type 冲突，报 USAGE。
	conflict := e.run("", "note", "new", "--workspace", e.workspace, "--title", "冲突", "--folder", "--type", "note", "--json")
	if conflict.code != 1 || conflict.jsonErrorCode(t) != "USAGE" {
		t.Errorf("--folder 与 --type note 冲突结果 = %+v，期望 USAGE/1", conflict)
	}

	// 建目录时忽略正文参数（也不读 stdin），目录不能有正文。
	dirNoBody := e.mustRun("不该保存的正文", "note", "new", "--workspace", e.workspace, "--folder", "--stdin", "--json").jsonOut(t)
	if body, ok := dirNoBody["body"].(string); !ok || body != "" {
		t.Errorf("目录 body = %v，期望空字符串", dirNoBody["body"])
	}
	byBody := e.mustRun("", "note", "new", "--workspace", e.workspace, "--title", "带正文目录", "--type", "folder", "--body", "正文也该被忽略", "--json").jsonOut(t)
	if body, _ := byBody["body"].(string); body != "" {
		t.Errorf("--type folder 带 --body 时 body = %q，期望空", body)
	}

	// 目录可以不给标题，store 补默认目录名。
	anonID := idOf(t, e.mustRun("", "note", "new", "--workspace", e.workspace, "--folder"))
	if title := e.mustRun("", "note", "show", anonID, "--json", "--workspace", e.workspace).jsonOut(t)["title"]; title != "未命名目录" {
		t.Errorf("无标题目录 title = %v，期望「未命名目录」", title)
	}

	// 普通笔记仍然要求标题。
	if res := e.run("", "note", "new", "--workspace", e.workspace, "--json"); res.code != 1 || res.jsonErrorCode(t) != "USAGE" {
		t.Errorf("缺 --title 结果 = %+v，期望 USAGE/1", res)
	}

	// 目录下建笔记，再来一层子目录。
	noteID := idOf(t, e.mustRun("", "note", "new", "--workspace", e.workspace, "--title", "目录里的笔记", "--parent", dirID, "--body", "正文"))
	subDirID := idOf(t, e.mustRun("", "note", "new", "--workspace", e.workspace, "--title", "二级目录", "--folder", "--parent", dirID))
	nestedID := idOf(t, e.mustRun("", "note", "new", "--workspace", e.workspace, "--title", "二级目录里的笔记", "--parent", subDirID))
	if parent := e.mustRun("", "note", "show", nestedID, "--json", "--workspace", e.workspace).jsonOut(t)["parent"]; parent != subDirID {
		t.Errorf("二级目录里的笔记 parent = %v，期望 %s", parent, subDirID)
	}

	// 往普通笔记下建笔记（含子目录）被拒绝：INVALID/1。
	for _, extra := range [][]string{nil, {"--type", "folder"}} {
		args := []string{"note", "new", "--workspace", e.workspace, "--title", "笔记里的笔记", "--parent", noteID, "--json"}
		args = append(args, extra...)
		res := e.run("", args...)
		if res.code != 1 || res.jsonErrorCode(t) != "INVALID" {
			t.Errorf("往笔记下建节点（%v）结果 = %+v，期望 INVALID/1", extra, res)
		}
		if !strings.Contains(res.stderr, "只有目录可以包含笔记") {
			t.Errorf("错误信息缺少说明: %s", res.stderr)
		}
	}
	// 父节点不存在：NOT_FOUND/1。
	missing := e.run("", "note", "new", "--workspace", e.workspace, "--title", "孤儿", "--parent", "no-such-id", "--json")
	if missing.code != 1 || missing.jsonErrorCode(t) != "NOT_FOUND" {
		t.Errorf("父节点不存在结果 = %+v，期望 NOT_FOUND/1", missing)
	}

	// list 人类模式有类型列，目录显示「目录」。
	list := e.mustRun("", "note", "list", "--workspace", e.workspace)
	if !strings.Contains(list.stdout, "类型") {
		t.Errorf("list 缺少类型列:\n%s", list.stdout)
	}
	rows := map[string]string{}
	for _, line := range strings.Split(list.stdout, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		rows[fields[0]] = fields[1]
	}
	if rows[dirID] != "目录" || rows[subDirID] != "目录" {
		t.Errorf("list 中目录行的类型列不正确: 目录=%q 二级目录=%q", rows[dirID], rows[subDirID])
	}
	if rows[noteID] != "note" {
		t.Errorf("list 中普通笔记行的类型列 = %q，期望 note", rows[noteID])
	}

	// list JSON 的 type 字段正确。
	listJSON := e.mustRun("", "note", "list", "--workspace", e.workspace, "--json").jsonArray(t)
	types := map[string]any{}
	for _, raw := range listJSON {
		item, _ := raw.(map[string]any)
		id, _ := item["id"].(string)
		types[id] = item["type"]
	}
	if types[dirID] != "folder" || types[noteID] != "note" || types[subDirID] != "folder" {
		t.Errorf("list JSON 类型不正确: 目录=%v 笔记=%v 二级目录=%v", types[dirID], types[noteID], types[subDirID])
	}

	// tree 人类模式目录带 [目录] 标记，普通笔记不带。
	tree := e.mustRun("", "tree", "--workspace", e.workspace)
	if !strings.Contains(tree.stdout, "- 资料  ("+dirID+") [目录]") {
		t.Errorf("tree 未标出目录:\n%s", tree.stdout)
	}
	if strings.Contains(tree.stdout, "- 目录里的笔记  ("+noteID+") [目录]") {
		t.Errorf("tree 把普通笔记标成了目录:\n%s", tree.stdout)
	}

	// tree JSON 带 type 字段（目录与叶子分别正确）。
	treeJSON := e.mustRun("", "tree", "--json", "--workspace", e.workspace).jsonArray(t)
	found := map[string]any{}
	var walk func(nodes []any)
	walk = func(nodes []any) {
		for _, raw := range nodes {
			node, _ := raw.(map[string]any)
			id, _ := node["id"].(string)
			found[id] = node["type"]
			children, _ := node["children"].([]any)
			walk(children)
		}
	}
	walk(treeJSON)
	if found[dirID] != "folder" || found[noteID] != "note" || found[subDirID] != "folder" || found[nestedID] != "note" {
		t.Errorf("tree JSON 类型不正确: %v", found)
	}

	// 目录移到笔记下被拒绝；目录移到别人的子目录下成环。
	intoNote := e.run("", "note", "mv", subDirID, "--parent", noteID, "--json", "--workspace", e.workspace)
	if intoNote.code != 1 || intoNote.jsonErrorCode(t) != "INVALID" {
		t.Errorf("目录移到笔记下结果 = %+v，期望 INVALID/1", intoNote)
	}
	cycle := e.run("", "note", "mv", dirID, "--parent", subDirID, "--json", "--workspace", e.workspace)
	if cycle.code != 1 || cycle.jsonErrorCode(t) != "CYCLE" {
		t.Errorf("目录移到后代下结果 = %+v，期望 CYCLE/1", cycle)
	}

	// 目录不能带正文：edit --body 被拒绝，但改名照常。
	editBody := e.run("", "note", "edit", dirID, "--body", "给目录加正文", "--json", "--workspace", e.workspace)
	if editBody.code != 1 || editBody.jsonErrorCode(t) != "INVALID" {
		t.Errorf("给目录加正文结果 = %+v，期望 INVALID/1", editBody)
	}
	renamed := e.mustRun("", "note", "edit", dirID, "--title", "资料（改）", "--json", "--workspace", e.workspace).jsonOut(t)
	if renamed["title"] != "资料（改）" || renamed["type"] != "folder" {
		t.Errorf("目录改名结果 = %v", renamed)
	}

	// 目录有子节点时不能直接删除，-r 递归删除。
	rmDenied := e.run("", "note", "rm", dirID, "--json", "--workspace", e.workspace)
	if rmDenied.code != 1 || rmDenied.jsonErrorCode(t) != "HAS_CHILDREN" {
		t.Errorf("删除有子节点的目录结果 = %+v，期望 HAS_CHILDREN/1", rmDenied)
	}
	e.mustRun("", "note", "rm", dirID, "-r", "--workspace", e.workspace)
	for _, id := range []string{dirID, noteID, subDirID, nestedID} {
		if res := e.run("", "note", "show", id, "--json", "--workspace", e.workspace); res.code != 1 || res.jsonErrorCode(t) != "NOT_FOUND" {
			t.Errorf("递归删除后 show %s 结果 = %+v，期望 NOT_FOUND/1", id, res)
		}
	}
}

// writeLegacyNote 直接写一条老格式笔记文件（模拟引入目录模型前的工作台）。
func writeLegacyNote(t *testing.T, wsDir, id, title, typ, parent string, order int) {
	t.Helper()
	front := "id: " + id + "\ntitle: \"" + title + "\"\n"
	if typ != "" {
		front += "type: " + typ + "\n"
	}
	if parent != "" {
		front += "parent: " + parent + "\n"
	}
	front += "order: " + strconv.Itoa(order) + "\n"
	// 正文带一级标题：空标题的笔记会由标题行推导出标题（store 读取老文件的规则）。
	content := "---\n" + front + "---\n\n# 老正文\n\n正文\n"
	path := filepath.Join(wsDir, "notes", id+".md")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestNoteMigrateFolders(t *testing.T) {
	e := newTestEnv(t)
	e.initWorkspace()

	// 老工作台：普通笔记当父节点，下面挂着子笔记；另有一个空标题的普通笔记当容器。
	writeLegacyNote(t, e.workspace, "20240101-000000-aaaa", "老容器", "", "", 0)
	writeLegacyNote(t, e.workspace, "20240101-000000-bbbb", "子笔记一", "note", "20240101-000000-aaaa", 0)
	writeLegacyNote(t, e.workspace, "20240101-000000-cccc", "子笔记二", "note", "20240101-000000-aaaa", 1)
	writeLegacyNote(t, e.workspace, "20240101-000000-dddd", "", "note", "", 1)
	writeLegacyNote(t, e.workspace, "20240101-000000-eeee", "叶子", "meeting", "20240101-000000-dddd", 0)

	// 迁移：两条有子节点的笔记转为目录，叶子不动。
	migrated := e.mustRun("", "note", "migrate-folders", "--json", "--workspace", e.workspace).jsonOut(t)
	if converted, _ := migrated["converted"].(float64); int(converted) != 2 {
		t.Errorf("migrate-folders converted = %v，期望 2", migrated["converted"])
	}

	for id, want := range map[string]string{
		"20240101-000000-aaaa": "folder",
		"20240101-000000-dddd": "folder",
		"20240101-000000-bbbb": "note",
		"20240101-000000-cccc": "note",
		"20240101-000000-eeee": "meeting",
	} {
		got := e.mustRun("", "note", "show", id, "--json", "--workspace", e.workspace).jsonOut(t)
		if got["type"] != want {
			t.Errorf("%s 迁移后 type = %v，期望 %s", id, got["type"], want)
		}
	}

	// 迁移只改 type：标题、正文、子节点都不变（目录的正文保留，不删用户数据）。
	container := e.mustRun("", "note", "show", "20240101-000000-dddd", "--json", "--workspace", e.workspace).jsonOut(t)
	if container["title"] != "老正文" {
		t.Errorf("容器迁移后 title = %v，期望「老正文」", container["title"])
	}
	leaf := e.mustRun("", "note", "show", "20240101-000000-eeee", "--json", "--workspace", e.workspace).jsonOut(t)
	if body, _ := leaf["body"].(string); !strings.Contains(body, "正文") {
		t.Errorf("叶子正文被改动: %q", leaf["body"])
	}
	if children := e.mustRun("", "note", "list", "--parent", "20240101-000000-aaaa", "--json", "--workspace", e.workspace).jsonArray(t); len(children) != 2 {
		t.Errorf("迁移后目录子节点数 = %d，期望 2", len(children))
	}

	// 幂等：再跑一次转为 0 条；人类模式输出中文提示。
	again := e.mustRun("", "note", "migrate-folders", "--json", "--workspace", e.workspace).jsonOut(t)
	if converted, _ := again["converted"].(float64); int(converted) != 0 {
		t.Errorf("重复 migrate-folders converted = %v，期望 0", again["converted"])
	}
	human := e.mustRun("", "note", "migrate-folders", "--workspace", e.workspace)
	if strings.TrimSpace(human.stdout) != "已把 0 条笔记转为目录" {
		t.Errorf("migrate-folders 人类模式输出 = %q", human.stdout)
	}

	// 迁移后可以继续在自动转成的目录下建笔记。
	e.mustRun("", "note", "new", "--workspace", e.workspace, "--title", "迁移后新建", "--parent", "20240101-000000-aaaa")
	if got := len(e.mustRun("", "note", "list", "--parent", "20240101-000000-aaaa", "--json", "--workspace", e.workspace).jsonArray(t)); got != 3 {
		t.Errorf("迁移后目录子节点数 = %d，期望 3", got)
	}

	// 不接受位置参数；note 子命令列表与帮助里出现 migrate-folders。
	if res := e.run("", "note", "migrate-folders", "多余", "--json", "--workspace", e.workspace); res.code != 1 || res.jsonErrorCode(t) != "USAGE" {
		t.Errorf("migrate-folders 带位置参数结果 = %+v，期望 USAGE/1", res)
	}
	// note 后面紧跟的第一个参数一律当子命令（既有约定），所以子命令清单只能从帮助文本核对。
	if unknown := e.run("", "note", "--json", "--workspace", e.workspace); unknown.code != 1 || !strings.Contains(unknown.stderr, "未知 note 子命令") {
		t.Errorf("note 未知子命令结果 = %+v，期望 1 并提示未知子命令", unknown)
	}
	help := e.mustRun("", "help")
	if !strings.Contains(help.stdout, "note migrate-folders") || !strings.Contains(help.stdout, "--folder") {
		t.Errorf("help 缺少 --folder / migrate-folders:\n%s", help.stdout)
	}
}
