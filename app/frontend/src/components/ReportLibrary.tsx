import { useCallback, useEffect, useMemo, useState } from "react";
import { ReportService } from "../../bindings/github.com/axu4/kuibu/app";
import type { ReportWeek } from "../../bindings/github.com/axu4/kuibu/app";
import type { LibraryReport } from "../../bindings/github.com/axu4/kuibu/internal/core";
import { Icon } from "./Icon";
import { readableError } from "../lib/todo";

interface ReportLibraryProps {
  initialWeek?: string;
  onOpenGroups: () => void;
}

/** 周报库：所有组已定稿组报，按组 / 项目 / 周查看。默认入口。 */
export function ReportLibrary({ initialWeek, onOpenGroups }: ReportLibraryProps) {
  const [week, setWeek] = useState<ReportWeek | null>(null);
  const [rows, setRows] = useState<LibraryReport[]>([]);
  const [group, setGroup] = useState("all");
  const [project, setProject] = useState("all");
  const [q, setQ] = useState("");
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [preview, setPreview] = useState<LibraryReport | null>(null);

  const load = useCallback(async (w: ReportWeek) => {
    setLoading(true);
    try {
      const list = (await ReportService.ReportLibrary(w.week, "", "")) ?? [];
      setRows(list);
      setError(null);
    } catch (err) {
      setRows([]);
      setError(readableError(err));
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    let alive = true;
    (async () => {
      try {
        const w = await ReportService.Week(initialWeek ?? "this");
        if (!alive) return;
        setWeek(w);
        await load(w);
      } catch (err) {
        if (alive) {
          setError(readableError(err));
          setLoading(false);
        }
      }
    })();
    return () => {
      alive = false;
    };
  }, [initialWeek, load]);

  const shift = async (delta: number) => {
    if (!week) return;
    try {
      const w = await ReportService.ShiftWeek(week.week, delta);
      setWeek(w);
      setGroup("all");
      setProject("all");
      setQ("");
      await load(w);
    } catch (err) {
      setError(readableError(err));
    }
  };

  const groups = useMemo(() => [...new Set(rows.map((r) => r.group))].sort(), [rows]);
  const projects = useMemo(() => [...new Set(rows.map((r) => r.project).filter(Boolean))].sort(), [rows]);
  const filtered = useMemo(() => {
    const text = q.trim();
    return rows.filter((r) => {
      if (group !== "all" && r.group !== group) return false;
      if (project !== "all" && r.project !== project) return false;
      if (text && !`${r.group} ${r.project} ${r.leadName} ${r.body}`.includes(text)) return false;
      return true;
    });
  }, [group, project, q, rows]);
  const totalItems = filtered.reduce((n, r) => n + r.count, 0);

  const refreshFilters = (g: string, p: string) => {
    setGroup(g);
    setProject(p);
  };

  return (
    <div className="lib-page">
      <div className="lib-inner">
        <div className="lib-head">
          <div>
            <div className="lib-eyebrow">Weekly archive · {week?.week ?? "…"}</div>
            <h1>周报库</h1>
            <p>所有组定稿后的周报都在这里。按组、项目、周统一查看；组报定稿后自动归档。</p>
          </div>
          <div className="lib-head-actions">
            <span className="lib-scope"><i />全部组</span>
            <button type="button" className="btn" onClick={onOpenGroups}>
              <Icon name="settings" size={14} />
              组管理
            </button>
          </div>
        </div>

        <div className="lib-stats">
          <div><b>{filtered.length}</b><span>份定稿组报</span></div>
          <div><b>{new Set(filtered.map((r) => r.group)).size}</b><span>个组 · 已归档</span></div>
          <div><b>{new Set(filtered.map((r) => r.project).filter(Boolean)).size}</b><span>个项目</span></div>
          <div className="is-accent"><b>{totalItems}</b><span>条定稿事项</span></div>
        </div>

        <div className="lib-filter">
          <div className="lib-seg">
            <button type="button" className={group === "all" ? "is-on" : ""} onClick={() => refreshFilters("all", project)}>
              全部组
            </button>
            {groups.map((g) => (
              <button key={g} type="button" className={group === g ? "is-on" : ""} onClick={() => refreshFilters(g, project)}>
                {g}
              </button>
            ))}
          </div>
          <span className="lib-divider" />
          <select className="lib-select" value={project} onChange={(e) => refreshFilters(group, e.target.value)}>
            <option value="all">全部项目</option>
            {projects.map((p) => (
              <option key={p} value={p}>{p}</option>
            ))}
          </select>
          <input className="lib-search" value={q} onChange={(e) => setQ(e.target.value)} placeholder="搜组、项目或事项…" />
          <span className="lib-note">只显示已定稿 · 只读</span>
        </div>

        <div className="lib-table">
          <div className="lib-thead">
            <span>组</span><span>项目 / 内容</span><span>条目</span><span>定稿</span><span style={{ textAlign: "right" }}>操作</span>
          </div>
          {loading ? (
            <div className="lib-empty">正在读取周报库…</div>
          ) : error ? (
            <div className="lib-empty">
              <b>读取失败</b>
              <span>{error}</span>
            </div>
          ) : filtered.length === 0 ? (
            <div className="lib-empty">
              <b>这一周还没有已定稿组报</b>
              <span>去「我的组」整理并定稿后，会自动出现在这里。</span>
              <button type="button" className="btn" onClick={() => void shift(0)}>刷新</button>
            </div>
          ) : (
            filtered.map((r) => (
              <button key={r.id} type="button" className="lib-row" onClick={() => setPreview(r)}>
                <span className="lib-group">
                  <i>{r.group.slice(0, 1)}</i>
                  <b>{r.group}</b>
                  <small>组长 {r.leadName || "—"}</small>
                </span>
                <span className="lib-project">
                  {r.project && <em>{r.project}</em>}
                  <span>{firstItem(r.body)}</span>
                </span>
                <span className="lib-count">{r.count}<small>条</small></span>
                <span className="lib-time">{formatFinalized(r.finalizedAt)}<small>{r.week} · 已定稿</small></span>
                <span className="lib-actions"><span>查看</span></span>
              </button>
            ))
          )}
        </div>

        <div className="lib-footer">
          <div className="lib-week-nav">
            <button type="button" className="icon-btn" onClick={() => void shift(-1)} title="上一周">‹</button>
            <span>{week?.week ?? "…"}</span>
            <button type="button" className="icon-btn" onClick={() => void shift(1)} title="下一周">›</button>
          </div>
          <span>组报定稿后自动归档，不需要再交给任何角色或组。</span>
        </div>
      </div>

      {preview && <LibraryPreview report={preview} onClose={() => setPreview(null)} />}
    </div>
  );
}

function formatFinalized(iso: string): string {
  if (!iso) return "";
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return iso;
  return `${["周日", "周一", "周二", "周三", "周四", "周五", "周六"][d.getDay()]} ${String(d.getHours()).padStart(2, "0")}:${String(d.getMinutes()).padStart(2, "0")}`;
}

function LibraryPreview({ report, onClose }: { report: LibraryReport; onClose: () => void }) {
  return (
    <div className="modal-mask is-center" onMouseDown={onClose}>
      <div className="modal lib-preview" onMouseDown={(e) => e.stopPropagation()}>
        <div className="lib-preview-head">
          <div>
            <div className="lib-eyebrow">已定稿 · 只读</div>
            <h2>{report.group}{report.project ? ` · ${report.project}` : ""}</h2>
            <p>{report.count} 条事项 · 组长 {report.leadName || "—"} · {report.week} {formatFinalized(report.finalizedAt)} 定稿</p>
          </div>
          <button type="button" className="icon-btn" onClick={onClose} title="关闭">✕</button>
        </div>
        <div className="lib-preview-body">
          {report.body.split("\n").map((line, i) => {
            if (line.startsWith("## ")) return <h3 key={i}>{line.slice(3)}</h3>;
            const bullet = line.match(/^[-*+]\s+(.*)$/);
            if (bullet) return <p key={i} className="lib-li">{bullet[1]}</p>;
            const num = line.match(/^\d+[.)]\s+(.*)$/);
            if (num) return <p key={i} className="lib-li">{num[1]}</p>;
            if (!line.trim()) return null;
            return <p key={i}>{line}</p>;
          })}
        </div>
        <div className="modal-buttons">
          <button type="button" className="btn" onClick={onClose}>关闭</button>
          <button type="button" className="btn" onClick={() => void navigator.clipboard?.writeText(report.body)}>复制 Markdown</button>
        </div>
      </div>
    </div>
  );
}

function firstItem(body: string): string {
  for (const line of body.split("\n")) {
    const bullet = line.match(/^[-*+]\s+(.*)$/);
    if (bullet) return bullet[1];
    const num = line.match(/^\d+[.)]\s+(.*)$/);
    if (num) return num[1];
  }
  return "组报";
}
