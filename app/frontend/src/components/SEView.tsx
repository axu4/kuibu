// 项目汇总（SE）：我是 SE 的项目，收各组 DE 交来的项目报，合并成项目周报 / 项目月报。
//
// 本工作台整理的组（比如自己的组）直接引用该组这个项目的定稿组报；其他 DE 交来的
// 原文在这里粘贴归档。合并由核心库按「总结 / 计划 / 其他事项 → 组」排好，不调 AI，
// 人工修改、定稿后才能复制和导出。周报、月报同一个组件，mode 区分。

import { useCallback, useEffect, useRef, useState } from "react";
import { Events } from "@wailsio/runtime";
import { ReportService } from "../../bindings/github.com/axu4/kuibu/app";
import type { GroupData, ReportMonth, ReportWeek } from "../../bindings/github.com/axu4/kuibu/app";
import type {
  ProjectInbox,
  ProjectInboxEntry,
  ProjectInboxSource,
} from "../../bindings/github.com/axu4/kuibu/internal/core";
import type { ReportProject } from "../../bindings/github.com/axu4/kuibu/internal/store";
import { copyRich } from "../lib/clipboard";
import { formatSubmittedAt, weekRangeLabel } from "../lib/report";
import { readableError } from "../lib/todo";
import { Editor } from "./LazyEditor";
import { Icon } from "./Icon";
import { TriageProjectSettings } from "./triage/TriageProjectSettings";

const SAVE_DEBOUNCE_MS = 800;
const PROJECT_KEY = "kuibu.se.project";

interface SEViewProps {
  mode: "week" | "month";
}

interface Period {
  id: string;
  label: string;
  isCurrent: boolean;
}

function fromWeek(w: ReportWeek): Period {
  return { id: w.week, label: `${w.week} · ${weekRangeLabel(w.start, w.end)}`, isCurrent: w.isCurrent };
}

function fromMonth(m: ReportMonth): Period {
  return { id: m.month, label: m.month, isCurrent: m.isCurrent };
}

/** 收件状态 → 行样式（复用周报列表的状态点）与文案。 */
function entryState(status: string, overdue: boolean): { cls: string; text: string } {
  switch (status) {
    case "linked":
      return { cls: "submitted", text: "引用本组定稿" };
    case "submitted":
      return { cls: "submitted", text: "已交" };
    case "late":
      return { cls: "late", text: "迟交" };
    case "draft":
      return { cls: "missing", text: "组报未定稿" };
  }
  return { cls: "missing", text: overdue ? "逾期未交" : "未交" };
}

function loadProjectPref(): string {
  try {
    return localStorage.getItem(PROJECT_KEY) ?? "";
  } catch {
    return "";
  }
}

function saveProjectPref(id: string) {
  try {
    localStorage.setItem(PROJECT_KEY, id);
  } catch {
    // 本地存储不可用时只是不记住上次的项目。
  }
}

export function SEView({ mode }: SEViewProps) {
  const noun = mode === "month" ? "项目月报" : "项目周报";
  const [toast, setToast] = useState<string | null>(null);
  const onToast = setToast;
  useEffect(() => {
    if (!toast) return;
    const timer = setTimeout(() => setToast(null), 3200);
    return () => clearTimeout(timer);
  }, [toast]);
  const [period, setPeriod] = useState<Period | null>(null);
  const [projects, setProjects] = useState<ReportProject[] | null>(null);
  const [project, setProject] = useState<string>(loadProjectPref);
  const [inbox, setInbox] = useState<ProjectInbox | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [panel, setPanel] = useState<{ kind: "source"; group: string } | { kind: "report" } | null>(null);
  const [paste, setPaste] = useState<{ group: string; text: string } | null>(null);
  const [settingsOpen, setSettingsOpen] = useState(false);
  const [refreshTick, setRefreshTick] = useState(0);
  const periodRef = useRef<Period | null>(null);
  periodRef.current = period;

  const loadProjects = useCallback(async () => {
    try {
      const list = (await ReportService.SEProjects()) ?? [];
      setProjects(list);
      setProject((cur) => (list.some((p) => p.id === cur) ? cur : (list[0]?.id ?? "")));
    } catch (err) {
      setProjects([]);
      setError(readableError(err));
    }
  }, []);

  const loadInbox = useCallback(async (p: Period | null, projectId: string) => {
    if (!p || !projectId) {
      setInbox(null);
      return;
    }
    try {
      setInbox(await ReportService.SEInbox(p.id, projectId));
      setError(null);
    } catch (err) {
      setInbox(null);
      setError(readableError(err));
    }
  }, []);

  useEffect(() => {
    void loadProjects();
    const first = mode === "month" ? ReportService.Month("this").then(fromMonth) : ReportService.Week("this").then(fromWeek);
    first.then(setPeriod).catch((err) => setError(readableError(err)));
  }, [loadProjects, mode]);

  useEffect(() => {
    if (project) saveProjectPref(project);
    void loadInbox(period, project);
  }, [loadInbox, period, project]);

  useEffect(() => {
    const offReport = Events.On("report:changed", () => {
      void loadInbox(periodRef.current, project);
      setRefreshTick((t) => t + 1);
    });
    const offTriage = Events.On("triage:changed", () => void loadProjects());
    return () => {
      offReport();
      offTriage();
    };
  }, [loadInbox, loadProjects, project]);

  const shift = async (delta: number) => {
    const p = periodRef.current;
    if (!p) return;
    try {
      setPanel(null);
      setPeriod(
        mode === "month"
          ? fromMonth(await ReportService.ShiftMonth(p.id, delta))
          : fromWeek(await ReportService.ShiftWeek(p.id, delta)),
      );
    } catch (err) {
      onToast(`切换失败：${readableError(err)}`);
    }
  };

  const backToCurrent = async () => {
    setPanel(null);
    setPeriod(mode === "month" ? fromMonth(await ReportService.Month("this")) : fromWeek(await ReportService.Week("this")));
  };

  const overdue = inbox ? Date.now() > new Date(inbox.deadline).getTime() : false;
  const counts = inbox?.counts ?? {};
  const activeProject = projects?.find((p) => p.id === project) ?? null;
  const entries = [...(inbox?.entries ?? []), ...(inbox?.extra ?? [])];

  const submitPaste = async () => {
    if (!paste || !period || !project) return;
    if (!paste.group) {
      onToast("先选是哪个组交来的");
      return;
    }
    if (!paste.text.trim()) {
      onToast("先把交来的内容贴进来");
      return;
    }
    try {
      setInbox(await ReportService.SEInboxSubmit(period.id, project, paste.group, paste.text));
      onToast("已归档");
      setPanel({ kind: "source", group: paste.group });
      setPaste(null);
      setRefreshTick((t) => t + 1);
    } catch (err) {
      onToast(`归档失败：${readableError(err)}`);
    }
  };

  return (
    <div className="report-view">
      <section className="report-main">
        <div className="toolbar">
          <div className="toolbar-spacer" />
        </div>
        <div className="report-scroll">
          <header className="report-head">
            <div className="report-title-row">
              <h1>项目汇总</h1>
              <div className="week-switcher">
                <button type="button" className="icon-btn" title={mode === "month" ? "上个月" : "上一周"} onClick={() => void shift(-1)}>
                  <Icon name="chevronLeft" size={14} />
                </button>
                <span className="week-label">{period?.label ?? "…"}</span>
                <button type="button" className="icon-btn" title={mode === "month" ? "下个月" : "下一周"} onClick={() => void shift(1)}>
                  <Icon name="chevron" size={14} />
                </button>
                {period && !period.isCurrent && (
                  <button type="button" className="btn btn-ghost" onClick={() => void backToCurrent()}>
                    {mode === "month" ? "回到本月" : "回到本周"}
                  </button>
                )}
              </div>
            </div>
            <div className="report-head-meta">
              <span>我当 SE 的项目：各组 DE 交来的内容在这里合并成{noun}</span>
              {inbox && <span>{formatSubmittedAt(inbox.deadline)}前交</span>}
            </div>
            {(projects?.length ?? 0) > 1 && (
              <div className="segmented report-tabs">
                {(projects ?? []).map((p) => (
                  <button
                    key={p.id}
                    type="button"
                    className={`segmented-btn${p.id === project ? " is-active" : ""}`}
                    onClick={() => {
                      setPanel(null);
                      setProject(p.id);
                    }}
                  >
                    {p.name}
                  </button>
                ))}
              </div>
            )}
          </header>

          {projects !== null && projects.length === 0 ? (
            <div className="report-empty">
              <div className="welcome-mark is-quiet">
                <Icon name="report" size={22} />
              </div>
              <p>还没有我当 SE 的项目。在项目设置里把项目设为「我是 SE」，各组交来的内容就在这里汇总。</p>
              <button type="button" className="btn btn-primary" onClick={() => setSettingsOpen(true)}>
                项目设置
              </button>
            </div>
          ) : (
            <>
              <div className="report-toolbar">
                <div className="report-stats">
                  <span>
                    应交 <b>{counts.expected ?? 0}</b>
                  </span>
                  <span>
                    已收 <b>{counts.received ?? 0}</b>
                  </span>
                  <span>
                    迟交 <b>{counts.late ?? 0}</b>
                  </span>
                  <span>
                    未交 <b>{(counts.missing ?? 0) + (counts.draft ?? 0)}</b>
                  </span>
                </div>
                <div className="report-actions">
                  <button
                    type="button"
                    className={`btn${panel?.kind === "report" ? " is-on" : ""}`}
                    disabled={!project}
                    onClick={() => setPanel((p) => (p?.kind === "report" ? null : { kind: "report" }))}
                  >
                    <Icon name="report" size={14} />
                    {noun}
                  </button>
                  <button
                    type="button"
                    className="btn btn-primary"
                    disabled={!project}
                    onClick={() => setPaste({ group: "", text: "" })}
                  >
                    <Icon name="paste" size={14} />
                    粘贴交来的{mode === "month" ? "月报" : "周报"}
                  </button>
                  <button type="button" className="btn btn-ghost" onClick={() => setSettingsOpen(true)}>
                    项目设置
                  </button>
                </div>
              </div>

              {error && <div className="settings-error">{error}</div>}

              <div className="report-list">
                <div className="report-section">
                  {activeProject && <div className="report-section-title">{activeProject.name} · 应交的组</div>}
                  {entries.map((e) => (
                    <InboxRow
                      key={e.groupId}
                      entry={e}
                      overdue={overdue}
                      selected={panel?.kind === "source" && panel.group === e.groupId}
                      onSelect={() => setPanel({ kind: "source", group: e.groupId })}
                      onPaste={() => setPaste({ group: e.groupId, text: "" })}
                    />
                  ))}
                  {inbox && entries.length === 0 && (
                    <div className="report-empty">
                      <p>还没有组。先在设置 → 组管理里加上各组（组名和 DE）。</p>
                    </div>
                  )}
                </div>
              </div>
              <p className="month-project-hint">
                本工作台整理的组（比如你自己的组）直接引用它这个项目的定稿，不用再贴；其他组的 DE 交来的内容点「粘贴」归档。
                {noun}按「总结 / 计划 / 其他事项」分节、节内按组合并，各组自己写的小标题原样保留。
              </p>
            </>
          )}
        </div>
      </section>

      {panel?.kind === "source" && period && project && (
        <SourcePanel
          period={period.id}
          project={project}
          group={panel.group}
          refreshTick={refreshTick}
          onRepaste={(group) => setPaste({ group, text: "" })}
          onDeleted={(view) => {
            setInbox(view);
            setPanel(null);
          }}
          onClose={() => setPanel(null)}
          onToast={onToast}
        />
      )}

      {panel?.kind === "report" && period && project && (
        <ProjectReportPanel
          period={period.id}
          project={project}
          noun={noun}
          refreshTick={refreshTick}
          onClose={() => setPanel(null)}
          onToast={onToast}
        />
      )}

      {paste && (
        <div className="modal-mask is-center" onMouseDown={() => setPaste(null)}>
          <div className="modal se-paste-modal" onMouseDown={(e) => e.stopPropagation()}>
            <h2>粘贴交来的{mode === "month" ? "月报" : "周报"}</h2>
            <p className="settings-desc">
              {activeProject?.name} · {period?.id}。原文原样归档；同一组再次粘贴会覆盖，旧版保留在历史里。
            </p>
            <select
              className="field"
              value={paste.group}
              onChange={(e) => setPaste({ ...paste, group: e.target.value })}
            >
              <option value="">选择交报的组…</option>
              {(inbox?.entries ?? []).map((e) => (
                <option key={e.groupId} value={e.groupId}>
                  {e.group}
                  {e.lead ? `（${e.lead}）` : ""}
                </option>
              ))}
            </select>
            <textarea
              className="field se-paste-text"
              autoFocus
              placeholder="把对方发来的内容贴在这里：本周 / 下周 / 其他这类小标题会自动识别"
              value={paste.text}
              onChange={(e) => setPaste({ ...paste, text: e.target.value })}
            />
            <div className="modal-buttons">
              <button type="button" className="btn" onClick={() => setPaste(null)}>
                取消
              </button>
              <button type="button" className="btn btn-primary" onClick={() => void submitPaste()}>
                归档
              </button>
            </div>
          </div>
        </div>
      )}

      {settingsOpen && (
        <TriageProjectSettings
          items={[]}
          onClose={() => setSettingsOpen(false)}
          onChanged={() => void loadProjects()}
          onToast={onToast}
        />
      )}

      {toast && (
        <div className="toast" role="alert" onClick={() => setToast(null)}>
          {toast}
        </div>
      )}
    </div>
  );
}

function InboxRow({
  entry,
  overdue,
  selected,
  onSelect,
  onPaste,
}: {
  entry: ProjectInboxEntry;
  overdue: boolean;
  selected: boolean;
  onSelect: () => void;
  onPaste: () => void;
}) {
  const state = entryState(entry.status, overdue);
  const canPaste = entry.status === "missing" || entry.status === "draft";
  return (
    <div className={`report-row is-${state.cls}${selected ? " is-selected" : ""}`}>
      <button type="button" className="report-row-main" onClick={onSelect}>
        <span className={`report-status-dot is-${state.cls}`} aria-hidden="true" />
        <span className="report-row-name">{entry.group}</span>
        {entry.lead && <span className="report-row-group">DE {entry.lead}</span>}
        {entry.items > 0 && <span className="report-row-time">{entry.items} 条</span>}
        <span className={`report-row-state is-${state.cls}`}>{state.text}</span>
        {entry.submittedAt && <span className="report-row-time">{formatSubmittedAt(entry.submittedAt)}</span>}
      </button>
      {canPaste && (
        <button type="button" className="report-row-paste" title="粘贴这个组交来的内容" onClick={onPaste}>
          <Icon name="paste" size={13} />
          粘贴
        </button>
      )}
    </div>
  );
}

function SourcePanel({
  period,
  project,
  group,
  refreshTick,
  onRepaste,
  onDeleted,
  onClose,
  onToast,
}: {
  period: string;
  project: string;
  group: string;
  refreshTick: number;
  onRepaste: (group: string) => void;
  onDeleted: (view: ProjectInbox) => void;
  onClose: () => void;
  onToast: (text: string) => void;
}) {
  const [src, setSrc] = useState<ProjectInboxSource | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [confirmDelete, setConfirmDelete] = useState(false);

  useEffect(() => {
    let alive = true;
    setConfirmDelete(false);
    ReportService.SEInboxSource(period, project, group)
      .then((s) => alive && (setSrc(s), setError(null)))
      .catch((err) => alive && setError(readableError(err)));
    return () => {
      alive = false;
    };
  }, [period, project, group, refreshTick]);

  const remove = async () => {
    try {
      onDeleted(await ReportService.SEInboxDelete(period, project, group));
      onToast("已移入历史");
    } catch (err) {
      onToast(`删除失败：${readableError(err)}`);
    }
  };

  const status = src?.status ?? "";
  return (
    <section className="report-detail">
      <div className="report-detail-head">
        <div className="report-detail-head-main">
          <div className="report-detail-title">{src?.group ?? "…"}</div>
          <div className="report-detail-meta">
            <span>{period}</span>
            {src?.lead && <span>DE {src.lead}</span>}
            {src?.submittedAt && <span>{formatSubmittedAt(src.submittedAt)}</span>}
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
        ) : !src ? (
          <div className="report-empty">正在读取…</div>
        ) : status === "linked" ? (
          <>
            <div className="settings-hint">引用本组这个项目的定稿组报，组报改动后这里自动更新。</div>
            <pre className="report-body">{src.body}</pre>
          </>
        ) : status === "submitted" || status === "late" ? (
          <>
            <div className="report-detail-actions">
              <button type="button" className="btn" onClick={() => onRepaste(src.groupId)}>
                重新粘贴
              </button>
              {confirmDelete ? (
                <button type="button" className="btn btn-danger" onClick={() => void remove()}>
                  确认删除
                </button>
              ) : (
                <button type="button" className="btn btn-ghost" onClick={() => setConfirmDelete(true)}>
                  删除
                </button>
              )}
            </div>
            <pre className="report-body">{src.body}</pre>
          </>
        ) : status === "draft" ? (
          <div className="report-empty">
            <p>本组这个项目的组报还没定稿。去「我的组」整理并定稿后，会自动出现在这里。</p>
          </div>
        ) : (
          <div className="report-empty">
            <p>还没交。收到后点「粘贴」归档。</p>
            <button type="button" className="btn btn-primary" onClick={() => onRepaste(src.groupId)}>
              <Icon name="paste" size={14} />
              粘贴
            </button>
          </div>
        )}
      </div>
    </section>
  );
}

function ProjectReportPanel({
  period,
  project,
  noun,
  refreshTick,
  onClose,
  onToast,
}: {
  period: string;
  project: string;
  noun: string;
  refreshTick: number;
  onClose: () => void;
  onToast: (text: string) => void;
}) {
  const [data, setData] = useState<GroupData | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  const [revision, setRevision] = useState(0);
  const [saving, setSaving] = useState<"saved" | "saving" | "dirty">("saved");
  const [ask, setAsk] = useState<{ text: string; allowMissing: boolean; force: boolean } | null>(null);
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

  useEffect(() => {
    ReportService.SEReport(period, project)
      .then((d) => {
        accept(d, false);
        setError(null);
      })
      .catch((err) => setError(readableError(err)));
  }, [accept, period, project, refreshTick]);

  const flush = useCallback(async () => {
    if (timerRef.current) {
      clearTimeout(timerRef.current);
      timerRef.current = null;
    }
    const body = pendingRef.current;
    if (body === null) return true;
    setSaving("saving");
    try {
      const d = await ReportService.SESave(period, project, body);
      knownBodyRef.current = d.body;
      if (pendingRef.current === body) pendingRef.current = null;
      setData(d);
      setSaving(pendingRef.current === null ? "saved" : "dirty");
      return true;
    } catch (err) {
      setSaving("dirty");
      onToast(`保存${noun}失败：${readableError(err)}`);
      return false;
    }
  }, [noun, onToast, period, project]);

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

  const generate = async (allowMissing: boolean, force: boolean) => {
    setBusy(true);
    setAsk(null);
    try {
      if (!(await flush())) return;
      const d = await ReportService.SEGenerate(period, project, allowMissing, force);
      pendingRef.current = null;
      accept(d, true);
      onToast(`已生成${noun}草稿，请审阅`);
    } catch (err) {
      const msg = readableError(err);
      if (!allowMissing && msg.includes("没交齐")) {
        setAsk({ text: msg.replace(/（确认先不纳入.*）/, ""), allowMissing: true, force });
      } else if (!force && msg.includes("--force")) {
        setAsk({ text: msg.replace(/；?确认要覆盖请加 --force/, ""), allowMissing, force: true });
      } else {
        onToast(`生成${noun}失败：${msg}`);
      }
    } finally {
      setBusy(false);
    }
  };

  const toggleFinal = async () => {
    if (!data?.exists) return;
    setBusy(true);
    try {
      if (!(await flush())) return;
      const final = data.status === "final";
      accept(final ? await ReportService.SEUnfinalize(period, project) : await ReportService.SEFinalize(period, project), true);
      onToast(final ? "已取消定稿" : "已定稿");
    } catch (err) {
      onToast(readableError(err));
    } finally {
      setBusy(false);
    }
  };

  const copyFeishu = async () => {
    try {
      const c = await ReportService.SEExportClipboard(period, project);
      const mode = await copyRich(c.html, c.text);
      onToast(mode === "html" ? "已复制，粘贴到飞书即可（保留标题、列表格式）" : "当前环境不支持富文本剪贴板，已复制为纯文本");
    } catch (err) {
      onToast(`复制失败：${readableError(err)}`);
    }
  };

  const exportMd = async () => {
    try {
      const path = await ReportService.SEExport(period, project);
      if (path) onToast(`已导出到 ${path}`);
    } catch (err) {
      onToast(`导出失败：${readableError(err)}`);
    }
  };

  const final = data?.status === "final";
  return (
    <section className="report-detail is-wide">
      <div className="report-detail-head">
        <div className="report-detail-head-main">
          <div className="report-detail-title">
            {data?.projectName ?? ""} {noun}
          </div>
          <div className="report-detail-meta">
            <span>{period}</span>
            {data?.finalizedAt && <span>{formatSubmittedAt(data.finalizedAt)} 定稿</span>}
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
          <div className="report-empty">正在读取{noun}…</div>
        ) : (
          <div className="report-draft">
            <div className="report-draft-bar">
              {data.exists ? (
                <span className={`draft-badge is-${data.status}`}>{final ? "已定稿" : "草稿"}</span>
              ) : (
                <span className="draft-badge">未生成</span>
              )}
              {data.sourceChanged && <span className="draft-badge is-warn">各组交来的内容有更新</span>}
              {data.exists && !final && saving !== "saved" && (
                <span className="draft-saving">{saving === "saving" ? "保存中…" : "未保存"}</span>
              )}
              <span className="report-draft-spacer" />
              <button type="button" className="btn" disabled={busy || final} onClick={() => void generate(false, false)}>
                <Icon name="bolt" size={14} />
                {data.exists ? "按各组内容重新合并" : "合并生成"}
              </button>
              {data.exists && (
                <button type="button" className={`btn${final ? "" : " btn-primary"}`} disabled={busy} onClick={() => void toggleFinal()}>
                  {final ? "取消定稿" : "定稿"}
                </button>
              )}
              <button type="button" className="btn btn-ghost" disabled={!final} title={final ? undefined : "定稿后才能复制"} onClick={() => void copyFeishu()}>
                复制飞书
              </button>
              <button type="button" className="btn btn-ghost" disabled={!final} title={final ? undefined : "定稿后才能导出"} onClick={() => void exportMd()}>
                导出
              </button>
            </div>

            {ask && (
              <div className="settings-hint report-draft-ask">
                <span>{ask.text}</span>
                <button type="button" className="link-btn" onClick={() => void generate(ask.allowMissing, ask.force)}>
                  {ask.allowMissing && !ask.force ? "仍然生成（标注未纳入）" : "仍要重新合并"}
                </button>
              </div>
            )}

            {data.exists && (data.missing ?? []).length > 0 && (
              <div className="settings-hint">未纳入：{(data.missing ?? []).join("、")}</div>
            )}

            {!data.exists ? (
              <div className="report-empty">
                <p>把各组交来的内容按「总结 / 计划 / 其他事项」分节、节内按组合并成{noun}草稿。不调 AI，合并后人工改、定稿。</p>
              </div>
            ) : final ? (
              <>
                <div className="settings-hint">已定稿，只读。需要修改请先「取消定稿」。</div>
                <pre className="report-body">{data.body}</pre>
              </>
            ) : (
              <div className="report-draft-editor">
                <Editor
                  key={`se-${period}-${project}-${revision}`}
                  noteId={`se/${period}/${project}/${revision}`}
                  initialBody={data.body}
                  onChange={onChange}
                  placeholder={`${noun}正文`}
                />
              </div>
            )}
          </div>
        )}
      </div>
    </section>
  );
}
