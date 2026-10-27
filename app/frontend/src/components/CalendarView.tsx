import { useCallback, useEffect, useMemo, useState } from "react";
import { Events } from "@wailsio/runtime";
import { ReportService } from "../../bindings/github.com/axu4/kuibu/app";
import type {
  CalendarDayMonth,
  CalendarMonth,
} from "../../bindings/github.com/axu4/kuibu/internal/core";
import { alignmentText } from "./ReportReview";
import { readableError } from "../lib/todo";
import { reportStatusLabel, weekdayLabel } from "../lib/report";
import type { ReportLevel } from "../lib/report";

/** 顶部切换：月视图 / 日视图。 */
type CalendarMode = "month" | "day";

interface CalendarViewProps {
  /** 月视图点格子：跳到该人该周的周报。 */
  onOpenReport: (week: string, personId: string, level: ReportLevel) => void;
  /** 日视图点会议：跳到笔记视图并打开该笔记。 */
  onOpenNote: (id: string) => void;
  /** 日视图点待办：跳到待办视图并打开该待办。 */
  onOpenTodo: (id: string) => void;
}

function currentMonth(): string {
  const now = new Date();
  return `${now.getFullYear()}-${String(now.getMonth() + 1).padStart(2, "0")}`;
}

function shiftMonth(month: string, delta: number): string {
  const [y, m] = month.split("-").map(Number);
  const d = new Date(y, m - 1 + delta, 1);
  return `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, "0")}`;
}

/** 把 YYYY-MM-DD 转成 MM/DD。 */
function monthDay(date: string): string {
  const m = /^(\d{4})-(\d{2})-(\d{2})$/.exec(date);
  if (!m) return date;
  return `${Number(m[2])}/${Number(m[3])}`;
}

const WEEKDAY_NAMES = ["周一", "周二", "周三", "周四", "周五", "周六", "周日"];

/**
 * 日历视图：只读汇总，计算全部在核心库，这里只展示与跳转。
 * 月视图点格子跳周报，日视图点会议跳笔记、点待办跳待办；不在日历里改周报。
 */
export function CalendarView({ onOpenReport, onOpenNote, onOpenTodo }: CalendarViewProps) {
  const [mode, setMode] = useState<CalendarMode>("month");
  const [month, setMonth] = useState(currentMonth);
  const [perspective, setPerspective] = useState<ReportLevel>("member");
  const [group, setGroup] = useState("");
  const [groups, setGroups] = useState<string[]>([]);
  const [monthData, setMonthData] = useState<CalendarMonth | null>(null);
  const [dayData, setDayData] = useState<CalendarDayMonth | null>(null);
  const [error, setError] = useState<string | null>(null);

  const load = useCallback(async () => {
    try {
      setError(null);
      if (mode === "month") {
        const data = await ReportService.CalendarMonth(month, group, perspective);
        setMonthData(data);
      } else {
        const data = await ReportService.CalendarDay(month);
        setDayData(data);
      }
    } catch (err) {
      setError(`读取日历失败：${readableError(err)}`);
    }
  }, [mode, month, group, perspective]);

  useEffect(() => {
    void load();
  }, [load]);

  // 首次加载组列表；外部写入周报/待办后刷新当前视图（CLI 写入后自动刷新）。
  useEffect(() => {
    void ReportService.Groups()
      .then((g) => setGroups(g ?? []))
      .catch(() => setGroups([]));
  }, []);

  useEffect(() => {
    const offReport = Events.On("report:changed", () => void load());
    const offTodo = Events.On("todo:changed", () => void load());
    return () => {
      offReport();
      offTodo();
    };
  }, [load]);

  const weeks = useMemo(() => {
    const rows: NonNullable<CalendarDayMonth["days"]>[] = [];
    const days = dayData?.days ?? [];
    for (let i = 0; i < days.length; i += 7) {
      rows.push(days.slice(i, i + 7));
    }
    return rows;
  }, [dayData]);

  // 绑定里的切片可能是 null，这里统一成数组。
  const monthWeeks = monthData?.weeks ?? [];
  const monthRows = monthData?.rows ?? [];

  return (
    <div className="report-view">
      <section className="report-main">
        <div className="report-scroll">
          <header className="report-head">
            <div className="report-title-row">
              <h1 className="report-title">日历</h1>
              <div className="segmented" role="tablist" aria-label="日历模式">
                <button
                  type="button"
                  className={`segmented-btn${mode === "month" ? " is-active" : ""}`}
                  onClick={() => setMode("month")}
                >
                  月视图
                </button>
                <button
                  type="button"
                  className={`segmented-btn${mode === "day" ? " is-active" : ""}`}
                  onClick={() => setMode("day")}
                >
                  日视图
                </button>
              </div>
            </div>
            <div className="report-head-meta">
              <button type="button" className="icon-btn" title="上个月" onClick={() => setMonth((m) => shiftMonth(m, -1))}>
                ←
              </button>
              <span>{month}</span>
              <button type="button" className="icon-btn" title="下个月" onClick={() => setMonth((m) => shiftMonth(m, 1))}>
                →
              </button>
              <button type="button" className="icon-btn" title="回到本月" onClick={() => setMonth(currentMonth())}>
                本月
              </button>
              {mode === "month" && (
                <>
                  <div className="segmented" role="tablist" aria-label="月视图视角">
                    <button
                      type="button"
                      className={`segmented-btn${perspective === "member" ? " is-active" : ""}`}
                      onClick={() => setPerspective("member")}
                    >
                      框架组长
                    </button>
                    <button
                      type="button"
                      className={`segmented-btn${perspective === "lead" ? " is-active" : ""}`}
                      onClick={() => setPerspective("lead")}
                    >
                      SE
                    </button>
                  </div>
                  <label>
                    组
                    <select value={group} onChange={(e) => setGroup(e.target.value)}>
                      <option value="">全部组</option>
                      {groups.map((g) => (
                        <option key={g} value={g}>
                          {g}
                        </option>
                      ))}
                    </select>
                  </label>
                </>
              )}
              {mode === "day" && dayData && (
                <span>周报日：{weekdayLabel(dayData.reportWeekday)}</span>
              )}
            </div>
          </header>

          {error && <div className="report-empty">{error}</div>}

          {mode === "month" && monthData && (
            <div className="calendar-table-wrap">
              <table className="calendar-table">
                <thead>
                  <tr>
                    <th>组员</th>
                    {monthWeeks.map((w) => (
                      <th key={w.week}>
                        {w.week}
                        <span className="calendar-week-range">
                          {monthDay(w.start)}–{monthDay(w.end)}
                        </span>
                      </th>
                    ))}
                  </tr>
                </thead>
                <tbody>
                  {monthRows.map((row) => (
                    <tr key={row.person}>
                      <td>
                        <span className="report-row-name">{row.name}</span>
                        {row.group && <span className="report-row-group">{row.group}</span>}
                      </td>
                      {(row.cells ?? []).map((cell) => (
                        <td key={cell.week}>
                          {cell.status ? (
                            <button
                              type="button"
                              className="calendar-cell"
                              title="打开该人该周的周报"
                              onClick={() => onOpenReport(cell.week, row.person, perspective)}
                            >
                              <span>{reportStatusLabel(cell.status)}</span>
                              <span className="calendar-cell-meta">
                                {cell.completed}完 · {cell.delayed}延 · {alignmentText(cell.alignment)}
                              </span>
                            </button>
                          ) : (
                            <span className="calendar-cell-empty">—</span>
                          )}
                        </td>
                      ))}
                    </tr>
                  ))}
                </tbody>
              </table>
              {monthRows.length === 0 && <div className="report-empty">本月没有组员周报数据。</div>}
            </div>
          )}

          {mode === "day" && dayData && (
            <div className="calendar-grid">
              <div className="calendar-grid-head">
                {WEEKDAY_NAMES.map((n) => (
                  <div key={n}>{n}</div>
                ))}
              </div>
              {weeks.map((week, i) => (
                <div className="calendar-grid-row" key={week[0]?.date ?? i}>
                  {week.map((d) => (
                    <div
                      key={d.date}
                      className={`calendar-day${d.isCurrentMonth ? "" : " is-other-month"}${d.isReportDay ? " is-report-day" : ""}`}
                    >
                      <div className="calendar-day-num">
                        {monthDay(d.date)}
                        {d.isReportDay && <span className="calendar-report-badge">周报日</span>}
                      </div>
                      {(d.meetings ?? []).map((m) => (
                        <button key={m.id} type="button" className="calendar-day-item" onClick={() => onOpenNote(m.id)}>
                          会议：{m.title}
                        </button>
                      ))}
                      {(d.todos ?? []).map((t) => (
                        <button key={t.id} type="button" className="calendar-day-item" onClick={() => onOpenTodo(t.id)}>
                          待办截止：{t.title}
                        </button>
                      ))}
                    </div>
                  ))}
                </div>
              ))}
            </div>
          )}
        </div>
      </section>
    </div>
  );
}
