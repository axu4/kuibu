import { useCallback, useEffect, useRef, useState } from "react";
import { TodoService } from "../../bindings/github.com/axu4/kuibu/app";
import type { TodoItem } from "../../bindings/github.com/axu4/kuibu/app";
import { Editor } from "./LazyEditor";
import { Icon } from "./Icon";
import {
  PRIORITY_OPTIONS,
  formatDueLong,
  linkDisplay,
  priorityLabel,
  readableError,
  statusLabel,
} from "../lib/todo";

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
  // 白名单外或 file: 链接点击前的确认。
  const [confirmLink, setConfirmLink] = useState<{ url: string; scheme: string } | null>(null);
  const [dropping, setDropping] = useState(false);
  // 链接输入默认收起，点「添加链接」再展开。
  const [linkOpen, setLinkOpen] = useState(false);
  // 截止输入框不在编辑时显示友好格式（2026-10-27 周二 15:00），聚焦后换成原文。
  const [dueFocused, setDueFocused] = useState(false);

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
      setLinkOpen(false);
    }
  }, [item.id, linkTitle, linkURL, run]);

  const openLink = useCallback(
    async (url: string, confirmed = false) => {
      try {
        if (!confirmed) {
          const check = await TodoService.CheckLink(url);
          if (check.action === "deny") {
            onError(`不能打开这个链接：${check.reason}`);
            return;
          }
          if (check.action === "confirm") {
            setConfirmLink({ url, scheme: check.scheme });
            return;
          }
        }
        await TodoService.OpenLink(url, confirmed);
      } catch (err) {
        onError(`打开链接失败：${readableError(err)}`);
      }
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

  const closed = detail.status === "done" || detail.status === "canceled";

  // Esc 关闭面板；正在输入时 Esc 只退出输入框。
  useEffect(() => {
    const onKeyDown = (e: KeyboardEvent) => {
      if (e.key !== "Escape" || confirmDelete || confirmDone !== null || confirmLink) {
        return;
      }
      const target = e.target as HTMLElement | null;
      if (target?.closest("input, textarea, [contenteditable='true']")) {
        target.blur();
        return;
      }
      onClose();
    };
    window.addEventListener("keydown", onKeyDown);
    return () => window.removeEventListener("keydown", onKeyDown);
  }, [confirmDelete, confirmDone, confirmLink, onClose]);

  return (
    <aside
      className={`todo-detail${dropping ? " is-dropping" : ""}`}
      data-todo-id={item.id}
      data-file-drop-target="todo-detail"
      onDragEnter={() => setDropping(true)}
      onDragLeave={() => setDropping(false)}
      onDrop={() => setDropping(false)}
    >
      <div className="toolbar">
        <span className="detail-saving">{stateText}</span>
        <div className="toolbar-spacer" />
        <button
          type="button"
          className="icon-btn is-danger"
          title="删除待办"
          onClick={() => setConfirmDelete(true)}
        >
          <Icon name="trash" />
        </button>
        <button type="button" className="icon-btn" title="关闭（Esc）" onClick={onClose}>
          <Icon name="close" />
        </button>
      </div>

      <div className="detail-body">
        <div className="detail-title-row">
          <input
            type="checkbox"
            className="check is-large"
            checked={closed}
            disabled={busy}
            aria-label={closed ? "标为未完成" : "标为完成"}
            onChange={() => void changeStatus(closed ? "todo" : "done")}
          />
          <input
            className={`detail-title${closed ? " is-closed" : ""}`}
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
        </div>

        <div className="props">
          <div className="prop">
            <span className="prop-label">
              <Icon name="flag" size={14} />
              状态
            </span>
            <div className="segmented">
              {STATUS_OPTIONS.map((s) => (
                <button
                  key={s}
                  type="button"
                  className={`segmented-btn is-${s}${detail.status === s ? " is-active" : ""}`}
                  disabled={busy}
                  onClick={() => void changeStatus(s)}
                >
                  {statusLabel(s)}
                </button>
              ))}
            </div>
          </div>

          <div className="prop">
            <span className="prop-label">
              <Icon name="calendar" size={14} />
              截止
            </span>
            <div className="prop-value">
              <div className={`due-editor${detail.due ? " has-due" : ""}`}>
                <input
                  className={`field due-text${detail.overdue && !dueFocused ? " is-overdue" : ""}`}
                  value={
                    dueFocused || !detail.due
                      ? dueText
                      : `${formatDueLong(detail.due)}${detail.overdue ? " · 已逾期" : ""}`
                  }
                  placeholder="写「明天下午三点」或选日期"
                  onChange={(e) => {
                    dueEditing.current = true;
                    setDueText(e.target.value);
                  }}
                  onFocus={(e) => {
                    dueEditing.current = true;
                    setDueFocused(true);
                    const input = e.currentTarget;
                    setTimeout(() => input.select(), 0);
                  }}
                  onBlur={() => {
                    setDueFocused(false);
                    void commitDue();
                  }}
                  onKeyDown={(e) => {
                    if (e.key === "Enter" && !e.nativeEvent.isComposing) {
                      e.preventDefault();
                      e.currentTarget.blur();
                    }
                  }}
                />
                <input
                  className="field due-date"
                  type="date"
                  title="选择日期"
                  value={dueDate}
                  onChange={(e) => void setDueParts(e.target.value, dueTime)}
                />
                <input
                  className="field due-time"
                  type="time"
                  title="选择时间"
                  value={dueTime}
                  disabled={!dueDate}
                  onChange={(e) => void setDueParts(dueDate, e.target.value)}
                />
                {detail.due && (
                  <button
                    type="button"
                    className="icon-btn is-small"
                    title="清除截止"
                    onClick={() => void run(() => TodoService.UpdateDue(item.id, ""), "清除截止失败")}
                  >
                    <Icon name="close" size={12} />
                  </button>
                )}
              </div>
            </div>
          </div>

          <div className="prop">
            <span className="prop-label">
              <Icon name="tag" size={14} />
              标签
            </span>
            <div className="tag-editor">
              {tags.map((t) => (
                <span key={t} className="chip is-removable">
                  #{t}
                  <button type="button" title="移除标签" onClick={() => void removeTag(t)}>
                    <Icon name="close" size={10} />
                  </button>
                </span>
              ))}
              <input
                className="tag-input"
                value={tagInput}
                placeholder={tags.length ? "添加" : "添加标签，回车确认"}
                onChange={(e) => setTagInput(e.target.value)}
                onBlur={() => void addTags()}
                onKeyDown={(e) => {
                  if (e.key === "Enter" && !e.nativeEvent.isComposing) {
                    e.preventDefault();
                    void addTags();
                  }
                }}
              />
            </div>
          </div>

          <div className="prop">
            <span className="prop-label">
              <Icon name="bolt" size={14} />
              优先级
            </span>
            <div className="segmented">
              <button
                type="button"
                className={`segmented-btn${detail.priority ? "" : " is-active"}`}
                disabled={busy}
                onClick={() => void run(() => TodoService.UpdatePriority(item.id, ""), "修改优先级失败")}
              >
                不设
              </button>
              {PRIORITY_OPTIONS.map((p) => (
                <button
                  key={p}
                  type="button"
                  className={`segmented-btn is-priority-${p}${detail.priority === p ? " is-active" : ""}`}
                  disabled={busy}
                  onClick={() => void run(() => TodoService.UpdatePriority(item.id, p), "修改优先级失败")}
                >
                  {priorityLabel(p)}
                </button>
              ))}
            </div>
          </div>

          {detail.source && (
            <div className="prop">
              <span className="prop-label">
                <Icon name="note" size={14} />
                来源
              </span>
              {detail.sourceTitle ? (
                <button
                  type="button"
                  className="source-link"
                  title="打开这条笔记"
                  onClick={() => onOpenNote(detail.source)}
                >
                  {detail.sourceTitle}
                </button>
              ) : (
                <span className="source-text" title={detail.externalId || undefined}>
                  {detail.source === "mail" ? "邮件" : detail.source}
                </span>
              )}
            </div>
          )}
        </div>

        <div className="detail-editor">
          <Editor
            key={`${item.id}-${editorRev}`}
            noteId={item.id}
            initialBody={body}
            onChange={onBodyChange}
            placeholder="添加描述…"
          />
        </div>

        <section className="detail-section">
          <h3>
            子任务
            {children.length > 0 && (
              <span className="detail-count">
                {children.filter((c) => c.status === "done").length}/{children.length}
              </span>
            )}
          </h3>
          {children.map((child) => {
            const childClosed = child.status === "done" || child.status === "canceled";
            return (
              <div key={child.id} className={`subtask${childClosed ? " is-closed" : ""}`}>
                <input
                  type="checkbox"
                  className="check is-small"
                  checked={childClosed}
                  disabled={busy}
                  onChange={() => void toggleChild(child)}
                />
                <span className="subtask-title">{child.title}</span>
                {child.due && (
                  <span className={`todo-due${child.overdue ? " is-overdue" : ""}`}>
                    {formatDueLong(child.due).slice(5)}
                  </span>
                )}
              </div>
            );
          })}
          <div className="add-row">
            <Icon name="plus" size={14} />
            <input
              value={childInput}
              placeholder="添加子任务"
              onChange={(e) => setChildInput(e.target.value)}
              onKeyDown={(e) => {
                if (e.key === "Enter" && !e.nativeEvent.isComposing) {
                  e.preventDefault();
                  void addChild();
                }
              }}
            />
          </div>
        </section>

        <section className="detail-section">
          <h3>链接与附件</h3>
          {links.map((l) => (
            <div key={l.url} className="resource">
              <Icon name="link" size={14} />
              <button
                type="button"
                className="resource-open"
                title={l.url}
                onClick={() => void openLink(l.url)}
              >
                {linkDisplay(l.url, l.title)}
              </button>
              <button
                type="button"
                className="icon-btn is-small"
                title="删除链接"
                onClick={() => void run(() => TodoService.RemoveLink(item.id, l.url), "删除链接失败")}
              >
                <Icon name="close" size={12} />
              </button>
            </div>
          ))}
          {attachments.map((a) => (
            <div key={a.path} className="resource">
              <Icon name="file" size={14} />
              <button
                type="button"
                className="resource-open"
                title={a.path}
                onClick={() => openAttachment(a.path)}
              >
                {a.name}
              </button>
              <button
                type="button"
                className="icon-btn is-small"
                title="移除附件"
                onClick={() => void detach(a.path)}
              >
                <Icon name="close" size={12} />
              </button>
            </div>
          ))}

          {linkOpen && (
            <div className="link-form">
              <input
                className="field"
                autoFocus
                value={linkURL}
                placeholder="粘贴链接 https://…"
                onChange={(e) => setLinkURL(e.target.value)}
                onKeyDown={(e) => {
                  if (e.key === "Enter" && !e.nativeEvent.isComposing) {
                    e.preventDefault();
                    void addLink();
                  } else if (e.key === "Escape") {
                    setLinkOpen(false);
                  }
                }}
              />
              <input
                className="field"
                value={linkTitle}
                placeholder="显示名称（可选）"
                onChange={(e) => setLinkTitle(e.target.value)}
                onKeyDown={(e) => {
                  if (e.key === "Enter" && !e.nativeEvent.isComposing) {
                    e.preventDefault();
                    void addLink();
                  } else if (e.key === "Escape") {
                    setLinkOpen(false);
                  }
                }}
              />
              <div className="link-form-actions">
                <button type="button" className="btn" onClick={() => setLinkOpen(false)}>
                  取消
                </button>
                <button
                  type="button"
                  className="btn btn-primary"
                  disabled={busy || !linkURL.trim()}
                  onClick={() => void addLink()}
                >
                  添加链接
                </button>
              </div>
            </div>
          )}

          <div className="resource-actions">
            {!linkOpen && (
              <button type="button" className="btn btn-ghost" onClick={() => setLinkOpen(true)}>
                <Icon name="link" size={14} />
                添加链接
              </button>
            )}
            <button type="button" className="btn btn-ghost" disabled={busy} onClick={() => void chooseFile()}>
              <Icon name="paperclip" size={14} />
              添加附件
            </button>
            <span className="resource-hint">也可以把文件拖进来</span>
          </div>
        </section>
      </div>

      {dropping && <div className="drop-overlay">松开即可添加为附件</div>}

      {confirmDelete && (
        <div className="modal-mask" onClick={() => setConfirmDelete(false)}>
          <div className="modal" onClick={(e) => e.stopPropagation()}>
            <h2>删除「{detail.title}」？</h2>
            <p>
              {detail.childTotal > 0
                ? `它的 ${detail.childTotal} 条子任务会一起删除，`
                : ""}
              删除后无法恢复。
            </p>
            <div className="modal-buttons">
              <button type="button" className="btn" onClick={() => setConfirmDelete(false)}>
                取消
              </button>
              <button
                type="button"
                className="btn btn-danger"
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

      {confirmLink && (
        <div className="modal-mask" onClick={() => setConfirmLink(null)}>
          <div className="modal" onClick={(e) => e.stopPropagation()}>
            <h2>打开 {confirmLink.scheme}: 链接？</h2>
            <p className="modal-url">{confirmLink.url}</p>
            <p>
              {confirmLink.scheme === "file"
                ? "这会用系统默认程序打开本地文件，请确认来源可信。"
                : `「${confirmLink.scheme}」不在链接白名单里，会交给系统打开。可以在设置里把它加入白名单。`}
            </p>
            <div className="modal-buttons">
              <button type="button" className="btn" onClick={() => setConfirmLink(null)}>
                取消
              </button>
              <button
                type="button"
                className="btn btn-primary"
                onClick={() => {
                  const url = confirmLink.url;
                  setConfirmLink(null);
                  void openLink(url, true);
                }}
              >
                打开
              </button>
            </div>
          </div>
        </div>
      )}

      {confirmDone !== null && (
        <div className="modal-mask" onClick={() => setConfirmDone(null)}>
          <div className="modal" onClick={(e) => e.stopPropagation()}>
            <h2>子任务还没做完</h2>
            <p>
              「{detail.title}」下还有 {confirmDone} 条子任务未完成，要一起标为完成吗？
            </p>
            <div className="modal-buttons">
              <button type="button" className="btn" onClick={() => setConfirmDone(null)}>
                取消
              </button>
              <button type="button" className="btn" onClick={() => void finishWithChildren(false)}>
                只完成父任务
              </button>
              <button type="button" className="btn btn-primary" onClick={() => void finishWithChildren(true)}>
                一起完成
              </button>
            </div>
          </div>
        </div>
      )}
    </aside>
  );
}
