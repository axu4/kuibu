import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { ReportService } from "../../bindings/github.com/axu4/kuibu/app";
import type { PersonData, ReportPreview } from "../../bindings/github.com/axu4/kuibu/app";
import { Icon } from "./Icon";
import { reportSummary } from "../lib/report";
import type { ReportLevel } from "../lib/report";
import { readableError } from "../lib/todo";

/** 一份预览 + 用户确认后的人员选择。 */
interface PreviewRow {
  preview: ReportPreview;
  personId: string;
}

interface ReportPasteProps {
  week: string;
  level: ReportLevel;
  /** 给出时按组长月报归档（level 应为 lead），week 传月份。 */
  month?: boolean;
  /** 预设人员（「重新粘贴」/未交者的「粘贴」），空字符串表示不预设。 */
  presetPersonId: string;
  onClose: () => void;
  onDone: () => void;
}

/**
 * 粘贴周报弹窗：大文本框 → 识别 → 预览（可改人员、可跳过）→ 确认写入。
 *
 * 预设了某人且整段没有头行时，直接归到此人；粘贴了多份时退回按头行识别。
 */
export function ReportPaste({
  week,
  level,
  month = false,
  presetPersonId,
  onClose,
  onDone,
}: ReportPasteProps) {
  // 组长月报在服务端用 lead-month 层级归档。
  const submitLevel: string = month ? "lead-month" : level;
  const [text, setText] = useState("");
  const [people, setPeople] = useState<PersonData[]>([]);
  const [rows, setRows] = useState<PreviewRow[] | null>(null);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const textareaRef = useRef<HTMLTextAreaElement | null>(null);

  useEffect(() => {
    void ReportService.People(true)
      .then((list) => setPeople(list ?? []))
      .catch(() => setPeople([]));
  }, []);

  useEffect(() => {
    textareaRef.current?.focus();
  }, []);

  const options = useMemo(() => {
    const seen = new Map<string, PersonData>();
    for (const p of people) {
      if (p.active && p.role === level) seen.set(p.id, p);
    }
    if (presetPersonId) {
      const preset = people.find((p) => p.id === presetPersonId);
      if (preset) seen.set(presetPersonId, preset);
    }
    return [...seen.values()].sort((a, b) => a.name.localeCompare(b.name, "zh-Hans-CN"));
  }, [people, level, presetPersonId]);

  const runPreview = useCallback(async () => {
    const raw = text.trim();
    if (!raw || busy) return;
    setBusy(true);
    setError(null);
    try {
      let previews: ReportPreview[];
      try {
        previews = (await ReportService.Preview(raw, submitLevel, week, presetPersonId)) ?? [];
      } catch {
        // 预设人员只允许单份文本；多份时退回按头行识别。
        previews = (await ReportService.Preview(raw, submitLevel, week, "")) ?? [];
      }
      setRows(
        previews.map((p) => ({
          preview: p,
          personId: p.personId || (p.hasHeader ? "" : presetPersonId),
        })),
      );
    } catch (err) {
      setError(`识别失败：${readableError(err)}`);
    } finally {
      setBusy(false);
    }
  }, [text, busy, submitLevel, week, presetPersonId]);

  const setPerson = useCallback((index: number, personId: string) => {
    setRows((prev) => (prev ? prev.map((r, i) => (i === index ? { ...r, personId } : r)) : null));
  }, []);

  const writable = useMemo(() => {
    if (!rows) return 0;
    return rows.filter((r) => !r.preview.duplicate && r.personId).length;
  }, [rows]);

  const submit = useCallback(async () => {
    if (!rows || busy) return;
    const items = rows
      .filter((r) => !r.preview.duplicate && r.personId)
      .map((r) => ({
        text: r.preview.text,
        person: r.personId,
        week: r.preview.week || week,
        level: submitLevel,
      }));
    if (items.length === 0) return;
    setBusy(true);
    setError(null);
    try {
      await ReportService.Submit(items);
      onDone();
    } catch (err) {
      setError(`写入失败：${readableError(err)}`);
    } finally {
      setBusy(false);
    }
  }, [rows, busy, submitLevel, week, onDone]);

  return (
    <div className="modal-mask" onClick={onClose}>
      <div className="modal report-paste" onClick={(e) => e.stopPropagation()}>
        <div className="report-paste-head">
          <h2>粘贴周报</h2>
          <button type="button" className="icon-btn" title="关闭" onClick={onClose}>
            <Icon name="close" />
          </button>
        </div>

        {!rows ? (
          <>
            <textarea
              ref={textareaRef}
              className="report-paste-input"
              value={text}
              placeholder={
                "粘贴组员发来的周报，可一次贴多份。\n\n每份以「【周报】姓名 日期」开头；没有头行时整段归到当前预设的人。"
              }
              spellCheck={false}
              onChange={(e) => setText(e.target.value)}
            />
            <div className="modal-buttons">
              <button type="button" className="btn" onClick={onClose}>
                取消
              </button>
              <button
                type="button"
                className="btn btn-primary"
                disabled={!text.trim() || busy}
                onClick={() => void runPreview()}
              >
                识别
              </button>
            </div>
          </>
        ) : (
          <>
            <div className="report-preview-list">
              {rows.map((row, i) => (
                <PreviewCard
                  key={i}
                  row={row}
                  index={i}
                  options={options}
                  onPerson={(id) => setPerson(i, id)}
                />
              ))}
            </div>
            {writable === 0 && (
              <div className="report-preview-hint">没有可写入的周报：请为每份指定人员，或跳过重复项。</div>
            )}
            <div className="modal-buttons">
              <button type="button" className="btn" onClick={() => setRows(null)}>
                返回编辑
              </button>
              <button
                type="button"
                className="btn btn-primary"
                disabled={writable === 0 || busy}
                onClick={() => void submit()}
              >
                确认写入 {writable} 份
              </button>
            </div>
          </>
        )}

        {error && <div className="settings-error">{error}</div>}
      </div>
    </div>
  );
}

interface PreviewCardProps {
  row: PreviewRow;
  index: number;
  options: PersonData[];
  onPerson: (id: string) => void;
}

/** 单份预览：序号、人员下拉、周、覆盖/迟交/重复标记与原文摘要。 */
function PreviewCard({ row, index, options, onPerson }: PreviewCardProps) {
  const p = row.preview;
  return (
    <div className={`report-preview-item${p.duplicate ? " is-duplicate" : ""}`}>
      <div className="report-preview-head">
        <span className="report-preview-index">{index + 1}</span>
        <select
          className="field report-preview-person"
          value={row.personId}
          disabled={p.duplicate}
          onChange={(e) => onPerson(e.target.value)}
        >
          <option value="">跳过</option>
          {options.map((o) => (
            <option key={o.id} value={o.id}>
              {o.name}
              {o.group ? ` · ${o.group}` : ""}
            </option>
          ))}
        </select>
        <span className="report-preview-week">{p.week || "未定周"}</span>
      </div>
      <div className="report-preview-flags">
        {p.overwrite && <span className="chip chip-due">覆盖</span>}
        {p.late && <span className="chip chip-late">迟交</span>}
        {p.duplicate && <span className="chip">重复</span>}
        {p.reason && <span className="report-preview-reason">{p.reason}</span>}
      </div>
      <div className="report-preview-summary">{reportSummary(p.text)}</div>
    </div>
  );
}
