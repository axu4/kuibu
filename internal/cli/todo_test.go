package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// setNow 固定待办测试的「现在」。
func (e *testEnv) setNow() {
	e.t.Helper()
	e.extraEnv["KUIBU_NOW"] = "2026-10-26T10:00:00+08:00"
}

// addTodo 执行 todo add --json 并返回解析后的 JSON 对象。
func (e *testEnv) addTodo(args ...string) map[string]any {
	e.t.Helper()
	full := append([]string{"todo", "add", "--workspace", e.workspace, "--json"}, args...)
	return e.mustRun("", full...).jsonOut(e.t)
}

// todoOut 从 todo add 的 JSON 输出里取 todo 子对象。
func todoOut(t *testing.T, out map[string]any) map[string]any {
	t.Helper()
	todo, _ := out["todo"].(map[string]any)
	if todo == nil {
		t.Fatalf("输出缺少 todo 对象: %v", out)
	}
	return todo
}

// todoID 从 todo add 的 JSON 输出里取待办 ID。
func todoID(t *testing.T, out map[string]any) string {
	t.Helper()
	id, _ := todoOut(t, out)["id"].(string)
	if id == "" {
		t.Fatalf("todo id 为空: %v", out)
	}
	return id
}

// parsedOut 从 todo add 的 JSON 输出里取 parsed 子对象。
func parsedOut(t *testing.T, out map[string]any) map[string]any {
	t.Helper()
	parsed, _ := out["parsed"].(map[string]any)
	if parsed == nil {
		t.Fatalf("输出缺少 parsed 对象: %v", out)
	}
	return parsed
}

// todoListJSON 执行 todo list --json，返回数组。
func (e *testEnv) todoListJSON(args ...string) []any {
	e.t.Helper()
	full := append([]string{"todo", "list", "--workspace", e.workspace, "--json"}, args...)
	return e.mustRun("", full...).jsonArray(e.t)
}

func TestTodoAddParseJSON(t *testing.T) {
	e := newTestEnv(t)
	e.initWorkspace()
	e.setNow()

	out := e.addTodo("明天", "15:00", "交周报", "#工作")
	todo := todoOut(t, out)
	id := todoID(t, out)
	if id == "" {
		t.Fatalf("id 为空")
	}
	if todo["title"] != "交周报" {
		t.Errorf("title = %v", todo["title"])
	}
	if todo["due"] != "2026-10-27 15:00" {
		t.Errorf("due = %v", todo["due"])
	}
	tags, _ := todo["tags"].([]any)
	if len(tags) != 1 || tags[0] != "工作" {
		t.Errorf("tags = %v", todo["tags"])
	}

	parsed := parsedOut(t, out)
	if parsed["title"] != "交周报" || parsed["due"] != "2026-10-27 15:00" {
		t.Errorf("parsed = %v", parsed)
	}
	matches, _ := parsed["matches"].([]any)
	if len(matches) != 3 {
		t.Fatalf("matches = %v，期望 3 个", parsed["matches"])
	}
	if _, err := os.Stat(filepath.Join(e.workspace, "todos", id+".md")); err != nil {
		t.Errorf("待办文件未落盘: %v", err)
	}
}

func TestTodoAddRaw(t *testing.T) {
	e := newTestEnv(t)
	e.initWorkspace()
	e.setNow()

	out := e.addTodo("--raw", "明天 15:00 交周报 #工作")
	todo := todoOut(t, out)
	if todo["title"] != "明天 15:00 交周报 #工作" {
		t.Errorf("title = %v", todo["title"])
	}
	if todo["due"] != nil {
		t.Errorf("due = %v，期望 null", todo["due"])
	}
}

func TestTodoAddDryRun(t *testing.T) {
	e := newTestEnv(t)
	e.initWorkspace()
	e.setNow()

	out := e.addTodo("--dry-run", "明天交周报")
	if _, ok := out["todo"]; !ok || out["todo"] != nil {
		t.Errorf("dry-run todo = %v，期望 null", out["todo"])
	}
	parsed := parsedOut(t, out)
	if parsed["due"] != "2026-10-27" {
		t.Errorf("parsed due = %v", parsed["due"])
	}
	entries, err := os.ReadDir(filepath.Join(e.workspace, "todos"))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Errorf("dry-run 不应创建待办: %v", entries)
	}
}

func TestTodoAddDueOverride(t *testing.T) {
	e := newTestEnv(t)
	e.initWorkspace()
	e.setNow()

	out := e.addTodo("明天交周报", "--due", "2026-11-01")
	if todoOut(t, out)["due"] != "2026-11-01" {
		t.Errorf("--due 未覆盖: %v", todoOut(t, out)["due"])
	}

	// --due 也接受快速录入表达式。
	out = e.addTodo("交周报", "--due", "周四 15:00")
	if todoOut(t, out)["due"] != "2026-10-29 15:00" {
		t.Errorf("--due 快速录入失败: %v", todoOut(t, out)["due"])
	}

	// --due "" 等价于不设（覆盖文本里的日期）。
	out = e.addTodo("明天交周报", "--due", "")
	if todoOut(t, out)["due"] != nil {
		t.Errorf("--due \"\" 应清除截止: %v", todoOut(t, out)["due"])
	}
}

func TestTodoListDefaultAndAll(t *testing.T) {
	e := newTestEnv(t)
	e.initWorkspace()
	e.setNow()

	e.addTodo("待办 A")
	e.addTodo("进行中 B", "--due", "2026-10-28")
	e.mustRun("", "todo", "start", todoID(t, e.addTodo("待办 C")), "--workspace", e.workspace)
	e.mustRun("", "todo", "done", todoID(t, e.addTodo("已完成 D")), "--workspace", e.workspace)
	e.mustRun("", "todo", "cancel", todoID(t, e.addTodo("已取消 E")), "--workspace", e.workspace)

	// 默认只列出未完成。
	ids := []string{}
	for _, raw := range e.todoListJSON() {
		m, _ := raw.(map[string]any)
		ids = append(ids, m["id"].(string))
	}
	if len(ids) != 3 {
		t.Fatalf("默认 list 条数 = %d，期望 3: %v", len(ids), ids)
	}

	// --status all 全部列出。
	if got := len(e.todoListJSON("--status", "all")); got != 5 {
		t.Errorf("--status all 条数 = %d，期望 5", got)
	}

	// 非法状态报 USAGE。
	res := e.run("", "todo", "list", "--status", "weird", "--json", "--workspace", e.workspace)
	if res.code != 1 || res.jsonErrorCode(t) != "USAGE" {
		t.Errorf("非法状态结果 = %+v", res)
	}
}

func TestTodoListDueFilters(t *testing.T) {
	e := newTestEnv(t)
	e.initWorkspace()
	e.setNow()

	e.addTodo("今天买咖啡")
	e.addTodo("明天交周报")
	e.addTodo("本周总结", "--due", "2026-11-01")
	e.addTodo("下周计划", "--due", "2026-11-02")
	e.addTodo("已逾期", "--due", "2026-10-25")
	e.addTodo("无截止")

	today := e.todoListJSON("--due", "today")
	if len(today) != 1 {
		t.Errorf("--due today 条数 = %d，期望 1: %v", len(today), today)
	}

	overdue := e.todoListJSON("--due", "overdue")
	if len(overdue) != 1 {
		t.Errorf("--due overdue 条数 = %d，期望 1: %v", len(overdue), overdue)
	}

	week := e.todoListJSON("--due", "week")
	if len(week) != 3 {
		t.Errorf("--due week 条数 = %d，期望 3（周一到周日）: %v", len(week), week)
	}
}

func TestTodoListTagRootSearch(t *testing.T) {
	e := newTestEnv(t)
	e.initWorkspace()
	e.setNow()

	e.addTodo("写周报", "--tag", "工作")
	e.addTodo("买菜", "--tag", "生活")
	e.addTodo("交周报材料")

	byTag := e.todoListJSON("--tag", "工作")
	if len(byTag) != 1 {
		t.Errorf("--tag 工作 条数 = %d，期望 1", len(byTag))
	}

	// 顶层过滤：先建一个子任务，再确认 --root 不含子任务。
	parent := todoID(t, e.addTodo("父任务"))
	child := todoID(t, e.addTodo("子任务", "--parent", parent))
	roots := e.todoListJSON("--root")
	if len(roots) != 4 {
		t.Fatalf("--root 条数 = %d，期望 4: %v", len(roots), roots)
	}
	for _, raw := range roots {
		m, _ := raw.(map[string]any)
		if m["id"] == child {
			t.Errorf("--root 不应包含子任务: %v", m)
		}
	}

	// 中文检索。
	search := e.todoListJSON("--search", "周报")
	if len(search) == 0 {
		t.Errorf("--search 周报 无结果")
	}
}

func TestTodoListTree(t *testing.T) {
	e := newTestEnv(t)
	e.initWorkspace()
	e.setNow()

	parent := todoID(t, e.addTodo("父任务"))
	child := todoID(t, e.addTodo("子任务", "--parent", parent, "--tag", "工作"))

	// 只有子任务命中标签，父任务应作为（上级）显示。
	human := e.mustRun("", "todo", "list", "--tag", "工作", "--tree", "--workspace", e.workspace)
	if !strings.Contains(human.stdout, "父任务") || !strings.Contains(human.stdout, "（上级）") {
		t.Errorf("tree 未显示上级父任务:\n%s", human.stdout)
	}
	if !strings.Contains(human.stdout, "子任务") {
		t.Errorf("tree 未显示子任务:\n%s", human.stdout)
	}

	// JSON 嵌套 children。
	tree := e.mustRun("", "todo", "list", "--tag", "工作", "--tree", "--json", "--workspace", e.workspace).jsonArray(t)
	if len(tree) != 1 {
		t.Fatalf("tree JSON 根数 = %d，期望 1", len(tree))
	}
	root, _ := tree[0].(map[string]any)
	if root["id"] != parent {
		t.Errorf("tree 根 id = %v，期望 %s", root["id"], parent)
	}
	children, _ := root["children"].([]any)
	if len(children) != 1 {
		t.Fatalf("children 数 = %d，期望 1", len(children))
	}
	childNode, _ := children[0].(map[string]any)
	if childNode["id"] != child {
		t.Errorf("child id = %v，期望 %s", childNode["id"], child)
	}
}

func TestTodoShow(t *testing.T) {
	e := newTestEnv(t)
	e.initWorkspace()
	e.setNow()

	parent := todoID(t, e.addTodo("父任务", "--body", "父任务详情"))
	child := todoID(t, e.addTodo("子任务", "--parent", parent))

	show := e.mustRun("", "todo", "show", parent, "--json", "--workspace", e.workspace).jsonOut(t)
	if show["title"] != "父任务" {
		t.Errorf("title = %v", show["title"])
	}
	if body, _ := show["body"].(string); body != "父任务详情" {
		t.Errorf("body = %v", show["body"])
	}
	children, _ := show["children"].([]any)
	if len(children) != 1 {
		t.Fatalf("children = %v", show["children"])
	}
	childNode, _ := children[0].(map[string]any)
	if childNode["id"] != child {
		t.Errorf("child id = %v", childNode["id"])
	}

	human := e.mustRun("", "todo", "show", parent, "--workspace", e.workspace)
	for _, want := range []string{"父任务", "状态:", "详情:", "父任务详情", "子任务:"} {
		if !strings.Contains(human.stdout, want) {
			t.Errorf("show 输出缺少 %q:\n%s", want, human.stdout)
		}
	}
}

func TestTodoDoneReopenStartCancel(t *testing.T) {
	e := newTestEnv(t)
	e.initWorkspace()
	e.setNow()

	id := todoID(t, e.addTodo("待办"))
	e.mustRun("", "todo", "start", id, "--workspace", e.workspace)
	doing := e.mustRun("", "todo", "show", id, "--json", "--workspace", e.workspace).jsonOut(t)
	if doing["status"] != "doing" {
		t.Errorf("start 后 status = %v", doing["status"])
	}

	e.mustRun("", "todo", "done", id, "--workspace", e.workspace)
	done := e.mustRun("", "todo", "show", id, "--json", "--workspace", e.workspace).jsonOut(t)
	if done["status"] != "done" {
		t.Errorf("done 后 status = %v", done["status"])
	}
	if done["doneAt"] == nil {
		t.Errorf("done 后 doneAt 应为非空: %v", done["doneAt"])
	}

	// 默认 list 不再出现。
	ids := e.todoListJSON()
	for _, raw := range ids {
		m, _ := raw.(map[string]any)
		if m["id"] == id {
			t.Errorf("done 后默认 list 仍包含 %s", id)
		}
	}

	e.mustRun("", "todo", "reopen", id, "--workspace", e.workspace)
	reopened := e.mustRun("", "todo", "show", id, "--json", "--workspace", e.workspace).jsonOut(t)
	if reopened["status"] != "todo" || reopened["doneAt"] != nil {
		t.Errorf("reopen 后 status=%v doneAt=%v", reopened["status"], reopened["doneAt"])
	}

	e.mustRun("", "todo", "cancel", id, "--workspace", e.workspace)
	canceled := e.mustRun("", "todo", "show", id, "--json", "--workspace", e.workspace).jsonOut(t)
	if canceled["status"] != "canceled" {
		t.Errorf("cancel 后 status = %v", canceled["status"])
	}
}

func TestTodoSub(t *testing.T) {
	e := newTestEnv(t)
	e.initWorkspace()
	e.setNow()

	parent := todoID(t, e.addTodo("父任务"))
	out := e.mustRun("", "todo", "sub", parent, "明天 15:00 交子方案", "--json", "--workspace", e.workspace).jsonOut(t)
	child := todoID(t, out)
	if child == "" {
		t.Fatal("sub 未返回子任务 id")
	}
	if todoOut(t, out)["parent"] != parent {
		t.Errorf("sub parent = %v", todoOut(t, out)["parent"])
	}
	if todoOut(t, out)["due"] != "2026-10-27 15:00" {
		t.Errorf("sub due = %v", todoOut(t, out)["due"])
	}
}

func TestTodoLinkDedupAndInvalidURL(t *testing.T) {
	e := newTestEnv(t)
	e.initWorkspace()
	e.setNow()

	id := todoID(t, e.addTodo("待办"))
	e.mustRun("", "todo", "link", id, "https://a.com", "--title", "A", "--workspace", e.workspace)
	e.mustRun("", "todo", "link", id, "https://a.com", "--title", "改名", "--workspace", e.workspace)
	show := e.mustRun("", "todo", "show", id, "--json", "--workspace", e.workspace).jsonOut(t)
	links, _ := show["links"].([]any)
	if len(links) != 1 {
		t.Fatalf("links = %v，期望 1 个（去重）", show["links"])
	}
	link, _ := links[0].(map[string]any)
	if link["title"] != "改名" {
		t.Errorf("同 URL 应更新标题: %v", link)
	}

	// 非法 URL 报错。
	res := e.run("", "todo", "link", id, "not-a-url", "--json", "--workspace", e.workspace)
	if res.code != 1 || res.jsonErrorCode(t) != "INVALID" {
		t.Errorf("非法 URL 结果 = %+v", res)
	}

	e.mustRun("", "todo", "unlink", id, "https://a.com", "--workspace", e.workspace)
	after := e.mustRun("", "todo", "show", id, "--json", "--workspace", e.workspace).jsonOut(t)
	if links, _ := after["links"].([]any); len(links) != 0 {
		t.Errorf("unlink 后 links = %v", after["links"])
	}
}

func TestTodoAttachDedup(t *testing.T) {
	e := newTestEnv(t)
	e.initWorkspace()
	e.setNow()

	src := filepath.Join(t.TempDir(), "截图.png")
	if err := os.WriteFile(src, []byte("png-bytes"), 0o644); err != nil {
		t.Fatal(err)
	}

	id := todoID(t, e.addTodo("待办"))
	rel := strings.TrimSpace(e.mustRun("", "todo", "attach", id, src, "--workspace", e.workspace).stdout)
	if !strings.HasPrefix(rel, "../assets/") {
		t.Fatalf("attach 相对路径 = %q", rel)
	}

	// 同一文件再 attach 一次：附件列表只一个，assets 目录只有一个文件。
	rel2 := strings.TrimSpace(e.mustRun("", "todo", "attach", id, src, "--workspace", e.workspace).stdout)
	if rel2 != rel {
		t.Errorf("同一文件返回不同路径: %q vs %q", rel2, rel)
	}
	show := e.mustRun("", "todo", "show", id, "--json", "--workspace", e.workspace).jsonOut(t)
	atts, _ := show["attachments"].([]any)
	if len(atts) != 1 {
		t.Errorf("attachments = %v，期望 1 个", show["attachments"])
	}
	entries, err := os.ReadDir(filepath.Join(e.workspace, "assets"))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Errorf("assets 文件数 = %d，期望 1", len(entries))
	}

	e.mustRun("", "todo", "detach", id, rel, "--workspace", e.workspace)
	after := e.mustRun("", "todo", "show", id, "--json", "--workspace", e.workspace).jsonOut(t)
	if atts, _ := after["attachments"].([]any); len(atts) != 0 {
		t.Errorf("detach 后 attachments = %v", after["attachments"])
	}
}

func TestTodoMvCycleAndRm(t *testing.T) {
	e := newTestEnv(t)
	e.initWorkspace()
	e.setNow()

	root := todoID(t, e.addTodo("父任务"))
	child := todoID(t, e.addTodo("子任务", "--parent", root))

	// 成环报 CYCLE。
	res := e.run("", "todo", "mv", root, "--parent", child, "--json", "--workspace", e.workspace)
	if res.code != 1 || res.jsonErrorCode(t) != "CYCLE" {
		t.Errorf("成环 mv 结果 = %+v", res)
	}

	// 无 -r 删除有子任务 → HAS_CHILDREN。
	res = e.run("", "todo", "rm", root, "--json", "--workspace", e.workspace)
	if res.code != 1 || res.jsonErrorCode(t) != "HAS_CHILDREN" {
		t.Errorf("非递归删除结果 = %+v", res)
	}

	e.mustRun("", "todo", "rm", root, "-r", "--workspace", e.workspace)
	if res := e.run("", "todo", "show", root, "--json", "--workspace", e.workspace); res.code != 1 || res.jsonErrorCode(t) != "NOT_FOUND" {
		t.Errorf("递归删除后 show = %+v", res)
	}
}

func TestTodoNotOpenAndNotFound(t *testing.T) {
	t.Chdir(t.TempDir())
	e := newTestEnv(t)

	res := e.run("", "todo", "list", "--json")
	if res.code != NotOpen {
		t.Errorf("未打开工作台退出码 = %d，期望 %d", res.code, NotOpen)
	}
	if res.jsonErrorCode(t) != "NOT_OPEN" {
		t.Errorf("错误码 = %q，期望 NOT_OPEN", res.jsonErrorCode(t))
	}

	e.initWorkspace()
	e.setNow()
	res = e.run("", "todo", "show", "nope", "--json", "--workspace", e.workspace)
	if res.code != 1 || res.jsonErrorCode(t) != "NOT_FOUND" {
		t.Errorf("找不到待办结果 = %+v", res)
	}
}

func TestTodoExternalChangeAndIndexRebuild(t *testing.T) {
	e := newTestEnv(t)
	e.initWorkspace()
	e.setNow()

	id := todoID(t, e.addTodo("交周报", "--due", "2026-10-27 15:00"))

	// 外部直接改写 status 与 due。
	path := filepath.Join(e.workspace, "todos", id+".md")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	text = strings.Replace(text, `status: "todo"`, `status: "doing"`, 1)
	text = strings.Replace(text, `due: "2026-10-27 15:00"`, `due: "2026-10-28"`, 1)
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}

	shown := e.mustRun("", "todo", "list", "--status", "all", "--json", "--workspace", e.workspace).jsonArray(t)
	found := false
	for _, raw := range shown {
		m, _ := raw.(map[string]any)
		if m["id"] == id {
			found = true
			if m["status"] != "doing" || m["due"] != "2026-10-28" {
				t.Errorf("外部改写后 list 元数据 = %v", m)
			}
		}
	}
	if !found {
		t.Errorf("外部改写后 list 未找到 %s", id)
	}

	// 删除索引数据库后 list 仍正确（重建）。
	if err := os.Remove(filepath.Join(e.workspace, ".kuibu", "index.db")); err != nil {
		t.Fatal(err)
	}
	rebuilt := e.mustRun("", "todo", "list", "--status", "all", "--json", "--workspace", e.workspace).jsonArray(t)
	found = false
	for _, raw := range rebuilt {
		m, _ := raw.(map[string]any)
		if m["id"] == id {
			found = true
			if m["due"] != "2026-10-28" {
				t.Errorf("重建后 due = %v", m["due"])
			}
		}
	}
	if !found {
		t.Errorf("重建后 list 未找到 %s", id)
	}
}

// jsonErrorMessage 解析 stderr 中 JSON 错误对象里的 message。
func jsonErrorMessage(t *testing.T, stderr string) string {
	t.Helper()
	var out struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal([]byte(stderr), &out); err != nil {
		t.Fatalf("stderr 不是合法 JSON: %v\n输出=%q", err, stderr)
	}
	return out.Error.Message
}

func TestTodoListStatusFilter(t *testing.T) {
	e := newTestEnv(t)
	e.initWorkspace()
	e.setNow()

	aID := todoID(t, e.addTodo("待办 A"))
	bID := todoID(t, e.addTodo("进行中 B"))
	e.mustRun("", "todo", "start", bID, "--workspace", e.workspace)
	cID := todoID(t, e.addTodo("已完成 C"))
	e.mustRun("", "todo", "done", cID, "--workspace", e.workspace)
	dID := todoID(t, e.addTodo("已取消 D"))
	e.mustRun("", "todo", "cancel", dID, "--workspace", e.workspace)

	// --status done 只返回 done。
	done := e.todoListJSON("--status", "done")
	if len(done) != 1 {
		t.Fatalf("--status done 条数 = %d，期望 1: %v", len(done), done)
	}
	if m, _ := done[0].(map[string]any); m["id"] != cID {
		t.Errorf("--status done 返回 %v，期望 %s", done, cID)
	}

	// --status todo,doing 与 --status todo --status doing 等价。
	comma := e.todoListJSON("--status", "todo,doing")
	repeat := e.todoListJSON("--status", "todo", "--status", "doing")
	if len(comma) != 2 || len(repeat) != 2 {
		t.Fatalf("todo,doing 条数 comma=%d repeat=%d，期望 2", len(comma), len(repeat))
	}
	var commaIDs, repeatIDs []string
	for _, raw := range comma {
		commaIDs = append(commaIDs, raw.(map[string]any)["id"].(string))
	}
	for _, raw := range repeat {
		repeatIDs = append(repeatIDs, raw.(map[string]any)["id"].(string))
	}
	if strings.Join(commaIDs, ",") != strings.Join(repeatIDs, ",") {
		t.Errorf("--status todo,doing 与 --status todo --status doing 不等价: %v vs %v", commaIDs, repeatIDs)
	}
	for _, id := range commaIDs {
		if id != aID && id != bID {
			t.Errorf("todo,doing 混入其他状态: %v", commaIDs)
		}
	}

	// --status all 不过滤。
	if got := len(e.todoListJSON("--status", "all")); got != 4 {
		t.Errorf("--status all 条数 = %d，期望 4", got)
	}

	// --status bad 退出 1 USAGE。
	res := e.run("", "todo", "list", "--status", "bad", "--json", "--workspace", e.workspace)
	if res.code != 1 || res.jsonErrorCode(t) != "USAGE" {
		t.Errorf("--status bad 结果 = %+v", res)
	}
}

func TestTodoErrorMessages(t *testing.T) {
	e := newTestEnv(t)
	e.initWorkspace()
	e.setNow()

	// 不存在：人类输出与 JSON message 都只显示 ID，不显示绝对路径。
	res := e.run("", "todo", "show", "nope", "--workspace", e.workspace)
	if res.code != 1 || !strings.Contains(res.stderr, "待办不存在: nope") {
		t.Errorf("show 人类输出 = %+v", res)
	}
	if strings.Contains(res.stderr, "todos") || strings.Contains(res.stderr, ".md") {
		t.Errorf("show 不应包含路径: %s", res.stderr)
	}
	resJSON := e.run("", "todo", "show", "nope", "--json", "--workspace", e.workspace)
	if resJSON.jsonErrorCode(t) != "NOT_FOUND" {
		t.Errorf("show JSON 错误码 = %q", resJSON.jsonErrorCode(t))
	}
	if msg := jsonErrorMessage(t, resJSON.stderr); !strings.Contains(msg, "待办不存在: nope") {
		t.Errorf("show JSON message = %q", msg)
	}

	root := todoID(t, e.addTodo("父任务"))
	child := todoID(t, e.addTodo("子任务", "--parent", root))

	// 删除有子任务 → HAS_CHILDREN + 待办专用文案。
	res = e.run("", "todo", "rm", root, "--workspace", e.workspace)
	if res.code != 1 || !strings.Contains(res.stderr, "待办仍有子任务: "+root+"（如需连同子任务删除请加 -r）") {
		t.Errorf("rm 人类输出 = %+v", res)
	}
	resJSON = e.run("", "todo", "rm", root, "--json", "--workspace", e.workspace)
	if resJSON.jsonErrorCode(t) != "HAS_CHILDREN" {
		t.Errorf("rm JSON 错误码 = %q", resJSON.jsonErrorCode(t))
	}
	if msg := jsonErrorMessage(t, resJSON.stderr); !strings.Contains(msg, "待办仍有子任务: "+root) {
		t.Errorf("rm JSON message = %q", msg)
	}

	// 成环 → CYCLE + 待办专用文案。
	res = e.run("", "todo", "mv", root, "--parent", child, "--workspace", e.workspace)
	if res.code != 1 || !strings.Contains(res.stderr, "移动会导致待办父子关系成环: "+root) {
		t.Errorf("mv 人类输出 = %+v", res)
	}
	resJSON = e.run("", "todo", "mv", root, "--parent", child, "--json", "--workspace", e.workspace)
	if resJSON.jsonErrorCode(t) != "CYCLE" {
		t.Errorf("mv JSON 错误码 = %q", resJSON.jsonErrorCode(t))
	}
	if msg := jsonErrorMessage(t, resJSON.stderr); !strings.Contains(msg, "移动会导致待办父子关系成环: "+root) {
		t.Errorf("mv JSON message = %q", msg)
	}

	// 父待办不存在。
	res = e.run("", "todo", "add", "x", "--parent", "nope", "--workspace", e.workspace)
	if res.code != 1 || !strings.Contains(res.stderr, "父待办不存在: nope") {
		t.Errorf("add 父不存在人类输出 = %+v", res)
	}
	resJSON = e.run("", "todo", "add", "x", "--parent", "nope", "--json", "--workspace", e.workspace)
	if resJSON.jsonErrorCode(t) != "NOT_FOUND" {
		t.Errorf("add 父不存在 JSON 错误码 = %q", resJSON.jsonErrorCode(t))
	}
	if msg := jsonErrorMessage(t, resJSON.stderr); !strings.Contains(msg, "父待办不存在: nope") {
		t.Errorf("add 父不存在 JSON message = %q", msg)
	}
}

func TestTodoListTreeRootOrder(t *testing.T) {
	e := newTestEnv(t)
	e.initWorkspace()
	e.setNow()

	// 先建未来截止，再建逾期：普通 list 的默认顺序是逾期在前。
	future := todoID(t, e.addTodo("未来", "--due", "2026-11-01"))
	overdue := todoID(t, e.addTodo("逾期", "--due", "2026-10-25"))

	list := e.todoListJSON()
	if len(list) != 2 {
		t.Fatalf("普通 list 条数 = %d，期望 2", len(list))
	}
	if list[0].(map[string]any)["id"] != overdue || list[1].(map[string]any)["id"] != future {
		t.Fatalf("普通 list 顺序 = %v，期望 [%s %s]", list, overdue, future)
	}

	// tree 根顺序应与普通 list 一致（逾期在前，未来在后）。
	tree := e.todoListJSON("--tree")
	if len(tree) != 2 {
		t.Fatalf("tree 根数 = %d，期望 2", len(tree))
	}
	if tree[0].(map[string]any)["id"] != overdue || tree[1].(map[string]any)["id"] != future {
		t.Errorf("tree 根顺序 = %v，期望与普通 list 一致 [%s %s]", tree, overdue, future)
	}

	// 子任务在父下按 order,id：把第二个子任务移到第 0 位后应排前面。
	parent := todoID(t, e.addTodo("父任务"))
	child1 := todoID(t, e.addTodo("子1", "--parent", parent))
	child2 := todoID(t, e.addTodo("子2", "--parent", parent))
	e.mustRun("", "todo", "mv", child2, "--position", "0", "--workspace", e.workspace)

	tree = e.todoListJSON("--tree")
	var parentNode map[string]any
	for _, raw := range tree {
		m, _ := raw.(map[string]any)
		if m["id"] == parent {
			parentNode = m
		}
	}
	if parentNode == nil {
		t.Fatalf("tree 未找到父任务: %v", tree)
	}
	children, _ := parentNode["children"].([]any)
	if len(children) != 2 {
		t.Fatalf("children = %v，期望 2", children)
	}
	if children[0].(map[string]any)["id"] != child2 || children[1].(map[string]any)["id"] != child1 {
		t.Errorf("子任务顺序 = %v，期望 [%s %s]", children, child2, child1)
	}
}
