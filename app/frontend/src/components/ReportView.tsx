import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { Clipboard, Events } from "@wailsio/runtime";
import { ReportService } from "../../bindings/github.com/axu4/kuibu/app";
import type {
  ReportData,
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
import {
  formatSubmittedAt,
  levelLabel,
  loadReportLevel,
  reportStatusLabel,
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
export function ReportView() {
  const [week, setWeek] = useState<ReportWeek | null>(null);
  const [level, setLevel] = useState<ReportLevel>(loadReportLevel);
  const [status, setStatus] = useState<ReportStatusData | null>(null);
  const [selectedId, setSelectedId] = useState<string | null>(null);
  const [detail, setDetail] = useState<ReportData | null>(null);
  const [detailError, setDetailError] = useState(false);
  const [paste, setPaste] = useState<{ personId: string } | null>(null);
  const [managerOpen, setManagerOpen] = useState(false);
  const [weekdayOpen, setWeekdayOpen] = useState(false);
  const [deleteTarget, setDeleteTarget] = useState<ReportPerson | null>(null);
  const [toast, setToast] = useState<string | null>(null);
  const [batchOpen, setBatchOpen] = useState(false);
  const [groupOpen, setGroupOpen] = useState(false);
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

  // 首次加载本周。
  useEffect(() => {
    let alive = true;
    (async () => {
      try {
        const w = await ReportService.Week("this");
        if (!alive) return;
        setWeek(w);
        await loadStatus(w.week, levelRef.current);
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
      setSelectedId(null);
      setDetail(null);
      const w = weekRef.current;
      if (w) void loadStatus(w.week, next);
    },
    [loadStatus],
  );

  const openRow = useCallback((entry: ReportPerson) => {
    setGroupOpen(false);
    setSelectedId(entry.personId);
    setDetail(null);
    setDetailError(false);
  }, []);

  const closeDetail = useCallback(() => {
    setSelectedId(null);
    setDetail(null);
    setDetailError(false);
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
                  className={`btn${groupOpen ? " is-on" : ""}`}
                  onClick={() => {
                    setSelectedId(null);
                    setGroupOpen((v) => !v);
                  }}
                >
                  <Icon name="report" size={14} />
                  组报
                </button>
              )}
              <button
                type="button"
                className="btn btn-primary"
                onClick={() => setPaste({ personId: "" })}
              >
                <Icon name="paste" size={14} />
                粘贴周报
              </button>
              <MoreMenu
                items={[
                  ...(level === "member"
                    ? [
                        {
                          label: "批量归一本周已交",
                          icon: "bolt" as const,
                          onClick: () => setBatchOpen(true),
                        },
                      ]
                    : []),
                  { label: "复制催交话术", icon: "copy", onClick: () => void copyReminder() },
                  { label: "管理名单", icon: "person", onClick: () => setManagerOpen(true) },
                  { label: "设置周报日", icon: "calendar", onClick: () => setWeekdayOpen(true) },
                ]}
              />
            </div>
          </div>

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
                      onSelect={() => openRow(e)}
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
                      onSelect={() => openRow(e)}
                    />
                  ))}
                </div>
              )}
            </div>
          )}
        </div>
      </section>

      {groupOpen && level === "member" && week && (
        <GroupReport
          week={week.week}
          refreshTick={refreshTick}
          onClose={() => setGroupOpen(false)}
          onToast={setToast}
        />
      )}

      {!groupOpen && selectedEntry && week && (
        <ReportDetailPanel
          entry={selectedEntry}
          report={detail}
          failed={detailError}
          week={week}
          level={level}
          refreshTick={refreshTick}
          onToast={setToast}
          onClose={closeDetail}
          onRepaste={() => setPaste({ personId: selectedEntry.personId })}
          onDelete={() => setDeleteTarget(selectedEntry)}
        />
      )}

      {paste && week && (
        <ReportPaste
          week={week.week}
          level={level}
          presetPersonId={paste.personId}
          onClose={() => setPaste(null)}
          onDone={() => {
            setPaste(null);
            void loadStatus(week.week, level);
            setRefreshTick((t) => t + 1);
          }}
        />
      )}

      {batchOpen && week && (
        <BatchNormalize week={week.week} onClose={() => setBatchOpen(false)} onToast={setToast} />
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
  onSelect: () => void;
}

/** 一行人员状态：姓名、组（SE tab）、状态点与提交时间。 */
function ReportRow({ entry, showGroup, selected, onSelect }: ReportRowProps) {
  return (
    <button
      type="button"
      className={`report-row${selected ? " is-selected" : ""}`}
      onClick={onSelect}
    >
      <span className="report-row-name">{entry.name}</span>
      {showGroup && entry.group && <span className="report-row-group">{entry.group}</span>}
      <span className={`report-row-status is-${entry.status}`}>
        <span className="report-status-dot" />
        {reportStatusLabel(entry.status)}
      </span>
      {entry.normalized && entry.normalized !== "none" && (
        <span
          className={`draft-badge is-${entry.normalized}${entry.sourceChanged ? " is-warn" : ""}`}
          title={entry.sourceChanged ? "归一后原文又更新过" : undefined}
        >
          {entry.normalized === "final" ? "定稿" : "草稿"}
          {entry.sourceChanged && " ·原文已更新"}
        </span>
      )}
      {entry.alignment !== null && entry.alignment !== undefined && (
        <span className="report-row-time" title="对照上周计划的对齐率">
          对齐 {alignmentText(entry.alignment)}
        </span>
      )}
      {entry.submittedAt && (
        <span className="report-row-time">{formatSubmittedAt(entry.submittedAt)}</span>
      )}
    </button>
  );
}

interface ReportDetailPanelProps {
  entry: ReportPerson;
  report: ReportData | null;
  failed: boolean;
  week: ReportWeek;
  level: ReportLevel;
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
  week,
  level,
  refreshTick,
  onToast,
  onClose,
  onRepaste,
  onDelete,
}: ReportDetailPanelProps) {
  const [tab, setTab] = useState<"raw" | "draft" | "review">("raw");
  const canNormalize = level === "member" && entry.status !== "missing";
  const showDraft = canNormalize && tab === "draft";
  const showReview = canNormalize && tab === "review";
  return (
    <section className={`report-detail${showReview ? " is-wide" : ""}`}>
      <div className="report-detail-head">
        <div className="report-detail-head-main">
          <div className="report-detail-title">{entry.name}</div>
          <div className="report-detail-meta">
            <span>{week.week}</span>
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
            <button
              type="button"
              className={`segmented-btn${tab === "review" ? " is-active" : ""}`}
              onClick={() => setTab("review")}
            >
              对照上周
            </button>
          </div>
        </div>
      )}

      {showReview ? (
        <div className="report-detail-body">
          <ReportReview
            week={week.week}
            personId={entry.personId}
            refreshTick={refreshTick}
            onToast={onToast}
          />
        </div>
      ) : showDraft ? (
        <div className="report-detail-body">
          <ReportDraft
            week={week.week}
            personId={entry.personId}
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
