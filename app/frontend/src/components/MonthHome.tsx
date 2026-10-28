// 月报首页（独立 tab）：我的组（DE）/ 项目汇总（SE）。
//
// 我的组：每个报送项目一份组月报，从本月各周已定稿的按项目组报拉候选，整理、定稿后
// 按项目去向交出（我是 SE 的自动进项目汇总，交给别人的复制后标记已交）。
// 项目汇总：我当 SE 的项目，收各组 DE 交来的月报，合并成项目月报。

import { useCallback, useEffect, useRef, useState } from "react";
import { Events } from "@wailsio/runtime";
import { ReportService } from "../../bindings/github.com/axu4/kuibu/app";
import type { ProjectMonthList, ReportMonth } from "../../bindings/github.com/axu4/kuibu/app";
import { readableError } from "../lib/todo";
import { deliveryShort } from "./DeliveryBar";
import { Icon } from "./Icon";
import { MonthProjectFlow } from "./MonthProjectFlow";
import { loadMonthProjectRows, projectMonthStatus } from "./ReportView";
import { SEView } from "./SEView";
import { TriageProjectSettings } from "./triage/TriageProjectSettings";

type MonthTab = "mine" | "se";

const TAB_KEY = "kuibu.month.tab";

function loadTab(): MonthTab {
  try {
    return localStorage.getItem(TAB_KEY) === "se" ? "se" : "mine";
  } catch {
    return "mine";
  }
}

export function MonthHome() {
  const [tab, setTab] = useState<MonthTab>(loadTab);
  const choose = (next: MonthTab) => {
    setTab(next);
    try {
      localStorage.setItem(TAB_KEY, next);
    } catch {
      // 本地存储不可用时只是不记住上次的 tab。
    }
  };
  return (
    <div className="report-home">
      <div className="report-home-nav">
        <div className="segmented">
          <button type="button" className={`segmented-btn${tab === "mine" ? " is-active" : ""}`} onClick={() => choose("mine")}>
            我的组
          </button>
          <button type="button" className={`segmented-btn${tab === "se" ? " is-active" : ""}`} onClick={() => choose("se")}>
            项目汇总
          </button>
        </div>
      </div>
      <div className="report-home-body">{tab === "mine" ? <MonthGroupView /> : <SEView mode="month" />}</div>
    </div>
  );
}

/** 我的组（DE）：本月各项目组月报的状态与去向，点开进三步整理。 */
function MonthGroupView() {
  const [month, setMonth] = useState<ReportMonth | null>(null);
  const [rows, setRows] = useState<ProjectMonthList[] | null>(null);
  const [group, setGroup] = useState("");
  const [flow, setFlow] = useState<{ project: string | null } | null>(null);
  const [settingsOpen, setSettingsOpen] = useState(false);
  const [toast, setToast] = useState<string | null>(null);
  const monthRef = useRef<ReportMonth | null>(null);
  monthRef.current = month;

  useEffect(() => {
    if (!toast) return;
    const timer = setTimeout(() => setToast(null), 3200);
    return () => clearTimeout(timer);
  }, [toast]);

  const load = useCallback(async (m: ReportMonth) => {
    setMonth(m);
    try {
      const res = await loadMonthProjectRows(m.month);
      setRows(res.rows);
      setGroup(res.group);
    } catch (err) {
      setRows([]);
      setToast(`读取项目月报失败：${readableError(err)}`);
    }
  }, []);

  useEffect(() => {
    void ReportService.Month("this")
      .then(load)
      .catch((err) => setToast(`读取月份失败：${readableError(err)}`));
  }, [load]);

  useEffect(() => {
    const reload = () => {
      const m = monthRef.current;
      if (m) void load(m);
    };
    const offReport = Events.On("report:changed", reload);
    const offTriage = Events.On("triage:changed", reload);
    return () => {
      offReport();
      offTriage();
    };
  }, [load]);

  const shift = async (delta: number) => {
    const m = monthRef.current;
    if (!m) return;
    try {
      await load(await ReportService.ShiftMonth(m.month, delta));
    } catch (err) {
      setToast(`切换月份失败：${readableError(err)}`);
    }
  };

  const finalized = (rows ?? []).filter((r) => r.status === "final").length;
  const pending = (rows ?? []).filter(
    (r) => r.status === "final" && !r.delivery.mine && (!r.delivery.deliveredAt || r.delivery.stale),
  ).length;

  return (
    <div className="report-view">
      <section className="report-main">
        <div className="toolbar">
          <div className="toolbar-spacer" />
        </div>
        <div className="report-scroll">
          <header className="report-head">
            <div className="report-title-row">
              <h1>月报</h1>
              <div className="week-switcher">
                <button type="button" className="icon-btn" title="上个月" onClick={() => void shift(-1)}>
                  <Icon name="chevronLeft" size={14} />
                </button>
                <span className="week-label">{month ? month.month : "…"}</span>
                <button type="button" className="icon-btn" title="下个月" onClick={() => void shift(1)}>
                  <Icon name="chevron" size={14} />
                </button>
                {month && !month.isCurrent && (
                  <button type="button" className="btn btn-ghost" onClick={() => void ReportService.Month("this").then(load)}>
                    回到本月
                  </button>
                )}
              </div>
            </div>
            <div className="report-head-meta">
              {month && <span>每月 {month.collectDay} 日交 · 本月包含 {(month.weeks ?? []).join("、")}</span>}
            </div>
          </header>

          <div className="report-toolbar">
            <div className="report-stats">
              <span>
                项目 <b>{rows?.length ?? 0}</b>
              </span>
              <span>
                已定稿 <b>{finalized}</b>/{rows?.length ?? 0}
              </span>
              {pending > 0 && (
                <span>
                  待交给别人 <b>{pending}</b>
                </span>
              )}
            </div>
            <div className="report-actions">
              <button type="button" className="btn btn-ghost" onClick={() => setSettingsOpen(true)}>
                项目设置
              </button>
              <button
                type="button"
                className="btn btn-primary"
                disabled={!rows || rows.length === 0}
                onClick={() => setFlow({ project: null })}
              >
                <Icon name="report" size={14} />
                整理月报
              </button>
            </div>
          </div>

          {rows === null ? (
            <div className="report-empty">
              <p>正在读取项目月报…</p>
            </div>
          ) : rows.length === 0 ? (
            <div className="report-empty">
              <p>还没有报送项目。先在项目设置里添加项目，并标明每个项目的 SE。</p>
              <button type="button" className="btn btn-primary" onClick={() => setSettingsOpen(true)}>
                项目设置
              </button>
            </div>
          ) : (
            <div className="month-project-list">
              <div className="month-project-head is-delivery">
                <span>项目</span>
                <span>候选</span>
                <span>交给</span>
                <span>状态</span>
                <span />
              </div>
              {rows.map((p) => {
                const state = projectMonthStatus(p);
                return (
                  <button
                    key={p.project}
                    type="button"
                    className="month-project-row is-delivery"
                    onClick={() => setFlow({ project: p.project })}
                  >
                    <span className="month-project-name">{p.projectName}</span>
                    <span>
                      {p.candidates} 条
                      {(p.missing ?? []).length > 0 && (
                        <span className="month-project-missing"> · 缺 {(p.missing ?? []).join("、")}</span>
                      )}
                    </span>
                    <span>{deliveryShort(p.delivery).replace(/^→ /, "")}</span>
                    <span className={`triage-rl-status ${state.cls}`}>{state.text}</span>
                    <span className="month-project-open">整理 ›</span>
                  </button>
                );
              })}
              <p className="month-project-hint">
                每个项目一份组月报：从本月各周已定稿的按项目组报拉候选，分「本月总结 / 下月计划」整理、定稿。
                我是 SE 的项目定稿后自动进「项目汇总」；交给别人的，在整理页复制定稿发给对方，再标记已交。
              </p>
            </div>
          )}
        </div>
      </section>

      {flow && month && (
        <MonthProjectFlow
          key={`${month.month}-${flow.project ?? "all"}`}
          month={month.month}
          group={group}
          initialProject={flow.project}
          onClose={() => {
            setFlow(null);
            void load(month);
          }}
          onToast={setToast}
        />
      )}

      {settingsOpen && (
        <TriageProjectSettings
          items={[]}
          onClose={() => setSettingsOpen(false)}
          onChanged={() => month && void load(month)}
          onToast={setToast}
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
