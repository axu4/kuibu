import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { createPortal } from "react-dom";
import { Events } from "@wailsio/runtime";
import { TodoService } from "../../bindings/github.com/axu4/kuibu/app";
import type { TodoItem, TodoTag } from "../../bindings/github.com/axu4/kuibu/app";
import { TodoDetail } from "./TodoDetail";
import { Icon } from "./Icon";
import type { IconName } from "./Icon";
import { PRIORITY_OPTIONS, formatDue, priorityLabel, readableError, statusClass } from "../lib/todo";

/** 列表视图。 */
type Group = "today" | "week" | "all" | "done";

/** 快速录入预览的防抖时间。 */
const PREVIEW_DEBOUNCE_MS = 120;

/** 检索防抖时间。 */
const SEARCH_DEBOUNCE_MS = 200;

interface GroupDef {
  key: Group;
  label: string;
  icon: IconName;
  /** 空列表时的引导文案。 */
  empty: string;
}

const GROUPS: GroupDef[] = [
  { key: "today", label: "今天", icon: "sun", empty: "今天没有到期的事。在上面记一条，写上「今天」就会出现在这里。" },
  { key: "week", label: "本周", icon: "week", empty: "未来 7 天没有到期的事。" },
  { key: "all", label: "全部未完成", icon: "list", empty: "所有事都做完了。" },
  { key: "done", label: "已完成", icon: "checkCircle", empty: "完成的待办会收在这里。" },
];

/** 列表标题下的日期，如「10月27日 周二」。 */
function todayText(): string {
  const now = new Date();
  const weekdays = ["周日", "周一", "周二", "周三", "周四", "周五", "周六"];
  return `${now.getMonth() + 1}月${now.getDate()}日 ${weekdays[now.getDay()]}`;
}

interface TodoViewProps {
  /** 点击来源笔记：切到笔记视图并打开该笔记。 */
  onOpenNote: (id: string) => void;
  /** 侧栏里留给分组与标签的容器。 */
  navHost: HTMLElement | null;
  /** 从日历跳转进来时初始打开的待办；不传则不自动打开详情。 */
  initialTodoId?: string;
}

/**
 * 待办视图：左侧分组与标签、中间列表、右侧详情面板。
 *
 * 所有读写都经 TodoService（Go 侧再走 internal/store 与 internal/index），
 * 前端不拼路径、不解析文件。
 */
export function TodoView({ onOpenNote, navHost, initialTodoId }: TodoViewProps) {
  const [group, setGroup] = useState<Group>("today");
  const [tag, setTag] = useState("");
  const [query, setQuery] = useState("");
  // 优先级筛选（""=全部，none=未设置）与排序（""=默认按截止，priority=按优先级）。
  const [priority, setPriority] = useState("");
  const [sort, setSort] = useState("");
  const [list, setList] = useState<TodoItem[]>([]);
  const [tags, setTags] = useState<TodoTag[]>([]);
  const [counts, setCounts] = useState<Record<Group, number>>({
    today: 0,
    week: 0,
    all: 0,
    done: 0,
  });
  const [selectedId, setSelectedId] = useState<string | null>(null);
  const [detail, setDetail] = useState<{ item: TodoItem; revision: number } | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [loading, setLoading] = useState(false);

  // 快速录入。
  const [draft, setDraft] = useState("");
  const [preview, setPreview] = useState<{ title: string; due: string; tags: string[] } | null>(null);
  const [saving, setSaving] = useState(false);
  const inputRef = useRef<HTMLInputElement | null>(null);
  const searchRef = useRef<HTMLInputElement | null>(null);
  const previewTimer = useRef<ReturnType<typeof setTimeout> | null>(null);

  const selectedIdRef = useRef<string | null>(null);
  selectedIdRef.current = selectedId;
  const groupRef = useRef(group);
  groupRef.current = group;
  const tagRef = useRef(tag);
  tagRef.current = tag;
  const queryRef = useRef(query);
  queryRef.current = query;
  const priorityRef = useRef(priority);
  priorityRef.current = priority;
  const sortRef = useRef(sort);
  sortRef.current = sort;

  /** 刷新分组数量（每组一次，成本很低）。 */
  const refreshCounts = useCallback(async () => {
    try {
      const results = await Promise.all(
        GROUPS.map((g) =>
          TodoService.List({ view: g.key, tag: "", query: "", priority: "", sort: "" }).then(
            (items) => items ?? [],
          ),
        ),
      );
      const next: Record<Group, number> = { today: 0, week: 0, all: 0, done: 0 };
      GROUPS.forEach((g, i) => {
        next[g.key] = results[i].length;
      });
      setCounts(next);
    } catch (err) {
      console.error("读取待办分组数量失败", err);
    }
  }, []);

  /** 按当前分组/标签/检索刷新列表。 */
  const refreshList = useCallback(async () => {
    setLoading(true);
    try {
      const items = await TodoService.List({
        view: groupRef.current,
        tag: tagRef.current,
        query: queryRef.current,
        priority: priorityRef.current,
        sort: sortRef.current,
      });
      setList(items ?? []);
      setError(null);
    } catch (err) {
      setError(`读取待办失败：${readableError(err)}`);
    } finally {
      setLoading(false);
    }
  }, []);

  /** 刷新标签列表。 */
  const refreshTags = useCallback(async () => {
    try {
      const items = await TodoService.Tags();
      setTags(items ?? []);
    } catch (err) {
      console.error("读取待办标签失败", err);
    }
  }, []);

  /** 刷新当前详情；外部删除时关闭面板并提示。 */
  const refreshDetail = useCallback(async (id: string) => {
    try {
      const item = await TodoService.Get(id);
      setDetail((prev) => ({ item, revision: (prev?.revision ?? 0) + 1 }));
    } catch {
      setSelectedId(null);
      setDetail(null);
      setError("这条待办已在外部被删除，详情面板已关闭。");
    }
  }, []);

  const refreshAll = useCallback(async () => {
    await Promise.all([refreshList(), refreshCounts(), refreshTags()]);
    const current = selectedIdRef.current;
    if (current) {
      await refreshDetail(current);
    }
  }, [refreshCounts, refreshDetail, refreshList, refreshTags]);

  // 从日历跳转进来时打开指定待办的详情。
  useEffect(() => {
    if (initialTodoId) void openDetail(initialTodoId);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  // 首次加载与分组/标签/检索变化时刷新列表。
  useEffect(() => {
    void refreshList();
  }, [group, tag, priority, sort, refreshList]);

  useEffect(() => {
    void refreshCounts();
    void refreshTags();
  }, [refreshCounts, refreshTags]);

  // 检索防抖。
  useEffect(() => {
    const timer = setTimeout(() => {
      void refreshList();
    }, SEARCH_DEBOUNCE_MS);
    return () => clearTimeout(timer);
  }, [query, refreshList]);

  // 外部（含 App 自己）写入待办后刷新：列表、分组数量、标签与当前详情。
  useEffect(() => {
    const off = Events.On("todo:changed", () => {
      void refreshAll();
    });
    return off;
  }, [refreshAll]);

  /** 快速录入预览。 */
  useEffect(() => {
    const text = draft.trim();
    if (previewTimer.current) {
      clearTimeout(previewTimer.current);
    }
    if (!text) {
      setPreview(null);
      return;
    }
    previewTimer.current = setTimeout(() => {
      void TodoService.Preview(draft)
        .then((p) => setPreview({ title: p.title, due: p.due, tags: p.tags ?? [] }))
        .catch(() => setPreview(null));
    }, PREVIEW_DEBOUNCE_MS);
    return () => {
      if (previewTimer.current) {
        clearTimeout(previewTimer.current);
      }
    };
  }, [draft]);

  /** 新建待办：在当前分组里不额外强加日期，完全按解析结果。 */
  const create = useCallback(async () => {
    const text = draft.trim();
    if (!text || saving) {
      return;
    }
    setSaving(true);
    try {
      await TodoService.Create(text, "");
      setDraft("");
      setPreview(null);
      // 建完清空并保持焦点，方便连着录下一条；不自动打开详情面板。
      inputRef.current?.focus();
      await refreshAll();
    } catch (err) {
      setError(`新建待办失败：${readableError(err)}`);
    } finally {
      setSaving(false);
    }
  }, [draft, refreshAll, saving]);

  const openDetail = useCallback(
    async (id: string) => {
      if (selectedIdRef.current === id) {
        return;
      }
      setSelectedId(id);
      try {
        const item = await TodoService.Get(id);
        setDetail({ item, revision: 0 });
      } catch (err) {
        setError(`打开待办失败：${readableError(err)}`);
      }
    },
    [],
  );

  const closeDetail = useCallback(() => {
    setSelectedId(null);
    setDetail(null);
  }, []);

  /** 详情被修改后：同步列表与数量。 */
  const onDetailChanged = useCallback(
    (item: TodoItem) => {
      setDetail((prev) => ({ item, revision: prev?.revision ?? 0 }));
      void refreshAll();
    },
    [refreshAll],
  );

  const groupDef = useMemo(() => GROUPS.find((g) => g.key === group) ?? GROUPS[0], [group]);

  // ⌘N 聚焦录入框，⌘F 聚焦搜索。
  useEffect(() => {
    const onKeyDown = (e: KeyboardEvent) => {
      if (!(e.metaKey || e.ctrlKey) || e.altKey || e.shiftKey) {
        return;
      }
      const key = e.key.toLowerCase();
      if (key === "n") {
        e.preventDefault();
        inputRef.current?.focus();
      } else if (key === "f") {
        e.preventDefault();
        searchRef.current?.focus();
        searchRef.current?.select();
      }
    };
    window.addEventListener("keydown", onKeyDown);
    return () => window.removeEventListener("keydown", onKeyDown);
  }, []);

  const nav = (
    <nav className="todo-nav">
      {GROUPS.map((g) => (
        <button
          key={g.key}
          type="button"
          className={`nav-item${group === g.key ? " is-active" : ""}`}
          onClick={() => {
            setGroup(g.key);
            setError(null);
          }}
        >
          <Icon name={g.icon} />
          <span className="nav-item-label">{g.label}</span>
          {g.key !== "done" && counts[g.key] > 0 && (
            <span className="nav-item-count">{counts[g.key]}</span>
          )}
        </button>
      ))}

      <div className="sidebar-section">
        <span>标签</span>
      </div>
      {tags.length === 0 && <div className="nav-empty">录入时写 #标签 就会出现在这里</div>}
      {tags.map((t) => (
        <button
          key={t.name}
          type="button"
          className={`nav-item is-tag${tag === t.name ? " is-active" : ""}`}
          title={tag === t.name ? "再点一次取消筛选" : `只看 #${t.name}`}
          onClick={() => setTag((prev) => (prev === t.name ? "" : t.name))}
        >
          <span className="tag-hash">#</span>
          <span className="nav-item-label">{t.name}</span>
          <span className="nav-item-count">{t.count}</span>
        </button>
      ))}
    </nav>
  );

  const hasPreview = !!preview && (preview.due !== "" || preview.tags.length > 0);

  return (
    <div className="todo-view">
      {navHost && createPortal(nav, navHost)}

      <section className="todo-main">
        <div className="toolbar">
          <div className="toolbar-spacer" />
          <label className="search">
            <Icon name="search" size={14} />
            <input
              ref={searchRef}
              value={query}
              placeholder="搜索待办"
              onChange={(e) => setQuery(e.target.value)}
              onKeyDown={(e) => {
                if (e.key === "Escape") {
                  setQuery("");
                  e.currentTarget.blur();
                }
              }}
            />
            {query && (
              <button type="button" title="清除搜索" onClick={() => setQuery("")}>
                <Icon name="close" size={12} />
              </button>
            )}
          </label>
        </div>

        <div className="todo-scroll">
          <header className="todo-head">
            <h1>{groupDef.label}</h1>
            <div className="todo-head-meta">
              {group === "today" && <span>{todayText()}</span>}
              <span>{loading ? "刷新中…" : `${list.length} 项`}</span>
              {tag && (
                <button type="button" className="filter-chip" onClick={() => setTag("")}>
                  #{tag}
                  <Icon name="close" size={11} />
                </button>
              )}
              <div className="toolbar-spacer" />
              <select
                className="head-select"
                value={priority}
                title="按优先级筛选"
                onChange={(e) => setPriority(e.target.value)}
              >
                <option value="">全部优先级</option>
                {PRIORITY_OPTIONS.map((p) => (
                  <option key={p} value={p}>
                    {priorityLabel(p)}优先级
                  </option>
                ))}
                <option value="none">未设优先级</option>
              </select>
              <select
                className="head-select"
                value={sort}
                title="排序方式"
                onChange={(e) => setSort(e.target.value)}
              >
                <option value="">按截止时间</option>
                <option value="priority">按优先级</option>
              </select>
            </div>
          </header>

          <div className={`composer${draft ? " has-text" : ""}`}>
            <span className="composer-icon">
              <Icon name="plus" />
            </span>
            <input
              ref={inputRef}
              className="composer-input"
              value={draft}
              placeholder="记一件事，如：明天下午三点 交周报 #工作"
              onChange={(e) => setDraft(e.target.value)}
              onKeyDown={(e) => {
                if (e.key === "Enter" && !e.nativeEvent.isComposing) {
                  e.preventDefault();
                  void create();
                } else if (e.key === "Escape") {
                  setDraft("");
                }
              }}
            />
            {draft.trim() && <kbd className="composer-enter">↵</kbd>}
            {hasPreview && preview && (
              <div className="composer-preview">
                <span className="composer-preview-label">识别为</span>
                {preview.due && (
                  <span className="chip chip-due">
                    <Icon name="calendar" size={12} />
                    {formatDue(preview.due)}
                  </span>
                )}
                {preview.tags.map((t) => (
                  <span key={t} className="chip">
                    #{t}
                  </span>
                ))}
                <span className="composer-preview-title">{preview.title || "（标题为空）"}</span>
              </div>
            )}
          </div>

          <div className="todo-list">
            {list.length === 0 && !loading && (
              <div className="todo-empty">
                {query ? `没有包含「${query}」的待办。` : groupDef.empty}
              </div>
            )}
            {list.map((item) => (
              <TodoRow
                key={item.id}
                item={item}
                depth={0}
                selectedId={selectedId}
                onSelect={(id) => void openDetail(id)}
                onChanged={() => void refreshAll()}
                onError={setError}
              />
            ))}
          </div>
        </div>
      </section>

      {detail && (
        <TodoDetail
          key={detail.item.id}
          item={detail.item}
          revision={detail.revision}
          onClose={closeDetail}
          onChange={onDetailChanged}
          onRefresh={() => void refreshAll()}
          onOpenNote={onOpenNote}
          onError={setError}
        />
      )}

      {error && (
        <div className="toast" role="alert" onClick={() => setError(null)}>
          {error}
        </div>
      )}
    </div>
  );
}

interface TodoRowProps {
  item: TodoItem;
  depth: number;
  selectedId: string | null;
  onSelect: (id: string) => void;
  onChanged: () => void;
  onError: (message: string) => void;
}

/** 一条待办：勾选框、标题、截止、标签、子任务进度，子任务缩进可折叠。 */
function TodoRow({ item, depth, selectedId, onSelect, onChanged, onError }: TodoRowProps) {
  const [open, setOpen] = useState(true);
  const [confirm, setConfirm] = useState<{ openChildren: number } | null>(null);
  const [busy, setBusy] = useState(false);
  const children = item.children ?? [];
  const closed = item.status === "done" || item.status === "canceled";
  const isSelected = selectedId === item.id;

  /** 勾选/取消勾选：有未完成子任务时先确认。 */
  const toggle = async () => {
    if (busy) {
      return;
    }
    setBusy(true);
    try {
      if (!closed) {
        const openChildren = await TodoService.OpenChildrenCount(item.id);
        if (openChildren > 0) {
          setConfirm({ openChildren });
          return;
        }
        await TodoService.SetStatus(item.id, "done", false);
      } else {
        await TodoService.SetStatus(item.id, "todo", false);
      }
      onChanged();
    } catch (err) {
      onError(`更新状态失败：${readableError(err)}`);
    } finally {
      setBusy(false);
    }
  };

  const finish = async (withChildren: boolean) => {
    setConfirm(null);
    setBusy(true);
    try {
      await TodoService.SetStatus(item.id, "done", withChildren);
      onChanged();
    } catch (err) {
      onError(`更新状态失败：${readableError(err)}`);
    } finally {
      setBusy(false);
    }
  };

  return (
    <>
      <div
        className={`todo-row${isSelected ? " is-selected" : ""}${statusClass(item.status)}`}
        style={{ paddingLeft: 6 + depth * 26 }}
        onClick={() => onSelect(item.id)}
      >
        {children.length > 0 ? (
          <button
            type="button"
            className={`todo-caret${open ? " is-open" : ""}`}
            aria-label={open ? "折叠子任务" : "展开子任务"}
            onClick={(e) => {
              e.stopPropagation();
              setOpen((prev) => !prev);
            }}
          >
            <Icon name="chevron" size={12} />
          </button>
        ) : (
          <span className="todo-caret is-hidden" />
        )}

        <input
          type="checkbox"
          className="check"
          checked={closed}
          disabled={busy}
          aria-label={closed ? "标为未完成" : "标为完成"}
          onClick={(e) => e.stopPropagation()}
          onChange={() => void toggle()}
        />

        {item.priority && (
          <span className={`todo-priority is-${item.priority}`} title={`${priorityLabel(item.priority)}优先级`}>
            {priorityLabel(item.priority)}
          </span>
        )}

        <span className="todo-title" title={item.title}>
          {item.title}
        </span>

        <span className="todo-meta">
          {(item.tags ?? []).map((t) => (
            <span key={t} className="todo-row-tag">
              #{t}
            </span>
          ))}

          {((item.links?.length ?? 0) > 0 || (item.attachments?.length ?? 0) > 0) && (
            <Icon name="paperclip" size={13} className="todo-meta-icon" />
          )}

          {item.childTotal > 0 && (
            <span className="todo-progress" title="子任务完成情况">
              <Icon name="checkCircle" size={13} />
              {item.childDone}/{item.childTotal}
            </span>
          )}

          {item.due && (
            <span className={`todo-due${item.overdue ? " is-overdue" : ""}`}>
              {formatDue(item.due)}
            </span>
          )}
        </span>
      </div>

      {open &&
        children.map((child) => (
          <TodoRow
            key={child.id}
            item={child}
            depth={depth + 1}
            selectedId={selectedId}
            onSelect={onSelect}
            onChanged={onChanged}
            onError={onError}
          />
        ))}

      {confirm && (
        <div className="modal-mask" onClick={() => setConfirm(null)}>
          <div className="modal" onClick={(e) => e.stopPropagation()}>
            <h2>子任务还没做完</h2>
            <p>
              「{item.title}」下还有 {confirm.openChildren} 条子任务未完成，要一起标为完成吗？
            </p>
            <div className="modal-buttons">
              <button type="button" className="btn" onClick={() => setConfirm(null)}>
                取消
              </button>
              <button type="button" className="btn" onClick={() => void finish(false)}>
                只完成父任务
              </button>
              <button type="button" className="btn btn-primary" onClick={() => void finish(true)}>
                一起完成
              </button>
            </div>
          </div>
        </div>
      )}
    </>
  );
}
