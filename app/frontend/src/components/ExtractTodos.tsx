import { useEffect, useState } from "react";
import { WorkspaceService } from "../../bindings/github.com/axu4/kuibu/app";
import type { TodoCandidate } from "../../bindings/github.com/axu4/kuibu/internal/core";
import { readableError } from "../lib/todo";

interface ExtractTodosProps {
  noteId: string;
  onClose: () => void;
  /** 创建完成后回调，参数是新建条数。 */
  onDone: (count: number) => void;
}

/**
 * 从笔记抽待办：列出笔记里未勾选的「- [ ] …」行，勾选后确认才创建。
 * 已抽过的行置灰，不会重复创建。
 */
export function ExtractTodos({ noteId, onClose, onDone }: ExtractTodosProps) {
  const [items, setItems] = useState<TodoCandidate[] | null>(null);
  const [picked, setPicked] = useState<Set<string>>(new Set());
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);

  useEffect(() => {
    void WorkspaceService.TodoCandidates(noteId)
      .then((list) => {
        const all = list ?? [];
        setItems(all);
        setPicked(new Set(all.filter((c) => !c.todoId).map((c) => c.key)));
      })
      .catch((err) => setError(readableError(err)));
  }, [noteId]);

  const toggle = (key: string) =>
    setPicked((prev) => {
      const next = new Set(prev);
      if (next.has(key)) next.delete(key);
      else next.add(key);
      return next;
    });

  const confirm = async () => {
    setBusy(true);
    try {
      onDone(await WorkspaceService.ExtractTodos(noteId, [...picked]));
    } catch (err) {
      setError(readableError(err));
      setBusy(false);
    }
  };

  const fresh = (items ?? []).filter((c) => !c.todoId);
  return (
    <div className="modal-mask" onClick={onClose}>
      <div className="modal extract-todos" onClick={(e) => e.stopPropagation()}>
        <h2>从此笔记抽待办</h2>
        {items === null && !error && <p>正在识别…</p>}
        {items !== null && items.length === 0 && (
          <p>笔记里没有「- [ ] …」这样的待办行。在笔记里写一行「- [ ] 事项 明天」就能抽出来。</p>
        )}
        {items !== null && items.length > 0 && (
          <div className="extract-list">
            {items.map((c) => (
              <label key={c.key} className={`extract-item${c.todoId ? " is-done" : ""}`}>
                <input
                  type="checkbox"
                  disabled={!!c.todoId}
                  checked={!c.todoId && picked.has(c.key)}
                  onChange={() => toggle(c.key)}
                />
                <span className="extract-title">{c.title}</span>
                {c.due && <span className="chip">{String(c.due)}</span>}
                {(c.tags ?? []).map((t) => (
                  <span key={t} className="chip">
                    #{t}
                  </span>
                ))}
                {c.todoId && <span className="extract-mark">已抽过</span>}
              </label>
            ))}
          </div>
        )}
        {error && <div className="settings-error">{error}</div>}
        <div className="modal-buttons">
          <button type="button" className="btn" onClick={onClose}>
            取消
          </button>
          <button
            type="button"
            className="btn btn-primary"
            disabled={busy || fresh.length === 0 || picked.size === 0}
            onClick={() => void confirm()}
          >
            创建 {picked.size} 条待办
          </button>
        </div>
      </div>
    </div>
  );
}
