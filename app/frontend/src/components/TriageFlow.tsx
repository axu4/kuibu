// 周报三步流程：筛选 → 项目分拣预览 → 分别整理各项目周报。
//
// 步骤状态不额外落盘，全部由「筛选记录 + 各项目草稿」推导；每一步都可以点回去。
// 数据读写一律走 TriageService / ReportService，监听 triage:changed 与 report:changed 刷新。

import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { Events } from "@wailsio/runtime";
import { ReportService, TriageService } from "../../bindings/github.com/axu4/kuibu/app";
import type {
  ProjectGroupList,
  TriageData,
  TriageItemData,
} from "../../bindings/github.com/axu4/kuibu/app";
import { readableError } from "../lib/todo";
import { Icon } from "./Icon";
import { TriageFilterStep } from "./triage/TriageFilterStep";
import type { ExcerptState } from "./triage/TriageFilterStep";
import { TriageHelp } from "./triage/TriageHelp";
import { TriagePreviewStep } from "./triage/TriagePreviewStep";
import { TriageProjectSettings } from "./triage/TriageProjectSettings";
import { TriageReportsStep } from "./triage/TriageReportsStep";
import type { TriageReportsHandle } from "./triage/TriageReportsStep";
import {
  defaultItem,
  defaultSection,
  entryItemOfSection,
  findItem,
  itemsInSection,
  nextSectionWithUndecided,
  nextUndecidedInSection,
  sectionTitle,
  sectionsOf,
  shortSection,
} from "./triage/shared";

const DEFAULT_GROUP = "";

type Step = 1 | 2 | 3;

interface TriageFlowProps {
  week: string;
  /** 组长所在组；空字符串由服务端按当前组长解析。 */
  group?: string;
  onClose: () => void;
  onToast: (text: string) => void;
}

interface HistoryEntry {
  itemId: string;
  prev: { decision: string; project: string } | null;
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

/** 卡住第 3 步的「生成草稿」：先读草稿状态，已有草稿的项目不覆盖。 */
export function TriageFlow({ week, group = DEFAULT_GROUP, onClose, onToast }: TriageFlowProps) {
  const [data, setData] = useState<TriageData | null>(null);
  const [rows, setRows] = useState<ProjectGroupList[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [step, setStep] = useState<Step>(1);
  const [busy, setBusy] = useState(false);
  const [filter, setFilter] = useState("本周完成");
  const [currentId, setCurrentId] = useState<string | null>(null);
  const [history, setHistory] = useState<HistoryEntry[]>([]);
  const [lastAction, setLastAction] = useState<string | null>(null);
  const [excerptOpen, setExcerptOpen] = useState(false);
  const [excerpts, setExcerpts] = useState<Record<string, ExcerptState>>({});
  const [settingsOpen, setSettingsOpen] = useState(false);
  const [helpOpen, setHelpOpen] = useState(false);
  const [reportsReady, setReportsReady] = useState(false);
  const [activeProject, setActiveProject] = useState<string | null>(null);
  const [dirtyCount, setDirtyCount] = useState(0);

  const busyRef = useRef(false);
  const excerptsRef = useRef<Record<string, ExcerptState>>({});
  const reportsRef = useRef<TriageReportsHandle | null>(null);
  const rowsRef = useRef<ProjectGroupList[]>([]);
  const bootstrappedRef = useRef(false);

  useEffect(() => {
    rowsRef.current = rows;
  }, [rows]);

  const projects = data?.projects ?? [];
  const items = data?.items ?? [];
  const allJudged = Boolean(data?.allJudged);
  const current = useMemo(() => findItem(items, currentId), [items, currentId]);

  const loadTriage = useCallback(async (): Promise<TriageData | null> => {
    try {
      const fresh = await TriageService.Triage(week, group);
      setData(fresh);
      setError(null);
      return fresh;
    } catch (err) {
      setData(null);
      setError(readableError(err));
      return null;
    } finally {
      setLoading(false);
    }
  }, [week, group]);

  const loadRows = useCallback(async (): Promise<ProjectGroupList[]> => {
    try {
      const list = (await ReportService.ProjectGroups(week, group)) ?? [];
      setRows(list);
      return list;
    } catch {
      // 第 1 步不依赖草稿状态；没有组长/组时保持旧值即可。
      return rowsRef.current;
    }
  }, [week, group]);

  useEffect(() => {
    setLoading(true);
    void (async () => {
      const fresh = await loadTriage();
      const freshRows = await loadRows();
      // 重开时接着上次的进度：没筛完回第 1 步，筛完了去预览；已有草稿就直接整理。
      if (!bootstrappedRef.current) {
        bootstrappedRef.current = true;
        if (fresh?.allJudged && (fresh.items ?? []).length > 0) {
          const existing = freshRows.filter((row) => row.exists);
          if (existing.length > 0) {
            setReportsReady(true);
            setActiveProject(existing[0].project);
            setStep(3);
          } else {
            setStep(2);
          }
        }
      }
    })();
  }, [loadTriage, loadRows]);

  // 服务端的变化（包括这里自己的写入）都刷新；草稿状态只刷新左侧列表，不动编辑中的正文。
  useEffect(() => {
    const offTriage = Events.On("triage:changed", () => {
      void loadTriage();
      void loadRows();
    });
    const offReport = Events.On("report:changed", () => {
      void loadRows();
    });
    return () => {
      offTriage?.();
      offReport?.();
    };
  }, [loadTriage, loadRows]);

  // 数据刷新后保证当前节与当前条仍然有效。
  useEffect(() => {
    if (!data) return;
    const list = data.items ?? [];
    setFilter((prev) => (sectionsOf(data).some((s) => s.section === prev) ? prev : defaultSection(data)));
    setCurrentId((prev) => {
      if (prev && list.some((i) => i.id === prev)) return prev;
      return defaultItem(data)?.id ?? null;
    });
  }, [data]);

  useEffect(() => {
    if (step !== 3) setDirtyCount(0);
  }, [step]);

  const advance = useCallback(
    (fresh: TriageData, item: TriageItemData, wasDecided: boolean) => {
      const list = fresh.items ?? [];
      if (wasDecided) {
        const sectionList = itemsInSection(list, item.section);
        const idx = sectionList.findIndex((i) => i.id === item.id);
        if (idx >= 0 && idx + 1 < sectionList.length) {
          setCurrentId(sectionList[idx + 1].id);
          return;
        }
        onToast(`「${sectionTitle(fresh, item.section)}」已回看完毕`);
        return;
      }
      const next = nextUndecidedInSection(list, item.section, item.id);
      if (next) {
        setCurrentId(next.id);
        return;
      }
      const nextSection = nextSectionWithUndecided(fresh, item.section);
      if (nextSection) {
        const entry = entryItemOfSection(list, nextSection);
        setFilter(nextSection);
        if (entry) setCurrentId(entry.id);
        onToast(
          `「${sectionTitle(fresh, item.section)}」已过完，进入「${sectionTitle(fresh, nextSection)}」`,
        );
        return;
      }
      setExcerptOpen(false);
      setStep(2);
      onToast("筛选完成，进入「项目分拣预览」");
    },
    [onToast],
  );

  const decide = useCallback(
    async (decision: "accepted" | "rejected" | "skipped", projectId?: string) => {
      if (busyRef.current) return;
      const item = current;
      if (!item) return;
      let targetProject = projectId;
      if (decision === "accepted" && !targetProject) {
        if (projects.length === 1) {
          targetProject = projects[0].id;
        } else {
          onToast("先按 1 / 2 选择项目，或点一下项目名");
          return;
        }
      }
      busyRef.current = true;
      setBusy(true);
      try {
        const fresh = await TriageService.TriageDecide(
          week,
          group,
          item.id,
          decision,
          decision === "accepted" ? (targetProject ?? "") : "",
        );
        setHistory((prev) => [
          ...prev,
          { itemId: item.id, prev: item.decision ? { decision: item.decision, project: item.project } : null },
        ]);
        const project = decision === "accepted" ? projects.find((p) => p.id === targetProject) : null;
        setLastAction(
          decision === "accepted"
            ? `放入「${project?.name ?? "项目"}」· ${item.name}`
            : decision === "rejected"
              ? `移除 · ${item.name}`
              : `待定 · ${item.name}`,
        );
        setData(fresh);
        setExcerptOpen(false);
        advance(fresh, item, Boolean(item.decision));
      } catch (err) {
        onToast(`记录失败：${readableError(err)}`);
      } finally {
        busyRef.current = false;
        setBusy(false);
      }
    },
    [advance, current, group, onToast, projects, week],
  );

  const moveItem = useCallback(
    async (itemId: string, decision: "accepted" | "rejected" | "skipped", projectId?: string) => {
      if (busyRef.current) return;
      const item = findItem(items, itemId);
      if (!item) return;
      if (
        item.decision === decision &&
        (decision !== "accepted" || item.project === projectId)
      ) {
        if (decision === "accepted") {
          const project = projects.find((p) => p.id === projectId);
          onToast(`已经在「${project?.name ?? "该项目"}」`);
        } else if (decision === "rejected") {
          onToast("已经在「已移除」");
        } else {
          onToast("已经是待定");
        }
        return;
      }
      busyRef.current = true;
      setBusy(true);
      try {
        const fresh = await TriageService.TriageDecide(
          week,
          group,
          itemId,
          decision,
          decision === "accepted" ? (projectId ?? "") : "",
        );
        setHistory((prev) => [
          ...prev,
          { itemId, prev: item.decision ? { decision: item.decision, project: item.project } : null },
        ]);
        const project = decision === "accepted" ? (fresh.projects ?? []).find((p) => p.id === projectId) : null;
        setData(fresh);
        onToast(
          decision === "accepted"
            ? `已移到「${project?.name ?? "项目"}」`
            : decision === "rejected"
              ? "已移到「已移除」"
              : "已移到「待定」",
        );
      } catch (err) {
        onToast(`移动失败：${readableError(err)}`);
      } finally {
        busyRef.current = false;
        setBusy(false);
      }
    },
    [group, items, onToast, projects, week],
  );

  const undo = useCallback(async () => {
    if (busyRef.current) return;
    const entry = history[history.length - 1];
    if (!entry) {
      onToast("没有可撤销的操作");
      return;
    }
    busyRef.current = true;
    setBusy(true);
    try {
      // SetTriageDecision 支持空决策：撤销到「未判」时用空字符串清除本条决策。
      const fresh = await TriageService.TriageDecide(
        week,
        group,
        entry.itemId,
        entry.prev?.decision ?? "",
        entry.prev?.project ?? "",
      );
      setHistory((prev) => prev.slice(0, -1));
      setData(fresh);
      setLastAction(null);
      setExcerptOpen(false);
      setStep(1);
      const item = findItem(fresh.items ?? [], entry.itemId);
      if (item) {
        setFilter(item.section);
        setCurrentId(item.id);
      }
      onToast("已撤销上一步");
    } catch (err) {
      onToast(`撤销失败：${readableError(err)}`);
    } finally {
      busyRef.current = false;
      setBusy(false);
    }
  }, [group, history, onToast, week]);

  const gotoItem = useCallback(
    (itemId: string) => {
      const item = findItem(items, itemId);
      if (!item) return;
      setStep(1);
      setFilter(item.section);
      setCurrentId(item.id);
      setExcerptOpen(false);
    },
    [items],
  );

  const selectFilter = useCallback(
    (section: string) => {
      setFilter(section);
      setExcerptOpen(false);
      const entry = entryItemOfSection(items, section);
      if (entry) setCurrentId(entry.id);
    },
    [items],
  );

  const moveCurrent = useCallback(
    (delta: number) => {
      const list = itemsInSection(items, filter);
      if (list.length === 0) return;
      let index = list.findIndex((i) => i.id === currentId);
      if (index < 0) index = 0;
      index = Math.max(0, Math.min(list.length - 1, index + delta));
      setCurrentId(list[index].id);
      setExcerptOpen(false);
    },
    [currentId, filter, items],
  );

  const loadExcerpt = useCallback(
    async (personId: string) => {
      const cached = excerptsRef.current[personId];
      if (cached && (cached.loading || cached.body || cached.error)) return;
      const loadingState: ExcerptState = { loading: true, body: "", status: "", error: null };
      setExcerpts((prev) => {
        const next = { ...prev, [personId]: loadingState };
        excerptsRef.current = next;
        return next;
      });
      try {
        const draft = await ReportService.GetDraft(week, personId, "member");
        const value: ExcerptState = {
          loading: false,
          body: draft.body ?? "",
          status: draft.status,
          error: draft.exists ? null : "这位组员还没有归一稿",
        };
        setExcerpts((prev) => {
          const next = { ...prev, [personId]: value };
          excerptsRef.current = next;
          return next;
        });
      } catch (err) {
        const value: ExcerptState = {
          loading: false,
          body: "",
          status: "",
          error: readableError(err),
        };
        setExcerpts((prev) => {
          const next = { ...prev, [personId]: value };
          excerptsRef.current = next;
          return next;
        });
      }
    },
    [week],
  );

  const toggleExcerpt = useCallback(() => {
    if (!current) return;
    const next = !excerptOpen;
    setExcerptOpen(next);
    if (next) void loadExcerpt(current.person);
  }, [current, excerptOpen, loadExcerpt]);

  const closeFlow = useCallback(async () => {
    if (step === 3) {
      const ok = await reportsRef.current?.flush();
      if (ok === false) return;
    }
    onClose();
  }, [onClose, step]);

  const navigate = useCallback(
    async (target: Step) => {
      if (target === step) return;
      if (step === 3 && target !== 3) {
        const ok = await reportsRef.current?.flush();
        if (ok === false) return;
      }
      if (target >= 2 && !allJudged) {
        onToast(`还有 ${data?.counts.undecided ?? 0} 条未判，先过完再进入这一步`);
        return;
      }
      if (target === 2 && (data?.items ?? []).length === 0) {
        onToast("这一周没有可筛选的条目");
        return;
      }
      if (target === 3) {
        const ready = reportsReady || rows.some((row) => row.exists);
        if (!ready) {
          onToast("先在「项目分拣预览」生成各项目草稿");
          return;
        }
      }
      setStep(target);
      if (target >= 2) void loadRows();
    },
    [allJudged, data, loadRows, onToast, reportsReady, rows, step],
  );

  const startReports = useCallback(
    async (targetProject?: string) => {
      const snapshot = data;
      if (!snapshot) return;
      const accepted = (snapshot.items ?? []).filter((i) => i.decision === "accepted" && i.project);
      if (accepted.length === 0) {
        onToast("没有候选条目，先回去筛选");
        return;
      }
      if (busyRef.current) return;
      busyRef.current = true;
      setBusy(true);
      try {
        let freshRows = rows;
        try {
          freshRows = (await ReportService.ProjectGroups(week, group)) ?? [];
          setRows(freshRows);
        } catch {
          // 读不到草稿状态时仍按已知状态尝试，失败会在下面收集。
        }
        const existing = new Set(freshRows.filter((row) => row.exists).map((row) => row.project));
        const byProject = new Set(accepted.map((i) => i.project));
        const failures: string[] = [];
        for (const project of snapshot.projects ?? []) {
          // 已有草稿的项目不覆盖：保留组长已经整理过的内容。
          if (!byProject.has(project.id) || existing.has(project.id)) continue;
          try {
            await ReportService.AssembleProjectGroup(week, group, project.id, false, false);
          } catch (err) {
            failures.push(`${project.name}：${readableError(err)}`);
          }
        }
        await loadRows();
        if (targetProject) {
          setActiveProject(targetProject);
        } else {
          setActiveProject((prev) =>
            prev && (snapshot.projects ?? []).some((p) => p.id === prev)
              ? prev
              : ((snapshot.projects ?? []).find((p) => byProject.has(p.id))?.id ?? null),
          );
        }
        setReportsReady(true);
        setStep(3);
        if (failures.length > 0) {
          onToast(
            `已进入整理周报；部分项目未生成：${failures[0]}${
              failures.length > 1 ? `（共 ${failures.length} 个）` : ""
            }`,
          );
        }
      } finally {
        busyRef.current = false;
        setBusy(false);
      }
    },
    [data, group, loadRows, onToast, rows, week],
  );

  const handleSettingsChanged = useCallback(() => {
    void (async () => {
      await loadTriage();
      await loadRows();
    })();
  }, [loadRows, loadTriage]);

  // 键盘：只在流程里生效；输入框 / 编辑器里不抢键。
  useEffect(() => {
    const onKey = (event: KeyboardEvent) => {
      if (settingsOpen || helpOpen) {
        if (event.key === "Escape") {
          event.preventDefault();
          if (settingsOpen) setSettingsOpen(false);
          if (helpOpen) setHelpOpen(false);
        }
        return;
      }
      const target = event.target as HTMLElement | null;
      const inField = Boolean(
        target && (target.isContentEditable || /^(INPUT|TEXTAREA|SELECT)$/.test(target.tagName)),
      );
      const mod = event.metaKey || event.ctrlKey;
      if (mod && event.key.toLowerCase() === "s") {
        if (step === 3) {
          event.preventDefault();
          void (async () => {
            if (await reportsRef.current?.flush()) onToast("已保存当前项目草稿");
          })();
        }
        return;
      }
      if (inField) return;
      if (mod && !event.shiftKey && event.key.toLowerCase() === "z") {
        if (step === 1) {
          event.preventDefault();
          void undo();
        }
        return;
      }
      if (event.key === "?") {
        event.preventDefault();
        setHelpOpen(true);
        return;
      }
      if (event.key === "Escape") {
        event.preventDefault();
        if (step === 3) void navigate(2);
        else if (step === 2) void navigate(1);
        else void closeFlow();
        return;
      }
      if (mod || event.altKey) return;
      if (step !== 1) return;
      if (event.key === " ") {
        event.preventDefault();
        void decide("skipped");
        return;
      }
      if (/^[1-9]$/.test(event.key)) {
        const project = projects[Number(event.key) - 1];
        if (project) void decide("accepted", project.id);
        else onToast(`没有第 ${event.key} 个项目`);
        return;
      }
      if (event.key === "Enter") {
        if (projects.length === 1) void decide("accepted", projects[0].id);
        else onToast("先按 1 / 2 选择项目，或点一下项目名");
        return;
      }
      const key = event.key.toLowerCase();
      if (key === "x" || event.key === "Backspace") {
        event.preventDefault();
        void decide("rejected");
        return;
      }
      if (key === "u") {
        void undo();
        return;
      }
      if (key === "e") {
        toggleExcerpt();
        return;
      }
      if (event.key === "ArrowUp" || key === "k") {
        event.preventDefault();
        moveCurrent(-1);
        return;
      }
      if (event.key === "ArrowDown" || key === "j") {
        event.preventDefault();
        moveCurrent(1);
      }
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [
    closeFlow,
    decide,
    helpOpen,
    moveCurrent,
    navigate,
    onToast,
    projects,
    settingsOpen,
    step,
    toggleExcerpt,
    undo,
  ]);

  const counts = data?.counts;
  const acceptedCount = counts?.accepted ?? 0;
  const undecided = counts?.undecided ?? 0;
  const hasItems = items.length > 0;
  const draftRows = rows.filter((row) => row.exists);
  const finalized = draftRows.filter((row) => row.status === "final").length;
  const stale = draftRows.filter((row) => row.sourceChanged).length;
  const allFinal =
    projects.length > 0 &&
    draftRows.length === projects.length &&
    draftRows.every((row) => row.status === "final");
  const step3Locked = !allJudged || (draftRows.length === 0 && !reportsReady);
  const sub1 = sectionsOf(data)
    .map((section) => `${shortSection(section.title)} ${section.done}/${section.total}`)
    .join(" · ");
  const sub2 = allJudged
    ? `${projects.length} 个项目 · ${acceptedCount} 条候选`
    : "筛选完成后可预览";
  const sub3 =
    draftRows.length > 0
      ? allFinal
        ? `${draftRows.length} 份已定稿`
        : `${draftRows.length} 份草稿${dirtyCount > 0 ? ` · ${dirtyCount} 份未保存` : ""}${
            finalized > 0 ? ` · ${finalized} 份定稿` : ""
          }${stale > 0 ? ` · ${stale} 份需更新` : ""}`
      : "预览后开始整理";

  const actions =
    step === 1 ? (
      <>
        <span className="triage-flow-hint">
          {undecided > 0 ? `还有 ${undecided} 条未判` : "筛选完成"}
        </span>
        <button
          type="button"
          className="btn btn-primary"
          disabled={!allJudged || !hasItems || busy}
          onClick={() => void navigate(2)}
        >
          下一步：项目分拣预览
        </button>
      </>
    ) : step === 2 ? (
      <>
        <button type="button" className="btn" disabled={busy} onClick={() => void navigate(1)}>
          上一步
        </button>
        <button
          type="button"
          className="btn btn-primary"
          disabled={busy || acceptedCount === 0}
          title={acceptedCount === 0 ? "回去第 1 步筛选后再生成" : "开始整理周报"}
          onClick={() => void startReports()}
        >
          {acceptedCount === 0 ? "没有候选条目" : "开始整理周报"}
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
          disabled={dirtyCount === 0}
          onClick={() =>
            void (async () => {
              if (await reportsRef.current?.flush()) onToast("已保存当前项目草稿");
            })()
          }
        >
          保存全部{dirtyCount > 0 ? `（${dirtyCount}）` : ""}
        </button>
        <button
          type="button"
          className="btn btn-primary"
          disabled={busy}
          onClick={() =>
            void (async () => {
              const ok = await reportsRef.current?.flush();
              if (ok === false) return;
              onToast("本周周报已整理，可随时再进");
              onClose();
            })()
          }
        >
          完成
        </button>
      </>
    );

  return (
    <section className="report-detail is-wide triage-flow">
      <header className="triage-topbar">
        <button type="button" className="triage-back" onClick={() => void closeFlow()} title="返回周报">
          ‹ 周报
        </button>
        <span className="triage-topbar-divider" />
        <span className="triage-topbar-title">周报整理</span>
        <span className="triage-topbar-week">{data?.week ?? week}</span>
        {data?.group && <span className="triage-topbar-group">{data.group}</span>}
        <span className="triage-topbar-spacer" />
        {undecided > 0 && <span className="triage-topbar-progress">未判 {undecided} 条</span>}
        <button type="button" className="btn btn-ghost" onClick={() => setSettingsOpen(true)}>
          项目设置
        </button>
        <button type="button" className="btn btn-ghost" onClick={() => setHelpOpen(true)}>
          键盘
        </button>
        <button type="button" className="icon-btn" title="关闭" onClick={() => void closeFlow()}>
          <Icon name="close" />
        </button>
      </header>

      <div className="triage-stepper">
        <div className="triage-flow-steps">
          <FlowStep
            n={1}
            title="筛选"
            sub={sub1 || "逐条筛选"}
            on={step === 1}
            done={allJudged && hasItems}
            locked={false}
            onClick={() => void navigate(1)}
          />
          <span className="triage-flow-line" />
          <FlowStep
            n={2}
            title="项目分拣预览"
            sub={sub2}
            on={step === 2}
            done={allJudged && hasItems && draftRows.length > 0}
            locked={!allJudged || !hasItems}
            onClick={() => void navigate(2)}
          />
          <span className="triage-flow-line" />
          <FlowStep
            n={3}
            title="整理周报"
            sub={sub3}
            on={step === 3}
            done={allFinal}
            locked={step3Locked}
            onClick={() => void navigate(3)}
          />
        </div>
        <div className="triage-flow-actions">{actions}</div>
      </div>

      <div className="triage-body">
        {loading ? (
          <div className="triage-empty">正在读取筛选记录…</div>
        ) : error ? (
          <div className="triage-empty">
            <p>{error}</p>
            <p className="triage-empty-hint">
              三步流程需要先有组长和 1~4 个报送项目。先完成设置，再回来逐条筛选。
            </p>
            <div className="triage-empty-actions">
              <button type="button" className="btn btn-primary" onClick={() => setSettingsOpen(true)}>
                项目设置
              </button>
              <button type="button" className="btn" onClick={() => void loadTriage()}>
                重试
              </button>
            </div>
          </div>
        ) : !data ? (
          <div className="triage-empty">正在读取筛选记录…</div>
        ) : items.length === 0 ? (
          <div className="triage-empty">
            <p>这一周还没有可筛选的条目。</p>
            <p className="triage-empty-hint">
              组员归一稿里没有能拆出的条目；可以先在「框架组长」里粘贴或归一周报，再回来筛选。
            </p>
            {(data.missing ?? []).length > 0 && (
              <p className="triage-empty-hint">未交 / 没有归一稿：{(data.missing ?? []).join("、")}</p>
            )}
            <div className="triage-empty-actions">
              <button type="button" className="btn btn-primary" onClick={() => setSettingsOpen(true)}>
                项目设置
              </button>
              <button type="button" className="btn" onClick={() => void loadTriage()}>
                重试
              </button>
            </div>
          </div>
        ) : step === 1 ? (
          <TriageFilterStep
            data={data}
            filter={filter}
            current={current}
            busy={busy}
            lastAction={lastAction}
            excerptOpen={excerptOpen}
            excerpt={current ? (excerpts[current.person] ?? null) : null}
            missing={data.missing ?? []}
            onFilter={selectFilter}
            onGoto={gotoItem}
            onDecide={(decision, projectId) => void decide(decision, projectId)}
            onToggleExcerpt={toggleExcerpt}
            onUndo={() => void undo()}
            onOpenSettings={() => setSettingsOpen(true)}
          />
        ) : step === 2 ? (
          <TriagePreviewStep
            data={data}
            rows={rows}
            busy={busy}
            onMove={(itemId, decision, projectId) => void moveItem(itemId, decision, projectId)}
            onGoto={gotoItem}
            onStartReports={(projectId) => void startReports(projectId)}
          />
        ) : (
          <TriageReportsStep
            ref={reportsRef}
            week={data.week}
            group={group}
            projects={projects}
            rows={rows}
            items={items}
            initialProject={activeProject}
            onToast={onToast}
            onDirtyChange={setDirtyCount}
          />
        )}
      </div>

      {settingsOpen && (
        <TriageProjectSettings
          items={items}
          onClose={() => setSettingsOpen(false)}
          onChanged={handleSettingsChanged}
          onToast={onToast}
        />
      )}

      {helpOpen && <TriageHelp onClose={() => setHelpOpen(false)} />}
    </section>
  );
}
