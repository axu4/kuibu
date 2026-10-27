import { useCallback, useEffect, useRef, useState } from "react";
import { ReportService } from "../../bindings/github.com/axu4/kuibu/app";
import type { DraftData, NormalizePlan as RawPlan } from "../../bindings/github.com/axu4/kuibu/app";
import { Editor } from "./Editor";
import { Icon } from "./Icon";
import { SendPreview } from "./SendPreview";
import { readableError } from "../lib/todo";

/** 绑定里的切片可能是 null，这里统一成数组。 */
type NormalizePlan = Omit<RawPlan, "persons" | "skipped"> & {
  persons: string[];
  skipped: NonNullable<RawPlan["skipped"]>;
};

async function prepare(
  week: string,
  level: string,
  persons: string[],
  force: boolean,
): Promise<NormalizePlan> {
  const p = await ReportService.PrepareNormalize(week, level, persons, "", force);
  return { ...p, persons: p.persons ?? [], skipped: p.skipped ?? [] };
}

async function normalize(week: string, level: string, persons: string[], force: boolean) {
  return (await ReportService.Normalize(week, level, persons, "", force)) ?? [];
}

/** 归一稿编辑防抖。 */
const SAVE_DEBOUNCE_MS = 800;

interface ReportDraftProps {
  week: string;
  personId: string;
  /** 层级：member（组员归一稿）或 lead（组长归一稿）。 */
  level?: string;
  /** report:changed 时递增，外部改动后重新读取。 */
  refreshTick: number;
  onToast: (text: string) => void;
}

/**
 * 归一稿子 tab：AI 生成（先预览确认）→ 人工修改 → 定稿。
 * 定稿后只读，需先「取消定稿」才能修改。
 */
export function ReportDraft({
  week,
  personId,
  level = "member",
  refreshTick,
  onToast,
}: ReportDraftProps) {
  const [draft, setDraft] = useState<DraftData | null>(null);
  const [failed, setFailed] = useState(false);
  // 编辑器只在打开或外部改动正文时重建，自己保存不重建。
  const [revision, setRevision] = useState(0);
  const [plan, setPlan] = useState<{ plan: NormalizePlan; force: boolean } | null>(null);
  const [overwriteAsk, setOverwriteAsk] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  const [saving, setSaving] = useState<"saved" | "saving" | "dirty">("saved");

  const knownBodyRef = useRef("");
  const pendingRef = useRef<string | null>(null);
  const timerRef = useRef<ReturnType<typeof setTimeout> | null>(null);

  const load = useCallback(async () => {
    try {
      const d = await ReportService.GetDraft(week, personId, level);
      setFailed(false);
      setDraft(d);
      // 有未保存输入时不覆盖编辑器；正文与已知一致时不重建。
      if (pendingRef.current === null && d.body !== knownBodyRef.current) {
        knownBodyRef.current = d.body;
        setRevision((r) => r + 1);
      }
    } catch {
      setFailed(true);
    }
  }, [week, personId]);

  useEffect(() => {
    knownBodyRef.current = "";
    pendingRef.current = null;
    setDraft(null);
    void load();
  }, [load]);

  useEffect(() => {
    if (refreshTick > 0) void load();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [refreshTick]);

  const flush = useCallback(async () => {
    if (timerRef.current) {
      clearTimeout(timerRef.current);
      timerRef.current = null;
    }
    const body = pendingRef.current;
    if (body === null) return;
    setSaving("saving");
    try {
      const d = await ReportService.SaveDraft(week, personId, level, body);
      knownBodyRef.current = d.body;
      if (pendingRef.current === body) pendingRef.current = null;
      setDraft(d);
      setSaving(pendingRef.current === null ? "saved" : "dirty");
    } catch (err) {
      setSaving("dirty");
      onToast(`保存归一稿失败：${readableError(err)}`);
    }
  }, [week, personId, onToast]);

  // 切换人或卸载前把未保存的修改写掉。
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

  /** AI 生成：先拿预览，弹确认框；人工改过时先问是否覆盖。 */
  const generate = async (force: boolean) => {
    setBusy(true);
    setOverwriteAsk(null);
    try {
      await flush();
      const p = await prepare(week, level, [personId], force);
      if (p.persons.length === 0) {
        onToast("没有可归一的周报");
        return;
      }
      setPlan({ plan: p, force });
    } catch (err) {
      const msg = readableError(err);
      if (!force && msg.includes("--force")) {
        setOverwriteAsk(msg.replace(/；?确认要覆盖请加 --force/, ""));
      } else {
        onToast(`准备归一失败：${msg}`);
      }
    } finally {
      setBusy(false);
    }
  };

  const send = async () => {
    if (!plan) return;
    try {
      const results = await normalize(week, level, plan.plan.persons, plan.force);
      const r = results[0];
      if (r?.error) onToast(`生成失败：${r.error}`);
      else if (r?.warning) onToast(r.warning);
      setPlan(null);
      pendingRef.current = null;
      knownBodyRef.current = "\u0000";
      await load();
    } catch (err) {
      onToast(`生成失败：${readableError(err)}`);
    }
  };

  const setFinal = async (final: boolean) => {
    setBusy(true);
    try {
      await flush();
      const d = final
        ? await ReportService.FinalizeDraft(week, personId, level)
        : await ReportService.UnfinalizeDraft(week, personId, level);
      setDraft(d);
      if (!final) setRevision((r) => r + 1);
    } catch (err) {
      onToast(`${final ? "定稿" : "取消定稿"}失败：${readableError(err)}`);
    } finally {
      setBusy(false);
    }
  };

  if (failed) return <div className="report-empty">读取归一稿失败。</div>;
  if (!draft) return <div className="report-empty">正在读取归一稿…</div>;

  const final = draft.status === "final";
  return (
    <div className="report-draft">
      <div className="report-draft-bar">
        {draft.exists ? (
          <span className={`draft-badge is-${draft.status}`}>{final ? "已定稿" : "草稿"}</span>
        ) : (
          <span className="draft-badge">未生成</span>
        )}
        {draft.sourceChanged && <span className="draft-badge is-warn">原文已更新</span>}
        {draft.exists && !final && saving !== "saved" && (
          <span className="draft-saving">{saving === "saving" ? "保存中…" : "未保存"}</span>
        )}
        <span className="report-draft-spacer" />
        <button type="button" className="btn" disabled={busy} onClick={() => void generate(false)}>
          <Icon name="bolt" size={14} />
          {draft.exists ? "重新生成" : "AI 生成"}
        </button>
        {draft.exists &&
          (final ? (
            <button type="button" className="btn" disabled={busy} onClick={() => void setFinal(false)}>
              取消定稿
            </button>
          ) : (
            <button
              type="button"
              className="btn btn-primary"
              disabled={busy}
              onClick={() => void setFinal(true)}
            >
              <Icon name="check" size={14} />
              定稿
            </button>
          ))}
      </div>

      {overwriteAsk && (
        <div className="settings-hint report-draft-ask">
          <span>{overwriteAsk}。重新生成会覆盖，旧版保留在历史记录里。</span>
          <button type="button" className="link-btn" onClick={() => void generate(true)}>
            仍要重新生成
          </button>
        </div>
      )}

      {!draft.exists ? (
        <div className="report-empty">
          <p>还没有归一稿。AI 会把原文整理成「本周完成 / 下周计划 / 风险与求助」，发送前先给你预览。</p>
        </div>
      ) : final ? (
        <>
          <div className="settings-hint">已定稿，只读。需要修改请先「取消定稿」。</div>
          <pre className="report-body">{draft.body}</pre>
        </>
      ) : (
        <div className="report-draft-editor">
          <Editor
            key={`${week}-${personId}-${revision}`}
            noteId={`${week}/${personId}/${revision}`}
            initialBody={draft.body}
            onChange={onChange}
            placeholder="归一稿正文"
          />
        </div>
      )}

      {plan && (
        <SendPreview
          title={`发送给 AI 归一周报（${plan.plan.persons.length} 人）`}
          preview={plan.plan.preview}
          onCancel={() => setPlan(null)}
          onConfirm={send}
        />
      )}
    </div>
  );
}

interface BatchNormalizeProps {
  week: string;
  level?: string;
  onClose: () => void;
  onToast: (text: string) => void;
}

/** 批量归一本周已交：逐人汇总进一个预览，一次确认。已人工修改或定稿的人跳过。 */
export function BatchNormalize({ week, level = "member", onClose, onToast }: BatchNormalizeProps) {
  const [plan, setPlan] = useState<NormalizePlan | null>(null);

  useEffect(() => {
    let alive = true;
    void prepare(week, level, [], false)
      .then((p) => {
        if (!alive) return;
        if (p.persons.length === 0) {
          onToast(
            p.skipped.length > 0
              ? `${p.skipped.length} 人的归一稿已修改或定稿，没有需要生成的`
              : "本周还没有已交的周报",
          );
          onClose();
          return;
        }
        setPlan(p);
      })
      .catch((err) => {
        if (!alive) return;
        onToast(`准备归一失败：${readableError(err)}`);
        onClose();
      });
    return () => {
      alive = false;
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [week]);

  if (!plan) return null;
  const skippedNote =
    plan.skipped.length > 0
      ? `（跳过 ${plan.skipped.map((s) => s.name).join("、")}：已修改或已定稿）`
      : "";
  return (
    <SendPreview
      title={`批量归一 ${plan.persons.length} 人${skippedNote}`}
      preview={plan.preview}
      onCancel={onClose}
      onConfirm={async () => {
        try {
          const results = await normalize(week, level, plan.persons, false);
          const failed = results.filter((r) => r.error);
          const warned = results.filter((r) => r.warning);
          let msg = `已生成 ${results.length - failed.length} 份草稿`;
          if (failed.length > 0) msg += `，${failed.length} 人失败（${failed.map((r) => r.name).join("、")}）`;
          if (warned.length > 0) msg += `，${warned.length} 份格式需人工整理`;
          onToast(msg);
          onClose();
        } catch (err) {
          onToast(`批量归一失败：${readableError(err)}`);
        }
      }}
    />
  );
}
