import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { Clipboard, Events } from "@wailsio/runtime";
import { ReportService } from "../../bindings/github.com/axu4/kuibu/app";
import type {
  ReportData,
  ReportMonth,
  ReportPerson,
  ReportStatusData,
  ReportWeek,
} from "../../bindings/github.com/axu4/kuibu/app";
import { Icon } from "./Icon";
import { MoreMenu } from "./Menu";
import { ReportPaste } from "./ReportPaste";
import { PeopleManager } from "./PeopleManager";
import { BatchNormalize, ReportDraft } from "./ReportDraft";
import { ReportReview, alignmentText } from "./ReportReview";
import { GroupReport } from "./GroupReport";
import { TriageFlow } from "./TriageFlow";
import {
  deadlineInfo,
  formatSubmittedAt,
  loadReportDetailWidth,
  REPORT_DETAIL_DEFAULT_WIDTH,
  REPORT_DETAIL_MIN_WIDTH,
  saveReportDetailWidth,
  groupLabel,
  groupProgressPercent,
  levelLabel,
  loadReportLevel,
  reportStatusLabel,
  rowHint,
  saveReportLevel,
  weekdayLabel,
  weekRangeLabel,
} from "../lib/report";
import type { ReportLevel } from "../lib/report";
import { readableError } from "../lib/todo";

/** 一组周报列表（按组或产品线聚合）。 */
interface ReportGroup {
  title: string;
  entries: ReportPerson[];
}

function sortByName(list: ReportPerson[]): ReportPerson[] {
  return [...list].sort((a, b) => a.name.localeCompare(b.name, "zh-Hans-CN"));
}

/**
 * 周报视图：周切换 + 框架组长/SE 两个 tab + 提交状态列表 + 原文详情面板。
 *
 * 读写一律经 ReportService；写入后服务端会发 report:changed，这里监听它整体
 * 刷新（不打断正在打开的粘贴弹窗）。
 */
interface ReportViewProps {
  /** 从日历跳转进来时初始打开的周、人员与层级；不传则为本周与上次层级。 */
  initialWeek?: string;
  initialPersonId?: string;
  initialLevel?: string;
}

export function ReportView({ initialWeek, initialPersonId, initialLevel }: ReportViewProps) {
  const [week, setWeek] = useState<ReportWeek | null>(null);
  const [level, setLevel] = useState<ReportLevel>(
    initialLevel === "lead" ? "lead" : initialLevel === "member" ? "member" : loadReportLevel(),
  );
  const [status, setStatus] = useState<ReportStatusData | null>(null);
  const [selectedId, setSelectedId] = useState<string | null>(null);
  const [detail, setDetail] = useState<ReportData | null>(null);
  const [detailError, setDetailError] = useState(false);
  const [paste, setPaste] = useState<{ personId: string; text: string } | null>(null);
  const [managerOpen, setManagerOpen] = useState(false);
  const [weekdayOpen, setWeekdayOpen] = useState(false);
  const [deleteTarget, setDeleteTarget] = useState<ReportPerson | null>(null);
  const [toast, setToast] = useState<string | null>(null);
  /** 刚归档的周报：用于「已归档 …」提示与撤销。 */
  const [saved, setSaved] = useState<{ week: string; level: ReportLevel; personId: string; name: string } | null>(null);
  const [batchOpen, setBatchOpen] = useState(false);
  const [groupOpen, setGroupOpen] = useState(false);
  const [triageOpen, setTriageOpen] = useState(false);
  const [period, setPeriod] = useState<"week" | "month">("week");
  // 外部变化后用于强制重载当前详情（status 没变时，effect 依赖可能不触发）。
  const [refreshTick, setRefreshTick] = useState(0);

  const weekRef = useRef<ReportWeek | null>(null);
  weekRef.current = week;
  const levelRef = useRef<ReportLevel>(level);
  levelRef.current = level;
  const statusRef = useRef<ReportStatusData | null>(null);
  statusRef.current = status;
  const weekBusyRef = useRef(false);

  const loadStatus = useCallback(async (weekStr: string, lv: ReportLevel) => {
    try {
      const st = await ReportService.Status(weekStr, lv, "", "");
      setStatus(st);
    } catch (err) {
      setToast(`读取周报状态失败：${readableError(err)}`);
    }
  }, []);

  /** 换周：重置选中与详情并加载该周状态。 */
  const applyWeek = useCallback(
    async (w: ReportWeek) => {
      setWeek(w);
      setSelectedId(null);
      setDetail(null);
      await loadStatus(w.week, levelRef.current);
    },
    [loadStatus],
  );

  // 首次加载：默认本周，日历跳转进来时打开指定周并选中该人。
  useEffect(() => {
    let alive = true;
    (async () => {
      try {
        const w = await ReportService.Week(initialWeek ?? "this");
        if (!alive) return;
        setWeek(w);
        await loadStatus(w.week, levelRef.current);
        if (initialPersonId) setSelectedId(initialPersonId);
      } catch (err) {
        if (alive) setToast(`读取周报失败：${readableError(err)}`);
      }
    })();
    return () => {
      alive = false;
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  const shiftWeek = useCallback(
    async (delta: number) => {
      const cur = weekRef.current;
      if (!cur || weekBusyRef.current) return;
      weekBusyRef.current = true;
      try {
        const w = await ReportService.ShiftWeek(cur.week, delta);
        await applyWeek(w);
      } catch (err) {
        setToast(`切换周失败：${readableError(err)}`);
      } finally {
        weekBusyRef.current = false;
      }
    },
    [applyWeek],
  );

  const backToCurrent = useCallback(async () => {
    try {
      const w = await ReportService.Week("this");
      await applyWeek(w);
    } catch (err) {
      setToast(`回到本周失败：${readableError(err)}`);
    }
  }, [applyWeek]);

  const changeLevel = useCallback(
    (next: ReportLevel) => {
      if (next === levelRef.current) return;
      setLevel(next);
      saveReportLevel(next);
      setGroupOpen(false);
      setTriageOpen(false);
      setSelectedId(null);
      setDetail(null);
      const w = weekRef.current;
      if (w) void loadStatus(w.week, next);
    },
    [loadStatus],
  );

  /** 打开粘贴弹窗；personId 非空时锁定这个人，不再问「归给谁」。 */
  const openPaste = useCallback((personId: string, text = "") => {
    // 从行上或 ⌘V 进来时先收起右侧详情，别让抽屉叠在弹窗后面。
    setTriageOpen(false);
    setSelectedId(null);
    setDetail(null);
    setDetailError(false);
    setPaste({ personId, text });
  }, []);

  const openRow = useCallback((entry: ReportPerson) => {
    setGroupOpen(false);
    setTriageOpen(false);
    setSelectedId(entry.personId);
    setDetail(null);
    setDetailError(false);
  }, []);

  const closeDetail = useCallback(() => {
    setSelectedId(null);
    setDetail(null);
    setDetailError(false);
  }, []);

  // 选中某人后按 ⌘V：直接用剪贴板内容打开「粘贴 · 张三」，一步归档。
  const selectedIdRef = useRef<string | null>(null);
  selectedIdRef.current = selectedId;
  const openPasteRef = useRef(openPaste);
  openPasteRef.current = openPaste;
  const pasteOpenRef = useRef(false);
  pasteOpenRef.current = paste !== null;

  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (!(e.metaKey || e.ctrlKey) || e.key.toLowerCase() !== "v") return;
      if (e.altKey || e.shiftKey) return;
      if (pasteOpenRef.current) return;
      const personId = selectedIdRef.current;
      if (!personId) return;
      const target = e.target as HTMLElement | null;
      // 正在输入框里粘贴时不要抢。
      if (target && (target.isContentEditable || /^(INPUT|TEXTAREA|SELECT)$/.test(target.tagName))) {
        return;
      }
      e.preventDefault();
      void Clipboard.Text()
        .then((value) => openPasteRef.current(personId, value ?? ""))
        .catch(() => openPasteRef.current(personId, ""));
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, []);

  // 外部（含本服务自己写入）变化时刷新：周信息、状态与当前详情。
  useEffect(() => {
    const off = Events.On("report:changed", () => {
      const w = weekRef.current;
      if (!w) return;
      void (async () => {
        try {
          const fresh = await ReportService.Week(w.week);
          setWeek(fresh);
          await loadStatus(fresh.week, levelRef.current);
          setRefreshTick((t) => t + 1);
        } catch (err) {
          setToast(`刷新周报失败：${readableError(err)}`);
        }
      })();
    });
    return off;
  }, [loadStatus]);

  // 选中条目变化、状态变化或外部刷新时，加载原文详情。
  const selectedEntry = useMemo(() => {
    if (!selectedId) return null;
    const all = [...(status?.entries ?? []), ...(status?.extra ?? [])];
    return all.find((e) => e.personId === selectedId) ?? null;
  }, [status, selectedId]);

  useEffect(() => {
    const w = weekRef.current;
    if (!selectedId || !w || !selectedEntry) {
      setDetail(null);
      return;
    }
    if (selectedEntry.status === "missing") {
      setDetail(null);
      setDetailError(false);
      return;
    }
    setDetailError(false);
    let alive = true;
    void ReportService.Get(w.week, selectedEntry.personId, levelRef.current)
      .then((d) => {
        if (alive) setDetail(d);
      })
      .catch(() => {
        if (alive) {
          setDetail(null);
          setDetailError(true);
        }
      });
    return () => {
      alive = false;
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [selectedId, selectedEntry?.status, selectedEntry?.personId, refreshTick]);

  const copyReminder = useCallback(async () => {
    const st = statusRef.current;
    const w = weekRef.current;
    if (!st || !w) return;
    const missing = (st.entries ?? []).filter((e) => e.status === "missing");
    if (missing.length === 0) {
      setToast("都交齐了");
      return;
    }
    const names = missing.map((e) => `@${e.name}`).join(" ");
    const text = `${names} ${w.week} 周报还没收到，${weekdayLabel(w.reportDay)}前发我一下，谢谢～`;
    try {
      // 优先走 Wails 原生剪贴板（WKWebView 下 navigator.clipboard 可能不可用），失败再退回浏览器 API。
      try {
        await Clipboard.SetText(text);
      } catch {
        await navigator.clipboard.writeText(text);
      }
      setToast("已复制催交话术");
    } catch {
      setToast("复制失败，请检查剪贴板权限");
    }
  }, []);

  /** 撤销刚写入的周报（移入 .history，等同删除）。 */
  const undoSaved = useCallback(async () => {
    const target = saved;
    setSaved(null);
    if (!target) return;
    try {
      await ReportService.Delete(target.week, target.personId, target.level);
      setToast(`已撤销「${target.name}」的周报`);
    } catch (err) {
      setToast(`撤销失败：${readableError(err)}`);
    }
  }, [saved]);

  // 「已归档」提示挂 8 秒，够看清也够点撤销。
  useEffect(() => {
    if (!saved) return;
    const timer = window.setTimeout(() => setSaved(null), 8000);
    return () => window.clearTimeout(timer);
  }, [saved]);

  const doDelete = useCallback(async () => {
    const target = deleteTarget;
    const w = weekRef.current;
    if (!target || !w) return;
    try {
      await ReportService.Delete(w.week, target.personId, levelRef.current);
      setDeleteTarget(null);
      setSelectedId(null);
      setDetail(null);
      await loadStatus(w.week, levelRef.current);
    } catch (err) {
      setToast(`删除失败：${readableError(err)}`);
    }
  }, [deleteTarget, loadStatus]);

  // 列表分组：框架组长按组（单组时平铺）；SE 按产品线（空产品线归「未分产品线」）。
  const groups = useMemo<ReportGroup[]>(() => {
    const entries = sortByName(status?.entries ?? []);
    if (level === "member") {
      const byGroup = new Map<string, ReportPerson[]>();
      for (const e of entries) {
        const key = e.group?.trim() ?? "";
        const list = byGroup.get(key);
        if (list) list.push(e);
        else byGroup.set(key, [e]);
      }
      if (byGroup.size <= 1) {
        return [{ title: "", entries }];
      }
      const keys = [...byGroup.keys()].sort((a, b) => {
        if (a === "") return 1;
        if (b === "") return -1;
        return a.localeCompare(b, "zh-Hans-CN");
      });
      return keys.map((k) => ({ title: k || "未分组", entries: byGroup.get(k) ?? [] }));
    }

    const byLine = new Map<string, ReportPerson[]>();
    for (const e of entries) {
      const lines = (e.productLines ?? []).map((l) => l.trim()).filter(Boolean);
      if (lines.length === 0) {
        const list = byLine.get("");
        if (list) list.push(e);
        else byLine.set("", [e]);
        continue;
      }
      for (const line of lines) {
        const list = byLine.get(line);
        if (list) list.push(e);
        else byLine.set(line, [e]);
      }
    }
    const keys = [...byLine.keys()].sort((a, b) => {
      if (a === "") return 1;
      if (b === "") return -1;
      return a.localeCompare(b, "zh-Hans-CN");
    });
    return keys.map((k) => ({ title: k || "未分产品线", entries: byLine.get(k) ?? [] }));
  }, [status, level]);

  const extra = useMemo(() => sortByName(status?.extra ?? []), [status]);
  const counts = status?.counts;
  const empty = status && (status.entries?.length ?? 0) === 0 && extra.length === 0;
  const deadline = week ? deadlineInfo(week.start, week.reportDay) : null;

  if (period === "month") {
    return (
      <MonthView
        level={level}
        onLevel={changeLevel}
        onWeek={() => {
          setPeriod("week");
          const w = weekRef.current;
          if (w) void loadStatus(w.week, levelRef.current);
        }}
      />
    );
  }

  return (
    <div className="report-view">
      <section className="report-main">
        <div className="toolbar">
          <div className="toolbar-spacer" />
        </div>

        <div className="report-scroll">
          <header className="report-head">
            <div className="report-title-row">
              <h1>周报</h1>
              <div className="week-switcher">
                <button
                  type="button"
                  className="icon-btn"
                  title="上一周"
                  onClick={() => void shiftWeek(-1)}
                >
                  <Icon name="chevronLeft" size={14} />
                </button>
                <span className="week-label">
                  {week ? `${week.week} · ${weekRangeLabel(week.start, week.end)}` : "…"}
                </span>
                <button
                  type="button"
                  className="icon-btn"
                  title="下一周"
                  onClick={() => void shiftWeek(1)}
                >
                  <Icon name="chevron" size={14} />
                </button>
                {week && !week.isCurrent && (
                  <button type="button" className="btn btn-ghost" onClick={() => void backToCurrent()}>
                    回到本周
                  </button>
                )}
              </div>
            </div>

            <div className="report-head-meta">
              <PeriodSwitch period="week" onChange={(p) => p === "month" && setPeriod("month")} />
              {week && <span>{weekdayLabel(week.reportDay)}截止</span>}
            </div>

            <div className="segmented report-tabs">
              <button
                type="button"
                className={`segmented-btn${level === "member" ? " is-active" : ""}`}
                onClick={() => changeLevel("member")}
              >
                {levelLabel("member")}
              </button>
              <button
                type="button"
                className={`segmented-btn${level === "lead" ? " is-active" : ""}`}
                onClick={() => changeLevel("lead")}
              >
                {levelLabel("lead")}
              </button>
            </div>
          </header>

          <div className="report-toolbar">
            <div className="report-stats">
              <span>
                应交 <b>{counts?.expected ?? 0}</b>
              </span>
              <span>
                已交 <b>{counts?.submitted ?? 0}</b>
              </span>
              <span>
                迟交 <b>{counts?.late ?? 0}</b>
              </span>
              <span>
                未交 <b>{counts?.missing ?? 0}</b>
              </span>
            </div>
            <div className="report-actions">
              {level === "member" && (
                <button
                  type="button"
                  className={`btn${triageOpen ? " is-on" : ""}`}
                  onClick={() => {
                    setSelectedId(null);
                    setGroupOpen(false);
                    setTriageOpen((v) => !v);
                  }}
                >
                  <Icon name="list" size={14} />
                  周报三步流程
                </button>
              )}
              <button
                type="button"
                className={`btn${groupOpen ? " is-on" : ""}`}
                onClick={() => {
                  setSelectedId(null);
                  setTriageOpen(false);
                  setGroupOpen((v) => !v);
                }}
              >
                <Icon name="report" size={14} />
                {level === "member" ? "组报" : "产品报"}
              </button>
              <button
                type="button"
                className="btn btn-primary"
                onClick={() => openPaste("")}
              >
                <Icon name="paste" size={14} />
                粘贴周报
              </button>
              <MoreMenu
                items={[
                  {
                    label: "批量归一本周已交",
                    icon: "bolt" as const,
                    onClick: () => setBatchOpen(true),
                  },
                  { label: "复制催交话术", icon: "copy", onClick: () => void copyReminder() },
                  { label: "管理名单", icon: "person", onClick: () => setManagerOpen(true) },
                  { label: "设置周报日", icon: "calendar", onClick: () => setWeekdayOpen(true) },
                ]}
              />
            </div>
          </div>

          {(status?.groups?.length ?? 0) > 0 && (
            <div className="report-group-stats">
              {(status?.groups ?? []).map((g) => (
                <div key={g.group || "\u0000ungrouped"} className="report-group-stat">
                  <div className="report-group-stat-head">
                    <span className="report-group-stat-name">{groupLabel(g.group)}</span>
                    <span className="report-group-stat-ratio">
                      {g.submitted + g.late}/{g.expected}
                    </span>
                  </div>
                  <div className="report-group-stat-bar" aria-hidden>
                    <span
                      className="report-group-stat-fill"
                      style={{ width: `${groupProgressPercent(g.expected, g.submitted, g.late)}%` }}
                    />
                  </div>
                  <div className="report-group-stat-meta">
                    {g.late > 0 && <span className="is-late">迟交 {g.late}</span>}
                    {g.missing > 0 && <span className="is-missing">未交 {g.missing}</span>}
                    {g.late === 0 && g.missing === 0 && <span className="is-done">全部已交</span>}
                  </div>
                </div>
              ))}
            </div>
          )}

          {empty ? (
            <div className="report-empty">
              <div className="welcome-mark is-quiet">
                <Icon name="person" size={22} />
              </div>
              <p>{level === "lead" ? "还没有组长，先添加组长吧。" : "还没有组员，先添加组员吧。"}</p>
              <button type="button" className="btn btn-primary" onClick={() => setManagerOpen(true)}>
                先添加组员
              </button>
            </div>
          ) : (
            <div className="report-list">
              {groups.map((g) => (
                <div key={g.title || "__flat"} className="report-section">
                  {g.title && <div className="report-section-title">{g.title}</div>}
                  {g.entries.map((e) => (
                    <ReportRow
                      key={`${e.personId}-${g.title}`}
                      entry={e}
                      showGroup={level === "lead"}
                      selected={selectedId === e.personId}
                      overdue={deadline?.overdue ?? false}
                      onSelect={() => openRow(e)}
                      onPaste={() => openPaste(e.personId)}
                    />
                  ))}
                </div>
              ))}
              {extra.length > 0 && (
                <div className="report-section">
                  <div className="report-section-title">名单外</div>
                  {extra.map((e) => (
                    <ReportRow
                      key={e.personId}
                      entry={e}
                      showGroup={false}
                      selected={selectedId === e.personId}
                      overdue={deadline?.overdue ?? false}
                      onSelect={() => openRow(e)}
                      onPaste={() => openPaste(e.personId)}
                    />
                  ))}
                </div>
              )}
            </div>
          )}
        </div>
      </section>

      {triageOpen && week && (
        <TriageFlow
          key={`triage-${week.week}`}
          week={week.week}
          onClose={() => setTriageOpen(false)}
          onToast={setToast}
        />
      )}

      {groupOpen && week && (
        <GroupReport
          key={level}
          kind={level === "member" ? "group" : "product"}
          week={week.week}
          refreshTick={refreshTick}
          onClose={() => setGroupOpen(false)}
          onToast={setToast}
        />
      )}

      {!groupOpen && !triageOpen && selectedEntry && week && (
        <ReportDetailPanel
          entry={selectedEntry}
          report={detail}
          failed={detailError}
          period={week.week}
          level={level}
          refreshTick={refreshTick}
          onToast={setToast}
          onClose={closeDetail}
          onRepaste={() => openPaste(selectedEntry.personId)}
          onDelete={() => setDeleteTarget(selectedEntry)}
        />
      )}

      {paste && week && (
        <ReportPaste
          week={week.week}
          level={level}
          presetPersonId={paste.personId}
          initialText={paste.text}
          onClose={() => setPaste(null)}
          onDone={() => {
            setPaste(null);
            void loadStatus(week.week, level);
            setRefreshTick((t) => t + 1);
            if (paste?.personId) {
              const all = [...(statusRef.current?.entries ?? []), ...(statusRef.current?.extra ?? [])];
              const name = all.find((e) => e.personId === paste.personId)?.name ?? "";
              setSaved({ week: week.week, level, personId: paste.personId, name });
            }
          }}
        />
      )}

      {batchOpen && week && (
        <BatchNormalize
          week={week.week}
          level={level}
          onClose={() => setBatchOpen(false)}
          onToast={setToast}
        />
      )}

      {managerOpen && <PeopleManager onClose={() => setManagerOpen(false)} />}

      {weekdayOpen && week && (
        <WeekdayModal current={week.reportDay} onClose={() => setWeekdayOpen(false)} />
      )}

      {deleteTarget && (
        <div className="modal-mask" onClick={() => setDeleteTarget(null)}>
          <div className="modal" onClick={(e) => e.stopPropagation()}>
            <h2>删除「{deleteTarget.name}」的周报？</h2>
            <p>删除后会移入历史记录，不参与索引与统计。</p>
            <div className="modal-buttons">
              <button type="button" className="btn" onClick={() => setDeleteTarget(null)}>
                取消
              </button>
              <button type="button" className="btn btn-danger" onClick={() => void doDelete()}>
                删除
              </button>
            </div>
          </div>
        </div>
      )}

      {saved && (
        <div className="toast is-action" role="status">
          <span>
            已归档{saved.name ? `「${saved.name}」` : ""}的周报 · {saved.week}
          </span>
          <button type="button" className="toast-action" onClick={() => void undoSaved()}>
            撤销
          </button>
        </div>
      )}

      {toast && (
        <div className="toast" role="alert" onClick={() => setToast(null)}>
          {toast}
        </div>
      )}
    </div>
  );
}

interface ReportRowProps {
  entry: ReportPerson;
  showGroup: boolean;
  selected: boolean;
  /** 已过截止时间：未交的人显示「逾期」。 */
  overdue: boolean;
  onSelect: () => void;
  /** 直接在行上粘贴（未交的人）：一次点击就进锁定了此人的粘贴框。 */
  onPaste: () => void;
}

/** 一行人员状态：状态点、姓名、组（SE tab）、状态字、提交时间；未交的行带「粘贴」。 */
function ReportRow({ entry, showGroup, selected, overdue, onSelect, onPaste }: ReportRowProps) {
  return (
    <div className={`report-row is-${entry.status}${selected ? " is-selected" : ""}`}>
      <button type="button" className="report-row-main" onClick={onSelect}>
        <span className={`report-status-dot is-${entry.status}`} aria-hidden="true" />
        <span className="report-row-name">{entry.name}</span>
        {showGroup && entry.group && <span className="report-row-group">{entry.group}</span>}
        {entry.normalized && entry.normalized !== "none" && (
          <span
            className={`draft-badge is-${entry.normalized}${entry.sourceChanged ? " is-warn" : ""}`}
            title={entry.sourceChanged ? "归一后原文又更新过" : undefined}
          >
            {entry.fromGroup ? "定稿（组报）" : entry.normalized === "final" ? "定稿" : "草稿"}
            {entry.sourceChanged && " ·原文已更新"}
          </span>
        )}
        {entry.alignment !== null && entry.alignment !== undefined && (
          <span className="report-row-time" title="对照上周计划的对齐率">
            对齐 {alignmentText(entry.alignment)}
          </span>
        )}
        <span className={`report-row-state is-${entry.status}`}>{reportStatusLabel(entry.status)}</span>
        {rowHint(entry.status, entry.submittedAt, overdue) && (
          <span className="report-row-time">{rowHint(entry.status, entry.submittedAt, overdue)}</span>
        )}
      </button>
      {entry.status === "missing" && (
        <button type="button" className="report-row-paste" title="粘贴这个人的周报" onClick={onPaste}>
          <Icon name="paste" size={13} />
          粘贴
        </button>
      )}
    </div>
  );
}

interface ReportDetailPanelProps {
  entry: ReportPerson;
  report: ReportData | null;
  failed: boolean;
  /** 周编号或月份编号。 */
  period: string;
  /** member / lead / lead-month。 */
  level: string;
  refreshTick: number;
  onToast: (text: string) => void;
  onClose: () => void;
  onRepaste: () => void;
  onDelete: () => void;
}

/**
 * 右侧详情面板：姓名、周、提交时间与原文（只读，保留换行）。
 * 组员周报已交时多一个「归一稿」子 tab。
 */
function ReportDetailPanel({
  entry,
  report,
  failed,
  period,
  level,
  refreshTick,
  onToast,
  onClose,
  onRepaste,
  onDelete,
}: ReportDetailPanelProps) {
  const [tab, setTab] = useState<"raw" | "draft" | "review">("raw");
  // 面板宽度可拖动调整，记住上次的宽度。
  const [width, setWidth] = useState<number>(loadReportDetailWidth);
  // 组长经组报带入的周报本身就是定稿，不需要归一。
  const canNormalize = entry.status !== "missing" && !entry.fromGroup;
  const canReview = canNormalize && level === "member";
  const showDraft = canNormalize && tab === "draft";
  const showReview = canReview && tab === "review";

  useEffect(() => {
    saveReportDetailWidth(width);
  }, [width]);

  // 拖动左边缘调宽度：向右拖变窄，向左拖变宽；最多占窗口的 85%。
  const startResize = useCallback(
    (e: React.MouseEvent) => {
      e.preventDefault();
      const startX = e.clientX;
      const startWidth = width;
      const maxWidth = Math.max(REPORT_DETAIL_MIN_WIDTH, window.innerWidth * 0.85);
      document.body.classList.add("is-resizing-x");
      const onMove = (ev: MouseEvent) => {
        const next = Math.min(maxWidth, Math.max(REPORT_DETAIL_MIN_WIDTH, startWidth + (startX - ev.clientX)));
        setWidth(next);
      };
      const onUp = () => {
        document.body.classList.remove("is-resizing-x");
        window.removeEventListener("mousemove", onMove);
        window.removeEventListener("mouseup", onUp);
      };
      window.addEventListener("mousemove", onMove);
      window.addEventListener("mouseup", onUp);
    },
    [width],
  );

  return (
    <section className={`report-detail${showReview ? " is-wide" : ""}`} style={{ width }}>
      <div
        className="report-detail-resize"
        role="separator"
        aria-orientation="vertical"
        aria-label="拖动调整宽度"
        title="拖动调整宽度（双击恢复默认）"
        onMouseDown={startResize}
        onDoubleClick={() => setWidth(REPORT_DETAIL_DEFAULT_WIDTH)}
      />
      <div className="report-detail-head">
        <div className="report-detail-head-main">
          <div className="report-detail-title">{entry.name}</div>
          <div className="report-detail-meta">
            <span>{period}</span>
            {entry.submittedAt && <span>{formatSubmittedAt(entry.submittedAt)} 提交</span>}
          </div>
        </div>
        <button type="button" className="icon-btn" title="关闭" onClick={onClose}>
          <Icon name="close" />
        </button>
      </div>

      {canNormalize && (
        <div className="report-detail-tabs">
          <div className="segmented">
            <button
              type="button"
              className={`segmented-btn${tab === "raw" ? " is-active" : ""}`}
              onClick={() => setTab("raw")}
            >
              原文
            </button>
            <button
              type="button"
              className={`segmented-btn${tab === "draft" ? " is-active" : ""}`}
              onClick={() => setTab("draft")}
            >
              归一稿
            </button>
            {canReview && (
              <button
                type="button"
                className={`segmented-btn${tab === "review" ? " is-active" : ""}`}
                onClick={() => setTab("review")}
              >
                对照上周
              </button>
            )}
          </div>
        </div>
      )}

      {showReview ? (
        <div className="report-detail-body">
          <ReportReview
            week={period}
            personId={entry.personId}
            refreshTick={refreshTick}
            onToast={onToast}
          />
        </div>
      ) : showDraft ? (
        <div className="report-detail-body">
          <ReportDraft
            week={period}
            personId={entry.personId}
            level={level}
            refreshTick={refreshTick}
            onToast={onToast}
          />
        </div>
      ) : (
        <>
          <div className="report-detail-actions">
            {report ? (
              <>
                <button type="button" className="btn" onClick={onRepaste}>
                  <Icon name="paste" size={14} />
                  重新粘贴
                </button>
                <button type="button" className="btn" onClick={onDelete}>
                  <Icon name="trash" size={14} />
                  删除
                </button>
              </>
            ) : entry.status !== "missing" ? (
              <button type="button" className="btn" onClick={onRepaste}>
                <Icon name="paste" size={14} />
                重新粘贴
              </button>
            ) : null}
          </div>

          <div className="report-detail-body">
            {report ? (
              <pre className="report-body">{report.body}</pre>
            ) : entry.status === "missing" ? (
              <div className="report-empty">
                <p>还没交这周的周报。</p>
                <button type="button" className="btn btn-primary" onClick={onRepaste}>
                  <Icon name="paste" size={14} />
                  粘贴
                </button>
              </div>
            ) : (
              <div className="report-empty">{failed ? "读取周报失败。" : "正在读取周报…"}</div>
            )}
          </div>
        </>
      )}
    </section>
  );
}

/** 设置周报日（周一到周日）。 */
function WeekdayModal({ current, onClose }: { current: number; onClose: () => void }) {
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const pick = async (n: number) => {
    if (busy || n === current) return;
    setBusy(true);
    setError(null);
    try {
      await ReportService.SetReportWeekday(n);
      onClose();
    } catch (err) {
      setError(`设置失败：${readableError(err)}`);
    } finally {
      setBusy(false);
    }
  };

  return (
    <div className="modal-mask" onClick={onClose}>
      <div className="modal" onClick={(e) => e.stopPropagation()}>
        <h2>设置周报日</h2>
        <p>迟交按该周周报日当天 24:00 判断。</p>
        <div className="weekday-grid">
          {[1, 2, 3, 4, 5, 6, 7].map((n) => (
            <button
              key={n}
              type="button"
              className={`weekday-option${n === current ? " is-active" : ""}`}
              disabled={busy}
              onClick={() => void pick(n)}
            >
              {weekdayLabel(n)}
            </button>
          ))}
        </div>
        {error && <div className="settings-error">{error}</div>}
        <div className="modal-buttons">
          <button type="button" className="btn" onClick={onClose}>
            完成
          </button>
        </div>
      </div>
    </div>
  );
}

/** 周 / 月切换，放在周（月）切换旁。 */
function PeriodSwitch({
  period,
  onChange,
}: {
  period: "week" | "month";
  onChange: (p: "week" | "month") => void;
}) {
  return (
    <div className="segmented period-switch">
      <button
        type="button"
        className={`segmented-btn${period === "week" ? " is-active" : ""}`}
        onClick={() => onChange("week")}
      >
        周
      </button>
      <button
        type="button"
        className={`segmented-btn${period === "month" ? " is-active" : ""}`}
        onClick={() => onChange("month")}
      >
        月
      </button>
    </div>
  );
}

interface MonthViewProps {
  level: ReportLevel;
  onLevel: (level: ReportLevel) => void;
  onWeek: () => void;
}

/**
 * 月视角：框架组长 = 组月报（当月各周定稿组报合并）；SE = 组长月报收集状态、
 * 粘贴归档、组长月报归一稿与产品月报。读写一律经 ReportService。
 */
function MonthView({ level, onLevel, onWeek }: MonthViewProps) {
  const [month, setMonth] = useState<ReportMonth | null>(null);
  const [status, setStatus] = useState<ReportStatusData | null>(null);
  const [selectedId, setSelectedId] = useState<string | null>(null);
  const [detail, setDetail] = useState<ReportData | null>(null);
  const [detailError, setDetailError] = useState(false);
  const [paste, setPaste] = useState<{ personId: string; text: string } | null>(null);
  const [batchOpen, setBatchOpen] = useState(false);
  const [panelOpen, setPanelOpen] = useState(level === "member");
  const [dayOpen, setDayOpen] = useState(false);
  const [toast, setToast] = useState<string | null>(null);
  const [refreshTick, setRefreshTick] = useState(0);
  const monthRef = useRef<ReportMonth | null>(null);
  monthRef.current = month;

  const load = useCallback(
    async (m: ReportMonth) => {
      setMonth(m);
      if (level !== "lead") {
        setStatus(null);
        return;
      }
      try {
        setStatus(await ReportService.Status(m.month, "lead-month", "", ""));
      } catch (err) {
        setToast(`读取月报状态失败：${readableError(err)}`);
      }
    },
    [level],
  );

  useEffect(() => {
    setSelectedId(null);
    setPanelOpen(level === "member");
    const cur = monthRef.current;
    void (cur ? load(cur) : ReportService.Month("this").then(load)).catch((err) =>
      setToast(`读取月份失败：${readableError(err)}`),
    );
  }, [level, load]);

  useEffect(() => {
    const off = Events.On("report:changed", () => {
      const m = monthRef.current;
      if (!m) return;
      void ReportService.Month(m.month)
        .then(load)
        .then(() => setRefreshTick((t) => t + 1))
        .catch(() => undefined);
    });
    return off;
  }, [load]);

  const shift = async (delta: number) => {
    const m = monthRef.current;
    if (!m) return;
    try {
      setSelectedId(null);
      await load(await ReportService.ShiftMonth(m.month, delta));
    } catch (err) {
      setToast(`切换月份失败：${readableError(err)}`);
    }
  };

  const entries = useMemo(
    () => sortByName([...(status?.entries ?? []), ...(status?.extra ?? [])]),
    [status],
  );
  const selected = entries.find((e) => e.personId === selectedId) ?? null;

  useEffect(() => {
    const m = monthRef.current;
    if (!selected || !m || selected.status === "missing") {
      setDetail(null);
      return;
    }
    setDetailError(false);
    let alive = true;
    void ReportService.Get(m.month, selected.personId, "lead-month")
      .then((d) => alive && setDetail(d))
      .catch(() => alive && setDetailError(true));
    return () => {
      alive = false;
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [selected?.personId, selected?.status, refreshTick]);

  const counts = status?.counts;
  // 月报收集日之后还没交的算逾期（收集日超过当月天数时按月末）。
  const monthDayOverdue = useMemo(() => {
    if (!month) return false;
    const m = /^(\d{4})-(\d{2})$/.exec(month.month);
    if (!m) return false;
    const year = Number(m[1]);
    const mon = Number(m[2]);
    const lastDay = new Date(year, mon, 0).getDate();
    const day = month.collectDay >= 1 ? Math.min(month.collectDay, lastDay) : lastDay;
    return Date.now() >= new Date(year, mon - 1, day + 1).getTime();
  }, [month]);

  return (
    <div className="report-view">
      <section className="report-main">
        <div className="toolbar">
          <div className="toolbar-spacer" />
        </div>
        <div className="report-scroll">
          <header className="report-head">
            <div className="report-title-row">
              <h1>周报</h1>
              <div className="week-switcher">
                <button type="button" className="icon-btn" title="上个月" onClick={() => void shift(-1)}>
                  <Icon name="chevronLeft" size={14} />
                </button>
                <span className="week-label">{month ? month.month : "…"}</span>
                <button type="button" className="icon-btn" title="下个月" onClick={() => void shift(1)}>
                  <Icon name="chevron" size={14} />
                </button>
                {month && !month.isCurrent && (
                  <button
                    type="button"
                    className="btn btn-ghost"
                    onClick={() => void ReportService.Month("this").then(load)}
                  >
                    回到本月
                  </button>
                )}
              </div>
            </div>
            <div className="report-head-meta">
              <PeriodSwitch period="month" onChange={(p) => p === "week" && onWeek()} />
              {month && (
                <span>
                  {level === "lead"
                    ? `每月 ${month.collectDay} 日收组长月报`
                    : `本月包含 ${(month.weeks ?? []).join("、")}`}
                </span>
              )}
            </div>
            <div className="segmented report-tabs">
              <button
                type="button"
                className={`segmented-btn${level === "member" ? " is-active" : ""}`}
                onClick={() => onLevel("member")}
              >
                {levelLabel("member")}
              </button>
              <button
                type="button"
                className={`segmented-btn${level === "lead" ? " is-active" : ""}`}
                onClick={() => onLevel("lead")}
              >
                {levelLabel("lead")}
              </button>
            </div>
          </header>

          <div className="report-toolbar">
            <div className="report-stats">
              {level === "lead" ? (
                <>
                  <span>
                    应交 <b>{counts?.expected ?? 0}</b>
                  </span>
                  <span>
                    已交 <b>{counts?.submitted ?? 0}</b>
                  </span>
                  <span>
                    迟交 <b>{counts?.late ?? 0}</b>
                  </span>
                  <span>
                    未交 <b>{counts?.missing ?? 0}</b>
                  </span>
                </>
              ) : (
                <span>组月报由本月各周已定稿的组报合并</span>
              )}
            </div>
            <div className="report-actions">
              <button
                type="button"
                className={`btn${panelOpen ? " is-on" : ""}`}
                onClick={() => {
                  setSelectedId(null);
                  setPanelOpen((v) => !v);
                }}
              >
                <Icon name="report" size={14} />
                {level === "member" ? "组月报" : "产品月报"}
              </button>
              {level === "lead" && (
                <>
                  <button
                    type="button"
                    className="btn btn-primary"
                    onClick={() => setPaste({ personId: "", text: "" })}
                  >
                    <Icon name="paste" size={14} />
                    粘贴月报
                  </button>
                  <MoreMenu
                    items={[
                      {
                        label: "批量归一本月已交",
                        icon: "bolt",
                        onClick: () => setBatchOpen(true),
                      },
                      { label: "设置收集日", icon: "calendar", onClick: () => setDayOpen(true) },
                    ]}
                  />
                </>
              )}
            </div>
          </div>

          {level === "lead" ? (
            <div className="report-list">
              <div className="report-section">
                {entries.map((e) => (
                  <ReportRow
                    key={e.personId}
                    entry={e}
                    showGroup
                    selected={selectedId === e.personId}
                    overdue={monthDayOverdue}
                    onSelect={() => {
                      setPanelOpen(false);
                      setSelectedId(e.personId);
                    }}
                    onPaste={() => setPaste({ personId: e.personId, text: "" })}
                  />
                ))}
                {status && entries.length === 0 && (
                  <div className="report-empty">
                    <p>还没有组长，先在名单里添加组长。</p>
                  </div>
                )}
              </div>
            </div>
          ) : (
            <div className="report-empty">
              <p>
                组月报合并本月各周（{(month?.weeks ?? []).join("、") || "…"}）已定稿的组报。点「组月报」生成、编辑、定稿和导出。
              </p>
            </div>
          )}
        </div>
      </section>

      {panelOpen && month && (
        <GroupReport
          key={`${level}-${month.month}`}
          kind={level === "member" ? "month-group" : "month-product"}
          week={month.month}
          refreshTick={refreshTick}
          onClose={() => setPanelOpen(false)}
          onToast={setToast}
        />
      )}

      {!panelOpen && selected && month && (
        <ReportDetailPanel
          entry={selected}
          report={detail}
          failed={detailError}
          period={month.month}
          level="lead-month"
          refreshTick={refreshTick}
          onToast={setToast}
          onClose={() => setSelectedId(null)}
          onRepaste={() => setPaste({ personId: selected.personId, text: "" })}
          onDelete={() => setToast("组长月报可重新粘贴覆盖，旧版自动保留在历史记录里")}
        />
      )}

      {paste && month && (
        <ReportPaste
          week={month.month}
          level="lead"
          month
          presetPersonId={paste.personId}
          initialText={paste.text}
          onClose={() => setPaste(null)}
          onDone={() => {
            setPaste(null);
            void load(month);
            setRefreshTick((t) => t + 1);
          }}
        />
      )}

      {batchOpen && month && (
        <BatchNormalize
          week={month.month}
          level="lead-month"
          onClose={() => setBatchOpen(false)}
          onToast={setToast}
        />
      )}

      {dayOpen && month && (
        <MonthDayModal current={month.collectDay} onClose={() => setDayOpen(false)} />
      )}

      {toast && (
        <div className="toast" role="alert" onClick={() => setToast(null)}>
          {toast}
        </div>
      )}
    </div>
  );
}

/** 设置月报收集日（1-31，超过当月天数按月末，不因周末顺延）。 */
function MonthDayModal({ current, onClose }: { current: number; onClose: () => void }) {
  const [day, setDay] = useState(String(current));
  const [error, setError] = useState<string | null>(null);
  const save = async () => {
    const n = Number(day);
    if (!Number.isInteger(n) || n < 1 || n > 31) {
      setError("收集日必须是 1-31 的整数");
      return;
    }
    try {
      await ReportService.SetReportMonthDay(n);
      onClose();
    } catch (err) {
      setError(`设置失败：${readableError(err)}`);
    }
  };
  return (
    <div className="modal-mask" onClick={onClose}>
      <div className="modal" onClick={(e) => e.stopPropagation()}>
        <h2>设置月报收集日</h2>
        <p>每月这一天收组长月报，晚于当天 24:00 算迟交；超过当月天数按月末，不因周末顺延。</p>
        <input
          className="field"
          type="number"
          min={1}
          max={31}
          value={day}
          onChange={(e) => setDay(e.target.value)}
        />
        {error && <div className="settings-error">{error}</div>}
        <div className="modal-buttons">
          <button type="button" className="btn" onClick={onClose}>
            取消
          </button>
          <button type="button" className="btn btn-primary" onClick={() => void save()}>
            保存
          </button>
        </div>
      </div>
    </div>
  );
}
