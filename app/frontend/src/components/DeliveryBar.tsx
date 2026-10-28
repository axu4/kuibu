// 按项目组报 / 月报的去向条：我是 SE 时定稿自动进「项目汇总」；交给别人时
// 定稿后复制给对方，再标记已交。周报第 3 步与月报整理页共用。

import type { ProjectGroupDelivery } from "../../bindings/github.com/axu4/kuibu/internal/core";
import { formatSubmittedAt } from "../lib/report";

interface DeliveryBarProps {
  delivery: ProjectGroupDelivery | null | undefined;
  projectName: string;
  isFinal: boolean;
  busy?: boolean;
  /** 复制定稿（飞书格式）。 */
  onCopy: () => void;
  /** 标记 / 撤销已交。 */
  onMark: (delivered: boolean) => void;
}

/** 列表里的一句话去向：→ 我（SE）/ → 老王 · 已交。 */
export function deliveryShort(delivery: ProjectGroupDelivery | null | undefined): string {
  if (!delivery) return "";
  if (delivery.mine) return "→ 我（SE）";
  const who = delivery.se || "SE 未设置";
  if (delivery.deliveredAt && !delivery.stale) return `→ ${who} · 已交`;
  return `→ ${who}`;
}

export function DeliveryBar({ delivery, projectName, isFinal, busy, onCopy, onMark }: DeliveryBarProps) {
  if (!delivery) return null;
  if (delivery.mine) {
    return (
      <div className="delivery-bar">
        <span className="delivery-to">
          交给 <b>我（{projectName} SE）</b>
        </span>
        <span className={`delivery-chip${isFinal ? " is-done" : ""}`}>
          {isFinal ? "已进入项目汇总" : "定稿后自动进入项目汇总"}
        </span>
        <span className="delivery-hint">不用复制粘贴，「项目汇总」里本组这一行直接引用这份定稿。</span>
      </div>
    );
  }
  const delivered = Boolean(delivery.deliveredAt);
  return (
    <div className="delivery-bar">
      <span className="delivery-to">
        交给 <b>{delivery.se || "SE 未设置"}</b>
      </span>
      {delivered && !delivery.stale && (
        <span className="delivery-chip is-sent">已交 · {formatSubmittedAt(delivery.deliveredAt ?? "")}</span>
      )}
      {delivered && delivery.stale && <span className="delivery-chip is-stale">交出后定稿有变化，需要重新交</span>}
      <span className="delivery-spacer" />
      <button type="button" className="btn" disabled={!isFinal || busy} onClick={onCopy}>
        复制定稿
      </button>
      {delivered && !delivery.stale ? (
        <button type="button" className="btn btn-ghost" disabled={busy} onClick={() => onMark(false)}>
          撤销已交
        </button>
      ) : (
        <button type="button" className="btn" disabled={!isFinal || busy} onClick={() => onMark(true)}>
          标记已交
        </button>
      )}
      <span className="delivery-hint">
        {!delivery.se
          ? "在「项目设置」里填上这个项目的 SE。"
          : isFinal
            ? "复制后发给对方，发完点「标记已交」。"
            : "定稿后才能复制和标记，避免把草稿交出去。"}
      </span>
    </div>
  );
}
