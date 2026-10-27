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
import { Editor } from "./components/LazyEditor";
import { Settings } from "./components/Settings";
import { TodoView } from "./components/TodoView";
import { ReportView } from "./components/ReportView";
import { CalendarView } from "./components/CalendarView";
import { Icon } from "./components/Icon";
import type { IconName } from "./components/Icon";
import { MoreMenu } from "./components/Menu";
import { ExtractTodos } from "./components/ExtractTodos";
import { FOLDER_TYPE, isFolder, keyMove } from "./lib/tree";
import type { MoveTarget } from "./lib/tree";
import { loadMainView, saveMainView } from "./lib/todo";
import type { MainView } from "./lib/todo";

/** 侧栏里的主视图，顺序即 ⌘1、⌘2… 的快捷键顺序；以后加视图往这里加一行。 */
const MAIN_VIEWS: { key: MainView; label: string; icon: IconName }[] = [
  { key: "notes", label: "笔记", icon: "note" },
  { key: "todo", label: "待办", icon: "todo" },
  { key: "report", label: "周报", icon: "report" },
  { key: "calendar", label: "日历", icon: "calendar" },
];

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

/** 树里找不到节点时（例如树还没刷新）用笔记元数据拼一个，供删除确认等使用。 */
function fallbackItem(data: NoteData): TreeItem {
  return {
    id: data.id,
    title: data.title,
    type: data.type,
    orphan: false,
    cycle: false,
    children: [],
  };
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
  // 从日历跳转进来时用 key 强制重挂目标视图，使初始周/人员/待办生效。
  const [reportJump, setReportJump] = useState<{ week: string; personId: string; level: string; key: number } | null>(null);
  const [todoJump, setTodoJump] = useState<{ id: string; key: number } | null>(null);
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
  // 正在写盘的正文：写盘触发的文件变化事件可能早于保存调用返回。
  const savingBodyRef = useRef<string | null>(null);
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
    savingBodyRef.current = content;
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
      savingBodyRef.current = null;
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
          if (
            latest.body === bodyRef.current ||
            latest.body === savedBodyRef.current ||
            latest.body === savingBodyRef.current
          ) {
            // 自己写盘引起的变化（保存、新建、重命名等）：磁盘正文是本地已保存
            // 或正在保存的版本，之后的输入照常等防抖保存，只同步元数据。
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
  // 当前打开的是目录：目录没有正文，右侧显示目录视图而不是编辑器。
  const folder = note?.data.type === FOLDER_TYPE;
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

  const [extractFor, setExtractFor] = useState<string | null>(null);
  const [notice, setNotice] = useState<string | null>(null);

  /** 打开本周周会笔记（没有就在「周会」节点下新建）。 */
  const openMeeting = useCallback(async () => {
    try {
      await saveNow();
      const meeting = await WorkspaceService.OpenMeeting();
      const items = await WorkspaceService.Tree();
      setTree(items ?? []);
      expandTo(meeting.id, items ?? []);
      await openNote(meeting.id);
    } catch (err) {
      setError(`打开周会失败：${String(err)}`);
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [openNote, saveNow]);

  /** 移动笔记（拖拽或 ⌥⌘ 方向键），只改 parent / order。 */
  const moveNote = useCallback(
    async (target: MoveTarget) => {
      try {
        await WorkspaceService.Move(target.id, target.parent, target.pos);
        if (target.parent) {
          setExpanded((prev) => new Set(prev).add(target.parent));
        }
        await refreshTree();
      } catch (err) {
        const msg = String(err);
        if (msg.includes("成环")) {
          setError("不能把笔记移到它自己或它的子笔记下面");
        } else if (msg.includes("只有目录")) {
          setError("只有目录能包含笔记，不能把笔记放进另一条笔记");
        } else {
          setError(`移动笔记失败：${msg}`);
        }
      }
    },
    [refreshTree],
  );

  // ⌥⌘↑/↓ 同级上下移动，⌥⌘←/→ 升级 / 降级（焦点在输入框或编辑器里时不处理）。
  useEffect(() => {
    const onKeyDown = (e: KeyboardEvent) => {
      if (view !== "notes" || !selectedId || !e.altKey || !(e.metaKey || e.ctrlKey) || e.shiftKey) {
        return;
      }
      const el = e.target as HTMLElement | null;
      if (el && (el.isContentEditable || el.tagName === "INPUT" || el.tagName === "TEXTAREA")) {
        return;
      }
      const dir = ({ ArrowUp: "up", ArrowDown: "down", ArrowLeft: "left", ArrowRight: "right" } as const)[
        e.key as "ArrowUp" | "ArrowDown" | "ArrowLeft" | "ArrowRight"
      ];
      if (!dir) return;
      e.preventDefault();
      const target = keyMove(tree, selectedId, dir);
      if (target) void moveNote(target);
    };
    window.addEventListener("keydown", onKeyDown);
    return () => window.removeEventListener("keydown", onKeyDown);
  }, [moveNote, selectedId, tree, view]);

  /** 新建笔记：直接建一条「未命名笔记」并聚焦标题，省掉弹窗。parent 必须为空或是目录。 */
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

  /** 新建目录：直接建一个「未命名目录」并聚焦标题；目录没有正文，右侧是目录视图。 */
  const createFolder = useCallback(
    async (parent: string) => {
      try {
        await saveNow();
        const created = await WorkspaceService.CreateFolder("", parent);
        if (parent) {
          setExpanded((prev) => new Set(prev).add(parent));
        }
        await refreshTree();
        focusTitleRef.current = true;
        await openNote(created.id);
      } catch (err) {
        setError(`新建目录失败：${String(err)}`);
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

  /** 从日历月视图格子跳到该人该周的周报（框架组长 tab）。 */
  const openReportFromCalendar = useCallback((week: string, personId: string, level: string) => {
    setReportJump({ week, personId, level, key: Date.now() });
    setView("report");
  }, []);

  /** 从日历日视图跳到待办视图并打开该待办。 */
  const openTodoFromCalendar = useCallback((id: string) => {
    setTodoJump({ id, key: Date.now() });
    setView("todo");
  }, []);

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

  // 全局快捷键：⌘S 立即保存，⌘1/⌘2 切换视图，笔记视图下 ⌘N 新建笔记（选中目录时建在目录里）。
  useEffect(() => {
    const onKeyDown = (e: KeyboardEvent) => {
      if (!(e.metaKey || e.ctrlKey) || e.altKey || e.shiftKey) {
        return;
      }
      const key = e.key.toLowerCase();
      if (key === "s") {
        e.preventDefault();
        void saveNow();
      } else if (/^[1-9]$/.test(key) && MAIN_VIEWS[Number(key) - 1]) {
        e.preventDefault();
        switchView(MAIN_VIEWS[Number(key) - 1].key);
      } else if (key === "n" && view === "notes" && workspace) {
        e.preventDefault();
        // 选中目录时在目录里建，否则建在根层级。
        void createNote(selectedItem && isFolder(selectedItem) ? selectedItem.id : "");
      }
    };
    window.addEventListener("keydown", onKeyDown);
    return () => window.removeEventListener("keydown", onKeyDown);
  }, [createNote, saveNow, selectedItem, switchView, view, workspace]);


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
        {/* 工作台是内部实现，不在界面上暴露：这里只留拖拽区，切换入口在「设置 → 工作台」。 */}
        <div className="sidebar-top" />

        <nav className="main-nav" aria-label="视图">
          {MAIN_VIEWS.map((item, i) => (
            <button
              key={item.key}
              type="button"
              className={`main-nav-item${view === item.key ? " is-active" : ""}`}
              onClick={() => switchView(item.key)}
            >
              <Icon name={item.icon} />
              <span className="main-nav-label">{item.label}</span>
              <kbd className="main-nav-key">⌘{i + 1}</kbd>
            </button>
          ))}
        </nav>

        {view === "notes" ? (
          <>
            <div className="sidebar-section">
              <span>全部笔记</span>
              <span className="sidebar-section-spacer" />
              <button
                type="button"
                className="icon-btn"
                title="本周周会"
                disabled={!workspace}
                onClick={() => void openMeeting()}
              >
                <Icon name="calendar" size={14} />
              </button>
              <button
                type="button"
                className="icon-btn"
                title="新建笔记（⌘N）"
                disabled={!workspace}
                onClick={() => void createNote("")}
              >
                <Icon name="plus" size={14} />
              </button>
              <button
                type="button"
                className="icon-btn"
                title="新建目录"
                disabled={!workspace}
                onClick={() => void createFolder("")}
              >
                <Icon name="folder" size={14} />
              </button>
            </div>
            {workspace ? (
              <Tree
                onMove={(target) => void moveNote(target)}
                items={tree}
                selectedId={selectedId}
                expanded={expanded}
                onToggle={toggleExpanded}
                onSelect={(id) => void selectNote(id)}
                onAddChild={(parent) => {
                  expandTo(parent, tree);
                  void createNote(parent);
                }}
                onAddFolder={(parent) => {
                  expandTo(parent, tree);
                  void createFolder(parent);
                }}
                onCreateRoot={() => void createNote("")}
                onCreateFolderRoot={() => void createFolder("")}
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
        ) : view === "report" || view === "calendar" ? (
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
            <TodoView
              key={todoJump ? `${workspace}-${todoJump.key}` : workspace}
              onOpenNote={openNoteFromTodo}
              navHost={todoNavHost}
              initialTodoId={todoJump?.id}
            />
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
            <ReportView
              key={reportJump ? `${workspace}-${reportJump.key}` : workspace}
              initialWeek={reportJump?.week}
              initialPersonId={reportJump?.personId}
              initialLevel={reportJump?.level}
            />
          </div>
        ) : (
          <main className="content">
            <div className="toolbar" />
            {welcome}
          </main>
        )
      ) : view === "calendar" ? (
        workspace ? (
          <div className="report-shell">
            {shortcutBanners}
            <CalendarView
              onOpenReport={openReportFromCalendar}
              onOpenNote={openNoteFromTodo}
              onOpenTodo={openTodoFromCalendar}
            />
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
                {!folder && (
                  <span className={`save-state is-${status}`}>
                    <span className="save-dot" />
                    {statusText}
                  </span>
                )}
                {/* 目录没有正文，不提供源码模式。 */}
                {!folder && (
                  <button
                    type="button"
                    className={`icon-btn${mode === "source" ? " is-on" : ""}`}
                    title={mode === "source" ? "切回所见即所得" : "查看 Markdown 源码"}
                    onClick={() => setMode(mode === "source" ? "wysiwyg" : "source")}
                  >
                    <Icon name="code" />
                  </button>
                )}
                <MoreMenu
                  items={
                    folder
                      ? [
                          { label: "重命名", icon: "edit", onClick: () => titleRef.current?.select() },
                          {
                            label: "新建笔记",
                            icon: "note",
                            onClick: () => {
                              expandTo(note.data.id, tree);
                              void createNote(note.data.id);
                            },
                          },
                          {
                            label: "新建子目录",
                            icon: "folder",
                            onClick: () => {
                              expandTo(note.data.id, tree);
                              void createFolder(note.data.id);
                            },
                          },
                          {
                            label: "删除…",
                            icon: "trash",
                            danger: true,
                            onClick: () => requestDelete(selectedItem ?? fallbackItem(note.data)),
                          },
                        ]
                      : [
                          { label: "重命名", icon: "edit", onClick: () => titleRef.current?.select() },
                          {
                            label: "从此笔记抽待办",
                            icon: "todo",
                            onClick: () => {
                              void saveNow().then(() => setExtractFor(note.data.id));
                            },
                          },
                          {
                            label: "删除…",
                            icon: "trash",
                            danger: true,
                            onClick: () => requestDelete(selectedItem ?? fallbackItem(note.data)),
                          },
                        ]
                  }
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
                    placeholder={folder ? "未命名目录" : "未命名笔记"}
                    onChange={(e) => setTitleDraft(e.target.value)}
                    onBlur={commitTitle}
                    onKeyDown={(e) => {
                      if (e.nativeEvent.isComposing) {
                        return;
                      }
                      if (e.key === "Enter") {
                        e.preventDefault();
                        e.currentTarget.blur();
                        // 回车后进入正文继续写（目录没有正文）。
                        if (!folder) {
                          const editable = document.querySelector<HTMLElement>(
                            ".page .ProseMirror, .page .source-editor",
                          );
                          editable?.focus();
                        }
                      } else if (e.key === "Escape") {
                        e.preventDefault();
                        setTitleDraft(note.data.title);
                        e.currentTarget.blur();
                      }
                    }}
                  />
                  {/* 目录没有正文，只显示里面有什么，并提供「在此新建笔记」。 */}
                  {folder ? (
                    <FolderView
                      item={selectedItem}
                      onAddNote={() => {
                        // 与笔记里「新建子笔记」同一手感：先展到该目录，再建并选中新笔记。
                        expandTo(note.data.id, tree);
                        void createNote(note.data.id);
                      }}
                      onAddFolder={() => {
                        expandTo(note.data.id, tree);
                        void createFolder(note.data.id);
                      }}
                      onOpenChild={(id) => {
                        expandTo(note.data.id, tree);
                        void selectNote(id);
                      }}
                    />
                  ) : (
                    <>
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
                          fileDrop
                        />
                      ) : (
                        <textarea
                          className="source-editor"
                          value={body}
                          spellCheck={false}
                          onChange={(e) => onBodyChange(e.target.value)}
                        />
                      )}
                    </>
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
          workspace={workspace}
          onChangeWorkspace={() => void chooseWorkspace()}
        />
      )}

      {modal.kind === "delete" && (
        <div className="modal-mask" onMouseDown={() => setModal({ kind: "none" })}>
          <div className="modal" onMouseDown={(e) => e.stopPropagation()}>
            <h2>删除{isFolder(modal.item) ? "目录" : "笔记"}「{modal.item.title}」？</h2>
            <p>
              {isFolder(modal.item) ? "目录" : "它"}下面还有 {modal.childCount} 项内容，会一起删除，删除后无法恢复。
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
      {extractFor && (
        <ExtractTodos
          noteId={extractFor}
          onClose={() => setExtractFor(null)}
          onDone={(n) => {
            setExtractFor(null);
            setNotice(n > 0 ? `已创建 ${n} 条待办，可在待办里查看` : "没有新建待办");
          }}
        />
      )}
      {notice && (
        <div className="toast" role="alert" onClick={() => setNotice(null)}>
          {notice}
        </div>
      )}
    </div>
  );
}

interface FolderViewProps {
  /** 树里的目录节点，拿子项列表用；还没刷新时为 null。 */
  item: TreeItem | null;
  /** 在此新建笔记。 */
  onAddNote: () => void;
  /** 在此新建子目录。 */
  onAddFolder: () => void;
  /** 打开子节点。 */
  onOpenChild: (id: string) => void;
}

/**
 * 目录视图：目录没有正文，这里只列出里面的内容，并给出「在此新建笔记」。
 */
function FolderView({ item, onAddNote, onAddFolder, onOpenChild }: FolderViewProps) {
  const children = item?.children ?? [];
  return (
    <div className="folder-view">
      <p className="folder-hint">
        {children.length === 0
          ? "这个目录还是空的，可以往里放笔记或子目录。"
          : `共 ${children.length} 项：${children.filter((c) => isFolder(c)).length} 个子目录、${
              children.filter((c) => !isFolder(c)).length
            } 条笔记。`}
      </p>
      <div className="folder-actions">
        <button type="button" className="btn btn-primary" onClick={onAddNote}>
          <Icon name="plus" size={14} />
          在此新建笔记
        </button>
        <button type="button" className="btn" onClick={onAddFolder}>
          <Icon name="folder" size={14} />
          新建子目录
        </button>
      </div>
      {children.length > 0 && (
        <ul className="folder-list">
          {children.map((child) => (
            <li key={child.id}>
              <button type="button" className="folder-entry" onClick={() => onOpenChild(child.id)}>
                <Icon name={isFolder(child) ? "folder" : "note"} size={14} />
                <span className="folder-entry-title">{child.title}</span>
              </button>
            </li>
          ))}
        </ul>
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
