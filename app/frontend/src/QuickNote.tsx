import { useEffect, useRef, useState } from "react";
import { Events } from "@wailsio/runtime";
import { ShellService } from "../bindings/github.com/axu4/kuibu/app";

/** localStorage 中随手记草稿的键。 */
const DRAFT_KEY = "kuibu.quicknote.draft";

/** 快捷键提示里用的默认展示（读不到配置时兜底）。 */
const FALLBACK_HINT = "⌃⌥N";

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
 */
export default function QuickNote() {
  const [text, setText] = useState<string>(loadDraft);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [hint, setHint] = useState<string>(FALLBACK_HINT);
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

  const close = () => {
    saveDraft(textRef.current);
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
      await ShellService.SaveQuickNote(content);
      clearDraft();
      // Go 侧保存成功后会关闭小窗，这里不需要再调用 Close。
    } catch (err) {
      setError(readableError(err));
      setSaving(false);
    }
  };

  const onKeyDown = (e: React.KeyboardEvent<HTMLTextAreaElement>) => {
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

  return (
    <div className="quicknote">
      <textarea
        ref={areaRef}
        className="quicknote-input"
        value={text}
        spellCheck={false}
        placeholder="随手记…（Markdown，⌘Enter 保存，Esc 关闭）"
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
      <div className="quicknote-footer">
        <span className="quicknote-target">保存到：随手记</span>
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
