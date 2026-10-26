package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// cliResult 是一次 CLI 调用的结果。
type cliResult struct {
	code   int
	stdout string
	stderr string
}

// testEnv 是测试用的环境：工作台目录与隔离的 KUIBU_CONFIG_DIR。
type testEnv struct {
	t          *testing.T
	workspace  string
	configDir  string
	extraEnv   map[string]string
	unsetExtra []string
}

// newTestEnv 创建隔离的测试环境，保证不读写真实用户配置。
func newTestEnv(t *testing.T) *testEnv {
	t.Helper()
	return &testEnv{
		t:         t,
		workspace: filepath.Join(t.TempDir(), "ws"),
		configDir: t.TempDir(),
		extraEnv:  map[string]string{},
	}
}

// envFunc 返回给 Run 使用的环境变量读取函数。
func (e *testEnv) envFunc() func(string) string {
	return func(key string) string {
		if v, ok := e.extraEnv[key]; ok {
			return v
		}
		if key == "KUIBU_CONFIG_DIR" {
			return e.configDir
		}
		return ""
	}
}

// run 执行一次 CLI 调用。
func (e *testEnv) run(stdin string, args ...string) cliResult {
	e.t.Helper()
	var stdout, stderr bytes.Buffer
	code := Run(args, strings.NewReader(stdin), &stdout, &stderr, e.envFunc())
	return cliResult{code: code, stdout: stdout.String(), stderr: stderr.String()}
}

// mustRun 执行并断言退出码为 0。
func (e *testEnv) mustRun(stdin string, args ...string) cliResult {
	e.t.Helper()
	res := e.run(stdin, args...)
	if res.code != 0 {
		e.t.Fatalf("命令 %v 退出码 = %d，期望 0\nstdout=%s\nstderr=%s", args, res.code, res.stdout, res.stderr)
	}
	return res
}

// jsonOut 把 stdout 解析为 map。
func (r cliResult) jsonOut(t *testing.T) map[string]any {
	t.Helper()
	var out map[string]any
	if err := json.Unmarshal([]byte(r.stdout), &out); err != nil {
		t.Fatalf("stdout 不是合法 JSON 对象: %v\n输出=%q", err, r.stdout)
	}
	return out
}

// jsonArray 把 stdout 解析为数组。
func (r cliResult) jsonArray(t *testing.T) []any {
	t.Helper()
	var out []any
	if err := json.Unmarshal([]byte(r.stdout), &out); err != nil {
		t.Fatalf("stdout 不是合法 JSON 数组: %v\n输出=%q", err, r.stdout)
	}
	return out
}

// jsonErrorCode 解析 stderr 中的 JSON 错误码。
func (r cliResult) jsonErrorCode(t *testing.T) string {
	t.Helper()
	var out struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal([]byte(r.stderr), &out); err != nil {
		t.Fatalf("stderr 不是合法 JSON: %v\n输出=%q", err, r.stderr)
	}
	return out.Error.Code
}

// idOf 从 note new 的纯文本输出中取出 ID。
func idOf(t *testing.T, r cliResult) string {
	t.Helper()
	id := strings.TrimSpace(r.stdout)
	if id == "" || strings.ContainsAny(id, " \n") {
		t.Fatalf("note new 输出不是单个 ID: %q", r.stdout)
	}
	return id
}

// initWorkspace 初始化测试工作台。
func (e *testEnv) initWorkspace() {
	e.t.Helper()
	e.mustRun("", "init", e.workspace)
}

func TestCLIWorkflow(t *testing.T) {
	e := newTestEnv(t)
	e.initWorkspace()

	// 根笔记。
	rootJSON := e.mustRun("", "note", "new", "--workspace", e.workspace, "--title", "根笔记", "--tag", "工作", "--body", "跬步笔记支持中文全文检索。", "--json")
	root := rootJSON.jsonOut(t)
	rootID, _ := root["id"].(string)
	if rootID == "" {
		t.Fatalf("root id 为空: %s", rootJSON.stdout)
	}
	if root["title"] != "根笔记" {
		t.Errorf("title = %v", root["title"])
	}
	if body, _ := root["body"].(string); !strings.Contains(body, "中文全文检索") {
		t.Errorf("body = %v", root["body"])
	}

	// 子笔记（纯文本模式输出 ID）。
	childID := idOf(t, e.mustRun("", "note", "new", "--workspace", e.workspace, "--title", "子笔记", "--parent", rootID))

	// list 人类模式包含两行。
	list := e.mustRun("", "note", "list", "--workspace", e.workspace)
	if !strings.Contains(list.stdout, "根笔记") || !strings.Contains(list.stdout, "子笔记") {
		t.Errorf("list 输出缺少笔记:\n%s", list.stdout)
	}
	if !strings.Contains(list.stdout, "ID") || !strings.Contains(list.stdout, "更新时间") {
		t.Errorf("list 输出缺少表头:\n%s", list.stdout)
	}

	// list JSON 数组。
	listJSON := e.mustRun("", "note", "list", "--workspace", e.workspace, "--json")
	if got := len(listJSON.jsonArray(t)); got != 2 {
		t.Errorf("list JSON 条数 = %d，期望 2", got)
	}

	// list --parent 过滤。
	onlyRoot := e.mustRun("", "note", "list", "--workspace", e.workspace, "--parent", rootID)
	if strings.Contains(onlyRoot.stdout, "根笔记") || !strings.Contains(onlyRoot.stdout, "子笔记") {
		t.Errorf("--parent 过滤不正确:\n%s", onlyRoot.stdout)
	}

	// list --tag 过滤。
	byTag := e.mustRun("", "note", "list", "--workspace", e.workspace, "--tag", "工作")
	if !strings.Contains(byTag.stdout, "根笔记") || strings.Contains(byTag.stdout, "子笔记") {
		t.Errorf("--tag 过滤不正确:\n%s", byTag.stdout)
	}

	// show 人类模式。
	show := e.mustRun("", "note", "show", rootID, "--workspace", e.workspace)
	if !strings.Contains(show.stdout, "# 根笔记") || !strings.Contains(show.stdout, "中文全文检索") {
		t.Errorf("show 输出不正确:\n%s", show.stdout)
	}

	// show JSON（flag 在位置参数之后）。
	showJSON := e.mustRun("", "note", "show", rootID, "--json", "--workspace", e.workspace).jsonOut(t)
	if showJSON["id"] != rootID {
		t.Errorf("show JSON id = %v", showJSON["id"])
	}
	if body, _ := showJSON["body"].(string); !strings.Contains(body, "中文全文检索") {
		t.Errorf("show JSON body = %v", showJSON["body"])
	}

	// show --meta-only 不带 body。
	metaOnly := e.mustRun("", "note", "show", rootID, "--meta-only", "--json", "--workspace", e.workspace).jsonOut(t)
	if _, ok := metaOnly["body"]; ok {
		t.Errorf("--meta-only 不应包含 body: %v", metaOnly)
	}

	// edit --stdin 替换正文。
	e.mustRun("来自标准输入的新正文", "note", "edit", rootID, "--stdin", "--workspace", e.workspace)
	edited := e.mustRun("", "note", "show", rootID, "--json", "--workspace", e.workspace).jsonOut(t)
	if edited["body"] != "来自标准输入的新正文" {
		t.Errorf("--stdin 编辑后 body = %v", edited["body"])
	}

	// edit --file 替换正文。
	bodyFile := filepath.Join(t.TempDir(), "body.md")
	if err := os.WriteFile(bodyFile, []byte("来自文件的正文\n第二行\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	e.mustRun("", "note", "edit", rootID, "--file", bodyFile, "--title", "根笔记（改）", "--tag", "a", "--tag", "b", "--workspace", e.workspace)
	afterFile := e.mustRun("", "note", "show", rootID, "--json", "--workspace", e.workspace).jsonOut(t)
	if afterFile["body"] != "来自文件的正文\n第二行\n" {
		t.Errorf("--file 编辑后 body = %q", afterFile["body"])
	}
	if afterFile["title"] != "根笔记（改）" {
		t.Errorf("--title 编辑未生效: %v", afterFile["title"])
	}
	tags, _ := afterFile["tags"].([]any)
	if len(tags) != 2 || tags[0] != "a" || tags[1] != "b" {
		t.Errorf("--tag 编辑后 tags = %v", afterFile["tags"])
	}

	// edit 无修改项报 USAGE。
	if res := e.run("", "note", "edit", rootID, "--json", "--workspace", e.workspace); res.code != 1 || res.jsonErrorCode(t) != "USAGE" {
		t.Errorf("空 edit 结果 = %+v，期望 USAGE/1", res)
	}

	// mv：把子笔记移到根层级。
	e.mustRun("", "note", "mv", childID, "--root", "--workspace", e.workspace)
	moved := e.mustRun("", "note", "show", childID, "--json", "--workspace", e.workspace).jsonOut(t)
	if moved["parent"] != "" {
		t.Errorf("mv --root 后 parent = %v", moved["parent"])
	}

	// mv：再挂回根笔记，然后尝试成环。
	e.mustRun("", "note", "mv", childID, "--parent", rootID, "--workspace", e.workspace)
	cycle := e.run("", "note", "mv", rootID, "--parent", childID, "--json", "--workspace", e.workspace)
	if cycle.code != 1 {
		t.Errorf("成环 mv 退出码 = %d，期望 1", cycle.code)
	}
	if code := cycle.jsonErrorCode(t); code != "CYCLE" {
		t.Errorf("成环 mv 错误码 = %q，期望 CYCLE", code)
	}

	// mv --pos 调整顺序。
	secondID := idOf(t, e.mustRun("", "note", "new", "--workspace", e.workspace, "--title", "第二根"))
	e.mustRun("", "note", "mv", secondID, "--root", "--pos", "0", "--workspace", e.workspace)
	tree := e.mustRun("", "tree", "--workspace", e.workspace)
	if idx := strings.Index(tree.stdout, "第二根"); idx < 0 {
		t.Errorf("tree 中缺少第二根:\n%s", tree.stdout)
	}

	// tree 人类模式带缩进与 ID。
	if !strings.Contains(tree.stdout, "- 根笔记（改）  ("+rootID+")") {
		t.Errorf("tree 输出格式不正确:\n%s", tree.stdout)
	}
	if !strings.Contains(tree.stdout, "\n  - 子笔记  ("+childID+")") {
		t.Errorf("tree 子节点缩进不正确:\n%s", tree.stdout)
	}

	// tree JSON 嵌套结构。
	treeJSON := e.mustRun("", "tree", "--json", "--workspace", e.workspace).jsonArray(t)
	if len(treeJSON) != 2 {
		t.Fatalf("tree JSON 根节点数 = %d，期望 2", len(treeJSON))
	}
	var foundRoot bool
	for _, raw := range treeJSON {
		node, _ := raw.(map[string]any)
		if node["id"] != rootID {
			continue
		}
		foundRoot = true
		children, _ := node["children"].([]any)
		if len(children) != 1 {
			t.Fatalf("根笔记子节点数 = %d，期望 1", len(children))
		}
		child, _ := children[0].(map[string]any)
		if child["id"] != childID {
			t.Errorf("子节点 id = %v，期望 %s", child["id"], childID)
		}
	}
	if !foundRoot {
		t.Errorf("tree JSON 未包含根笔记")
	}

	// tree --root 只输出子树。
	subtree := e.mustRun("", "tree", "--root", rootID, "--json", "--workspace", e.workspace).jsonArray(t)
	if len(subtree) != 1 {
		t.Errorf("tree --root 根节点数 = %d，期望 1", len(subtree))
	}

	// search 中文。
	search := e.mustRun("", "search", "来自文件", "--workspace", e.workspace)
	if !strings.Contains(search.stdout, rootID) || !strings.Contains(search.stdout, "来自文件") {
		t.Errorf("search 输出不正确:\n%s", search.stdout)
	}
	searchJSON := e.mustRun("", "search", "来自文件", "--json", "--workspace", e.workspace).jsonArray(t)
	if len(searchJSON) == 0 {
		t.Fatalf("search JSON 无结果")
	}
	first, _ := searchJSON[0].(map[string]any)
	if first["id"] != rootID {
		t.Errorf("search JSON 首条 id = %v，期望 %s", first["id"], rootID)
	}
	if snippet, _ := first["snippet"].(string); !strings.Contains(snippet, "[来自文件]") {
		t.Errorf("search snippet 未高亮: %v", first["snippet"])
	}

	// search --limit。
	if got := len(e.mustRun("", "search", "正文", "--limit", "1", "--json", "--workspace", e.workspace).jsonArray(t)); got != 1 {
		t.Errorf("search --limit=1 返回 %d 条", got)
	}

	// asset add。
	assetSrc := filepath.Join(t.TempDir(), "图片 1.png")
	if err := os.WriteFile(assetSrc, []byte("png-bytes"), 0o644); err != nil {
		t.Fatal(err)
	}
	assetHuman := e.mustRun("", "asset", "add", assetSrc, "--workspace", e.workspace)
	rel := strings.TrimSpace(assetHuman.stdout)
	if !strings.HasPrefix(rel, "../assets/") {
		t.Errorf("asset add 相对路径 = %q", rel)
	}
	if _, err := os.Stat(filepath.Join(e.workspace, "assets", filepath.Base(rel))); err != nil {
		t.Errorf("asset 文件未落盘: %v", err)
	}
	assetJSON := e.mustRun("", "asset", "add", assetSrc, "--json", "--workspace", e.workspace).jsonOut(t)
	if assetJSON["path"] != rel {
		t.Errorf("asset add JSON path = %v，期望 %q", assetJSON["path"], rel)
	}

	// rm 无 -r 有子节点被拒。
	rmDenied := e.run("", "note", "rm", rootID, "--json", "--workspace", e.workspace)
	if rmDenied.code != 1 || rmDenied.jsonErrorCode(t) != "HAS_CHILDREN" {
		t.Errorf("非递归删除结果 = %+v，期望 HAS_CHILDREN/1", rmDenied)
	}
	// 递归删除成功。
	e.mustRun("", "note", "rm", rootID, "-r", "--workspace", e.workspace)
	if res := e.run("", "note", "show", rootID, "--json", "--workspace", e.workspace); res.code != 1 || res.jsonErrorCode(t) != "NOT_FOUND" {
		t.Errorf("删除后 show 结果 = %+v，期望 NOT_FOUND/1", res)
	}

	// index rebuild。
	rebuild := e.mustRun("", "index", "rebuild", "--json", "--workspace", e.workspace).jsonOut(t)
	if total, _ := rebuild["total"].(float64); int(total) != 1 {
		t.Errorf("rebuild 后 total = %v，期望 1", rebuild["total"])
	}
}

func TestCLINotOpen(t *testing.T) {
	// 不在任何工作台内，也没有配置默认工作台。
	t.Chdir(t.TempDir())
	e := newTestEnv(t)

	res := e.run("", "note", "list", "--json")
	if res.code != NotOpen {
		t.Errorf("退出码 = %d，期望 %d；stderr=%s", res.code, NotOpen, res.stderr)
	}
	if code := res.jsonErrorCode(t); code != "NOT_OPEN" {
		t.Errorf("错误码 = %q，期望 NOT_OPEN", code)
	}

	// 人类模式也返回 10。
	res = e.run("", "tree")
	if res.code != NotOpen {
		t.Errorf("人类模式退出码 = %d，期望 %d", res.code, NotOpen)
	}
	if !strings.Contains(res.stderr, "不是跬步工作台") {
		t.Errorf("stderr 缺少提示: %s", res.stderr)
	}
}

func TestWorkspacePriority(t *testing.T) {
	e := newTestEnv(t)
	wsFromEnv := filepath.Join(t.TempDir(), "env-ws")
	wsFromFlag := filepath.Join(t.TempDir(), "flag-ws")
	e.mustRun("", "init", wsFromEnv)
	e.mustRun("", "init", wsFromFlag)

	// KUIBU_WORKSPACE 生效。
	e.extraEnv["KUIBU_WORKSPACE"] = wsFromEnv
	idInEnv := idOf(t, e.mustRun("", "note", "new", "--title", "env 笔记"))
	if _, err := os.Stat(filepath.Join(wsFromEnv, "notes", idInEnv+".md")); err != nil {
		t.Errorf("KUIBU_WORKSPACE 未生效: %v", err)
	}

	// --workspace 优先于 KUIBU_WORKSPACE。
	idInFlag := idOf(t, e.mustRun("", "note", "new", "--title", "flag 笔记", "--workspace", wsFromFlag))
	if _, err := os.Stat(filepath.Join(wsFromFlag, "notes", idInFlag+".md")); err != nil {
		t.Errorf("--workspace 未优先: %v", err)
	}
	if _, err := os.Stat(filepath.Join(wsFromEnv, "notes", idInFlag+".md")); !os.IsNotExist(err) {
		t.Errorf("--workspace 笔记不应写入 KUIBU_WORKSPACE")
	}
}

func TestInitDefaultWritesConfig(t *testing.T) {
	e := newTestEnv(t)
	res := e.mustRun("", "init", e.workspace, "--default", "--json").jsonOut(t)
	if res["default"] != true {
		t.Errorf("init --default JSON = %v", res)
	}

	path := filepath.Join(e.configDir, "config.json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("读取用户配置失败: %v", err)
	}
	var cfg userConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		t.Fatalf("解析用户配置失败: %v", err)
	}
	want, _ := filepath.Abs(e.workspace)
	if cfg.Workspace != want {
		t.Errorf("配置 workspace = %q，期望 %q", cfg.Workspace, want)
	}

	// 之后不带 --workspace 也能定位到默认工作台（从临时目录运行）。
	t.Chdir(t.TempDir())
	id := idOf(t, e.mustRun("", "note", "new", "--title", "默认工作台笔记"))
	if _, err := os.Stat(filepath.Join(e.workspace, "notes", id+".md")); err != nil {
		t.Errorf("默认工作台未生效: %v", err)
	}
}

func TestInitAlreadyInitJSON(t *testing.T) {
	e := newTestEnv(t)
	e.initWorkspace()
	res := e.run("", "init", e.workspace, "--json")
	if res.code != 1 {
		t.Errorf("重复 init 退出码 = %d，期望 1", res.code)
	}
	if code := res.jsonErrorCode(t); code != "ALREADY_INIT" {
		t.Errorf("错误码 = %q，期望 ALREADY_INIT", code)
	}
}

func TestHelpAndNoArgs(t *testing.T) {
	e := newTestEnv(t)

	help := e.run("", "help")
	if help.code != 0 {
		t.Errorf("help 退出码 = %d，期望 0", help.code)
	}
	if !strings.Contains(help.stdout, "用法") || !strings.Contains(help.stdout, "退出码") {
		t.Errorf("help 输出缺少用法/退出码:\n%s", help.stdout)
	}

	noArgs := e.run("")
	if noArgs.code != 1 {
		t.Errorf("无参数退出码 = %d，期望 1", noArgs.code)
	}
	if !strings.Contains(noArgs.stderr, "用法") {
		t.Errorf("无参数 stderr 缺少用法:\n%s", noArgs.stderr)
	}

	// 未知命令。
	unknown := e.run("", "nope", "--json")
	if unknown.code != 1 || unknown.jsonErrorCode(t) != "USAGE" {
		t.Errorf("未知命令结果 = %+v", unknown)
	}
}

func TestFlagOrderMixed(t *testing.T) {
	e := newTestEnv(t)
	e.initWorkspace()
	id := idOf(t, e.mustRun("", "note", "new", "--workspace", e.workspace, "--title", "混排"))

	// flag 与位置参数混排。
	res := e.mustRun("", "note", "show", "--json", id, "--workspace", e.workspace)
	if res.jsonOut(t)["id"] != id {
		t.Errorf("混排参数 show 结果不正确: %s", res.stdout)
	}

	// 全局参数放在子命令之前。
	res = e.mustRun("", "--workspace", e.workspace, "note", "show", id, "--json")
	if res.jsonOut(t)["id"] != id {
		t.Errorf("全局参数前置结果不正确: %s", res.stdout)
	}
}

func TestWatchEmitsEvents(t *testing.T) {
	e := newTestEnv(t)
	e.initWorkspace()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var mu sync.Mutex
	var out bytes.Buffer
	done := make(chan int, 1)
	go func() {
		// stdin 为 nil 表示不因 stdin 关闭而退出，由 ctx 控制。
		code := RunContext(ctx, []string{"watch", "--workspace", e.workspace, "--json"},
			nil, &lockedWriter{mu: &mu, w: &out}, &lockedWriter{mu: &mu, w: &out}, e.envFunc())
		done <- code
	}()

	// 先创建探针笔记，再周期性触发写入，轮询直到观察到事件。
	// 写入间隔必须大于 watcher 的防抖窗口，避免事件被无限合并。
	id := idOf(t, e.mustRun("", "note", "new", "--workspace", e.workspace, "--title", "监听笔记", "--body", "监听正文"))

	deadline := time.Now().Add(15 * time.Second)
	nextWrite := time.Now()
	for time.Now().Before(deadline) {
		if !time.Now().Before(nextWrite) {
			e.mustRun("", "note", "edit", id, "--body", "监听正文 更新", "--workspace", e.workspace)
			nextWrite = time.Now().Add(time.Second)
		}

		mu.Lock()
		content := out.String()
		mu.Unlock()
		if strings.Contains(content, id) {
			cancel()
			select {
			case code := <-done:
				if code != 0 {
					t.Errorf("watch 退出码 = %d，期望 0", code)
				}
			case <-time.After(10 * time.Second):
				t.Fatalf("取消后 watch 未返回")
			}
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	mu.Lock()
	content := out.String()
	mu.Unlock()
	t.Fatalf("等待 watch 事件超时，输出=%q", content)
}

// lockedWriter 是加锁的 io.Writer，供并发读取测试使用。
type lockedWriter struct {
	mu *sync.Mutex
	w  *bytes.Buffer
}

// Write 实现 io.Writer。
func (l *lockedWriter) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.w.Write(p)
}
