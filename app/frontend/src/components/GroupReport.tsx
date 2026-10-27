import { useCallback, useEffect, useRef, useState } from "react";
import { ReportService } from "../../bindings/github.com/axu4/kuibu/app";
import type { GroupData } from "../../bindings/github.com/axu4/kuibu/app";
import type { Preview } from "../../bindings/github.com/axu4/kuibu/internal/llm";
import { Editor } from "./Editor";
import { Icon } from "./Icon";
import { MoreMenu } from "./Menu";
import { copyRich } from "../lib/clipboard";
import { SendPreview } from "./SendPreview";
import { readableError } from "../lib/todo";

const SAVE_DEBOUNCE_MS = 800;

/** 组报 / 产品报的服务调用，面板本身两者共用。 */
interface ReportApi {
  noun: string;
  template: string;
  intro: string;
  get: () => Promise<GroupData>;
  save: (body: string) => Promise<GroupData>;
  prepare: (allowMissing: boolean, force: boolean) => ReturnType<typeof ReportService.PrepareGroup>;
  generate: (allowMissing: boolean, force: boolean) => Promise<GroupData>;
  finalize: () => Promise<GroupData>;
  unfinalize: () => Promise<GroupData>;
  submit?: () => Promise<unknown>;
}

function reportApi(kind: "group" | "product", week: string): ReportApi {
  if (kind === "product") {
    return {
      noun: "产品报",
      template: "product",
      intro:
        "合并各组长已定稿的周报（含经组报带入的），按「产品线 → 栏目 → 类别」整理成产品 / 项目周报，发送前先给你预览。",
      get: () => ReportService.ProductReport(week),
      save: (body) => ReportService.SaveProduct(week, body),
      prepare: (a, f) => ReportService.PrepareProduct(week, a, f),
      generate: (a, f) => ReportService.GenerateProduct(week, a, f),
      finalize: () => ReportService.FinalizeProduct(week),
      unfinalize: () => ReportService.UnfinalizeProduct(week),
    };
  }
  return {
    noun: "组报",
    template: "group",
    intro:
      "合并本组所有组员已定稿的归一稿，按「产品线 → 类别」整理成组报。发送前先给你预览，定稿后可一键带入 SE tab。",
    get: () => ReportService.GroupReport(week, ""),
    save: (body) => ReportService.SaveGroup(week, "", body),
    prepare: (a, f) => ReportService.PrepareGroup(week, "", a, f),
    generate: (a, f) => ReportService.GenerateGroup(week, "", a, f),
    finalize: () => ReportService.FinalizeGroup(week, ""),
    unfinalize: () => ReportService.UnfinalizeGroup(week, ""),
    submit: () => ReportService.SubmitGroup(week, ""),
  };
}

interface GroupReportProps {
  /** group = 框架组长的组报；product = SE 的产品报。 */
  kind?: "group" | "product";
  week: string;
  refreshTick: number;
  onClose: () => void;
  onToast: (text: string) => void;
}

/**
 * 组报面板（框架组长 tab）：合并组员定稿归一稿 → AI 草稿（先预览确认）→
 * 人工修改 → 定稿 → 一键带入 SE。结构（产品线 × 类别）由核心库渲染。
 */
export function GroupReport({
  kind = "group",
  week,
  refreshTick,
  onClose,
  onToast,
}: GroupReportProps) {
  const api = reportApi(kind, week);
  const [data, setData] = useState<GroupData | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  const [revision, setRevision] = useState(0);
  const [saving, setSaving] = useState<"saved" | "saving" | "dirty">("saved");
  const [plan, setPlan] = useState<{
    preview: Preview;
    allowMissing: boolean;
    force: boolean;
  } | null>(null);
  const [ask, setAsk] = useState<{ text: string; allowMissing: boolean; force: boolean } | null>(
    null,
  );
  const [templateOpen, setTemplateOpen] = useState(false);

  const knownBodyRef = useRef("");
  const pendingRef = useRef<string | null>(null);
  const timerRef = useRef<ReturnType<typeof setTimeout> | null>(null);

  const accept = useCallback((d: GroupData, rebuild: boolean) => {
    setData(d);
    if (rebuild || (pendingRef.current === null && d.body !== knownBodyRef.current)) {
      knownBodyRef.current = d.body;
      setRevision((r) => r + 1);
    }
  }, []);

  const load = useCallback(async () => {
    try {
      accept(await api.get(), false);
      setError(null);
    } catch (err) {
      setError(readableError(err));
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [week, kind, accept]);

  useEffect(() => {
    void load();
  }, [load, refreshTick]);

  const flush = useCallback(async () => {
    if (timerRef.current) {
      clearTimeout(timerRef.current);
      timerRef.current = null;
    }
    const body = pendingRef.current;
    if (body === null) return;
    setSaving("saving");
    try {
      const d = await api.save(body);
      knownBodyRef.current = d.body;
      if (pendingRef.current === body) pendingRef.current = null;
      setData(d);
      setSaving(pendingRef.current === null ? "saved" : "dirty");
    } catch (err) {
      setSaving("dirty");
      onToast(`保存${api.noun}失败：${readableError(err)}`);
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [week, kind, onToast]);

  useEffect(() => () => void flush(), [flush]);

  const onChange = useCallback(
    (body: string) => {
      pendingRef.current = body;
      setSaving("dirty");
      if (timerRef.current) clearTimeout(timerRef.current);
      timerRef.current = setTimeout(() => void flush(), SAVE_DEBOUNCE_MS);
    },
    [flush],
  );

  /** 生成：先拿预览；有人未定稿或已人工修改时先问一句。 */
  const prepare = async (allowMissing: boolean, force: boolean) => {
    setBusy(true);
    setAsk(null);
    try {
      await flush();
      const p = await api.prepare(allowMissing, force);
      setPlan({ preview: p.preview, allowMissing, force });
    } catch (err) {
      const msg = readableError(err);
      if (!allowMissing && msg.includes("未定稿")) {
        setAsk({ text: msg.replace(/（确认先不纳入.*）/, ""), allowMissing: true, force });
      } else if (!force && msg.includes("--force")) {
        setAsk({ text: msg.replace(/；?确认要覆盖请加 --force/, ""), allowMissing, force: true });
      } else {
        onToast(`准备${api.noun}失败：${msg}`);
      }
    } finally {
      setBusy(false);
    }
  };

  const generate = async () => {
    if (!plan) return;
    try {
      const d = await api.generate(plan.allowMissing, plan.force);
      pendingRef.current = null;
      accept(d, true);
      setPlan(null);
      if (d.warning) onToast(d.warning);
    } catch (err) {
      onToast(`生成${api.noun}失败：${readableError(err)}`);
    }
  };

  const act = async (fn: () => Promise<GroupData>, rebuild = false) => {
    setBusy(true);
    try {
      await flush();
      accept(await fn(), rebuild);
    } catch (err) {
      onToast(readableError(err));
    } finally {
      setBusy(false);
    }
  };

  const submit = async () => {
    setBusy(true);
    try {
      await api.submit?.();
      await load();
      onToast("已带入 SE tab，组长名下显示「已交」");
    } catch (err) {
      onToast(`带入 SE 失败：${readableError(err)}`);
    } finally {
      setBusy(false);
    }
  };

  const final = data?.status === "final";

  const exportMd = async () => {
    try {
      const path = await ReportService.ExportReport(kind, week);
      if (path) onToast(`已导出到 ${path}`);
    } catch (err) {
      onToast(`导出失败：${readableError(err)}`);
    }
  };

  const copyFeishu = async () => {
    try {
      const c = await ReportService.ExportClipboard(kind, week);
      const mode = await copyRich(c.html, c.text);
      onToast(
        mode === "html"
          ? "已复制，粘贴到飞书即可（保留标题、列表格式）"
          : "当前环境不支持富文本剪贴板，已复制为纯文本",
      );
    } catch (err) {
      onToast(`复制失败：${readableError(err)}`);
    }
  };

  return (
    <section className="report-detail is-wide">
      <div className="report-detail-head">
        <div className="report-detail-head-main">
          <div className="report-detail-title">
            {data?.group ? `${data.group} ${api.noun}` : api.noun}
          </div>
          <div className="report-detail-meta">
            <span>{week}</span>
            {data?.leadName && <span>{data.leadName}</span>}
          </div>
        </div>
        <MoreMenu
          items={[
            {
              label: "导出 Markdown",
              icon: "download",
              disabled: !final,
              title: final ? undefined : "定稿后才能导出",
              onClick: () => void exportMd(),
            },
            {
              label: "复制为飞书格式",
              icon: "copy",
              disabled: !final,
              title: final ? undefined : "定稿后才能复制",
              onClick: () => void copyFeishu(),
            },
          ]}
        />
        <button type="button" className="icon-btn" title="关闭" onClick={onClose}>
          <Icon name="close" />
        </button>
      </div>

      <div className="report-detail-body">
        {error ? (
          <div className="report-empty">
            <p>{error}</p>
          </div>
        ) : !data ? (
          <div className="report-empty">正在读取{api.noun}…</div>
        ) : (
          <div className="report-draft">
            <div className="report-draft-bar">
              {data.exists ? (
                <span className={`draft-badge is-${data.status}`}>{final ? "已定稿" : "草稿"}</span>
              ) : (
                <span className="draft-badge">未生成</span>
              )}
              {data.sourceChanged && <span className="draft-badge is-warn">组员定稿已更新</span>}
              {data.submitted && <span className="draft-badge is-final">已带入 SE</span>}
              {data.exists && !final && saving !== "saved" && (
                <span className="draft-saving">{saving === "saving" ? "保存中…" : "未保存"}</span>
              )}
              <span className="report-draft-spacer" />
              <button type="button" className="btn" onClick={() => setTemplateOpen(true)}>
                编辑模板
              </button>
              <button
                type="button"
                className="btn"
                disabled={busy}
                onClick={() => void prepare(false, false)}
              >
                <Icon name="bolt" size={14} />
                {data.exists ? "重新生成" : "AI 生成"}
              </button>
              {data.exists &&
                (final ? (
                  <>
                    <button
                      type="button"
                      className="btn"
                      disabled={busy}
                      onClick={() => void act(api.unfinalize, true)}
                    >
                      取消定稿
                    </button>
                    {api.submit && (
                      <button
                        type="button"
                        className="btn btn-primary"
                        disabled={busy}
                        onClick={() => void submit()}
                      >
                        带入 SE
                      </button>
                    )}
                  </>
                ) : (
                  <button
                    type="button"
                    className="btn btn-primary"
                    disabled={busy}
                    onClick={() => void act(api.finalize)}
                  >
                    <Icon name="check" size={14} />
                    定稿
                  </button>
                ))}
            </div>

            {ask && (
              <div className="settings-hint report-draft-ask">
                <span>{ask.text}</span>
                <button
                  type="button"
                  className="link-btn"
                  onClick={() => void prepare(ask.allowMissing, ask.force)}
                >
                  {ask.allowMissing && ask.text.includes("未定稿") ? "仍然生成（标注未纳入）" : "仍要重新生成"}
                </button>
              </div>
            )}

            {data.exists && (data.missing ?? []).length > 0 && (
              <div className="settings-hint">
                未纳入（归一稿未定稿）：{(data.missing ?? []).join("、")}
              </div>
            )}

            {!data.exists ? (
              <div className="report-empty">
                <p>{api.intro}</p>
              </div>
            ) : final ? (
              <>
                <div className="settings-hint">已定稿，只读。需要修改请先「取消定稿」。</div>
                <pre className="report-body">{data.body}</pre>
              </>
            ) : (
              <div className="report-draft-editor">
                <Editor
                  key={`group-${week}-${revision}`}
                  noteId={`group/${week}/${revision}`}
                  initialBody={data.body}
                  onChange={onChange}
                  placeholder={`${api.noun}正文`}
                />
              </div>
            )}
          </div>
        )}
      </div>

      {plan && (
        <SendPreview
          title={`发送给 AI 生成${api.noun}`}
          preview={plan.preview}
          onCancel={() => setPlan(null)}
          onConfirm={generate}
        />
      )}

      {templateOpen && (
        <TemplateModal
          name={api.template}
          noun={api.noun}
          onClose={() => setTemplateOpen(false)}
          onToast={onToast}
        />
      )}
    </section>
  );
}

/** 编辑报告模板：「## 栏目」一行一个，紧跟的「> 」行是给 AI 的说明。 */
function TemplateModal({
  name,
  noun,
  onClose,
  onToast,
}: {
  name: string;
  noun: string;
  onClose: () => void;
  onToast: (t: string) => void;
}) {
  const [body, setBody] = useState<string | null>(null);
  const [isDefault, setIsDefault] = useState(false);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    void ReportService.Template(name)
      .then((t) => {
        setBody(t.body);
        setIsDefault(t.isDefault);
      })
      .catch((err) => setError(readableError(err)));
  }, [name]);

  const save = async () => {
    if (body === null) return;
    try {
      await ReportService.SaveTemplate(name, body);
      onToast(`已保存模板，下次生成${noun}时生效`);
      onClose();
    } catch (err) {
      setError(readableError(err));
    }
  };

  return (
    <div className="modal-mask is-center is-nested" onMouseDown={onClose}>
      <div className="modal template-modal" onMouseDown={(e) => e.stopPropagation()}>
        <h2>{noun}模板</h2>
        <div className="settings-desc">
          「## 栏目名」一行一个栏目，可增删、改名、调整顺序；紧跟的「&gt; 」行是给 AI 的说明，含
          [产品线×类别] 的栏目按产品线 → 类别输出。
          {isDefault && ` 当前是内置默认模板，保存后写到工作台 templates/${name}.md。`}
        </div>
        <textarea
          className="field template-text"
          value={body ?? ""}
          disabled={body === null}
          onChange={(e) => setBody(e.target.value)}
        />
        {error && <div className="settings-error">{error}</div>}
        <div className="modal-buttons">
          <button type="button" className="btn" onClick={onClose}>
            取消
          </button>
          <button
            type="button"
            className="btn btn-primary"
            disabled={body === null}
            onClick={() => void save()}
          >
            保存
          </button>
        </div>
      </div>
    </div>
  );
}
