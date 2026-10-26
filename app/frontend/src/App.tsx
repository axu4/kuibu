import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { Events } from "@wailsio/runtime";
import { WorkspaceService } from "../bindings/github.com/axu4/kuibu/app";
import type {
  NoteData,
  TreeItem,
} from "../bindings/github.com/axu4/kuibu/app";
import { Tree } from "./components/Tree";
import { Editor } from "./components/Editor";

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
  | { kind: "create"; parent: string; title: string }
  | { kind: "rename"; id: string; title: string }
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

  // 展开状态持久化。
  useEffect(() => {
    localStorage.setItem(EXPANDED_KEY, JSON.stringify([...expanded]));
  }, [expanded]);

  // 侧栏宽度持久化。
  useEffect(() => {
    localStorage.setItem(SIDEBAR_WIDTH_KEY, String(sidebarWidth));
  }, [sidebarWidth]);

  // ⌘S 立即保存。
  useEffect(() => {
    const onKeyDown = (e: KeyboardEvent) => {
      if ((e.metaKey || e.ctrlKey) && e.key.toLowerCase() === "s") {
        e.preventDefault();
        void saveNow();
      }
    };
    window.addEventListener("keydown", onKeyDown);
    return () => window.removeEventListener("keydown", onKeyDown);
  }, [saveNow]);

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

  /** 新建笔记。 */
  const createNote = useCallback(
    async (parent: string, title: string) => {
      try {
        const created = await WorkspaceService.Create(title, parent);
        if (parent) {
          setExpanded((prev) => new Set(prev).add(parent));
        }
        await refreshTree();
        await openNote(created.id);
        setModal({ kind: "none" });
      } catch (err) {
        setError(`新建笔记失败：${String(err)}`);
      }
    },
    [openNote, refreshTree],
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

  const selectedItem = useMemo(
    () => (selectedId ? findItem(tree, selectedId) : null),
    [tree, selectedId],
  );

  const statusText =
    status === "saved" ? "已保存" : status === "saving" ? "保存中…" : "未保存";

  return (
    <div className="app">
      <aside className="sidebar" style={{ width: sidebarWidth }}>
        <div className="sidebar-header">
          <span className="sidebar-title" title={workspace || "未打开工作台"}>
            {workspace ? workspace.split("/").filter(Boolean).pop() : "跬步"}
          </span>
          <div className="sidebar-actions">
            <button
              type="button"
              className="icon-btn"
              title="选择或新建工作台文件夹"
              onClick={() => void chooseWorkspace()}
            >
              ⌂
            </button>
            <button
              type="button"
              className="icon-btn"
              title="新建根笔记"
              disabled={!workspace}
              onClick={() => setModal({ kind: "create", parent: "", title: "" })}
            >
              +
            </button>
          </div>
        </div>
        <Tree
          items={tree}
          selectedId={selectedId}
          expanded={expanded}
          onToggle={toggleExpanded}
          onSelect={(id) => void selectNote(id)}
          onAddChild={(parent) => {
            expandTo(parent, tree);
            setModal({ kind: "create", parent, title: "" });
          }}
          onRename={(item) => setRenamingId(item.id)}
          onDelete={requestDelete}
          renamingId={renamingId}
          onRenameSubmit={(id, title) => void renameNote(id, title)}
          onRenameCancel={() => setRenamingId(null)}
        />
        <div className="resize-handle" onMouseDown={startResize} />
      </aside>

      <main className="content">
        {!workspace ? (
          <div className="welcome">
            <h1>跬步</h1>
            <p>
              工作台里的 Markdown 文件是唯一事实来源。
              <br />
              先选择或新建一个工作台文件夹开始记录。
            </p>
            <button
              type="button"
              className="primary-btn"
              onClick={() => void chooseWorkspace()}
            >
              选择或新建工作台文件夹
            </button>
          </div>
        ) : !note ? (
          <div className="welcome">
            <h1>跬步</h1>
            <p>从左侧选择一条笔记，或新建一条。</p>
            <button
              type="button"
              className="primary-btn"
              onClick={() => setModal({ kind: "create", parent: "", title: "" })}
            >
              新建根笔记
            </button>
          </div>
        ) : (
          <>
            <div className="content-header">
              <span className="note-title" title={note.data.title}>
                {note.data.title}
              </span>
              <div className="mode-toggle">
                <button
                  type="button"
                  className={mode === "wysiwyg" ? "is-active" : ""}
                  onClick={() => setMode("wysiwyg")}
                >
                  所见即所得
                </button>
                <button
                  type="button"
                  className={mode === "source" ? "is-active" : ""}
                  onClick={() => setMode("source")}
                >
                  源码
                </button>
              </div>
              <button
                type="button"
                className="icon-btn"
                title="重命名"
                onClick={() =>
                  setModal({ kind: "rename", id: note.data.id, title: note.data.title })
                }
              >
                ✎
              </button>
              <button
                type="button"
                className="icon-btn"
                title="删除"
                onClick={() => {
                  const item = selectedItem ?? {
                    id: note.data.id,
                    title: note.data.title,
                    orphan: false,
                    cycle: false,
                    children: [],
                  };
                  requestDelete(item);
                }}
              >
                ⌫
              </button>
            </div>

            {external === "modified" && (
              <div className="banner">
                <span>文件已在外部修改，当前还有未保存的修改。</span>
                <button
                  type="button"
                  onClick={() => {
                    const id = note.data.id;
                    setExternal("none");
                    void openNote(id);
                  }}
                >
                  重新加载
                </button>
                <button
                  type="button"
                  onClick={() => setExternal("none")}
                >
                  保留我的
                </button>
              </div>
            )}

            {external === "deleted" && (
              <div className="banner">
                <span>当前笔记已在外部被删除。</span>
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

            <div className="status-bar">
              <span className={`status-dot${status === "dirty" ? " is-dirty" : status === "saving" ? " is-saving" : ""}`} />
              <span>{statusText}</span>
              <span style={{ marginLeft: "auto" }}>{note.data.id}</span>
            </div>
          </>
        )}
      </main>

      {error && (
        <div className="modal-mask" onClick={() => setError(null)}>
          <div className="modal" onClick={(e) => e.stopPropagation()}>
            <h2>出错了</h2>
            <p>{error}</p>
            <div className="modal-buttons">
              <button type="button" className="is-primary" onClick={() => setError(null)}>
                知道了
              </button>
            </div>
          </div>
        </div>
      )}

      {modal.kind === "create" && (
        <TitleModal
          title={modal.parent ? "新建子笔记" : "新建根笔记"}
          initial=""
          placeholder="笔记标题"
          confirmText="新建"
          onCancel={() => setModal({ kind: "none" })}
          onConfirm={(title) => void createNote(modal.parent, title)}
        />
      )}

      {modal.kind === "rename" && (
        <TitleModal
          title="重命名笔记"
          initial={modal.title}
          placeholder="笔记标题"
          confirmText="保存"
          onCancel={() => setModal({ kind: "none" })}
          onConfirm={(title) => {
            setModal({ kind: "none" });
            void renameNote(modal.id, title);
          }}
        />
      )}

      {modal.kind === "delete" && (
        <div className="modal-mask">
          <div className="modal">
            <h2>删除笔记</h2>
            <p>
              「{modal.item.title}」下还有 {modal.childCount} 条子笔记，删除后会一起删掉，且无法撤销。
            </p>
            <div className="modal-buttons">
              <button type="button" onClick={() => setModal({ kind: "none" })}>
                取消
              </button>
              <button
                type="button"
                className="is-danger"
                onClick={() => void deleteNote(modal.item, true)}
              >
                递归删除
              </button>
            </div>
          </div>
        </div>
      )}
    </div>
  );
}

interface TitleModalProps {
  title: string;
  initial: string;
  placeholder: string;
  confirmText: string;
  onCancel: () => void;
  onConfirm: (title: string) => void;
}

/** 输入标题的简易弹窗。 */
function TitleModal(props: TitleModalProps) {
  const [value, setValue] = useState(props.initial);
  const inputRef = useRef<HTMLInputElement | null>(null);

  useEffect(() => {
    inputRef.current?.focus();
    inputRef.current?.select();
  }, []);

  const submit = () => {
    const trimmed = value.trim();
    if (!trimmed) {
      return;
    }
    props.onConfirm(trimmed);
  };

  return (
    <div className="modal-mask" onMouseDown={props.onCancel}>
      <div className="modal" onMouseDown={(e) => e.stopPropagation()}>
        <h2>{props.title}</h2>
        <input
          ref={inputRef}
          value={value}
          placeholder={props.placeholder}
          onChange={(e) => setValue(e.target.value)}
          onKeyDown={(e) => {
            if (e.key === "Enter") {
              e.preventDefault();
              submit();
            } else if (e.key === "Escape") {
              e.preventDefault();
              props.onCancel();
            }
          }}
        />
        <div className="modal-buttons">
          <button type="button" onClick={props.onCancel}>
            取消
          </button>
          <button type="button" className="is-primary" onClick={submit}>
            {props.confirmText}
          </button>
        </div>
      </div>
    </div>
  );
}
