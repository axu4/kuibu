import { useCallback, useEffect, useState } from "react";
import { ReportService } from "../../bindings/github.com/axu4/kuibu/app";
import type {
  DoneReview,
  PlanReview as RawPlan,
  ReviewResult as RawResult,
} from "../../bindings/github.com/axu4/kuibu/internal/core";
import type { Preview } from "../../bindings/github.com/axu4/kuibu/internal/llm";
import { Icon } from "./Icon";
import { Menu } from "./Menu";
import { SendPreview } from "./SendPreview";
import { readableError } from "../lib/todo";

/** 绑定里的切片可能是 null，这里统一成数组。 */
interface PlanReview extends Omit<RawPlan, "matches"> {
  matches: string[];
}

interface ReviewResult extends Omit<RawResult, "plans" | "done" | "next"> {
  plans: PlanReview[];
  done: DoneReview[];
  next: NonNullable<RawResult["next"]>;
}

function clean(r: RawResult): ReviewResult {
  return {
    ...r,
    plans: (r.plans ?? []).map((p) => ({ ...p, matches: p.matches ?? [] })),
    done: r.done ?? [],
    next: r.next ?? [],
  };
}

const STATUSES: { value: string; label: string }[] = [
  { value: "done", label: "完成" },
  { value: "partial", label: "部分" },
  { value: "delayed", label: "延期" },
  { value: "canceled", label: "取消" },
];

/** 对齐率显示成百分比。 */
export function alignmentText(v: number | null | undefined): string {
  return v === null || v === undefined ? "—" : `${Math.round(v * 100)}%`;
}

interface ReportReviewProps {
  week: string;
  personId: string;
  refreshTick: number;
  onToast: (text: string) => void;
}

/**
 * 「对照上周」子 tab：左栏上周定稿的计划（标状态、对应到本周完成项），
 * 右栏本周完成（未对应的标「计划外」）与下周计划；顶部对齐率。
 * 计算全部在核心库，这里只展示并调用 ReportService。
 */
export function ReportReview({ week, personId, refreshTick, onToast }: ReportReviewProps) {
  const [res, setRes] = useState<ReviewResult | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  const [preview, setPreview] = useState<Preview | null>(null);
  const [matchMenu, setMatchMenu] = useState<{ plan: PlanReview; x: number; y: number } | null>(
    null,
  );

  const load = useCallback(async () => {
    try {
      setRes(clean(await ReportService.Review(week, personId)));
      setError(null);
    } catch (err) {
      setRes(null);
      setError(readableError(err));
    }
  }, [week, personId]);

  useEffect(() => {
    void load();
  }, [load, refreshTick]);

  const run = async (fn: () => Promise<RawResult>) => {
    setBusy(true);
    try {
      setRes(clean(await fn()));
    } catch (err) {
      onToast(readableError(err));
    } finally {
      setBusy(false);
    }
  };

  const setPlan = (p: PlanReview, status: string, matches: string[] = p.matches) =>
    run(() => ReportService.SetReview(week, personId, p.id, status, matches, p.note));

  const toggleMatch = (p: PlanReview, doneId: string) => {
    const has = p.matches.includes(doneId);
    const next = has ? p.matches.filter((m) => m !== doneId) : [...p.matches, doneId];
    // 还没标状态就连线时默认标「完成」。
    void setPlan(p, p.status || "done", next);
  };

  const askSuggest = async () => {
    setBusy(true);
    try {
      setPreview(await ReportService.PrepareReviewSuggest(week, personId, ""));
    } catch (err) {
      onToast(`准备 AI 建议失败：${readableError(err)}`);
    } finally {
      setBusy(false);
    }
  };

  const carry = async () => {
    setBusy(true);
    try {
      const r = await ReportService.Carry(week, personId, []);
      setRes(clean(r.review));
      onToast(r.added > 0 ? `已顺延 ${r.added} 条到本周下周计划` : "没有需要顺延的计划");
    } catch (err) {
      onToast(`顺延失败：${readableError(err)}`);
    } finally {
      setBusy(false);
    }
  };

  if (error) {
    return (
      <div className="report-empty">
        <p>{error.includes("定稿") ? `${error}。先把上周的归一稿定稿，再来对照。` : error}</p>
      </div>
    );
  }
  if (!res) return <div className="report-empty">正在读取对照…</div>;

  const doneText = new Map(res.done.map((d) => [d.id, d.text]));
  const carryable = res.plans.some(
    (p) => (p.status === "partial" || p.status === "delayed") && !p.carried,
  );
  const c = res.counts;

  return (
    <div className="review">
      <div className="review-head">
        <div className="review-rate">
          <span className="review-rate-num">{alignmentText(res.alignment)}</span>
          <span className="settings-desc">对齐率</span>
        </div>
        <div className="review-counts">
          完成 {c.done} · 部分 {c.partial} · 延期 {c.delayed} · 取消 {c.canceled}
          {c.unmarked > 0 && ` · 未标 ${c.unmarked}`}
        </div>
        <span className="report-draft-spacer" />
        <button
          type="button"
          className="btn"
          disabled={busy || res.plans.length === 0}
          onClick={() => void askSuggest()}
        >
          <Icon name="bolt" size={14} />
          AI 建议
        </button>
        <button
          type="button"
          className="btn"
          disabled={busy || !carryable}
          title={res.thisFinal ? "本周归一稿已定稿，顺延前需先取消定稿" : undefined}
          onClick={() => void carry()}
        >
          一键顺延
        </button>
      </div>

      <div className="review-cols">
        <div className="review-col">
          <div className="review-col-title">{res.prevWeek} 计划</div>
          {res.plans.length === 0 && <div className="settings-desc">上周没有计划条目。</div>}
          {res.plans.map((p) => (
            <div key={p.id} className={`review-plan is-${p.status || "unmarked"}`}>
              <div className="review-plan-text">
                {p.text}
                {p.source === "ai" && <span className="draft-badge is-draft">AI 建议</span>}
                {p.carried && <span className="draft-badge">已顺延</span>}
              </div>
              <div className="review-plan-row">
                <div className="segmented">
                  {STATUSES.map((s) => (
                    <button
                      key={s.value}
                      type="button"
                      disabled={busy}
                      className={`segmented-btn${p.status === s.value ? " is-active" : ""}`}
                      onClick={() => void setPlan(p, p.status === s.value ? "" : s.value)}
                    >
                      {s.label}
                    </button>
                  ))}
                </div>
                <button
                  type="button"
                  className="link-btn"
                  disabled={busy || res.done.length === 0}
                  onMouseDown={(e) => e.stopPropagation()}
                  onClick={(e) => {
                    const r = e.currentTarget.getBoundingClientRect();
                    setMatchMenu({ plan: p, x: r.left, y: r.bottom + 4 });
                  }}
                >
                  对应…
                </button>
              </div>
              {p.matches.length > 0 && (
                <div className="review-matches">
                  {p.matches.map((m) => (
                    <span key={m} className="review-match">
                      ↳ {doneText.get(m) ?? m}
                    </span>
                  ))}
                </div>
              )}
            </div>
          ))}
        </div>

        <div className="review-col">
          <div className="review-col-title">{res.week} 完成</div>
          {res.done.length === 0 && <div className="settings-desc">本周没有完成条目。</div>}
          {res.done.map((d) => (
            <div key={d.id} className="review-done">
              {d.text}
              {d.unplanned && <span className="draft-badge is-warn">计划外</span>}
            </div>
          ))}
          <div className="review-col-title is-sub">{res.week} 下周计划</div>
          {res.next.length === 0 && <div className="settings-desc">无</div>}
          {res.next.map((n) => (
            <div key={n.id} className="review-done">
              {n.text}
            </div>
          ))}
        </div>
      </div>

      {matchMenu && (
        <Menu
          x={matchMenu.x}
          y={matchMenu.y}
          onClose={() => setMatchMenu(null)}
          items={res.done.map((d) => ({
            label: `${matchMenu.plan.matches.includes(d.id) ? "✓ " : "　"}${d.text}`,
            onClick: () => toggleMatch(matchMenu.plan, d.id),
          }))}
        />
      )}

      {preview && (
        <SendPreview
          title="发送给 AI 生成对照建议"
          preview={preview}
          onCancel={() => setPreview(null)}
          onConfirm={async () => {
            try {
              setRes(clean(await ReportService.ReviewSuggest(week, personId, "")));
              setPreview(null);
              onToast("AI 建议已填入（人工标过的不覆盖），请逐条确认");
            } catch (err) {
              onToast(`AI 建议失败：${readableError(err)}`);
            }
          }}
        />
      )}
    </div>
  );
}
