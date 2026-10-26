import { useEffect, useRef, useState } from "react";
import { Events } from "@wailsio/runtime";
import { ShellService, TodoService } from "../bindings/github.com/axu4/kuibu/app";
import type { QuickAddPreview } from "../bindings/github.com/axu4/kuibu/app";
import { QUICKNOTE_MODE_KEY, formatDue } from "./lib/todo";

/** localStorage 中随手记草稿的键。 */
const DRAFT_KEY = "kuibu.quicknote.draft";

/** 快捷键提示里用的默认展示（读不到配置时兜底）。 */
const FALLBACK_HINT = "⌃⌥N";

/** 随手记模式：笔记 / 待办。 */
type Mode = "note" | "todo";

/** 读取上次的随手记模式。 */
function loadMode(): Mode {
  try {
    return localStorage.getItem(QUICKNOTE_MODE_KEY) === "todo" ? "todo" : "note";
  } catch {
    return "note";
  }
}

function saveMode(mode: Mode) {
  try {
    localStorage.setItem(QUICKNOTE_MODE_KEY, mode);
  } catch {
    // 忽略本地存储不可用。
  }
}

function loadDraft(): string {
  try {
    return localStorage.getItem(DRAFT_KEY) ?? "";
  } catch {
    return "";
  }
}

function saveDraft(text: string) {
  try {
    if (text.trim()) {
      localStorage.setItem(DRAFT_KEY, text);
    } else {
      localStorage.removeItem(DRAFT_KEY);
    }
  } catch {
    // 忽略本地存储不可用。
  }
}

function clearDraft() {
  try {
    localStorage.removeItem(DRAFT_KEY);
  } catch {
    // 忽略本地存储不可用。
  }
}

/**
 * 随手记小窗（/?view=quicknote）。
 *
 * 秒开优先：这里不用 Milkdown，只有一个 textarea。关闭（Esc 或保存）前
 * 会把未保存内容写进 localStorage，下次打开自动恢复。
 *
 * Tab 键在「笔记 / 待办」两种模式间切换（输入法组合输入中不切换）。待办模式
 * 下第一行是快速录入（日期/时间/标签自动解析），其余行作为待办正文。
 */
export default function QuickNote() {
  const [text, setText] = useState<string>(loadDraft);
  const [mode, setMode] = useState<Mode>(loadMode);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [hint, setHint] = useState<string>(FALLBACK_HINT);
  const [preview, setPreview] = useState<QuickAddPreview | null>(null);
  const areaRef = useRef<HTMLTextAreaElement | null>(null);
  const textRef = useRef(text);
  textRef.current = text;

  // 打开即聚焦输入框（光标放到末尾，方便接着上次的草稿写）。
  useEffect(() => {
    const area = areaRef.current;
    if (!area) {
      return;
    }
    area.focus();
    const end = area.value.length;
    area.setSelectionRange(end, end);
    // 小窗已存在时再按快捷键：Go 侧发 quicknote:focus，这里重新聚焦输入框。
    const off = Events.On("quicknote:focus", () => {
      const current = areaRef.current;
      if (!current) {
        return;
      }
      const pos = current.value.length;
      current.focus();
      current.setSelectionRange(pos, pos);
    });
    void ShellService.GetShortcutStatus()
      .then((status) => {
        if (status?.display) {
          setHint(status.display);
        }
      })
      .catch(() => {
        // 状态读不到时保留兜底提示。
      });
    return off;
  }, []);

  // 待办模式下预览第一行的解析结果。
  useEffect(() => {
    if (mode !== "todo") {
      setPreview(null);
      return;
    }
    const first = text.split("\n")[0]?.trim() ?? "";
    if (!first) {
      setPreview(null);
      return;
    }
    let cancelled = false;
    const timer = setTimeout(() => {
      void TodoService.Preview(first)
        .then((p) => {
          if (!cancelled) {
            setPreview(p);
          }
        })
        .catch(() => {
          if (!cancelled) {
            setPreview(null);
          }
        });
    }, 120);
    return () => {
      cancelled = true;
      clearTimeout(timer);
    };
  }, [text, mode]);

  const switchMode = (next: Mode) => {
    setMode(next);
    saveMode(next);
    setError(null);
    setTimeout(() => areaRef.current?.focus(), 0);
  };

  const close = () => {
    saveDraft(textRef.current);
    saveMode(mode);
    void ShellService.CloseQuickNote().catch(() => {
      // 关闭失败时至少已经存了草稿。
    });
  };

  const save = async () => {
    const content = textRef.current;
    if (!content.trim()) {
      setError("随手记内容为空");
      return;
    }
    setSaving(true);
    setError(null);
    try {
      if (mode === "todo") {
        await ShellService.SaveQuickTodo(content);
      } else {
        await ShellService.SaveQuickNote(content);
      }
      clearDraft();
      // Go 侧保存成功后会关闭小窗，这里不需要再调用 Close。
    } catch (err) {
      setError(readableError(err));
      setSaving(false);
    }
  };

  const onKeyDown = (e: React.KeyboardEvent<HTMLTextAreaElement>) => {
    // 输入法组合输入中不切换模式（isComposing 与 keyCode 229 双保险）。
    if (e.key === "Tab" && !e.nativeEvent.isComposing && e.keyCode !== 229) {
      e.preventDefault();
      switchMode(mode === "note" ? "todo" : "note");
      return;
    }
    if (e.key === "Enter" && (e.metaKey || e.ctrlKey)) {
      e.preventDefault();
      void save();
      return;
    }
    if (e.key === "Escape") {
      e.preventDefault();
      close();
    }
  };

  const target = mode === "todo" ? "保存为待办" : "保存到：随手记";
  const placeholder =
    mode === "todo"
      ? "待办：第一行如「明天下午三点 交周报 #工作」，其余行是详情…"
      : "随手记…（Markdown，⌘Enter 保存，Esc 关闭）";

  return (
    <div className="quicknote">
      <div className="quicknote-modes">
        <div className="quicknote-mode-group">
          <button
            type="button"
            className={`quicknote-mode${mode === "note" ? " is-active" : ""}`}
            onClick={() => switchMode("note")}
          >
            笔记
          </button>
          <button
            type="button"
            className={`quicknote-mode${mode === "todo" ? " is-active" : ""}`}
            onClick={() => switchMode("todo")}
          >
            待办
          </button>
        </div>
        <span className="quicknote-mode-hint">Tab 切换</span>
      </div>

      <textarea
        ref={areaRef}
        className="quicknote-input"
        value={text}
        spellCheck={false}
        placeholder={placeholder}
        onChange={(e) => {
          const next = e.target.value;
          setText(next);
          setError(null);
          // 每次改动都落草稿：这样无论 Esc、⌘W 还是别的方式关掉窗口，
          // 下次打开都能恢复（保存成功后整体清空）。
          saveDraft(next);
        }}
        onKeyDown={onKeyDown}
      />

      {mode === "todo" && preview && (preview.title || preview.due) && (
        <div className="quicknote-preview">
          <span className="quicknote-preview-title">{preview.title || "（标题为空）"}</span>
          {preview.due && <span className="quicknote-preview-due">{formatDue(preview.due)}</span>}
          {(preview.tags ?? []).map((t) => (
            <span key={t} className="quicknote-preview-tag">
              #{t}
            </span>
          ))}
        </div>
      )}

      <div className="quicknote-footer">
        <span className="quicknote-target">{target}</span>
        {error ? (
          <span className="quicknote-error" title={error}>
            {error}
          </span>
        ) : (
          <span className="quicknote-hint">
            ⌘Enter 保存 · Esc 关闭 · 快捷键 {hint}
          </span>
        )}
        <button
          type="button"
          className="quicknote-save"
          disabled={saving}
          onClick={() => void save()}
        >
          {saving ? "保存中…" : "保存"}
        </button>
      </div>
    </div>
  );
}

/** 把绑定调用抛出的错误整理成一行可读文本。 */
function readableError(err: unknown): string {
  const text = err instanceof Error ? err.message : String(err);
  return text.replace(/^Error:\s*/, "");
}
