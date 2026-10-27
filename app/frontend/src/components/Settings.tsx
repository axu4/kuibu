import { useCallback, useEffect, useState } from "react";
import { Events } from "@wailsio/runtime";
import { ShellService, UpdateService } from "../../bindings/github.com/axu4/kuibu/app";
import type {
  CheckResult,
  ShortcutStatus,
  UpdateInfo,
} from "../../bindings/github.com/axu4/kuibu/app";
import { Icon } from "./Icon";
import { parseReleaseNotes } from "../lib/releaseNotes";
import {
  keyboardEventToAccelerator,
  modifiersOnlyDisplay,
  shortcutDisplay,
} from "../lib/shortcut";

interface SettingsProps {
  onClose: () => void;
  /** 快捷键状态变化后通知外层刷新（例如顶部提示条）。 */
  onStatusChange: (status: ShortcutStatus) => void;
  /** 打开后定位到「软件更新」一节（从顶部更新提示条进入）。 */
  focusUpdate?: boolean;
  /** 当前工作台目录。 */
  workspace: string;
  /** 更换工作台（弹出目录选择框）。 */
  onChangeWorkspace: () => void;
}

/**
 * 设置弹层。目前只有「随手记快捷键」一项。
 *
 * 录制模式：窗口级监听 keydown 并 preventDefault/stopPropagation，
 * 避免按键触发其它操作（例如 ⌘W 关窗、⌘Q 退出）。
 */
export function Settings({
  onClose,
  onStatusChange,
  focusUpdate,
  workspace,
  onChangeWorkspace,
}: SettingsProps) {
  const [status, setStatus] = useState<ShortcutStatus | null>(null);
  const [recording, setRecording] = useState(false);
  const [preview, setPreview] = useState<string>("");
  const [pending, setPending] = useState<string>("");
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);

  // 软件更新状态。
  const [updateInfo, setUpdateInfo] = useState<UpdateInfo | null>(null);
  const [checkResult, setCheckResult] = useState<CheckResult | null>(null);
  const [checking, setChecking] = useState(false);
  const [applying, setApplying] = useState(false);
  const [updateError, setUpdateError] = useState<string | null>(null);
  const [progress, setProgress] = useState<{ stage: string; message: string } | null>(null);
  const [tokenDraft, setTokenDraft] = useState("");
  const [tokenBusy, setTokenBusy] = useState(false);
  const [autoCheckBusy, setAutoCheckBusy] = useState(false);

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

  const loadUpdateInfo = useCallback(async () => {
    try {
      setUpdateInfo(await UpdateService.Info());
    } catch (err) {
      setUpdateError(readableError(err));
    }
  }, []);

  useEffect(() => {
    void loadUpdateInfo();
  }, [loadUpdateInfo]);

  // 从顶部「发现新版本」提示条进入时，滚动到软件更新一节。
  useEffect(() => {
    if (!focusUpdate) {
      return;
    }
    const id = window.setTimeout(() => {
      document.getElementById("settings-update")?.scrollIntoView({ block: "start" });
    }, 0);
    return () => window.clearTimeout(id);
  }, [focusUpdate]);

  // Esc 关闭设置；录制快捷键时按键已在捕获阶段被吞掉，不会走到这里。
  useEffect(() => {
    const onKeyDown = (e: KeyboardEvent) => {
      if (e.key === "Escape" && !recording) {
        onClose();
      }
    };
    window.addEventListener("keydown", onKeyDown);
    return () => window.removeEventListener("keydown", onKeyDown);
  }, [onClose, recording]);

  // 监听 Apply 期间推送的进度阶段。
  useEffect(() => {
    const off = Events.On("update:progress", (event) => {
      const data = event.data as { stage?: string; message?: string } | null;
      if (data?.stage) {
        setProgress({ stage: data.stage, message: data.message ?? "" });
      }
    });
    return off;
  }, []);

  const checkUpdate = async () => {
    setChecking(true);
    setUpdateError(null);
    setCheckResult(null);
    try {
      setCheckResult(await UpdateService.Check());
    } catch (err) {
      setUpdateError(readableError(err));
    } finally {
      setChecking(false);
    }
  };

  const toggleAutoCheck = async () => {
    if (!updateInfo || autoCheckBusy) {
      return;
    }
    setAutoCheckBusy(true);
    setUpdateError(null);
    const next = !updateInfo.autoCheck;
    try {
      await UpdateService.SetAutoCheck(next);
      setUpdateInfo({ ...updateInfo, autoCheck: next });
    } catch (err) {
      setUpdateError(readableError(err));
    } finally {
      setAutoCheckBusy(false);
    }
  };

  const applyUpdate = async () => {
    setApplying(true);
    setUpdateError(null);
    setProgress({ stage: "download", message: "正在准备更新…" });
    try {
      await UpdateService.Apply();
    } catch (err) {
      setUpdateError(readableError(err));
    } finally {
      setApplying(false);
      setProgress(null);
    }
  };

  const saveToken = async () => {
    if (!tokenDraft.trim()) {
      return;
    }
    setTokenBusy(true);
    setUpdateError(null);
    try {
      await UpdateService.SetToken(tokenDraft);
      setTokenDraft("");
      await loadUpdateInfo();
    } catch (err) {
      setUpdateError(readableError(err));
    } finally {
      setTokenBusy(false);
    }
  };

  const clearToken = async () => {
    setTokenBusy(true);
    setUpdateError(null);
    try {
      await UpdateService.SetToken("");
      setTokenDraft("");
      await loadUpdateInfo();
    } catch (err) {
      setUpdateError(readableError(err));
    } finally {
      setTokenBusy(false);
    }
  };

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
    <div className="modal-mask is-center" onMouseDown={onClose}>
      <div className="settings" onMouseDown={(e) => e.stopPropagation()}>
        <div className="settings-head">
          <h2>设置</h2>
          <button type="button" className="icon-btn" title="关闭（Esc）" onClick={onClose}>
            <Icon name="close" />
          </button>
        </div>

        {/* 只有中间这一段滚动，标题和底部始终可见。 */}
        <div className="settings-body">

        <div className="settings-row">
          <div className="settings-label">
            <span>工作台</span>
            <span className="settings-desc settings-path" title={workspace}>
              {workspace || "尚未打开"}
            </span>
          </div>
          <button type="button" className="btn" onClick={onChangeWorkspace}>
            <Icon name="folder" size={14} />
            更换…
          </button>
        </div>

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

        <section className="settings-section" id="settings-update">
          <h3>软件更新</h3>

          <div className="settings-row">
            <div className="settings-label">
              <span>当前版本</span>
              <span className="settings-desc">v{updateInfo?.current ?? "…"}</span>
            </div>
            <button
              type="button"
              className="btn"
              onClick={() => void checkUpdate()}
              disabled={checking || applying || (updateInfo?.isDev ?? false)}
            >
              <Icon name="refresh" size={14} />
              {checking ? "检查中…" : "检查更新"}
            </button>
          </div>

          {updateInfo?.isDev && (
            <div className="settings-hint">
              {updateInfo.devReason || "开发版不支持自更新，请打包成 .app 后再试。"}
            </div>
          )}

          <div className="settings-row">
            <div className="settings-label">
              <span>启动时自动检查更新</span>
            </div>
            <button
              type="button"
              role="switch"
              aria-checked={updateInfo?.autoCheck ?? false}
              className={`switch${updateInfo?.autoCheck ? " is-on" : ""}`}
              disabled={!updateInfo || autoCheckBusy}
              onClick={() => void toggleAutoCheck()}
            >
              <span className="switch-knob" />
            </button>
          </div>

          {checkResult && (
            <div className="update-result">
              {checkResult.hasUpdate ? (
                <>
                  <div className="update-result-head">
                    <span className="update-new">发现新版本 v{checkResult.latest}</span>
                    <span className="settings-desc">
                      {formatDateTime(checkResult.publishedAt)} 发布
                    </span>
                  </div>
                  {checkResult.notes && (
                    <div className="update-notes">
                      {parseReleaseNotes(checkResult.notes).map((line, i) =>
                        line.kind === "heading" ? (
                          <h4 key={i}>{line.text}</h4>
                        ) : line.kind === "item" ? (
                          <p key={i} className="is-item">
                            {line.text}
                          </p>
                        ) : (
                          <p key={i}>{line.text}</p>
                        ),
                      )}
                    </div>
                  )}
                  <div className="settings-actions">
                    <button
                      type="button"
                      className="is-primary"
                      disabled={applying}
                      onClick={() => void applyUpdate()}
                    >
                      <Icon name="download" size={14} />
                      {applying ? "更新中…" : "更新并重启"}
                    </button>
                  </div>
                </>
              ) : (
                <div className="settings-hint">
                  已是最新版本（v{checkResult.latest}）
                </div>
              )}
            </div>
          )}

          {progress && (
            <div className="settings-hint">更新进度：{progress.message}</div>
          )}
          {updateError && <div className="settings-error">{updateError}</div>}

          <div className="settings-row">
            <div className="settings-label">
              <span>GitHub Token</span>
              <span className="settings-desc">
                当前来源：{updateInfo ? tokenSourceText(updateInfo) : "…"}。
                仅在未登录 gh 时需要；保存在本机配置目录，权限 600。
              </span>
            </div>
          </div>
          <div className="token-row">
            <input
              type="password"
              className="field"
              placeholder="填写有 axu4/kuibu 读权限的 token"
              value={tokenDraft}
              onChange={(e) => setTokenDraft(e.target.value)}
            />
            <button
              type="button"
              className="btn"
              onClick={() => void saveToken()}
              disabled={tokenBusy || !tokenDraft.trim()}
            >
              保存
            </button>
            <button
              type="button"
              className="btn"
              onClick={() => void clearToken()}
              disabled={tokenBusy || !updateInfo?.savedToken}
            >
              清除
            </button>
          </div>
        </section>
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

/** 把 RFC3339 时间显示成「2025年1月2日 03:04」。 */
function formatDateTime(value: string): string {
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) {
    return value;
  }
  const hm = `${String(date.getHours()).padStart(2, "0")}:${String(date.getMinutes()).padStart(2, "0")}`;
  return `${date.getFullYear()}年${date.getMonth() + 1}月${date.getDate()}日 ${hm}`;
}

/** 把 Go 侧的 token 来源映射成更自然的展示文案。 */
function tokenSourceText(info: UpdateInfo): string {
  switch (info.tokenSource) {
    case "gh 登录":
      return "已通过 gh 登录获取";
    case "设置中填写":
      return "已在设置中填写";
    default:
      return info.tokenSource || "未找到";
  }
}
