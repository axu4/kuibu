import { useCallback, useEffect, useRef, useState } from "react";
import { TodoService } from "../../bindings/github.com/axu4/kuibu/app";
import type { TodoItem } from "../../bindings/github.com/axu4/kuibu/app";
import { Editor } from "./Editor";
import { formatDueLong, linkDisplay, readableError, statusLabel } from "../lib/todo";

/** 正文自动保存防抖时间（与笔记编辑器一致）。 */
const SAVE_DEBOUNCE_MS = 600;

interface TodoDetailProps {
  /** 打开时的待办（含正文、子任务、链接、附件）。 */
  item: TodoItem;
  /** 每次父级刷新详情时递增，用于识别外部变化。 */
  revision: number;
  onClose: () => void;
  /** 本地修改成功后把最新详情交给父级。 */
  onChange: (item: TodoItem) => void;
  /** 外部变化后要求父级整体刷新。 */
  onRefresh: () => void;
  /** 点击来源笔记：切到笔记视图并打开。 */
  onOpenNote: (id: string) => void;
  onError: (message: string) => void;
}

const STATUS_OPTIONS = ["todo", "doing", "done", "canceled"] as const;

/**
 * 待办详情面板。
 *
 * 字段各自维护本地编辑态：标题、截止、正文、标签在「用户正在编辑」时
 * 不被外部刷新打断（与笔记编辑器同一套思路），其余字段直接跟随 props。
 */
export function TodoDetail({
  item,
  revision,
  onClose,
  onChange,
  onRefresh,
  onOpenNote,
  onError,
}: TodoDetailProps) {
  const [detail, setDetail] = useState<TodoItem>(item);
  const [title, setTitle] = useState(item.title);
  const [body, setBody] = useState(item.body);
  const [savedBody, setSavedBody] = useState(item.body);
  const [dueText, setDueText] = useState(item.due);
  const [tagInput, setTagInput] = useState("");
  const [childInput, setChildInput] = useState("");
  const [linkURL, setLinkURL] = useState("");
  const [linkTitle, setLinkTitle] = useState("");
  // 编辑器只在外部把正文改成别的值时重建（key 变化），避免打断本地输入。
  const [editorRev, setEditorRev] = useState(0);
  const [busy, setBusy] = useState(false);
  const [confirmDone, setConfirmDone] = useState<number | null>(null);
  const [confirmDelete, setConfirmDelete] = useState(false);
  const [dropping, setDropping] = useState(false);

  const bodyRef = useRef(body);
  bodyRef.current = body;
  const savedBodyRef = useRef(savedBody);
  savedBodyRef.current = savedBody;
  const titleEditing = useRef(false);
  const dueEditing = useRef(false);
  const saveTimer = useRef<ReturnType<typeof setTimeout> | null>(null);
  const savingRef = useRef(false);

  // 外部刷新：只在本地没有未保存修改时采用新值，避免打断编辑。
  useEffect(() => {
    setDetail(item);
    if (!titleEditing.current) {
      setTitle(item.title);
    }
    if (!dueEditing.current) {
      setDueText(item.due);
    }
    if (bodyRef.current === savedBodyRef.current && item.body !== bodyRef.current) {
      bodyRef.current = item.body;
      savedBodyRef.current = item.body;
      setBody(item.body);
      setSavedBody(item.body);
      setEditorRev((prev) => prev + 1);
    }
    // revision 变化代表父级刷新过详情；item 是同一份新数据。
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [revision]);

  /** 立即保存正文。 */
  const saveBody = useCallback(async () => {
    if (saveTimer.current) {
      clearTimeout(saveTimer.current);
      saveTimer.current = null;
    }
    if (savingRef.current || bodyRef.current === savedBodyRef.current) {
      return;
    }
    const content = bodyRef.current;
    savingRef.current = true;
    try {
      await TodoService.UpdateBody(item.id, content);
      savedBodyRef.current = content;
      setSavedBody(content);
    } catch (err) {
      onError(`保存详情失败：${readableError(err)}`);
    } finally {
      savingRef.current = false;
    }
  }, [item.id, onError]);

  // 关闭面板前保存正文。
  useEffect(() => {
    return () => {
      if (saveTimer.current) {
        clearTimeout(saveTimer.current);
        saveTimer.current = null;
      }
      if (bodyRef.current !== savedBodyRef.current) {
        void TodoService.UpdateBody(item.id, bodyRef.current).catch(() => {
          // 关闭时的保存失败只能下次再试。
        });
      }
    };
  }, [item.id]);

  const onBodyChange = useCallback((next: string) => {
    setBody(next);
    bodyRef.current = next;
    if (saveTimer.current) {
      clearTimeout(saveTimer.current);
    }
    saveTimer.current = setTimeout(() => {
      saveTimer.current = null;
      void saveBody();
    }, SAVE_DEBOUNCE_MS);
  }, [saveBody]);

  /** 统一的写操作包装：成功后更新本地详情并通知父级。 */
  const run = useCallback(
    async (fn: () => Promise<TodoItem>, failure: string) => {
      setBusy(true);
      try {
        const next = await fn();
        setDetail(next);
        if (!titleEditing.current) {
          setTitle(next.title);
        }
        if (!dueEditing.current) {
          setDueText(next.due);
        }
        onChange(next);
        return next;
      } catch (err) {
        onError(`${failure}：${readableError(err)}`);
        return null;
      } finally {
        setBusy(false);
      }
    },
    [onChange, onError],
  );

  /** 提交标题（回车或失焦）。 */
  const commitTitle = useCallback(async () => {
    titleEditing.current = false;
    const next = title.trim();
    if (!next || next === detail.title) {
      setTitle(detail.title);
      return;
    }
    const updated = await run(() => TodoService.UpdateTitle(item.id, next), "修改标题失败");
    if (!updated) {
      setTitle(detail.title);
    }
  }, [detail.title, item.id, run, title]);

  /** 提交截止文本（空 = 清空）。 */
  const commitDue = useCallback(async () => {
    dueEditing.current = false;
    const raw = dueText.trim();
    if (raw === detail.due) {
      return;
    }
    const updated = await run(() => TodoService.UpdateDue(item.id, raw), "修改截止失败");
    if (!updated) {
      setDueText(detail.due);
    }
  }, [detail.due, dueText, item.id, run]);

  /** 用日期/时间控件修改截止。 */
  const setDueParts = useCallback(
    async (date: string, time: string) => {
      const value = date ? (time ? `${date} ${time}` : date) : "";
      dueEditing.current = true;
      setDueText(value);
      dueEditing.current = false;
      const updated = await run(() => TodoService.UpdateDue(item.id, value), "修改截止失败");
      if (!updated) {
        setDueText(detail.due);
      }
    },
    [detail.due, item.id, run],
  );

  /** 切换状态；完成父任务且仍有未完成子任务时先确认。 */
  const changeStatus = useCallback(
    async (status: string) => {
      if (status === detail.status) {
        return;
      }
      if (status === "done") {
        const open = await TodoService.OpenChildrenCount(item.id).catch(() => 0);
        if (open > 0) {
          setConfirmDone(open);
          return;
        }
      }
      await run(() => TodoService.SetStatus(item.id, status, false), "修改状态失败");
    },
    [detail.status, item.id, run],
  );

  const finishWithChildren = useCallback(
    async (withChildren: boolean) => {
      setConfirmDone(null);
      await run(
        () => TodoService.SetStatus(item.id, "done", withChildren),
        "修改状态失败",
      );
    },
    [item.id, run],
  );

  /** 新增标签：支持空格/逗号分隔，可带 #。 */
  const addTags = useCallback(async () => {
    const parts = tagInput
      .split(/[\s,，]+/)
      .map((t) => t.replace(/^#+/, "").trim())
      .filter(Boolean);
    if (parts.length === 0) {
      return;
    }
    setTagInput("");
    await run(
      () => TodoService.UpdateTags(item.id, [...(detail.tags ?? []), ...parts]),
      "添加标签失败",
    );
  }, [detail.tags, item.id, run, tagInput]);

  const removeTag = useCallback(
    async (name: string) => {
      await run(
        () => TodoService.UpdateTags(item.id, (detail.tags ?? []).filter((t) => t !== name)),
        "移除标签失败",
      );
    },
    [detail.tags, item.id, run],
  );

  /** 新增子任务：走同一个快速录入解析。 */
  const addChild = useCallback(async () => {
    const text = childInput.trim();
    if (!text) {
      return;
    }
    setChildInput("");
    setBusy(true);
    try {
      await TodoService.Create(text, item.id);
      const next = await TodoService.Get(item.id);
      setDetail(next);
      onChange(next);
    } catch (err) {
      onError(`添加子任务失败：${readableError(err)}`);
    } finally {
      setBusy(false);
    }
  }, [childInput, item.id, onChange, onError]);

  const toggleChild = useCallback(
    async (child: TodoItem) => {
      const closed = child.status === "done" || child.status === "canceled";
      setBusy(true);
      try {
        // 子任务的结果不能直接当成父任务详情，改完重新读一次父任务。
        await TodoService.SetStatus(child.id, closed ? "todo" : "done", false);
        const next = await TodoService.Get(item.id);
        setDetail(next);
        onChange(next);
        onRefresh();
      } catch (err) {
        onError(`更新子任务失败：${readableError(err)}`);
      } finally {
        setBusy(false);
      }
    },
    [item.id, onChange, onError, onRefresh],
  );

  const addLink = useCallback(async () => {
    const url = linkURL.trim();
    if (!url) {
      return;
    }
    const updated = await run(
      () => TodoService.AddLink(item.id, url, linkTitle.trim()),
      "添加链接失败",
    );
    if (updated) {
      setLinkURL("");
      setLinkTitle("");
    }
  }, [item.id, linkTitle, linkURL, run]);

  const openLink = useCallback(
    (url: string) => {
      void TodoService.OpenLink(url).catch((err) => {
        onError(`打开链接失败：${readableError(err)}`);
      });
    },
    [onError],
  );

  const chooseFile = useCallback(async () => {
    await run(() => TodoService.ChooseAndAttach(item.id), "添加附件失败");
  }, [item.id, run]);

  const openAttachment = useCallback(
    (rel: string) => {
      void TodoService.OpenAttachment(item.id, rel).catch((err) => {
        onError(`打开附件失败：${readableError(err)}`);
      });
    },
    [item.id, onError],
  );

  const detach = useCallback(
    async (rel: string) => {
      await run(() => TodoService.Detach(item.id, rel), "移除附件失败");
    },
    [item.id, run],
  );

  const children = detail.children ?? [];
  const tags = detail.tags ?? [];
  const links = detail.links ?? [];
  const attachments = detail.attachments ?? [];
  const stateText = savedBody === body ? "" : "未保存";
  const dueDate = /^\d{4}-\d{2}-\d{2}$/.test(detail.due.slice(0, 10))
    ? detail.due.slice(0, 10)
    : "";
  const dueTime = detail.due.length > 10 ? detail.due.slice(11, 16) : "";

  return (
    <aside
      className={`todo-detail${dropping ? " is-dropping" : ""}`}
      data-todo-id={item.id}
      data-file-drop-target="todo-detail"
      onDragEnter={() => setDropping(true)}
      onDragLeave={() => setDropping(false)}
      onDrop={() => setDropping(false)}
    >
      <div className="todo-detail-header">
        <span className="todo-detail-id" title={item.id}>
          {detail.source ? "来自笔记" : item.id}
        </span>
        <button
          type="button"
          className="icon-btn"
          title="删除待办"
          onClick={() => setConfirmDelete(true)}
        >
          ⌫
        </button>
        <button type="button" className="icon-btn" title="关闭详情" onClick={onClose}>
          ✕
        </button>
      </div>

      <div className="todo-detail-body">
        <input
          className="todo-detail-title"
          value={title}
          onChange={(e) => {
            titleEditing.current = true;
            setTitle(e.target.value);
          }}
          onFocus={() => {
            titleEditing.current = true;
          }}
          onBlur={() => void commitTitle()}
          onKeyDown={(e) => {
            if (e.key === "Enter" && !e.nativeEvent.isComposing) {
              e.preventDefault();
              e.currentTarget.blur();
            }
          }}
        />

        <div className="todo-detail-row">
          <span className="todo-detail-label">状态</span>
          <div className="todo-status-group">
            {STATUS_OPTIONS.map((s) => (
              <button
                key={s}
                type="button"
                className={`todo-status-btn${detail.status === s ? " is-active" : ""}`}
                disabled={busy}
                onClick={() => void changeStatus(s)}
              >
                {statusLabel(s)}
              </button>
            ))}
          </div>
        </div>

        <div className="todo-detail-row is-column">
          <span className="todo-detail-label">截止</span>
          <div className="todo-due-editor">
            <input
              className="todo-due-text"
              value={dueText}
              placeholder="如：明天下午三点（留空清除）"
              onChange={(e) => {
                dueEditing.current = true;
                setDueText(e.target.value);
              }}
              onFocus={() => {
                dueEditing.current = true;
              }}
              onBlur={() => void commitDue()}
              onKeyDown={(e) => {
                if (e.key === "Enter" && !e.nativeEvent.isComposing) {
                  e.preventDefault();
                  e.currentTarget.blur();
                }
              }}
            />
            <input
              className="todo-due-date"
              type="date"
              value={dueDate}
              onChange={(e) => void setDueParts(e.target.value, dueTime)}
            />
            <input
              className="todo-due-time"
              type="time"
              value={dueTime}
              disabled={!dueDate}
              onChange={(e) => void setDueParts(dueDate, e.target.value)}
            />
            {detail.due && (
              <button
                type="button"
                className="todo-link-btn"
                onClick={() => void run(() => TodoService.UpdateDue(item.id, ""), "清除截止失败")}
              >
                清除
              </button>
            )}
          </div>
          {detail.due && (
            <div className={`todo-due-hint${detail.overdue ? " is-overdue" : ""}`}>
              {formatDueLong(detail.due)}
              {detail.overdue ? " · 已逾期" : ""}
            </div>
          )}
        </div>

        <div className="todo-detail-row is-column">
          <span className="todo-detail-label">标签</span>
          <div className="todo-tag-editor">
            {tags.map((t) => (
              <span key={t} className="todo-tag-chip">
                #{t}
                <button type="button" title="移除标签" onClick={() => void removeTag(t)}>
                  ✕
                </button>
              </span>
            ))}
            <input
              className="todo-tag-input"
              value={tagInput}
              placeholder="加标签后回车"
              onChange={(e) => setTagInput(e.target.value)}
              onKeyDown={(e) => {
                if (e.key === "Enter" && !e.nativeEvent.isComposing) {
                  e.preventDefault();
                  void addTags();
                }
              }}
            />
          </div>
        </div>

        <div className="todo-detail-section">
          <div className="todo-detail-section-title">
            详情
            {stateText && <span className="todo-detail-saving">{stateText}</span>}
          </div>
          <div className="todo-detail-editor">
            <Editor
              key={`${item.id}-${editorRev}`}
              noteId={item.id}
              initialBody={body}
              onChange={onBodyChange}
            />
          </div>
        </div>

        <div className="todo-detail-section">
          <div className="todo-detail-section-title">
            子任务
            <span className="todo-detail-count">
              {children.filter((c) => c.status === "done").length}/{children.length}
            </span>
          </div>
          <div className="todo-subtasks">
            {children.length === 0 && <div className="todo-detail-empty">还没有子任务</div>}
            {children.map((child) => {
              const closed = child.status === "done" || child.status === "canceled";
              return (
                <div key={child.id} className={`todo-subtask${closed ? " is-closed" : ""}`}>
                  <input
                    type="checkbox"
                    checked={closed}
                    disabled={busy}
                    onChange={() => void toggleChild(child)}
                  />
                  <span className="todo-subtask-title">{child.title}</span>
                  {child.due && (
                    <span className={`todo-due${child.overdue ? " is-overdue" : ""}`}>
                      {formatDueLong(child.due).slice(5)}
                    </span>
                  )}
                </div>
              );
            })}
            <input
              className="todo-subtask-input"
              value={childInput}
              placeholder="添加子任务，回车即建"
              onChange={(e) => setChildInput(e.target.value)}
              onKeyDown={(e) => {
                if (e.key === "Enter" && !e.nativeEvent.isComposing) {
                  e.preventDefault();
                  void addChild();
                }
              }}
            />
          </div>
        </div>

        <div className="todo-detail-section">
          <div className="todo-detail-section-title">链接</div>
          {links.length === 0 && <div className="todo-detail-empty">还没有链接</div>}
          <div className="todo-links">
            {links.map((l) => (
              <div key={l.url} className="todo-link">
                <button
                  type="button"
                  className="todo-link-open"
                  title={l.url}
                  onClick={() => openLink(l.url)}
                >
                  {linkDisplay(l.url, l.title)}
                </button>
                <button
                  type="button"
                  className="todo-link-btn"
                  onClick={() => void run(() => TodoService.RemoveLink(item.id, l.url), "删除链接失败")}
                >
                  删除
                </button>
              </div>
            ))}
            <div className="todo-link-add">
              <input
                className="todo-link-url"
                value={linkURL}
                placeholder="https://…"
                onChange={(e) => setLinkURL(e.target.value)}
                onKeyDown={(e) => {
                  if (e.key === "Enter" && !e.nativeEvent.isComposing) {
                    e.preventDefault();
                    void addLink();
                  }
                }}
              />
              <input
                className="todo-link-title"
                value={linkTitle}
                placeholder="标题（可选）"
                onChange={(e) => setLinkTitle(e.target.value)}
                onKeyDown={(e) => {
                  if (e.key === "Enter" && !e.nativeEvent.isComposing) {
                    e.preventDefault();
                    void addLink();
                  }
                }}
              />
              <button type="button" className="todo-link-btn" disabled={busy} onClick={() => void addLink()}>
                添加
              </button>
            </div>
          </div>
        </div>

        <div className="todo-detail-section">
          <div className="todo-detail-section-title">
            附件
            <button type="button" className="todo-link-btn" disabled={busy} onClick={() => void chooseFile()}>
              选择文件
            </button>
          </div>
          {attachments.length === 0 && (
            <div className="todo-detail-empty">把文件拖到这里，或点「选择文件」</div>
          )}
          <div className="todo-attachments">
            {attachments.map((a) => (
              <div key={a.path} className="todo-attachment">
                <button
                  type="button"
                  className="todo-attachment-open"
                  title={a.path}
                  onClick={() => openAttachment(a.path)}
                >
                  {a.name}
                </button>
                <button
                  type="button"
                  className="todo-link-btn"
                  onClick={() => void detach(a.path)}
                >
                  移除
                </button>
              </div>
            ))}
          </div>
        </div>

        {detail.source && (
          <div className="todo-detail-section">
            <div className="todo-detail-section-title">来源笔记</div>
            <button
              type="button"
              className="todo-source"
              onClick={() => onOpenNote(detail.source)}
            >
              {detail.sourceTitle || detail.source}
            </button>
          </div>
        )}
      </div>

      {confirmDelete && (
        <div className="modal-mask" onClick={() => setConfirmDelete(false)}>
          <div className="modal" onClick={(e) => e.stopPropagation()}>
            <h2>删除待办</h2>
            <p>
              「{detail.title}」
              {detail.childTotal > 0
                ? `还有 ${detail.childTotal} 条子任务，会一起删掉，`
                : "删除后"}
              无法撤销。
            </p>
            <div className="modal-buttons">
              <button type="button" onClick={() => setConfirmDelete(false)}>
                取消
              </button>
              <button
                type="button"
                className="is-danger"
                disabled={busy}
                onClick={() => {
                  setConfirmDelete(false);
                  setBusy(true);
                  void TodoService.Delete(item.id, true)
                    .then(() => {
                      onRefresh();
                      onClose();
                    })
                    .catch((err) => onError(`删除待办失败：${readableError(err)}`))
                    .finally(() => setBusy(false));
                }}
              >
                删除
              </button>
            </div>
          </div>
        </div>
      )}

      {confirmDone !== null && (
        <div className="modal-mask" onClick={() => setConfirmDone(null)}>
          <div className="modal" onClick={(e) => e.stopPropagation()}>
            <h2>完成父任务</h2>
            <p>
              「{detail.title}」还有 {confirmDone} 条未完成的子任务，要一起完成吗？
            </p>
            <div className="modal-buttons">
              <button type="button" onClick={() => setConfirmDone(null)}>
                取消
              </button>
              <button type="button" onClick={() => void finishWithChildren(false)}>
                只完成父任务
              </button>
              <button type="button" className="is-primary" onClick={() => void finishWithChildren(true)}>
                一起完成
              </button>
            </div>
          </div>
        </div>
      )}
    </aside>
  );
}
