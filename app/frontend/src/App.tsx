import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { Events } from "@wailsio/runtime";
import {
  ShellService,
  WorkspaceService,
} from "../bindings/github.com/axu4/kuibu/app";
import type {
  CheckResult,
  NoteData,
  QuickNoteSaved,
  ShortcutStatus,
  TreeItem,
} from "../bindings/github.com/axu4/kuibu/app";
import { Tree } from "./components/Tree";
import { Editor } from "./components/Editor";
import { Settings } from "./components/Settings";
import { TodoView } from "./components/TodoView";
import { ReportView } from "./components/ReportView";
import { Icon } from "./components/Icon";
import { MoreMenu } from "./components/Menu";
import { loadMainView, saveMainView } from "./lib/todo";
import type { MainView } from "./lib/todo";

/** 自动保存防抖时间（毫秒）。 */
const SAVE_DEBOUNCE_MS = 600;

/** localStorage 中保存展开状态的键。 */
const EXPANDED_KEY = "kuibu.tree.expanded";

/** localStorage 中保存侧栏宽度的键。 */
const SIDEBAR_WIDTH_KEY = "kuibu.sidebar.width";

/** 保存状态。 */
type SaveStatus = "saved" | "saving" | "dirty";

/** 当前笔记的外部变化提示。 */
type ExternalState = "none" | "modified" | "deleted";

interface LoadedNote {
  data: NoteData;
  /** 每次从磁盘重新加载递增，用于重建编辑器。 */
  revision: number;
}

/** 弹窗类型。 */
type ModalState =
  | { kind: "none" }
  | { kind: "delete"; item: TreeItem; childCount: number };

function loadExpanded(): Set<string> {
  try {
    const raw = localStorage.getItem(EXPANDED_KEY);
    if (!raw) {
      return new Set();
    }
    const list = JSON.parse(raw) as unknown;
    if (Array.isArray(list)) {
      return new Set(list.filter((v): v is string => typeof v === "string"));
    }
  } catch {
    // 忽略损坏的本地状态。
  }
  return new Set();
}

function loadSidebarWidth(): number {
  const raw = Number(localStorage.getItem(SIDEBAR_WIDTH_KEY));
  if (Number.isFinite(raw) && raw >= 180 && raw <= 520) {
    return raw;
  }
  return 260;
}

/** 统计树节点的后代数量。 */
function countDescendants(item: TreeItem): number {
  let total = 0;
  for (const child of item.children ?? []) {
    total += 1 + countDescendants(child);
  }
  return total;
}

/** 在树中按 ID 查找节点。 */
function findItem(items: TreeItem[], id: string): TreeItem | null {
  for (const item of items) {
    if (item.id === id) {
      return item;
    }
    const found = findItem(item.children ?? [], id);
    if (found) {
      return found;
    }
  }
  return null;
}

export default function App() {
  const [workspace, setWorkspace] = useState("");
  const [tree, setTree] = useState<TreeItem[]>([]);
  const [selectedId, setSelectedId] = useState<string | null>(null);
  const [note, setNote] = useState<LoadedNote | null>(null);
  const [body, setBody] = useState("");
  const [savedBody, setSavedBody] = useState("");
  const [status, setStatus] = useState<SaveStatus>("saved");
  const [mode, setMode] = useState<"wysiwyg" | "source">("wysiwyg");
  const [external, setExternal] = useState<ExternalState>("none");
  const [expanded, setExpanded] = useState<Set<string>>(loadExpanded);
  const [sidebarWidth, setSidebarWidth] = useState<number>(loadSidebarWidth);
  const [modal, setModal] = useState<ModalState>({ kind: "none" });
  const [renamingId, setRenamingId] = useState<string | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [settingsOpen, setSettingsOpen] = useState(false);
  const [shortcut, setShortcut] = useState<ShortcutStatus | null>(null);
  const [updateBanner, setUpdateBanner] = useState<CheckResult | null>(null);
  // 从更新提示条进入设置时定位到「软件更新」一节。
  const [settingsFocusUpdate, setSettingsFocusUpdate] = useState(false);
  const [view, setView] = useState<MainView>(loadMainView);
  const [titleDraft, setTitleDraft] = useState("");
  // 待办视图把分组与标签渲染进侧栏的这个容器。
  const [todoNavHost, setTodoNavHost] = useState<HTMLDivElement | null>(null);
  const titleRef = useRef<HTMLInputElement | null>(null);
  // 新建笔记后需要聚焦标题，等笔记加载完再处理。
  const focusTitleRef = useRef(false);

  // 稳定的回调：Settings 里用它触发状态刷新，引用变化会导致重复拉取。
  const applyShortcutStatus = useCallback((status: ShortcutStatus) => {
    setShortcut(status);
  }, []);

  // 用 ref 保存最新的正文与保存状态，供事件回调与定时器读取。
  const bodyRef = useRef(body);
  bodyRef.current = body;
  const savedBodyRef = useRef(savedBody);
  savedBodyRef.current = savedBody;
  const selectedIdRef = useRef(selectedId);
  selectedIdRef.current = selectedId;
  const noteIdRef = useRef<string | null>(note?.data.id ?? null);
  noteIdRef.current = note?.data.id ?? null;
  const saveTimer = useRef<ReturnType<typeof setTimeout> | null>(null);
  const savingRef = useRef(false);

  /** 刷新树。 */
  const refreshTree = useCallback(async () => {
    try {
      const items = await WorkspaceService.Tree();
      setTree(items ?? []);
    } catch (err) {
      console.error("读取笔记树失败", err);
    }
  }, []);

  /** 打开一条笔记（从磁盘加载）。 */
  const openNote = useCallback(async (id: string) => {
    try {
      const data = await WorkspaceService.Get(id);
      setNote((prev) => ({ data, revision: (prev?.revision ?? 0) + 1 }));
      setBody(data.body);
      setSavedBody(data.body);
      setStatus("saved");
      setExternal("none");
      setSelectedId(id);
      setError(null);
      return data;
    } catch (err) {
      setError(`打开笔记失败：${String(err)}`);
      return null;
    }
  }, []);

  /** 立即保存当前笔记。 */
  const saveNow = useCallback(async () => {
    const id = noteIdRef.current;
    if (!id) {
      return;
    }
    if (saveTimer.current) {
      clearTimeout(saveTimer.current);
      saveTimer.current = null;
    }
    if (savingRef.current || bodyRef.current === savedBodyRef.current) {
      return;
    }
    const content = bodyRef.current;
    savingRef.current = true;
    setStatus("saving");
    try {
      await WorkspaceService.SaveBody(id, content);
      setSavedBody(content);
      savedBodyRef.current = content;
      // 保存期间用户可能继续输入。
      setStatus(bodyRef.current === content ? "saved" : "dirty");
    } catch (err) {
      setError(`保存失败：${String(err)}`);
      setStatus("dirty");
    } finally {
      savingRef.current = false;
    }
  }, []);

  /** 正文变化：标记未保存并安排防抖保存。 */
  const onBodyChange = useCallback(
    (next: string) => {
      setBody(next);
      bodyRef.current = next;
      setStatus(next === savedBodyRef.current ? "saved" : "dirty");
      if (saveTimer.current) {
        clearTimeout(saveTimer.current);
      }
      saveTimer.current = setTimeout(() => {
        saveTimer.current = null;
        void saveNow();
      }, SAVE_DEBOUNCE_MS);
    },
    [saveNow],
  );

  /** 选中树节点。 */
  const selectNote = useCallback(
    async (id: string) => {
      if (id === selectedIdRef.current) {
        return;
      }
      await saveNow();
      await openNote(id);
    },
    [openNote, saveNow],
  );

  /** 切换工作台后重新加载。 */
  const reloadWorkspace = useCallback(async () => {
    const info = await WorkspaceService.Current();
    setWorkspace(info.path);
    if (!info.path) {
      setTree([]);
      setSelectedId(null);
      setNote(null);
      setBody("");
      setSavedBody("");
      return;
    }
    await refreshTree();
  }, [refreshTree]);

  // 启动：读取当前工作台并加载树。
  useEffect(() => {
    void reloadWorkspace();
  }, [reloadWorkspace]);

  // 监听外部文件变化。
  useEffect(() => {
    const off = Events.On("workspace:changed", (event) => {
      const data = event.data as { ids?: string[] } | null;
      const ids = data?.ids ?? [];
      void refreshTree();
      const current = noteIdRef.current;
      if (!current || ids.length === 0 || !ids.includes(current)) {
        return;
      }
      void (async () => {
        try {
          const latest = await WorkspaceService.Get(current);
          if (latest.body === bodyRef.current) {
            // 自己保存引起的变化：正文与本地一致，只同步元数据。
            setNote((prev) =>
              prev ? { ...prev, data: { ...latest, body: prev.data.body } } : prev,
            );
            return;
          }
          if (bodyRef.current === savedBodyRef.current) {
            // 本地没有未保存修改，直接重新加载。
            setNote((prev) => ({
              data: latest,
              revision: (prev?.revision ?? 0) + 1,
            }));
            setBody(latest.body);
            setSavedBody(latest.body);
            bodyRef.current = latest.body;
            savedBodyRef.current = latest.body;
            setStatus("saved");
            setExternal("none");
          } else {
            // 有未保存修改，交给用户决定。
            setExternal("modified");
          }
        } catch {
          // 文件已被外部删除。
          setExternal("deleted");
        }
      })();
    });
    return off;
  }, [refreshTree]);

  // 随手记保存成功：刷新树并展开收件箱。
  useEffect(() => {
    const off = Events.On("quicknote:saved", (event) => {
      const data = event.data as QuickNoteSaved | null;
      void (async () => {
        // 先保存当前笔记，避免刷新树/展开时把本地改动丢掉。
        await saveNow();
        await refreshTree();
        if (data?.inboxId) {
          setExpanded((prev) => new Set(prev).add(data.inboxId));
        }
      })();
    });
    return off;
  }, [refreshTree, saveNow]);

  // 托盘「设置…」让主窗口打开设置面板。
  useEffect(() => {
    const off = Events.On("shell:open-settings", () => {
      setSettingsOpen(true);
    });
    return off;
  }, []);

  // 启动时后台自动检查发现新版本：显示一条不打扰的提示条。
  useEffect(() => {
    const off = Events.On("update:available", (event) => {
      const data = event.data as CheckResult | null;
      if (data?.hasUpdate) {
        setUpdateBanner(data);
      }
    });
    return off;
  }, []);

  // 快捷键状态（启动时读取一次，之后随改动事件刷新）。
  useEffect(() => {
    const load = () => {
      void ShellService.GetShortcutStatus()
        .then((status) => setShortcut(status))
        .catch((err) => console.error("读取快捷键状态失败", err));
    };
    load();
    const off = Events.On("shell:shortcut-changed", (event) => {
      const data = event.data as ShortcutStatus | null;
      if (data?.accelerator) {
        setShortcut(data);
      } else {
        load();
      }
    });
    return off;
  }, []);

  // 主视图（笔记/待办）持久化。
  useEffect(() => {
    saveMainView(view);
  }, [view]);

  // 展开状态持久化。
  useEffect(() => {
    localStorage.setItem(EXPANDED_KEY, JSON.stringify([...expanded]));
  }, [expanded]);

  // 侧栏宽度持久化。
  useEffect(() => {
    localStorage.setItem(SIDEBAR_WIDTH_KEY, String(sidebarWidth));
  }, [sidebarWidth]);

  // 打开的笔记变化时同步标题输入框；新建的笔记直接选中标题等待输入。
  const noteTitle = note?.data.title ?? "";
  const noteId = note?.data.id ?? "";
  useEffect(() => {
    setTitleDraft(noteTitle);
  }, [noteId, noteTitle]);
  useEffect(() => {
    if (noteId && focusTitleRef.current) {
      focusTitleRef.current = false;
      titleRef.current?.focus();
      titleRef.current?.select();
    }
  }, [noteId]);

  /** 切换节点展开状态。 */
  const toggleExpanded = useCallback((id: string) => {
    setExpanded((prev) => {
      const next = new Set(prev);
      if (next.has(id)) {
        next.delete(id);
      } else {
        next.add(id);
      }
      return next;
    });
  }, []);

  /** 展开某个节点的所有祖先。 */
  const expandTo = useCallback((id: string, items: TreeItem[]) => {
    setExpanded((prev) => {
      const next = new Set(prev);
      const walk = (nodes: TreeItem[], trail: string[]): boolean => {
        for (const node of nodes) {
          if (node.id === id) {
            trail.forEach((t) => next.add(t));
            return true;
          }
          if (walk(node.children ?? [], [...trail, node.id])) {
            return true;
          }
        }
        return false;
      };
      walk(items, []);
      return next;
    });
  }, []);

  /** 新建笔记：直接建一条「未命名笔记」并聚焦标题，省掉弹窗。 */
  const createNote = useCallback(
    async (parent: string) => {
      try {
        await saveNow();
        const created = await WorkspaceService.Create("", parent);
        if (parent) {
          setExpanded((prev) => new Set(prev).add(parent));
        }
        await refreshTree();
        focusTitleRef.current = true;
        await openNote(created.id);
      } catch (err) {
        setError(`新建笔记失败：${String(err)}`);
      }
    },
    [openNote, refreshTree, saveNow],
  );

  /** 重命名笔记。 */
  const renameNote = useCallback(
    async (id: string, title: string) => {
      setRenamingId(null);
      const trimmed = title.trim();
      if (!trimmed) {
        return;
      }
      try {
        await WorkspaceService.Rename(id, trimmed);
        await refreshTree();
        if (noteIdRef.current === id) {
          const latest = await WorkspaceService.Get(id);
          setNote((prev) => (prev ? { ...prev, data: latest } : prev));
        }
      } catch (err) {
        setError(`重命名失败：${String(err)}`);
      }
    },
    [refreshTree],
  );

  /** 删除笔记。 */
  const deleteNote = useCallback(
    async (item: TreeItem, recursive: boolean) => {
      try {
        await WorkspaceService.Delete(item.id, recursive);
        await refreshTree();
        if (noteIdRef.current === item.id) {
          setSelectedId(null);
          setNote(null);
          setBody("");
          setSavedBody("");
          setStatus("saved");
          setExternal("none");
        }
        setModal({ kind: "none" });
      } catch (err) {
        setError(`删除失败：${String(err)}`);
      }
    },
    [refreshTree],
  );

  /** 请求删除：有子节点时二次确认。 */
  const requestDelete = useCallback((item: TreeItem) => {
    const childCount = countDescendants(item);
    if (childCount > 0) {
      setModal({ kind: "delete", item, childCount });
    } else {
      void deleteNote(item, false);
    }
  }, [deleteNote]);

  /** 选择工作台。 */
  const chooseWorkspace = useCallback(async () => {
    try {
      await saveNow();
      const info = await WorkspaceService.ChooseWorkspace();
      if (!info.path) {
        return;
      }
      setWorkspace(info.path);
      setSelectedId(null);
      setNote(null);
      setBody("");
      setSavedBody("");
      setExternal("none");
      await refreshTree();
    } catch (err) {
      setError(`选择工作台失败：${String(err)}`);
    }
  }, [refreshTree, saveNow]);

  // 侧栏拖拽调整宽度。
  const startResize = useCallback(
    (e: React.MouseEvent) => {
      e.preventDefault();
      const startX = e.clientX;
      const startWidth = sidebarWidth;
      const onMove = (ev: MouseEvent) => {
        const next = Math.min(520, Math.max(180, startWidth + ev.clientX - startX));
        setSidebarWidth(next);
      };
      const onUp = () => {
        window.removeEventListener("mousemove", onMove);
        window.removeEventListener("mouseup", onUp);
      };
      window.addEventListener("mousemove", onMove);
      window.addEventListener("mouseup", onUp);
    },
    [sidebarWidth],
  );

  /** 提交页面标题的修改。 */
  const commitTitle = useCallback(() => {
    if (!note) {
      return;
    }
    const next = titleDraft.trim();
    if (!next) {
      setTitleDraft(note.data.title);
      return;
    }
    if (next !== note.data.title) {
      void renameNote(note.data.id, next);
    }
  }, [note, renameNote, titleDraft]);

  const selectedItem = useMemo(
    () => (selectedId ? findItem(tree, selectedId) : null),
    [tree, selectedId],
  );

  const statusText =
    status === "saved" ? "已保存" : status === "saving" ? "保存中…" : "未保存";

  /** 从待办详情的来源笔记跳到笔记视图并打开该笔记。 */
  const openNoteFromTodo = useCallback(
    (id: string) => {
      setView("notes");
      void openNote(id);
    },
    [openNote],
  );

  /** 切换主视图；切到待办前先保存当前笔记。 */
  const switchView = useCallback(
    (next: MainView) => {
      if (next === view) {
        return;
      }
      void saveNow();
      setView(next);
    },
    [saveNow, view],
  );

  // 全局快捷键：⌘S 立即保存，⌘1/⌘2 切换视图，笔记视图下 ⌘N 新建笔记。
  useEffect(() => {
    const onKeyDown = (e: KeyboardEvent) => {
      if (!(e.metaKey || e.ctrlKey) || e.altKey || e.shiftKey) {
        return;
      }
      const key = e.key.toLowerCase();
      if (key === "s") {
        e.preventDefault();
        void saveNow();
      } else if (key === "1") {
        e.preventDefault();
        switchView("notes");
      } else if (key === "2") {
        e.preventDefault();
        switchView("todo");
      } else if (key === "3") {
        e.preventDefault();
        switchView("report");
      } else if (key === "n" && view === "notes" && workspace) {
        e.preventDefault();
        void createNote("");
      }
    };
    window.addEventListener("keydown", onKeyDown);
    return () => window.removeEventListener("keydown", onKeyDown);
  }, [createNote, saveNow, switchView, view, workspace]);


  /** 面包屑：当前笔记的祖先标题。 */
  const breadcrumb = useMemo(() => {
    if (!selectedId) {
      return [] as TreeItem[];
    }
    const trail: TreeItem[] = [];
    const walk = (nodes: TreeItem[]): boolean => {
      for (const node of nodes) {
        if (node.id === selectedId) {
          return true;
        }
        trail.push(node);
        if (walk(node.children ?? [])) {
          return true;
        }
        trail.pop();
      }
      return false;
    };
    walk(tree);
    return trail;
  }, [tree, selectedId]);

  // 快捷键注册失败/回退的提示条，两个视图共用。
  const shortcutBanners = (
    <>
      {shortcut && !shortcut.registered && shortcut.error && (
        <div className="banner">
          {/* Go 侧的错误已经带上「快捷键 ⌃⌥N 注册失败：原因」，这里只补建议。 */}
          <span>{shortcut.error}，请在设置里换一个。</span>
          <button type="button" onClick={() => setSettingsOpen(true)}>
            打开设置
          </button>
        </div>
      )}
      {/* 已临时回退到默认值但仍需用户处理（例如配置里的快捷键不合法）。 */}
      {shortcut && shortcut.registered && shortcut.notice && (
        <div className="banner">
          <span>{shortcut.notice}</span>
          <button type="button" onClick={() => setSettingsOpen(true)}>
            打开设置
          </button>
        </div>
      )}
    </>
  );

  // 启动自动检查发现新版本时的提示条，点击「查看」打开设置并定位到更新一节。
  const updateBannerEl = updateBanner && (
    <div className="banner">
      <span>发现新版本 v{updateBanner.latest}</span>
      <button
        type="button"
        onClick={() => {
          setSettingsFocusUpdate(true);
          setSettingsOpen(true);
        }}
      >
        查看
      </button>
      <button
        type="button"
        className="banner-close"
        title="关闭"
        onClick={() => setUpdateBanner(null)}
      >
        <Icon name="close" size={14} />
      </button>
    </div>
  );

  const workspaceName = workspace ? workspace.split("/").filter(Boolean).pop() : "";

  const welcome = (
    <div className="welcome">
      <div className="welcome-mark">跬</div>
      <h1>不积跬步，无以至千里</h1>
      <p>笔记和待办都以 Markdown 文件存在你选的文件夹里，随时可以用别的工具打开。</p>
      <button type="button" className="btn btn-primary" onClick={() => void chooseWorkspace()}>
        选择工作台文件夹
      </button>
    </div>
  );

  return (
    <div className="app">
      <aside className="sidebar" style={{ width: sidebarWidth }}>
        <div className="sidebar-top">
          <button
            type="button"
            className="workspace-switch"
            title={workspace ? `${workspace}\n点击切换工作台` : "选择工作台文件夹"}
            onClick={() => void chooseWorkspace()}
          >
            <span className="workspace-mark">跬</span>
            <span className="workspace-name">{workspaceName || "选择工作台"}</span>
            <Icon name="chevronDown" size={14} className="workspace-chevron" />
          </button>
        </div>

        <nav className="view-switch" aria-label="视图">
          <button
            type="button"
            className={`view-switch-btn${view === "notes" ? " is-active" : ""}`}
            title="笔记（⌘1）"
            onClick={() => switchView("notes")}
          >
            <Icon name="note" />
            笔记
          </button>
          <button
            type="button"
            className={`view-switch-btn${view === "todo" ? " is-active" : ""}`}
            title="待办（⌘2）"
            onClick={() => switchView("todo")}
          >
            <Icon name="todo" />
            待办
          </button>
          <button
            type="button"
            className={`view-switch-btn${view === "report" ? " is-active" : ""}`}
            title="周报（⌘3）"
            onClick={() => switchView("report")}
          >
            <Icon name="report" />
            周报
          </button>
        </nav>

        {view === "notes" ? (
          <>
            <div className="sidebar-section">
              <span>全部笔记</span>
              <button
                type="button"
                className="icon-btn"
                title="新建笔记（⌘N）"
                disabled={!workspace}
                onClick={() => void createNote("")}
              >
                <Icon name="plus" size={14} />
              </button>
            </div>
            {workspace ? (
              <Tree
                items={tree}
                selectedId={selectedId}
                expanded={expanded}
                onToggle={toggleExpanded}
                onSelect={(id) => void selectNote(id)}
                onAddChild={(parent) => {
                  expandTo(parent, tree);
                  void createNote(parent);
                }}
                onCreateRoot={() => void createNote("")}
                onRename={(item) => setRenamingId(item.id)}
                onDelete={requestDelete}
                renamingId={renamingId}
                onRenameSubmit={(id, title) => void renameNote(id, title)}
                onRenameCancel={() => setRenamingId(null)}
              />
            ) : (
              <div className="tree" />
            )}
          </>
        ) : view === "report" ? (
          <div className="sidebar-slot" />
        ) : (
          <div className="sidebar-slot" ref={setTodoNavHost} />
        )}

        <div className="sidebar-bottom">
          <span
            className="sidebar-hint"
            title="在任意应用里按下，弹出随手记小窗"
          >
            <Icon name="bolt" size={14} />
            随手记
            {shortcut?.display && <kbd>{shortcut.display}</kbd>}
          </span>
          <button
            type="button"
            className="icon-btn"
            title="设置"
            onClick={() => setSettingsOpen(true)}
          >
            <Icon name="settings" />
          </button>
        </div>
        <div className="resize-handle" onMouseDown={startResize} />
      </aside>

      {view === "todo" ? (
        workspace ? (
          <div className="todo-shell">
            {shortcutBanners}
            {updateBannerEl}
            <TodoView onOpenNote={openNoteFromTodo} navHost={todoNavHost} />
          </div>
        ) : (
          <main className="content">
            <div className="toolbar" />
            {updateBannerEl}
            {welcome}
          </main>
        )
      ) : view === "report" ? (
        workspace ? (
          <div className="report-shell">
            {shortcutBanners}
            <ReportView key={workspace} />
          </div>
        ) : (
          <main className="content">
            <div className="toolbar" />
            {welcome}
          </main>
        )
      ) : (
        <main className="content">
          {!workspace ? (
            <>
              <div className="toolbar" />
              {updateBannerEl}
              {welcome}
            </>
          ) : !note ? (
            <>
              <div className="toolbar" />
              {shortcutBanners}
              {updateBannerEl}
              <div className="welcome">
                <div className="welcome-mark is-quiet">
                  <Icon name="note" size={22} />
                </div>
                <h1>{tree.length === 0 ? "写下第一条笔记" : "选一条笔记开始"}</h1>
                <p>从左侧打开，或者新建一条。按 {shortcut?.display || "⌃⌥N"} 可以在任意应用里随手记。</p>
                <button type="button" className="btn btn-primary" onClick={() => void createNote("")}>
                  <Icon name="plus" size={14} />
                  新建笔记
                </button>
              </div>
            </>
          ) : (
            <>
              <div className="toolbar">
                <div className="breadcrumb">
                  {breadcrumb.map((item) => (
                    <span key={item.id} className="breadcrumb-part">
                      <button type="button" onClick={() => void selectNote(item.id)}>
                        {item.title}
                      </button>
                      <Icon name="chevron" size={12} />
                    </span>
                  ))}
                  <span className="breadcrumb-current">{note.data.title}</span>
                </div>
                <span className={`save-state is-${status}`}>
                  <span className="save-dot" />
                  {statusText}
                </span>
                <button
                  type="button"
                  className={`icon-btn${mode === "source" ? " is-on" : ""}`}
                  title={mode === "source" ? "切回所见即所得" : "查看 Markdown 源码"}
                  onClick={() => setMode(mode === "source" ? "wysiwyg" : "source")}
                >
                  <Icon name="code" />
                </button>
                <MoreMenu
                  items={[
                    { label: "重命名", icon: "edit", onClick: () => titleRef.current?.select() },
                    {
                      label: "新建子笔记",
                      icon: "plus",
                      onClick: () => {
                        expandTo(note.data.id, tree);
                        void createNote(note.data.id);
                      },
                    },
                    {
                      label: "删除…",
                      icon: "trash",
                      danger: true,
                      onClick: () => {
                        const item = selectedItem ?? {
                          id: note.data.id,
                          title: note.data.title,
                          orphan: false,
                          cycle: false,
                          children: [],
                        };
                        requestDelete(item);
                      },
                    },
                  ]}
                />
              </div>

              {shortcutBanners}
              {updateBannerEl}

              {external === "modified" && (
                <div className="banner">
                  <span>文件已在外部修改，而这里还有没保存的改动。</span>
                  <button
                    type="button"
                    onClick={() => {
                      const id = note.data.id;
                      setExternal("none");
                      void openNote(id);
                    }}
                  >
                    载入外部版本
                  </button>
                  <button type="button" onClick={() => setExternal("none")}>
                    保留我的
                  </button>
                </div>
              )}

              {external === "deleted" && (
                <div className="banner">
                  <span>这条笔记已在外部被删除。</span>
                  <button
                    type="button"
                    onClick={() => {
                      setSelectedId(null);
                      setNote(null);
                      setBody("");
                      setSavedBody("");
                      setExternal("none");
                    }}
                  >
                    关闭
                  </button>
                </div>
              )}

              <div className="editor-area">
                <div className="page">
                  <input
                    ref={titleRef}
                    className="page-title"
                    value={titleDraft}
                    placeholder="未命名笔记"
                    onChange={(e) => setTitleDraft(e.target.value)}
                    onBlur={commitTitle}
                    onKeyDown={(e) => {
                      if (e.nativeEvent.isComposing) {
                        return;
                      }
                      if (e.key === "Enter") {
                        e.preventDefault();
                        e.currentTarget.blur();
                        // 回车后进入正文继续写。
                        const editable = document.querySelector<HTMLElement>(
                          ".page .ProseMirror, .page .source-editor",
                        );
                        editable?.focus();
                      } else if (e.key === "Escape") {
                        e.preventDefault();
                        setTitleDraft(note.data.title);
                        e.currentTarget.blur();
                      }
                    }}
                  />
                  <div className="page-meta">
                    {note.data.updated && <span>更新于 {formatTime(note.data.updated)}</span>}
                    {selectedItem && (selectedItem.children?.length ?? 0) > 0 && (
                      <span>{selectedItem.children?.length} 条子笔记</span>
                    )}
                  </div>
                  {mode === "wysiwyg" ? (
                    <Editor
                      key={`${note.data.id}-${note.revision}`}
                      noteId={note.data.id}
                      initialBody={body}
                      onChange={onBodyChange}
                    />
                  ) : (
                    <textarea
                      className="source-editor"
                      value={body}
                      spellCheck={false}
                      onChange={(e) => onBodyChange(e.target.value)}
                    />
                  )}
                </div>
              </div>
            </>
          )}
        </main>
      )}

      {error && (
        <div className="toast" role="alert" onClick={() => setError(null)}>
          {error}
        </div>
      )}

      {settingsOpen && (
        <Settings
          onClose={() => {
            setSettingsOpen(false);
            setSettingsFocusUpdate(false);
          }}
          onStatusChange={applyShortcutStatus}
          focusUpdate={settingsFocusUpdate}
        />
      )}

      {modal.kind === "delete" && (
        <div className="modal-mask" onMouseDown={() => setModal({ kind: "none" })}>
          <div className="modal" onMouseDown={(e) => e.stopPropagation()}>
            <h2>删除「{modal.item.title}」？</h2>
            <p>
              它下面还有 {modal.childCount} 条子笔记，会一起删除，删除后无法恢复。
            </p>
            <div className="modal-buttons">
              <button type="button" className="btn" onClick={() => setModal({ kind: "none" })}>
                取消
              </button>
              <button
                type="button"
                className="btn btn-danger"
                onClick={() => void deleteNote(modal.item, true)}
              >
                全部删除
              </button>
            </div>
          </div>
        </div>
      )}
    </div>
  );
}

/** 把 RFC3339 时间显示成「今天 10:12」「10月27日 10:12」或带年份。 */
function formatTime(value: string): string {
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) {
    return value;
  }
  const now = new Date();
  const hm = `${String(date.getHours()).padStart(2, "0")}:${String(date.getMinutes()).padStart(2, "0")}`;
  if (date.toDateString() === now.toDateString()) {
    return `今天 ${hm}`;
  }
  const md = `${date.getMonth() + 1}月${date.getDate()}日`;
  if (date.getFullYear() === now.getFullYear()) {
    return `${md} ${hm}`;
  }
  return `${date.getFullYear()}年${md}`;
}
