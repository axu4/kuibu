// 项目月报三步流程：范围与来源 → 候选 → 整理月报。
//
// 与周报三步流程（TriageFlow）同一套骨架，区别是候选来源从「组员原文」换成
// 「本期各周已定稿的按项目组报」。步骤状态不额外落盘，全部由「月报拉取记录 +
// 各项目月报草稿」推导，刷新 / 重开能接着上次的进度。
//
// 数据读写一律走 ReportService 的 *MonthProject* 方法；服务端写入后会发
// report:changed，这里只刷新项目列表与候选，不打断正在编辑的正文。

import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { Events } from "@wailsio/runtime";
import { ReportService } from "../../bindings/github.com/axu4/kuibu/app";
import type { GroupData, GroupInfoData, ProjectMonthList } from "../../bindings/github.com/axu4/kuibu/app";
import type {
  MonthCandidatesData,
  MonthItem,
  MonthSourceView,
} from "../../bindings/github.com/axu4/kuibu/internal/core";
import { copyRich } from "../lib/clipboard";
import { readableError } from "../lib/todo";
import { Icon } from "./Icon";
import { DeliveryBar } from "./DeliveryBar";
import { formatClock, normalizeBody } from "./triage/shared";

type Step = 1 | 2 | 3;

interface MonthProjectFlowProps {
  /** 月份编号，形如 2026-10。 */
  month: string;
  /** 进入时预选的组；空字符串由服务端按当前组长解析。 */
  group?: string;
  /** 进入时直接打开的项目（月报首页卡片点进来）。 */
  initialProject?: string | null;
  onClose: () => void;
  onToast: (text: string) => void;
}

interface ReportState {
  data: GroupData | null;
  body: string;
  savedBody: string;
  loading: boolean;
  error: string | null;
  saving: "saved" | "saving" | "dirty";
}

function emptyReportState(): ReportState {
  return { data: null, body: "", savedBody: "", loading: false, error: null, saving: "saved" };
}

/** 2026-W44 -> W44。 */
function shortWeek(week: string): string {
  const m = /^\d{4}-(W\d{2})$/.exec(week);
  return m ? m[1] : week;
}

/** 拼接周范围：连续周压缩成 W40–W44，中间有缺口时用顿号分开。 */
function weeksLabel(weeks: string[] | null | undefined): string {
  const list = Array.from(new Set(weeks ?? [])).sort();
  if (list.length === 0) return "—";
  const chunks: Array<{ start: number; end: number }> = [];
  const nums = list.map((w) => {
    const m = /-W(\d+)$/.exec(w);
    return m ? Number(m[1]) : 0;
  });
  let start = nums[0];
  let prev = nums[0];
  for (const n of nums.slice(1)) {
    if (n === prev + 1) {
      prev = n;
      continue;
    }
    chunks.push({ start, end: prev });
    start = n;
    prev = n;
  }
  chunks.push({ start, end: prev });
  return chunks
    .map(({ start: a, end: b }) =>
      a === b ? `W${a}` : `W${a}–W${b}`,
    )
    .join("、");
}

function weekStatusWord(status: string): string {
  if (status === "final") return "已定稿";
  if (status === "draft") return "未定稿";
  if (status === "missing") return "未提交";
  return status || "未知";
}

/** 项目月报列表的一行 → 状态文案与颜色。 */
function rowStatus(row: ProjectMonthList | undefined): { text: string; cls: string } {
  if (!row || !row.exists) return { text: "未生成", cls: "is-none" };
  if (row.status === "final") return { text: "已定稿", cls: "is-final" };
  if (row.sourceChanged) return { text: "候选已更新", cls: "is-stale" };
  const clock = formatClock(row.editedAt);
  return { text: `已保存${clock ? ` · ${clock}` : ""}`, cls: "is-saved" };
}

interface FlowStepProps {
  n: number;
  title: string;
  sub: string;
  on: boolean;
  done: boolean;
  locked: boolean;
  onClick: () => void;
}

function FlowStep({ n, title, sub, on, done, locked, onClick }: FlowStepProps) {
  return (
    <button
      type="button"
      className={`triage-flow-step${on ? " is-on" : ""}${done ? " is-done" : ""}${
        locked ? " is-locked" : ""
      }`}
      onClick={onClick}
      title={locked ? "先完成上一步" : title}
    >
      <span className="triage-flow-num">{done && !on ? "✓" : n}</span>
      <span className="triage-flow-text">
        <span className="triage-flow-title">{title}</span>
        <span className="triage-flow-sub">{sub}</span>
      </span>
    </button>
  );
}

interface PoolItemRowProps {
  item: MonthItem;
  busy: boolean;
  onMove: (item: MonthItem, pool: string) => void;
}

/** 候选池里的一条：正文 + 来源周标签 + 改归口按钮。 */
function PoolItemRow({ item, busy, onMove }: PoolItemRowProps) {
  const weeks = item.weeks ?? [];
  return (
    <div className={`month-pool-item${item.pool === "excluded" ? " is-excluded" : ""}`}>
      <div className="month-pool-text">{item.text}</div>
      <div className="month-pool-item-meta">
        {weeks.map((week) => (
          <span key={week} className="month-week-tag">
            {shortWeek(week)}
          </span>
        ))}
        {item.duplicated > 1 && <span className="month-dup">合并 {item.duplicated} 周</span>}
      </div>
      <div className="month-item-actions">
        {item.pool === "summary" && (
          <>
            <button
              type="button"
              className="month-item-btn is-accent"
              disabled={busy}
              onClick={() => onMove(item, "plan")}
            >
              转为下月计划
            </button>
            <button
              type="button"
              className="month-item-btn"
              disabled={busy}
              onClick={() => onMove(item, "excluded")}
            >
              不纳入
            </button>
          </>
        )}
        {item.pool === "plan" && (
          <>
            <button
              type="button"
              className="month-item-btn is-accent"
              disabled={busy}
              onClick={() => onMove(item, "summary")}
            >
              移回本月总结
            </button>
            <button
              type="button"
              className="month-item-btn"
              disabled={busy}
              onClick={() => onMove(item, "excluded")}
            >
              不纳入
            </button>
          </>
        )}
        {item.pool === "archive" && (
          <>
            <button
              type="button"
              className="month-item-btn is-accent"
              disabled={busy}
              onClick={() => onMove(item, "plan")}
            >
              纳入计划
            </button>
            <button
              type="button"
              className="month-item-btn"
              disabled={busy}
              onClick={() => onMove(item, "excluded")}
            >
              不纳入
            </button>
          </>
        )}
        {(item.pool === "excluded" || item.pool === "") && (
          <button
            type="button"
            className="month-item-btn is-accent"
            disabled={busy}
            onClick={() => onMove(item, item.section === "summary" ? "summary" : "plan")}
          >
            恢复
          </button>
        )}
      </div>
    </div>
  );
}

/** 项目月报三步流程：全屏覆盖层，组内所有服务调用都显式带 group。 */
export function MonthProjectFlow({
  month,
  group = "",
  initialProject = null,
  onClose,
  onToast,
}: MonthProjectFlowProps) {
  const [groupOptions, setGroupOptions] = useState<GroupInfoData[]>([]);
  const [activeGroup, setActiveGroup] = useState(group);
  const [sources, setSources] = useState<MonthSourceView | null>(null);
  const [rows, setRows] = useState<ProjectMonthList[]>([]);
  const [candidates, setCandidates] = useState<MonthCandidatesData | null>(null);
  const [activeProject, setActiveProject] = useState<string | null>(initialProject);
  const [step, setStep] = useState<Step>(1);
  const [allowMissing, setAllowMissing] = useState(false);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  const [report, setReport] = useState<ReportState>(emptyReportState);

  const busyRef = useRef(false);
  const reportRef = useRef<ReportState>(report);
  const bootstrappedRef = useRef(false);
  const loadedProjectRef = useRef<string | null>(null);
  const generateArmedRef = useRef<string | null>(null);
  // 换组 / 换项目时可能有旧请求回来，用递增序号丢弃过期结果。
  const loadSeqRef = useRef(0);
  const rowsSeqRef = useRef(0);
  const candidatesSeqRef = useRef(0);

  const patchReport = useCallback((patch: (state: ReportState) => ReportState) => {
    setReport((prev) => {
      const next = patch(prev);
      reportRef.current = next;
      return next;
    });
  }, []);

  const applyReport = useCallback((data: GroupData) => {
    const next: ReportState = {
      data,
      body: data.body ?? "",
      savedBody: data.body ?? "",
      loading: false,
      error: null,
      saving: "saved",
    };
    reportRef.current = next;
    setReport(next);
  }, []);

  const resetReport = useCallback(() => {
    const next = emptyReportState();
    reportRef.current = next;
    setReport(next);
  }, []);

  // 一个组长可能带多个组；先读组列表，多个组时先选组。
  useEffect(() => {
    let alive = true;
    ReportService.GroupList()
      .then((list) => {
        if (!alive) return;
        const groups = list ?? [];
        setGroupOptions(groups);
        if (groups.length === 0) {
          setError("还没有组，请先在「设置 → 组与成员」里建组");
          setLoading(false);
          return;
        }
        if (!activeGroup) {
          if (groups.length === 1) setActiveGroup(groups[0].name);
        } else if (!groups.some((g) => g.name === activeGroup)) {
          setActiveGroup(groups.length === 1 ? groups[0].name : "");
        }
      })
      .catch((err) => {
        if (alive) {
          setError(readableError(err));
          setLoading(false);
        }
      });
    return () => {
      alive = false;
    };
    // activeGroup 只用于判断首次自动选组，不重复拉名单。
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  const loadRows = useCallback(async () => {
    if (!activeGroup) return;
    const seq = ++rowsSeqRef.current;
    try {
      const list = (await ReportService.MonthProjectList(month, activeGroup)) ?? [];
      if (seq === rowsSeqRef.current) setRows(list);
    } catch {
      // 列表刷新失败时保持旧值，不打断当前操作。
    }
  }, [month, activeGroup]);

  const loadCandidates = useCallback(
    async (project: string) => {
      if (!activeGroup) return;
      const seq = ++candidatesSeqRef.current;
      try {
        const data = await ReportService.MonthProjectCandidates(month, activeGroup, project);
        if (seq === candidatesSeqRef.current) setCandidates(data);
      } catch (err) {
        if (seq !== candidatesSeqRef.current) return;
        setCandidates(null);
        onToast(`读取项目候选失败：${readableError(err)}`);
      }
    },
    [month, activeGroup, onToast],
  );

  // 首次进入（或换组）时拉来源视图 + 项目列表，并按已有草稿推导步骤。
  const loadAll = useCallback(async () => {
    if (!activeGroup) return;
    const seq = ++loadSeqRef.current;
    setLoading(true);
    setError(null);
    try {
      const [src, list] = await Promise.all([
        ReportService.MonthSources(month, activeGroup),
        ReportService.MonthProjectList(month, activeGroup).then((v) => v ?? []),
      ]);
      if (seq !== loadSeqRef.current) return;
      setSources(src);
      rowsSeqRef.current += 1;
      setRows(list);
      const projects = src.projects ?? [];
      const first = list.find((r) => r.exists)?.project ?? projects[0]?.id ?? null;
      setActiveProject((prev) => {
        if (prev && projects.some((p) => p.id === prev)) return prev;
        return first;
      });
      if (!bootstrappedRef.current) {
        bootstrappedRef.current = true;
        const preferred =
          initialProject && projects.some((p) => p.id === initialProject) ? initialProject : null;
        if (preferred) {
          setActiveProject(preferred);
          setStep(3);
        } else if (list.some((r) => r.exists)) {
          setStep(3);
        } else {
          setStep(1);
        }
      }
    } catch (err) {
      if (seq === loadSeqRef.current) setError(readableError(err));
    } finally {
      if (seq === loadSeqRef.current) setLoading(false);
    }
  }, [activeGroup, month, initialProject]);

  useEffect(() => {
    if (!activeGroup) return;
    void loadAll();
  }, [activeGroup, loadAll]);

  // 服务端写入后只刷新项目列表与第 2 步候选，不动编辑中的正文。
  useEffect(() => {
    if (!activeGroup) return;
    const off = Events.On("report:changed", () => {
      void loadRows();
      if (step === 2 && activeProject) void loadCandidates(activeProject);
    });
    return off;
  }, [activeGroup, activeProject, loadCandidates, loadRows, step]);

  // 第 2 步读当前项目候选；第 3 步读候选 + 月报（只切项目时读正文，避免冲掉编辑）。
  useEffect(() => {
    if (!activeProject) return;
    if (step === 2) {
      void loadCandidates(activeProject);
      return;
    }
    if (step !== 3) return;
    let alive = true;
    void loadCandidates(activeProject);
    if (loadedProjectRef.current === activeProject) return;
    loadedProjectRef.current = activeProject;
    patchReport(() => ({ ...emptyReportState(), loading: true }));
    ReportService.MonthProjectReport(month, activeGroup, activeProject)
      .then((data) => {
        if (!alive) return;
        patchReport(() => ({
          data,
          body: data.body ?? "",
          savedBody: data.body ?? "",
          loading: false,
          error: null,
          saving: "saved",
        }));
      })
      .catch((err) => {
        if (!alive) return;
        patchReport((state) => ({ ...state, loading: false, error: readableError(err) }));
      });
    return () => {
      alive = false;
    };
  }, [activeGroup, activeProject, loadCandidates, month, patchReport, step]);

  const projects = useMemo(() => sources?.projects ?? [], [sources]);
  const includedSlots = sources?.includedSlots ?? 0;
  const slotCount = sources?.slots ?? 0;
  const missing = sources?.missing ?? [];
  const scopeWeeks = sources?.scopeWeeks ?? [];
  const defaultWeeks = sources?.defaultWeeks ?? [];
  const scopeIsDefault = useMemo(() => {
    const a = [...scopeWeeks].sort().join(",");
    const b = [...defaultWeeks].sort().join(",");
    return a === b;
  }, [scopeWeeks, defaultWeeks]);
  const draftCount = rows.filter((row) => row.exists).length;
  const finalCount = rows.filter((row) => row.status === "final").length;
  const allFinal = projects.length > 0 && rows.length === projects.length && finalCount === projects.length;
  const dirty = Boolean(
    report.data &&
      report.data.exists &&
      report.data.status !== "final" &&
      normalizeBody(report.body) !== report.savedBody,
  );

  const activeRow = rows.find((row) => row.project === activeProject);
  const activeProjectInfo = projects.find((p) => p.id === activeProject) ?? null;
  const activeStatus = (() => {
    if (dirty) return { text: "未保存", cls: "is-dirty" };
    const data = report.data;
    if (!data || !data.exists) return rowStatus(activeRow);
    if (data.status === "final") return { text: "已定稿", cls: "is-final" };
    if (data.sourceChanged || activeRow?.sourceChanged) {
      return { text: "候选已更新", cls: "is-stale" };
    }
    const clock = formatClock(activeRow?.editedAt);
    return { text: `已保存${clock ? ` · ${clock}` : ""}`, cls: "is-saved" };
  })();

  /* ------------------------------ 数据操作 ------------------------------ */

  const toggleScope = useCallback(
    async (week: string, nextOn: boolean) => {
      if (!sources || busyRef.current) return;
      const current = sources.scopeWeeks ?? [];
      const wanted = nextOn ? [...current, week] : current.filter((w) => w !== week);
      const ordered = (sources.weeks ?? []).map((row) => row.week).filter((w) => wanted.includes(w));
      busyRef.current = true;
      setBusy(true);
      try {
        const fresh = await ReportService.SetMonthScope(month, activeGroup, ordered);
        setSources(fresh);
        // 周范围变了，缺周确认要重新勾。
        setAllowMissing(false);
        void loadRows();
      } catch (err) {
        onToast(`调整本期范围失败：${readableError(err)}`);
      } finally {
        busyRef.current = false;
        setBusy(false);
      }
    },
    [activeGroup, loadRows, month, onToast, sources],
  );

  const resetScope = useCallback(async () => {
    if (!sources || busyRef.current) return;
    const ordered = (sources.weeks ?? []).map((row) => row.week).filter((w) => defaultWeeks.includes(w));
    busyRef.current = true;
    setBusy(true);
    try {
      const fresh = await ReportService.SetMonthScope(month, activeGroup, ordered);
      setSources(fresh);
      setAllowMissing(false);
      void loadRows();
    } catch (err) {
      onToast(`恢复默认范围失败：${readableError(err)}`);
    } finally {
      busyRef.current = false;
      setBusy(false);
    }
  }, [activeGroup, defaultWeeks, loadRows, month, onToast, sources]);

  const toggleSource = useCallback(
    async (week: string, projectId: string, included: boolean) => {
      if (busyRef.current) return;
      busyRef.current = true;
      setBusy(true);
      try {
        const fresh = await ReportService.SetMonthSource(month, activeGroup, week, projectId, included);
        setSources(fresh);
        void loadRows();
      } catch (err) {
        onToast(`纳入 / 去选失败：${readableError(err)}`);
      } finally {
        busyRef.current = false;
        setBusy(false);
      }
    },
    [activeGroup, loadRows, month, onToast],
  );

  const moveItem = useCallback(
    async (item: MonthItem, pool: string) => {
      if (!activeProject || busyRef.current || item.pool === pool) return;
      busyRef.current = true;
      setBusy(true);
      try {
        const fresh = await ReportService.SetMonthItemPool(
          month,
          activeGroup,
          activeProject,
          item.section,
          item.id,
          pool,
        );
        setCandidates(fresh);
        void loadRows();
      } catch (err) {
        onToast(`调整归口失败：${readableError(err)}`);
      } finally {
        busyRef.current = false;
        setBusy(false);
      }
    },
    [activeGroup, activeProject, loadRows, month, onToast],
  );

  const flush = useCallback(async (): Promise<boolean> => {
    const state = reportRef.current;
    if (!state.data?.exists || state.data.status === "final") return true;
    if (normalizeBody(state.body) === state.savedBody) return true;
    const project = activeProject;
    if (!project) return true;
    patchReport((prev) => ({ ...prev, saving: "saving" }));
    try {
      const data = await ReportService.SaveMonthProject(month, activeGroup, project, state.body);
      applyReport(data);
      void loadRows();
      return true;
    } catch (err) {
      patchReport((prev) => ({ ...prev, saving: "dirty" }));
      onToast(`保存失败：${readableError(err)}`);
      return false;
    }
  }, [activeGroup, activeProject, applyReport, loadRows, month, onToast, patchReport]);

  const saveActive = useCallback(async () => {
    const state = reportRef.current;
    if (!state.data?.exists) {
      onToast("这份月报还没有草稿，先生成");
      return;
    }
    if (state.data.status === "final") {
      onToast("已定稿，只读；先取消定稿再改");
      return;
    }
    if (normalizeBody(state.body) === state.savedBody) {
      onToast("没有未保存的修改");
      return;
    }
    if (await flush()) onToast("已保存当前项目月报");
  }, [flush, onToast]);

  const generate = useCallback(async () => {
    const project = activeProject;
    if (!project || busyRef.current) return;
    const state = reportRef.current;
    if (state.data?.status === "final") {
      onToast("已定稿，先取消定稿再重新生成");
      return;
    }
    const missingWeeks = candidates?.missing ?? state.data?.missing ?? [];
    if (missingWeeks.length > 0 && !allowMissing) {
      onToast(
        `本期还有 ${missingWeeks.length} 份缺周（${missingWeeks.join(
          "、",
        )}）：回第 ① 步勾选「缺周也继续」再生成`,
      );
      return;
    }
    const hasDirty = Boolean(
      state.data?.exists && normalizeBody(state.body) !== state.savedBody,
    );
    const needsConfirm = Boolean(state.data?.humanEdited || hasDirty);
    if (needsConfirm && generateArmedRef.current !== project) {
      generateArmedRef.current = project;
      window.setTimeout(() => {
        if (generateArmedRef.current === project) generateArmedRef.current = null;
      }, 3000);
      onToast("再点一次：用当前候选重新生成这份月报，人工修改会丢");
      return;
    }
    generateArmedRef.current = null;
    busyRef.current = true;
    setBusy(true);
    try {
      const data = await ReportService.GenerateMonthProject(
        month,
        activeGroup,
        project,
        allowMissing,
        needsConfirm,
      );
      loadedProjectRef.current = project;
      applyReport(data);
      onToast(state.data?.exists ? "已按当前候选重新生成" : "已生成月报草稿");
      void loadRows();
      void loadCandidates(project);
    } catch (err) {
      onToast(`生成失败：${readableError(err)}`);
    } finally {
      busyRef.current = false;
      setBusy(false);
    }
  }, [activeGroup, activeProject, allowMissing, applyReport, candidates, loadCandidates, loadRows, month, onToast]);

  const toggleFinal = useCallback(async () => {
    const project = activeProject;
    const state = reportRef.current;
    if (!project || !state.data?.exists) {
      onToast("这份月报还没有草稿");
      return;
    }
    if (!(await flush())) return;
    busyRef.current = true;
    setBusy(true);
    try {
      const isFinal = reportRef.current.data?.status === "final";
      const data = isFinal
        ? await ReportService.UnfinalizeMonthProject(month, activeGroup, project)
        : await ReportService.FinalizeMonthProject(month, activeGroup, project);
      applyReport(data);
      onToast(isFinal ? "已取消定稿" : "已定稿，取消定稿后才能再改");
      void loadRows();
    } catch (err) {
      onToast(`操作失败：${readableError(err)}`);
    } finally {
      busyRef.current = false;
      setBusy(false);
    }
  }, [activeGroup, activeProject, applyReport, flush, loadRows, month, onToast]);

  const exportMd = useCallback(async () => {
    const project = activeProject;
    if (!project) return;
    if (!(await flush())) return;
    try {
      const path = await ReportService.ExportMonthProject(month, activeGroup, project);
      if (path) onToast(`已导出到 ${path}`);
    } catch (err) {
      onToast(`导出失败：${readableError(err)}`);
    }
  }, [activeGroup, activeProject, flush, month, onToast]);

  const copyFeishu = useCallback(async () => {
    const project = activeProject;
    if (!project) return;
    if (!(await flush())) return;
    try {
      const content = await ReportService.ExportMonthProjectClipboard(month, activeGroup, project);
      const mode = await copyRich(content.html, content.text);
      onToast(
        mode === "html"
          ? "已复制，粘贴到飞书即可（保留标题、列表格式）"
          : "当前环境不支持富文本剪贴板，已复制为纯文本",
      );
    } catch (err) {
      onToast(`复制失败：${readableError(err)}`);
    }
  }, [activeGroup, activeProject, flush, month, onToast]);

  const markDelivered = useCallback(
    async (delivered: boolean) => {
      const project = activeProject;
      if (!project) return;
      busyRef.current = true;
      setBusy(true);
      try {
        const data = await ReportService.MarkProjectDelivered(month, activeGroup, project, delivered);
        applyReport(data);
        onToast(delivered ? `已标记交给 ${data.delivery?.se || "SE"}` : "已撤销已交标记");
        void loadRows();
      } catch (err) {
        onToast(`操作失败：${readableError(err)}`);
      } finally {
        busyRef.current = false;
        setBusy(false);
      }
    },
    [activeGroup, activeProject, applyReport, loadRows, month, onToast],
  );

  /* ------------------------------ 导航 ------------------------------ */

  const switchProject = useCallback(
    async (project: string) => {
      if (project === activeProject) return;
      if (step === 3 && !(await flush())) return;
      setCandidates(null);
      if (step === 3) {
        loadedProjectRef.current = null;
        const next = { ...emptyReportState(), loading: true };
        reportRef.current = next;
        setReport(next);
      }
      setActiveProject(project);
    },
    [activeProject, flush, step],
  );

  const chooseGroup = useCallback(
    async (name: string) => {
      if (!name || name === activeGroup) return;
      if (step === 3 && !(await flush())) return;
      bootstrappedRef.current = false;
      loadedProjectRef.current = null;
      loadSeqRef.current += 1;
      rowsSeqRef.current += 1;
      candidatesSeqRef.current += 1;
      setActiveGroup(name);
      setSources(null);
      setRows([]);
      setCandidates(null);
      resetReport();
      setActiveProject(null);
      setStep(1);
      setAllowMissing(false);
      setError(null);
      setLoading(true);
    },
    [activeGroup, flush, resetReport, step],
  );

  const navigate = useCallback(
    async (target: Step) => {
      if (target === step) return;
      if (step === 3 && target !== 3) {
        if (!(await flush())) return;
      }
      if (target >= 2 && includedSlots === 0) {
        onToast("先在第 ① 步纳入至少一份已定稿的周组报");
        return;
      }
      if (target === 3) {
        const project =
          activeProject ?? rows.find((row) => row.exists)?.project ?? projects[0]?.id ?? null;
        if (!project) {
          onToast("还没有可整理的项目");
          return;
        }
        if (!activeProject) setActiveProject(project);
      }
      setStep(target);
    },
    [activeProject, flush, includedSlots, onToast, projects, rows, step],
  );

  const closeFlow = useCallback(async () => {
    if (!(await flush())) return;
    onClose();
  }, [flush, onClose]);

  // 键盘：⌘S 保存、Esc 回上一步 / 关闭；输入框里也接管 ⌘S。
  useEffect(() => {
    const onKey = (event: KeyboardEvent) => {
      const mod = event.metaKey || event.ctrlKey;
      if (mod && event.key.toLowerCase() === "s") {
        event.preventDefault();
        void saveActive();
        return;
      }
      if (event.key === "Escape") {
        event.preventDefault();
        void (async () => {
          if (step === 3) await navigate(2);
          else if (step === 2) await navigate(1);
          else await closeFlow();
        })();
      }
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [closeFlow, navigate, saveActive, step]);

  /* ------------------------------ 渲染 ------------------------------ */

  const sub1 = sources
    ? `${includedSlots}/${slotCount} 份周组报${missing.length > 0 ? ` · 缺 ${missing.length}` : ""}`
    : "拉取本期周组报";
  const sub2 = `${projects.length} 个项目 · ${sources?.candidateTotal ?? 0} 条候选`;
  const sub3 =
    draftCount > 0
      ? `${draftCount} 份草稿${finalCount > 0 ? ` · ${finalCount} 份定稿` : ""}${
          dirty ? " · 有未保存" : ""
        }`
      : "候选确认后开始整理";

  if (!activeGroup && groupOptions.length > 1 && !error) {
    return (
      <section className="report-detail is-wide triage-flow month-flow">
        <header className="triage-topbar">
          <button type="button" className="triage-back" onClick={() => void closeFlow()} title="返回周报">
            ‹ 周报
          </button>
          <span className="triage-topbar-divider" />
          <span className="triage-topbar-title">项目月报</span>
          <span className="triage-topbar-week">{month}</span>
          <span className="triage-topbar-spacer" />
          <button type="button" className="icon-btn" title="关闭" onClick={() => void closeFlow()}>
            <Icon name="close" />
          </button>
        </header>
        <div className="triage-body">
          <div className="triage-group-choose">
            <div className="triage-eyebrow">选择一个组开始</div>
            <h1 className="triage-h1">这次整理哪个组的月报？</h1>
            <p className="triage-lede">
              项目月报按组独立拉取：每个组分别拉本期周组报、分项目预览、成稿。你带着多个组，先选一个。
            </p>
            <div className="triage-group-picks">
              {groupOptions.map((g) => (
                <button
                  key={g.id}
                  type="button"
                  className="triage-group-pick"
                  onClick={() => void chooseGroup(g.name)}
                >
                  <b>{g.name}</b>
                  <span>
                    {(g.members ?? []).length} 位成员 · 组长 {g.leadName || "未指定"}
                  </span>
                </button>
              ))}
            </div>
            <div className="triage-group-actions">
              没有要选的组？去「设置 → 组与成员」里新增或调整。
            </div>
          </div>
        </div>
      </section>
    );
  }

  const actions =
    step === 1 ? (
      <>
        <span className="triage-flow-hint">
          {missing.length > 0
            ? allowMissing
              ? "已确认缺周继续"
              : `缺 ${missing.length} 份周组报`
            : "来源已完整"}
        </span>
        <button
          type="button"
          className="btn btn-primary"
          disabled={busy || includedSlots === 0}
          title={includedSlots === 0 ? "先纳入至少一份已定稿的周组报" : "进入项目候选"}
          onClick={() => void navigate(2)}
        >
          下一步：项目候选
        </button>
      </>
    ) : step === 2 ? (
      <>
        <button type="button" className="btn" disabled={busy} onClick={() => void navigate(1)}>
          上一步
        </button>
        <button type="button" className="btn btn-primary" disabled={busy} onClick={() => void navigate(3)}>
          下一步：整理月报
        </button>
      </>
    ) : (
      <>
        <button type="button" className="btn" disabled={busy} onClick={() => void navigate(2)}>
          上一步
        </button>
        <button
          type="button"
          className="btn"
          disabled={busy || !dirty}
          onClick={() => void saveActive()}
        >
          保存 <kbd>⌘S</kbd>
        </button>
        <button type="button" className="btn btn-primary" disabled={busy} onClick={() => void closeFlow()}>
          完成
        </button>
      </>
    );

  const cand = candidates;
  const planFrom = cand?.planFrom ?? "";
  const planMeta = planFrom
    ? `取自 ${shortWeek(planFrom)} 的下周计划`
    : "还没有可用的周组报";
  const lastScopeWeek = scopeWeeks.length > 0 ? scopeWeeks[scopeWeeks.length - 1] : "";
  const planGap = planFrom && lastScopeWeek && planFrom !== lastScopeWeek
    ? `（${shortWeek(lastScopeWeek)} 未纳入）`
    : "";
  const sourceGridStyle = {
    gridTemplateColumns: `210px 96px repeat(${projects.length}, minmax(150px, 1fr))`,
  };

  return (
    <section className="report-detail is-wide triage-flow month-flow">
      <header className="triage-topbar">
        <button type="button" className="triage-back" onClick={() => void closeFlow()} title="返回周报">
          ‹ 周报
        </button>
        <span className="triage-topbar-divider" />
        <span className="triage-topbar-title">项目月报</span>
        <span className="triage-topbar-week">{month}</span>
        {groupOptions.length > 1 ? (
          <select
            className="triage-group-select"
            value={activeGroup}
            onChange={(e) => void chooseGroup(e.target.value)}
            title="切换组"
          >
            {groupOptions.map((g) => (
              <option key={g.id} value={g.name}>
                {g.name}
              </option>
            ))}
          </select>
        ) : (
          (sources?.group || activeGroup) && (
            <span className="triage-topbar-group">{sources?.group || activeGroup}</span>
          )
        )}
        <span className="triage-topbar-spacer" />
        {step === 1 && missing.length > 0 && (
          <span className="triage-topbar-progress">缺周 {missing.length}</span>
        )}
        <button type="button" className="icon-btn" title="关闭" onClick={() => void closeFlow()}>
          <Icon name="close" />
        </button>
      </header>

      <div className="triage-stepper">
        <div className="triage-flow-steps">
          <FlowStep
            n={1}
            title="范围与来源"
            sub={sub1}
            on={step === 1}
            done={includedSlots > 0}
            locked={false}
            onClick={() => void navigate(1)}
          />
          <span className="triage-flow-line" />
          <FlowStep
            n={2}
            title="候选"
            sub={sub2}
            on={step === 2}
            done={projects.length > 0 && includedSlots > 0}
            locked={includedSlots === 0}
            onClick={() => void navigate(2)}
          />
          <span className="triage-flow-line" />
          <FlowStep
            n={3}
            title="整理月报"
            sub={sub3}
            on={step === 3}
            done={allFinal && !dirty}
            locked={includedSlots === 0 || !activeProject}
            onClick={() => void navigate(3)}
          />
        </div>
        <div className="triage-flow-actions">{actions}</div>
      </div>

      <div className="triage-body">
        {loading ? (
          <div className="triage-empty">正在读取本月周组报…</div>
        ) : error ? (
          <div className="triage-empty">
            <p>{error}</p>
            <p className="triage-empty-hint">
              项目月报需要先有组长、组和 1~4 个报送项目。先完成设置再回来拉取本周周组报。
            </p>
            <div className="triage-empty-actions">
              <button
                type="button"
                className="btn"
                onClick={() => {
                  setError(null);
                  void loadAll();
                }}
              >
                重试
              </button>
              <button type="button" className="btn" onClick={() => void closeFlow()}>
                返回周报
              </button>
            </div>
          </div>
        ) : !sources ? (
          <div className="triage-empty">正在读取本月周组报…</div>
        ) : step === 1 ? (
          <div className="month-source">
            <div className="month-source-head">
              <div className="triage-eyebrow">STEP 1 · 范围与来源</div>
              <h1 className="triage-h1">从周组报里拉出本期</h1>
              <p className="triage-lede">
                月报不重新读组员原文，只拉本期各周<b>已定稿</b>的按项目组报；跨周重复自动合并，再按「本月总结 /
                下月计划」分拣。月初 / 月末可以自己调整本期周范围。
              </p>
            </div>

            <div className="month-scope-bar">
              <span className="month-scope-label">
                本期范围 <b>{weeksLabel(scopeWeeks)}</b> · {scopeWeeks.length} 周
              </span>
              <span className="month-scope-note">
                {scopeIsDefault ? "默认按周报日归属" : "自定义范围"}；可勾选上月最后一周 / 下月第一周，或取消月末周
              </span>
              <button
                type="button"
                className="month-item-btn month-scope-reset"
                disabled={busy || scopeIsDefault}
                onClick={() => void resetScope()}
              >
                恢复默认
              </button>
            </div>

            <div className="month-src-stats">
              <span>
                已纳入 <b>{includedSlots}</b>/{slotCount} 份周组报
              </span>
              <span>
                候选 <b>{sources.candidateTotal}</b> 条 · 跨周重复已合并
              </span>
              <span>
                已定稿 <b>{sources.finalSlots}</b>/{slotCount} 份
              </span>
              <span className={missing.length > 0 ? "is-warn" : ""}>
                缺周 <b>{missing.length}</b> 份
              </span>
            </div>

            <div className="month-src-scroll">
              <div className="month-src-table">
                <div className="month-src-head" style={sourceGridStyle}>
                  <span>本期 / 周</span>
                  <span>周报日</span>
                  {projects.map((project) => (
                    <span key={project.id}>{project.name}</span>
                  ))}
                </div>
                {(sources.weeks ?? []).map((row) => (
                  <div
                    key={row.week}
                    className={`month-src-row${row.scope ? "" : " is-out"}`}
                    style={sourceGridStyle}
                  >
                    <div className="month-src-week">
                      <button
                        type="button"
                        className={`month-scope-btn${row.scope ? " is-on" : ""}`}
                        disabled={busy}
                        onClick={() => void toggleScope(row.week, !row.scope)}
                        title={row.scope ? "已纳入本期，点击取消" : "点击纳入本期"}
                      >
                        <span className="month-src-tick">{row.scope ? "✓" : ""}</span>
                        <span className="month-src-week-text">
                          <b>{shortWeek(row.week)}</b>
                          {row.label && <em className="month-side-tag">{row.label}</em>}
                        </span>
                      </button>
                    </div>
                    <div className="month-src-day">
                      {row.reportDay.slice(5)}
                      <small>{row.default ? "周报日" : "相邻周"}</small>
                    </div>
                    {projects.map((project) => {
                      const cell = (row.reports ?? []).find((r) => r.project === project.id);
                      if (!cell) {
                        return (
                          <div key={project.id} className="month-src-cell is-locked">
                            <span className="month-src-tick">–</span>
                            <span className="month-src-info">没有这份周组报</span>
                          </div>
                        );
                      }
                      if (!row.scope) {
                        return (
                          <div
                            key={project.id}
                            className={`month-src-cell is-out${
                              cell.status === "final" ? " is-final" : ""
                            }`}
                          >
                            <span className="month-src-tick">–</span>
                            <span className="month-src-info">
                              <b>{project.name}</b>
                              <span>
                                {cell.status === "final"
                                  ? `已定稿 ${cell.items} 条 · 不在本期`
                                  : `${weekStatusWord(cell.status)} · 不在本期`}
                              </span>
                            </span>
                          </div>
                        );
                      }
                      if (cell.status === "final") {
                        return (
                          <button
                            key={project.id}
                            type="button"
                            className={`month-src-cell${cell.included ? " is-on" : ""}`}
                            disabled={busy}
                            onClick={() => void toggleSource(row.week, project.id, !cell.included)}
                            title={cell.included ? "已纳入月报，点击去选" : "点击纳入月报"}
                          >
                            <span className="month-src-tick">{cell.included ? "✓" : ""}</span>
                            <span className="month-src-info">
                              <b>{project.name}</b>
                              <span>{cell.items} 条 · 已定稿</span>
                            </span>
                          </button>
                        );
                      }
                      return (
                        <div key={project.id} className="month-src-cell is-locked">
                          <span className="month-src-tick">!</span>
                          <span className="month-src-info">
                            <b>{project.name}</b>
                            <span>{weekStatusWord(cell.status)} · 不能纳入</span>
                          </span>
                        </div>
                      );
                    })}
                  </div>
                ))}
              </div>
            </div>

            {missing.length > 0 ? (
              <label className="month-gate">
                <input
                  type="checkbox"
                  checked={allowMissing}
                  onChange={(e) => setAllowMissing(e.target.checked)}
                />
                <span>
                  <b>已知 {missing.length} 份周组报未定稿 / 未提交</b>，仍按当前纳入的 {includedSlots}{" "}
                  份整理；月报开头会标注未纳入：{missing.join("、")}。
                </span>
              </label>
            ) : (
              <div className="month-notice is-ok">
                <b>本期 {slotCount} 份周组报全部已定稿</b> · 可直接下一步。
              </div>
            )}

            <p className="month-foot">
              本期由勾选的周组成；默认按周报日归属。只影响月报拉取范围，周组报仍只认定稿。范围变化后，已有草稿会标「候选已更新」。
            </p>
          </div>
        ) : step === 2 ? (
          <div className="triage-step triage-preview-step month-preview-step">
            <aside className="triage-report-list">
              <div className="triage-rl-head">
                <b>{projects.length} 个项目</b>
                <span>本月候选</span>
              </div>
              <div className="triage-rl-body">
                {projects.map((project) => {
                  const row = rows.find((r) => r.project === project.id);
                  const status = rowStatus(row);
                  return (
                    <button
                      key={project.id}
                      type="button"
                      className={`triage-rl-item${project.id === activeProject ? " is-active" : ""}`}
                      onClick={() => void switchProject(project.id)}
                    >
                      <span className="triage-rl-name">
                        {project.name}
                        <span className="triage-rl-count">{row?.candidates ?? 0} 条</span>
                      </span>
                      <span className={`triage-rl-status ${status.cls}`}>{status.text}</span>
                    </button>
                  );
                })}
              </div>
              <div className="triage-rl-foot">每份月报独立保存、定稿、导出。</div>
            </aside>

            <section className="month-preview">
              <header className="month-preview-head">
                <div>
                  <div className="triage-eyebrow">STEP 2 · 项目候选</div>
                  <h1 className="triage-h1">{activeProjectInfo?.name ?? "选择项目"}</h1>
                  <div className="month-preview-meta">
                    <span className="month-badge">{month}</span>
                    <span>
                      来源 {cand?.sourceWeeks?.length ?? 0} 份周组报 ·{" "}
                      {weeksLabel(cand?.sourceWeeks)}
                    </span>
                    <span>候选 {cand?.counts.total ?? 0} 条</span>
                    {planFrom && <span>下月计划取自 {shortWeek(planFrom)}</span>}
                  </div>
                </div>
              </header>

              {cand && (cand.missing ?? []).length > 0 && (
                <div className="month-notice">
                  <b>未纳入 {(cand.missing ?? []).join("、")}</b>
                  {planGap ? ` · 下月计划取自 ${shortWeek(planFrom)}` : ""}
                  。缺周会在月报开头标注。
                </div>
              )}
              {cand && (cand.sourceWeeks ?? []).length === 0 && (
                <div className="month-notice">
                  <b>这个项目本期还没有纳入周组报</b> · 回第 ① 步勾选，或直接生成空草稿手动补。
                </div>
              )}

              {!cand ? (
                <div className="triage-empty">正在读取这个项目的候选…</div>
              ) : (
                <>
                  <div className="month-pool-grid">
                <section className="month-pool">
                  <div className="month-pool-head">
                    <h3>本月总结</h3>
                    <span>{cand?.summary?.length ?? 0} 条</span>
                  </div>
                  <div className="month-pool-meta">
                    来自 {weeksLabel(cand?.sourceWeeks)} 的本周总结
                  </div>
                  <div className="month-pool-body">
                    {(cand?.summary ?? []).length === 0 ? (
                      <div className="month-pool-empty">本月没有可纳入的总结条目。</div>
                    ) : (
                      (cand?.summary ?? []).map((item) => (
                        <PoolItemRow key={item.id} item={item} busy={busy} onMove={(it, pool) => void moveItem(it, pool)} />
                      ))
                    )}
                  </div>
                </section>

                <section className="month-pool">
                  <div className="month-pool-head">
                    <h3>下月计划</h3>
                    <span>{cand?.plan?.length ?? 0} 条</span>
                  </div>
                  <div className="month-pool-meta">
                    {planMeta}
                    {planGap}
                  </div>
                  <div className="month-pool-body">
                    {(cand?.plan ?? []).length === 0 ? (
                      <div className="month-pool-empty">
                        还没有下月计划候选，可从更早的计划或本月总结里结转。
                      </div>
                    ) : (
                      (cand?.plan ?? []).map((item) => (
                        <PoolItemRow key={item.id} item={item} busy={busy} onMove={(it, pool) => void moveItem(it, pool)} />
                      ))
                    )}
                  </div>
                </section>
              </div>

              {(cand?.archive?.length ?? 0) > 0 && (
                <details className="month-fold">
                  <summary>
                    <span>更早的计划 · {cand?.archive?.length ?? 0} 条</span>
                    <span className="month-fold-hint">默认不纳入，可挑几条结转</span>
                  </summary>
                  <div className="month-fold-body">
                    {(cand?.archive ?? []).map((item) => (
                      <div key={item.id} className="month-archive-row">
                        <span className="month-week-tags">
                          {(item.weeks ?? []).map((week) => (
                            <span key={week} className="month-week-tag">
                              {shortWeek(week)}
                            </span>
                          ))}
                        </span>
                        <span className="month-archive-text">{item.text}</span>
                        <button
                          type="button"
                          className="month-item-btn is-accent"
                          disabled={busy}
                          onClick={() => void moveItem(item, "plan")}
                        >
                          纳入计划
                        </button>
                        <button
                          type="button"
                          className="month-item-btn"
                          disabled={busy}
                          onClick={() => void moveItem(item, "excluded")}
                        >
                          不纳入
                        </button>
                      </div>
                    ))}
                  </div>
                </details>
              )}

              <section className="month-exclude">
                <div className="month-exclude-head">
                  <b>不纳入</b>
                  <span>{cand?.excluded?.length ?? 0} 条 · 点「恢复」放回总结 / 计划</span>
                </div>
                <div className="month-exclude-list">
                  {(cand?.excluded ?? []).map((item) => (
                    <div key={item.id} className="month-exclude-row">
                      <span className="month-exclude-text">{item.text}</span>
                      <button
                        type="button"
                        className="month-item-btn is-accent"
                        disabled={busy}
                        onClick={() =>
                          void moveItem(item, item.section === "summary" ? "summary" : "plan")
                        }
                      >
                        恢复
                      </button>
                    </div>
                  ))}
                  {(cand?.excluded ?? []).length === 0 && (
                    <div className="month-pool-empty">还没有不纳入的条目。</div>
                  )}
                </div>
              </section>
                </>
              )}

              <p className="month-foot">
                跨周重复默认合并成一条，取最新写法，正文不带周次和姓名。改归口只影响月报候选，不覆盖已生成的草稿。
              </p>
            </section>
          </div>
        ) : (
          <div className="triage-step triage-reports-step">
            <aside className="triage-report-list">
              <div className="triage-rl-head">
                <b>{projects.length} 个项目</b>
                <span>月报状态</span>
              </div>
              <div className="triage-rl-body">
                {projects.map((project) => {
                  const row = rows.find((r) => r.project === project.id);
                  const status =
                    project.id === activeProject ? activeStatus : rowStatus(row);
                  return (
                    <button
                      key={project.id}
                      type="button"
                      className={`triage-rl-item${project.id === activeProject ? " is-active" : ""}`}
                      onClick={() => void switchProject(project.id)}
                    >
                      <span className="triage-rl-name">{project.name}</span>
                      <span className={`triage-rl-status ${status.cls}`}>{status.text}</span>
                    </button>
                  );
                })}
              </div>
              <div className="triage-rl-foot">每份月报独立保存；定稿后只读。</div>
            </aside>

            <section className="triage-report-editor">
              {!activeProject || !activeProjectInfo ? (
                <div className="triage-empty">
                  <p>先在「设置 → 项目」里添加报送项目。</p>
                </div>
              ) : (
                <>
                  <div className="triage-re-head">
                    <div className="triage-re-head-main">
                      <div className="triage-eyebrow">STEP 3 · 整理月报</div>
                      <h2>
                        {activeProjectInfo.name} · {month}
                      </h2>
                      <div className="triage-re-meta">
                        <span className="month-badge">{month}</span>
                        {cand && (
                          <span>
                            来源 {cand.sourceWeeks?.length ?? 0} 份周组报（
                            {weeksLabel(cand.sourceWeeks)}）· 总结 {cand.counts.summary} · 计划{" "}
                            {cand.counts.plan}
                          </span>
                        )}
                        {planFrom && <span>下月计划取自 {shortWeek(planFrom)}</span>}
                        <span className={`triage-rl-status ${activeStatus.cls}`}>
                          {activeStatus.text}
                        </span>
                      </div>
                    </div>
                    <div className="triage-re-actions">
                      <button
                        type="button"
                        className={`btn${report.data?.exists ? "" : " btn-primary"}`}
                        disabled={busy || report.loading || report.data?.status === "final"}
                        onClick={() => void generate()}
                      >
                        <Icon name="bolt" size={14} />
                        {report.data?.exists ? "按当前候选重新生成" : "生成草稿"}
                      </button>
                      <button
                        type="button"
                        className="btn btn-primary"
                        disabled={busy || !report.data?.exists || report.data.status === "final" || !dirty}
                        onClick={() => void saveActive()}
                      >
                        保存 <kbd>⌘S</kbd>
                      </button>
                      <button
                        type="button"
                        className="btn"
                        disabled={busy || !report.data?.exists}
                        onClick={() => void toggleFinal()}
                      >
                        {report.data?.status === "final" ? "取消定稿" : "定稿"}
                      </button>
                      <button
                        type="button"
                        className="btn btn-ghost"
                        disabled={busy || report.data?.status !== "final"}
                        title={report.data?.status === "final" ? "导出定稿" : "定稿后才能导出"}
                        onClick={() => void exportMd()}
                      >
                        导出
                      </button>
                      <button
                        type="button"
                        className="btn btn-ghost"
                        disabled={busy || report.data?.status !== "final"}
                        title={report.data?.status === "final" ? "复制飞书格式" : "定稿后才能复制"}
                        onClick={() => void copyFeishu()}
                      >
                        复制飞书
                      </button>
                    </div>
                  </div>

                  {report.data?.exists && (
                    <DeliveryBar
                      delivery={report.data.delivery ?? activeRow?.delivery}
                      projectName={activeProjectInfo.name}
                      isFinal={report.data.status === "final"}
                      busy={busy}
                      onCopy={() => void copyFeishu()}
                      onMark={(d) => void markDelivered(d)}
                    />
                  )}

                  {cand && (cand.missing ?? []).length > 0 && (
                    <div className="month-notice">
                      <b>未纳入 {(cand.missing ?? []).join("、")}</b>
                      {planGap ? ` · 下月计划取自 ${shortWeek(planFrom)}` : ""}。
                      {!allowMissing && "需要生成请回第 ① 步勾选「缺周也继续」。"}
                    </div>
                  )}
                  {report.data?.exists && (report.data.sourceChanged || activeRow?.sourceChanged) && (
                    <div className="month-notice">
                      <b>来源已更新</b>：勾选的周组报或归口变了，这份月报仍基于旧候选。
                      {report.data.status === "final"
                        ? "已定稿，不自动改；需要重做先取消定稿。"
                        : "如需按当前候选重来，点「按当前候选重新生成」。"}
                    </div>
                  )}
                  {report.data?.status === "final" && (
                    <div className="month-notice is-ok">
                      <b>已定稿，只读</b> · 导出和复制只认定稿版本。
                    </div>
                  )}

                  {report.loading ? (
                    <div className="triage-empty">正在读取月报…</div>
                  ) : report.error ? (
                    <div className="triage-empty">{report.error}</div>
                  ) : !report.data?.exists ? (
                    <div className="triage-re-empty">
                      <span>这份月报还没有草稿。</span>
                      <button
                        type="button"
                        className="btn btn-primary"
                        disabled={busy}
                        onClick={() => void generate()}
                      >
                        <Icon name="bolt" size={14} />
                        生成草稿
                      </button>
                    </div>
                  ) : report.data.status === "final" ? (
                    <>
                      <div className="settings-hint">已定稿，只读。需要修改请先「取消定稿」。</div>
                      <pre className="report-body triage-readonly-body">{report.data.body}</pre>
                    </>
                  ) : (
                    <textarea
                      className="month-flow-editor"
                      value={report.body}
                      spellCheck={false}
                      placeholder="项目月报正文（Markdown）"
                      onChange={(e) =>
                        patchReport((state) => ({
                          ...state,
                          body: e.target.value,
                          saving: normalizeBody(e.target.value) === state.savedBody ? "saved" : "dirty",
                        }))
                      }
                    />
                  )}

                  <div className="triage-re-foot">
                    <span>
                      来源：{weeksLabel(cand?.sourceWeeks)} · 草稿是 Markdown，人工改完再定稿
                    </span>
                    <span>
                      {report.data?.status === "final"
                        ? "已定稿，取消定稿后才能改。"
                        : dirty
                          ? "当前有未保存修改；按 ⌘S 保存。"
                          : "编辑后按 ⌘S 或点保存。"}
                    </span>
                  </div>
                </>
              )}
            </section>
          </div>
        )}
      </div>
    </section>
  );
}
