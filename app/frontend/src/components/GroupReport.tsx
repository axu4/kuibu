import { useCallback, useEffect, useRef, useState } from "react";
import { ReportService } from "../../bindings/github.com/axu4/kuibu/app";
import type { GroupData } from "../../bindings/github.com/axu4/kuibu/app";
import type { Preview } from "../../bindings/github.com/axu4/kuibu/internal/llm";
import { Editor } from "./Editor";
import { Icon } from "./Icon";
import { SendPreview } from "./SendPreview";
import { readableError } from "../lib/todo";

const SAVE_DEBOUNCE_MS = 800;

interface GroupReportProps {
  week: string;
  refreshTick: number;
  onClose: () => void;
  onToast: (text: string) => void;
}

/**
 * 组报面板（框架组长 tab）：合并组员定稿归一稿 → AI 草稿（先预览确认）→
 * 人工修改 → 定稿 → 一键带入 SE。结构（产品线 × 类别）由核心库渲染。
 */
export function GroupReport({ week, refreshTick, onClose, onToast }: GroupReportProps) {
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
      accept(await ReportService.GroupReport(week, ""), false);
      setError(null);
    } catch (err) {
      setError(readableError(err));
    }
  }, [week, accept]);

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
      const d = await ReportService.SaveGroup(week, "", body);
      knownBodyRef.current = d.body;
      if (pendingRef.current === body) pendingRef.current = null;
      setData(d);
      setSaving(pendingRef.current === null ? "saved" : "dirty");
    } catch (err) {
      setSaving("dirty");
      onToast(`保存组报失败：${readableError(err)}`);
    }
  }, [week, onToast]);

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
      const p = await ReportService.PrepareGroup(week, "", allowMissing, force);
      setPlan({ preview: p.preview, allowMissing, force });
    } catch (err) {
      const msg = readableError(err);
      if (!allowMissing && msg.includes("未定稿")) {
        setAsk({ text: msg.replace(/（确认先不纳入.*）/, ""), allowMissing: true, force });
      } else if (!force && msg.includes("--force")) {
        setAsk({ text: msg.replace(/；?确认要覆盖请加 --force/, ""), allowMissing, force: true });
      } else {
        onToast(`准备组报失败：${msg}`);
      }
    } finally {
      setBusy(false);
    }
  };

  const generate = async () => {
    if (!plan) return;
    try {
      const d = await ReportService.GenerateGroup(week, "", plan.allowMissing, plan.force);
      pendingRef.current = null;
      accept(d, true);
      setPlan(null);
      if (d.warning) onToast(d.warning);
    } catch (err) {
      onToast(`生成组报失败：${readableError(err)}`);
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
      await ReportService.SubmitGroup(week, "");
      await load();
      onToast("已带入 SE tab，组长名下显示「已交」");
    } catch (err) {
      onToast(`带入 SE 失败：${readableError(err)}`);
    } finally {
      setBusy(false);
    }
  };

  const final = data?.status === "final";

  return (
    <section className="report-detail is-wide">
      <div className="report-detail-head">
        <div className="report-detail-head-main">
          <div className="report-detail-title">{data?.group ? `${data.group} 组报` : "组报"}</div>
          <div className="report-detail-meta">
            <span>{week}</span>
            {data?.leadName && <span>{data.leadName}</span>}
          </div>
        </div>
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
          <div className="report-empty">正在读取组报…</div>
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
                      onClick={() => void act(() => ReportService.UnfinalizeGroup(week, ""), true)}
                    >
                      取消定稿
                    </button>
                    <button
                      type="button"
                      className="btn btn-primary"
                      disabled={busy}
                      onClick={() => void submit()}
                    >
                      带入 SE
                    </button>
                  </>
                ) : (
                  <button
                    type="button"
                    className="btn btn-primary"
                    disabled={busy}
                    onClick={() => void act(() => ReportService.FinalizeGroup(week, ""))}
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
                <p>
                  合并本组所有组员已定稿的归一稿，按「产品线 → 类别」整理成组报。发送前先给你预览，定稿后可一键带入
                  SE tab。
                </p>
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
                  placeholder="组报正文"
                />
              </div>
            )}
          </div>
        )}
      </div>

      {plan && (
        <SendPreview
          title="发送给 AI 生成组报"
          preview={plan.preview}
          onCancel={() => setPlan(null)}
          onConfirm={generate}
        />
      )}

      {templateOpen && <TemplateModal onClose={() => setTemplateOpen(false)} onToast={onToast} />}
    </section>
  );
}

/** 编辑组报模板：「## 栏目」一行一个，紧跟的「> 」行是给 AI 的说明。 */
function TemplateModal({ onClose, onToast }: { onClose: () => void; onToast: (t: string) => void }) {
  const [body, setBody] = useState<string | null>(null);
  const [isDefault, setIsDefault] = useState(false);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    void ReportService.Template("group")
      .then((t) => {
        setBody(t.body);
        setIsDefault(t.isDefault);
      })
      .catch((err) => setError(readableError(err)));
  }, []);

  const save = async () => {
    if (body === null) return;
    try {
      await ReportService.SaveTemplate("group", body);
      onToast("已保存模板，下次生成组报时生效");
      onClose();
    } catch (err) {
      setError(readableError(err));
    }
  };

  return (
    <div className="modal-mask is-center is-nested" onMouseDown={onClose}>
      <div className="modal template-modal" onMouseDown={(e) => e.stopPropagation()}>
        <h2>组报模板</h2>
        <div className="settings-desc">
          「## 栏目名」一行一个栏目，可增删、改名、调整顺序；紧跟的「&gt; 」行是给 AI 的说明，含
          [产品线×类别] 的栏目按产品线 → 类别输出。
          {isDefault && " 当前是内置默认模板，保存后写到工作台 templates/group.md。"}
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
