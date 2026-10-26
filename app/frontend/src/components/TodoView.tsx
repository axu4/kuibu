import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { Events } from "@wailsio/runtime";
import { TodoService } from "../../bindings/github.com/axu4/kuibu/app";
import type { TodoItem, TodoTag } from "../../bindings/github.com/axu4/kuibu/app";
import { TodoDetail } from "./TodoDetail";
import { formatDue, readableError, statusClass } from "../lib/todo";

/** 列表视图。 */
type Group = "today" | "week" | "all" | "done";

/** 快速录入预览的防抖时间。 */
const PREVIEW_DEBOUNCE_MS = 120;

/** 检索防抖时间。 */
const SEARCH_DEBOUNCE_MS = 200;

interface GroupDef {
  key: Group;
  label: string;
}

const GROUPS: GroupDef[] = [
  { key: "today", label: "今天" },
  { key: "week", label: "本周" },
  { key: "all", label: "全部未完成" },
  { key: "done", label: "已完成" },
];

interface TodoViewProps {
  /** 点击来源笔记：切到笔记视图并打开该笔记。 */
  onOpenNote: (id: string) => void;
}

/**
 * 待办视图：左侧分组与标签、中间列表、右侧详情面板。
 *
 * 所有读写都经 TodoService（Go 侧再走 internal/store 与 internal/index），
 * 前端不拼路径、不解析文件。
 */
export function TodoView({ onOpenNote }: TodoViewProps) {
  const [group, setGroup] = useState<Group>("today");
  const [tag, setTag] = useState("");
  const [query, setQuery] = useState("");
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
  const previewTimer = useRef<ReturnType<typeof setTimeout> | null>(null);

  const selectedIdRef = useRef<string | null>(null);
  selectedIdRef.current = selectedId;
  const groupRef = useRef(group);
  groupRef.current = group;
  const tagRef = useRef(tag);
  tagRef.current = tag;
  const queryRef = useRef(query);
  queryRef.current = query;

  /** 刷新分组数量（每组一次，成本很低）。 */
  const refreshCounts = useCallback(async () => {
    try {
      const results = await Promise.all(
        GROUPS.map((g) =>
          TodoService.List({ view: g.key, tag: "", query: "" }).then((items) => items ?? []),
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

  // 首次加载与分组/标签/检索变化时刷新列表。
  useEffect(() => {
    void refreshList();
  }, [group, tag, refreshList]);

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

  const groupLabel = useMemo(() => GROUPS.find((g) => g.key === group)?.label ?? "", [group]);

  return (
    <div className="todo-view">
      <nav className="todo-nav">
        <div className="todo-nav-groups">
          {GROUPS.map((g) => (
            <button
              key={g.key}
              type="button"
              className={`todo-nav-item${group === g.key ? " is-active" : ""}`}
              onClick={() => {
                setGroup(g.key);
                setError(null);
              }}
            >
              <span className="todo-nav-label">{g.label}</span>
              <span className="todo-nav-count">{counts[g.key]}</span>
            </button>
          ))}
        </div>

        <div className="todo-nav-section">标签</div>
        <div className="todo-nav-tags">
          {tags.length === 0 && <div className="todo-nav-empty">还没有标签</div>}
          {tags.map((t) => (
            <button
              key={t.name}
              type="button"
              className={`todo-tag-chip${tag === t.name ? " is-active" : ""}`}
              title={tag === t.name ? "再点一次取消筛选" : `只看 #${t.name}`}
              onClick={() => setTag((prev) => (prev === t.name ? "" : t.name))}
            >
              #{t.name}
              <span className="todo-tag-count">{t.count}</span>
            </button>
          ))}
        </div>
      </nav>

      <section className="todo-main">
        <div className="todo-toolbar">
          <div className="todo-quickadd">
            <input
              ref={inputRef}
              className="todo-quickadd-input"
              value={draft}
              placeholder="添加待办，如：明天下午三点 交周报 #工作"
              onChange={(e) => setDraft(e.target.value)}
              onKeyDown={(e) => {
                if (e.key === "Enter" && !e.nativeEvent.isComposing) {
                  e.preventDefault();
                  void create();
                }
              }}
            />
            {preview && (preview.title || preview.due || preview.tags.length > 0) && (
              <div className="todo-preview">
                <span className="todo-preview-title">{preview.title || "（标题为空）"}</span>
                {preview.due && <span className="todo-preview-due">{formatDue(preview.due)}</span>}
                {preview.tags.map((t) => (
                  <span key={t} className="todo-preview-tag">
                    #{t}
                  </span>
                ))}
              </div>
            )}
          </div>
          <input
            className="todo-search"
            value={query}
            placeholder="搜索标题或正文"
            onChange={(e) => setQuery(e.target.value)}
          />
        </div>

        <div className="todo-list-header">
          <span>{groupLabel}</span>
          {tag && (
            <button type="button" className="todo-filter-chip" onClick={() => setTag("")}>
              #{tag} ✕
            </button>
          )}
          <span className="todo-list-count">{loading ? "刷新中…" : `${list.length} 条`}</span>
        </div>

        <div className="todo-list">
          {list.length === 0 && !loading && (
            <div className="todo-empty">
              {query
                ? "没有匹配的待办。"
                : group === "done"
                  ? "还没有已完成的待办。"
                  : "这个分组是空的，在上面的输入框里记一条。"}
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
        <div className="todo-toast" onClick={() => setError(null)}>
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
        style={{ paddingLeft: 10 + depth * 18 }}
        onClick={() => onSelect(item.id)}
      >
        {children.length > 0 ? (
          <button
            type="button"
            className="todo-caret"
            aria-label={open ? "折叠子任务" : "展开子任务"}
            onClick={(e) => {
              e.stopPropagation();
              setOpen((prev) => !prev);
            }}
          >
            {open ? "▾" : "▸"}
          </button>
        ) : (
          <span className="todo-caret is-hidden" />
        )}

        <input
          type="checkbox"
          className="todo-check"
          checked={closed}
          disabled={busy}
          onClick={(e) => e.stopPropagation()}
          onChange={() => void toggle()}
        />

        <span className="todo-title" title={item.title}>
          {item.title}
        </span>

        {item.childTotal > 0 && (
          <span className="todo-progress">
            {item.childDone}/{item.childTotal}
          </span>
        )}

        {item.due && (
          <span className={`todo-due${item.overdue ? " is-overdue" : ""}`}>
            {formatDue(item.due)}
          </span>
        )}

        {(item.tags ?? []).map((t) => (
          <span key={t} className="todo-row-tag">
            #{t}
          </span>
        ))}
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
            <h2>完成父任务</h2>
            <p>
              「{item.title}」还有 {confirm.openChildren} 条未完成的子任务，要一起完成吗？
            </p>
            <div className="modal-buttons">
              <button type="button" onClick={() => setConfirm(null)}>
                取消
              </button>
              <button type="button" onClick={() => void finish(false)}>
                只完成父任务
              </button>
              <button type="button" className="is-primary" onClick={() => void finish(true)}>
                一起完成
              </button>
            </div>
          </div>
        </div>
      )}
    </>
  );
}
