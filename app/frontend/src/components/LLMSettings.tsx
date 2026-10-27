import { useCallback, useEffect, useState } from "react";
import { createPortal } from "react-dom";
import { LLMService } from "../../bindings/github.com/axu4/kuibu/app";
import type { LLMList } from "../../bindings/github.com/axu4/kuibu/app";
import type { ModelInfo, Preset } from "../../bindings/github.com/axu4/kuibu/internal/llm";
import { Icon } from "./Icon";
import { Menu, MoreMenu } from "./Menu";

/** 表单草稿。apiKey 为空且是编辑时表示保留原密钥。 */
interface Draft {
  editing: boolean;
  name: string;
  provider: string;
  baseUrl: string;
  model: string;
  apiKey: string;
  makeDefault: boolean;
}

const PROVIDER_LABEL: Record<string, string> = {
  openai: "OpenAI 兼容",
  claude: "Claude",
};

/**
 * 设置里的「AI 模型」一节：列表、添加 / 编辑 / 删除、设默认、测试连通。
 * 密钥只在表单里输入，列表和返回值都只有掩码。
 */
export function LLMSettings() {
  const [list, setList] = useState<LLMList | null>(null);
  const [presets, setPresets] = useState<Preset[]>([]);
  const [draft, setDraft] = useState<Draft | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [formError, setFormError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  const [testing, setTesting] = useState<string | null>(null);
  const [addAnchor, setAddAnchor] = useState<{ x: number; y: number } | null>(null);
  const [testResult, setTestResult] = useState<{ name: string; ok: boolean; text: string } | null>(
    null,
  );

  const load = useCallback(async () => {
    try {
      setList(await LLMService.List());
      setPresets((await LLMService.Presets()) ?? []);
    } catch (err) {
      setError(readableError(err));
    }
  }, []);

  useEffect(() => {
    void load();
  }, [load]);

  const models = list?.models ?? [];

  const startAdd = (preset?: Preset) => {
    setFormError(null);
    setDraft({
      editing: false,
      name: preset ? uniqueName(preset.label, models) : "",
      provider: preset?.provider ?? "openai",
      baseUrl: preset?.baseUrl ?? "",
      model: preset?.model ?? "",
      apiKey: "",
      makeDefault: models.length === 0,
    });
  };

  const startEdit = (m: ModelInfo) => {
    setFormError(null);
    setDraft({
      editing: true,
      name: m.name,
      provider: m.provider,
      baseUrl: m.baseUrl,
      model: m.model,
      apiKey: "",
      makeDefault: m.default,
    });
  };

  const run = async (fn: () => Promise<LLMList>) => {
    setBusy(true);
    setError(null);
    try {
      setList(await fn());
    } catch (err) {
      setError(readableError(err));
    } finally {
      setBusy(false);
    }
  };

  const submit = async () => {
    if (!draft) {
      return;
    }
    setBusy(true);
    setFormError(null);
    try {
      setList(
        await LLMService.Save({
          name: draft.name,
          provider: draft.provider,
          baseUrl: draft.baseUrl,
          model: draft.model,
          apiKey: draft.apiKey,
          makeDefault: draft.makeDefault,
        }),
      );
      setDraft(null);
    } catch (err) {
      setFormError(readableError(err));
    } finally {
      setBusy(false);
    }
  };

  const test = async (name: string) => {
    setTesting(name);
    setTestResult(null);
    try {
      const reply = await LLMService.Test(name);
      setTestResult({ name, ok: true, text: reply || "（空回复）" });
    } catch (err) {
      setTestResult({ name, ok: false, text: readableError(err) });
    } finally {
      setTesting(null);
    }
  };

  return (
    <section className="settings-section" id="settings-llm">
      <div className="settings-section-head">
        <h3>AI 模型</h3>
        <button
          type="button"
          className="btn"
          disabled={busy}
          onMouseDown={(e) => e.stopPropagation()}
          onClick={(e) => {
            if (addAnchor) {
              setAddAnchor(null);
              return;
            }
            const rect = e.currentTarget.getBoundingClientRect();
            setAddAnchor({ x: rect.right - 168, y: rect.bottom + 4 });
          }}
        >
          <Icon name="plus" size={14} />
          添加
        </button>
        {addAnchor && (
          <Menu
            x={addAnchor.x}
            y={addAnchor.y}
            onClose={() => setAddAnchor(null)}
            items={[
              ...presets.map((p) => ({ label: p.label, onClick: () => startAdd(p) })),
              { label: "其他 OpenAI 兼容", onClick: () => startAdd() },
            ]}
          />
        )}
      </div>
      <div className="settings-desc llm-intro">
        用于周报归一、组报等草稿生成。他人内容发送前都会先预览、由你确认；密钥只存本机配置目录（权限
        600）。
      </div>

      {models.length === 0 && list && (
        <div className="settings-hint">还没有配置模型，点「添加」选一个模板开始。</div>
      )}

      {models.map((m) => (
        <div className="settings-row llm-row" key={m.name}>
          <div className="settings-label">
            <span>
              {m.name}
              {m.default && <span className="llm-default">默认</span>}
            </span>
            <span className="settings-desc">
              {PROVIDER_LABEL[m.provider] ?? m.provider} · {m.model} · 密钥 {m.keyMasked || "未填"}
            </span>
            {testResult?.name === m.name && (
              <span className={`settings-desc ${testResult.ok ? "llm-ok" : "llm-bad"}`}>
                {testResult.ok ? `连通正常：${testResult.text}` : testResult.text}
              </span>
            )}
          </div>
          <div className="llm-actions">
            <button
              type="button"
              className="btn"
              disabled={testing !== null}
              onClick={() => void test(m.name)}
            >
              {testing === m.name ? "测试中…" : "测试"}
            </button>
            <MoreMenu
              items={[
                { label: "编辑", icon: "edit", onClick: () => startEdit(m) },
                ...(m.default
                  ? []
                  : [
                      {
                        label: "设为默认",
                        icon: "check" as const,
                        onClick: () => void run(() => LLMService.SetDefault(m.name)),
                      },
                    ]),
                {
                  label: "删除",
                  icon: "trash",
                  danger: true,
                  onClick: () => void run(() => LLMService.Remove(m.name)),
                },
              ]}
            />
          </div>
        </div>
      ))}

      {error && <div className="settings-error">{error}</div>}

      {draft &&
        // 挂到 body：设置弹窗有入场 transform，fixed 子元素会相对它定位。
        createPortal(
          <div
            className="modal-mask is-center is-nested"
            onMouseDown={(e) => {
              e.stopPropagation();
              setDraft(null);
            }}
          >
            <form
              className="modal llm-form"
              onMouseDown={(e) => e.stopPropagation()}
              onSubmit={(e) => {
                e.preventDefault();
                void submit();
              }}
            >
              <h2>{draft.editing ? `编辑 ${draft.name}` : "添加 AI 模型"}</h2>
              {!draft.editing && (
                <label className="field-label">
                  名称
                  <input
                    className="field"
                    value={draft.name}
                    autoFocus
                    onChange={(e) => setDraft({ ...draft, name: e.target.value })}
                  />
                </label>
              )}
              <label className="field-label">
                提供方
                <select
                  className="field"
                  value={draft.provider}
                  onChange={(e) => setDraft({ ...draft, provider: e.target.value })}
                >
                  <option value="openai">OpenAI 兼容（DeepSeek、通义、内网网关等）</option>
                  <option value="claude">Claude（Anthropic）</option>
                </select>
              </label>
              <label className="field-label">
                接口地址
                <input
                  className="field"
                  placeholder={draft.provider === "claude" ? "https://api.anthropic.com" : "https://api.deepseek.com"}
                  value={draft.baseUrl}
                  onChange={(e) => setDraft({ ...draft, baseUrl: e.target.value })}
                />
              </label>
              <label className="field-label">
                模型 ID
                <input
                  className="field"
                  value={draft.model}
                  onChange={(e) => setDraft({ ...draft, model: e.target.value })}
                />
              </label>
              <label className="field-label">
                密钥
                <input
                  className="field"
                  type="password"
                  autoFocus={draft.editing}
                  placeholder={draft.editing ? "留空表示不修改" : "粘贴 API Key"}
                  value={draft.apiKey}
                  onChange={(e) => setDraft({ ...draft, apiKey: e.target.value })}
                />
              </label>
              <label className="llm-check">
                <input
                  type="checkbox"
                  checked={draft.makeDefault}
                  onChange={(e) => setDraft({ ...draft, makeDefault: e.target.checked })}
                />
                设为默认模型
              </label>
              {formError && <div className="settings-error">{formError}</div>}
              <div className="modal-buttons">
                <button type="button" className="btn" onClick={() => setDraft(null)}>
                  取消
                </button>
                <button type="submit" className="btn btn-primary" disabled={busy}>
                  保存
                </button>
              </div>
            </form>
          </div>,
          document.body,
        )}
    </section>
  );
}

/** 模板名已被占用时加序号。 */
function uniqueName(base: string, models: ModelInfo[]): string {
  const taken = new Set(models.map((m) => m.name));
  if (!taken.has(base)) {
    return base;
  }
  for (let i = 2; ; i++) {
    const name = `${base} ${i}`;
    if (!taken.has(name)) {
      return name;
    }
  }
}

function readableError(err: unknown): string {
  const text = err instanceof Error ? err.message : String(err);
  return text.replace(/^Error:\s*/, "");
}
