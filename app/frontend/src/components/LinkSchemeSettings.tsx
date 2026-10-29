import { useEffect, useState } from "react";
import { TodoService } from "../../bindings/github.com/axu4/kuibu/app";
import { readableError } from "../lib/todo";

/**
 * 待办链接协议白名单：白名单内的协议点击后直接交给系统打开，
 * 白名单外的协议和 file: 点击前要确认，javascript:/data: 一律拒绝（规则在核心库）。
 */
export function LinkSchemeSettings() {
  const [saved, setSaved] = useState<string[]>([]);
  const [text, setText] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [notice, setNotice] = useState<string | null>(null);

  useEffect(() => {
    void TodoService.LinkSchemes()
      .then((list) => {
        setSaved(list ?? []);
        setText((list ?? []).join(", "));
      })
      .catch((err) => setError(`读取链接白名单失败：${readableError(err)}`));
  }, []);

  const apply = async (reset: boolean) => {
    setBusy(true);
    setError(null);
    setNotice(null);
    try {
      const parts = text
        .split(/[\s,，、]+/)
        .map((s) => s.trim())
        .filter(Boolean);
      const list = (await TodoService.SetLinkSchemes(parts, reset)) ?? [];
      setSaved(list);
      setText(list.join(", "));
      setNotice(reset ? "已恢复默认白名单" : "已保存");
    } catch (err) {
      setError(`保存链接白名单失败：${readableError(err)}`);
    } finally {
      setBusy(false);
    }
  };

  const dirty = text.trim() !== saved.join(", ");

  return (
    <section className="settings-section">
      <h3>待办链接</h3>
      <div className="settings-row">
        <div className="settings-label">
          <span>直接打开的协议</span>
          <span className="settings-desc">
            这些协议的链接点击后直接交给系统打开（如 message: 跳到 Mail）；其他协议和 file: 会先确认，javascript:/data: 不会打开
          </span>
        </div>
      </div>
      <input
        className="field"
        value={text}
        placeholder="http, https, mailto, message"
        onChange={(e) => setText(e.target.value)}
        onKeyDown={(e) => {
          if (e.key === "Enter" && !e.nativeEvent.isComposing && dirty) {
            e.preventDefault();
            void apply(false);
          }
        }}
      />
      {notice && <div className="settings-hint">{notice}</div>}
      {error && <div className="settings-error">{error}</div>}
      <div className="settings-actions">
        <button type="button" onClick={() => void apply(true)} disabled={busy}>
          恢复默认
        </button>
        <button type="button" className="is-primary" onClick={() => void apply(false)} disabled={busy || !dirty}>
          保存
        </button>
      </div>
    </section>
  );
}
