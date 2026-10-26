import { useCallback, useEffect, useState } from "react";
import { ShellService } from "../../bindings/github.com/axu4/kuibu/app";
import type { ShortcutStatus } from "../../bindings/github.com/axu4/kuibu/app";
import {
  keyboardEventToAccelerator,
  modifiersOnlyDisplay,
  shortcutDisplay,
} from "../lib/shortcut";

interface SettingsProps {
  onClose: () => void;
  /** 快捷键状态变化后通知外层刷新（例如顶部提示条）。 */
  onStatusChange: (status: ShortcutStatus) => void;
}

/**
 * 设置弹层。目前只有「随手记快捷键」一项。
 *
 * 录制模式：窗口级监听 keydown 并 preventDefault/stopPropagation，
 * 避免按键触发其它操作（例如 ⌘W 关窗、⌘Q 退出）。
 */
export function Settings({ onClose, onStatusChange }: SettingsProps) {
  const [status, setStatus] = useState<ShortcutStatus | null>(null);
  const [recording, setRecording] = useState(false);
  const [preview, setPreview] = useState<string>("");
  const [pending, setPending] = useState<string>("");
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);

  const refresh = useCallback(async () => {
    try {
      const next = await ShellService.GetShortcutStatus();
      setStatus(next);
      onStatusChange(next);
    } catch (err) {
      setError(readableError(err));
    }
  }, [onStatusChange]);

  useEffect(() => {
    void refresh();
  }, [refresh]);

  /** 录制时的按键处理。 */
  const onRecordKeyDown = useCallback((e: KeyboardEvent) => {
    // 录制期间吞掉所有按键，避免触发 ⌘W / ⌘Q 等原生行为。
    e.preventDefault();
    e.stopPropagation();

    if (e.key === "Escape") {
      setRecording(false);
      setPreview("");
      setPending("");
      setError(null);
      return;
    }

    const accel = keyboardEventToAccelerator(e);
    if (!accel) {
      // 只按下修饰键：显示已按下的符号，继续等待主键。
      setPreview(modifiersOnlyDisplay(e));
      setPending("");
      return;
    }
    setPending(accel);
    setPreview(shortcutDisplay(accel));
  }, []);

  useEffect(() => {
    if (!recording) {
      return;
    }
    // capture 阶段监听，抢在页面其它快捷键之前。
    window.addEventListener("keydown", onRecordKeyDown, true);
    return () => window.removeEventListener("keydown", onRecordKeyDown, true);
  }, [recording, onRecordKeyDown]);

  const startRecording = () => {
    setRecording(true);
    setPreview("");
    setPending("");
    setError(null);
  };

  const save = async () => {
    if (!pending) {
      return;
    }
    setBusy(true);
    setError(null);
    try {
      const next = await ShellService.SetQuickNoteShortcut(pending);
      setStatus(next);
      onStatusChange(next);
      setRecording(false);
      setPreview("");
      setPending("");
    } catch (err) {
      setError(readableError(err));
      // 注册失败时保留录制态让用户重按，同时刷新状态（旧快捷键仍生效）。
      await refresh();
    } finally {
      setBusy(false);
    }
  };

  const reset = async () => {
    setBusy(true);
    setError(null);
    try {
      const next = await ShellService.ResetQuickNoteShortcut();
      setStatus(next);
      onStatusChange(next);
      setRecording(false);
      setPreview("");
      setPending("");
    } catch (err) {
      setError(readableError(err));
    } finally {
      setBusy(false);
    }
  };

  const currentDisplay = status ? status.display || shortcutDisplay(status.accelerator) : "—";
  const registered = status?.registered ?? false;
  // 状态里的错误可能是历史遗留（后来改成功了），只在存在且未注册时展示。
  const statusError = status?.error && !registered ? status.error : "";
  // 已回退到默认值但需用户处理的提示（例如配置里的快捷键不合法）。
  const statusNotice = status?.notice ?? "";

  return (
    <div className="modal-mask" onMouseDown={onClose}>
      <div className="settings" onMouseDown={(e) => e.stopPropagation()}>
        <h2>设置</h2>

        <div className="settings-row">
          <div className="settings-label">
            <span>随手记快捷键</span>
            <span className="settings-desc">
              在任意应用里按下就弹出随手记小窗
            </span>
          </div>
          <div className="settings-value">
            <kbd className={`shortcut-key${registered ? "" : " is-broken"}`}>
              {recording ? preview || "按下组合键…" : currentDisplay}
            </kbd>
            {status && (
              <span className={`shortcut-state${registered ? " is-ok" : " is-bad"}`}>
                {registered ? "已生效" : "未注册"}
              </span>
            )}
          </div>
        </div>

        {recording && (
          <div className="settings-hint">
            按下新的组合键（至少一个修饰键），Esc 取消。为避免误触，请先不要用 ⌘Q。
          </div>
        )}

        {statusNotice && <div className="settings-hint">{statusNotice}</div>}
        {statusError && <div className="settings-error">{statusError}</div>}
        {error && <div className="settings-error">{error}</div>}

        <div className="settings-actions">
          {recording ? (
            <>
              <button type="button" onClick={() => {
                setRecording(false);
                setPreview("");
                setPending("");
              }}>
                取消录制
              </button>
              <button
                type="button"
                className="is-primary"
                disabled={!pending || busy}
                onClick={() => void save()}
              >
                保存
              </button>
            </>
          ) : (
            <>
              <button type="button" onClick={() => void reset()} disabled={busy}>
                恢复默认
              </button>
              <button
                type="button"
                className="is-primary"
                onClick={startRecording}
                disabled={busy}
              >
                录制新快捷键
              </button>
            </>
          )}
        </div>

        <div className="settings-footer">
          <span>快捷键保存在 config.json（可用 KUIBU_CONFIG_DIR 覆盖目录）</span>
          <button type="button" className="settings-close" onClick={onClose}>
            关闭
          </button>
        </div>
      </div>
    </div>
  );
}

/** 把绑定调用抛出的错误整理成一行可读文本。 */
function readableError(err: unknown): string {
  const text = err instanceof Error ? err.message : String(err);
  return text.replace(/^Error:\s*/, "");
}
