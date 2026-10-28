import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { ReportService } from "../../bindings/github.com/axu4/kuibu/app";
import type { PersonData, ReportPreview, ReportSectionData } from "../../bindings/github.com/axu4/kuibu/app";
import { Icon } from "./Icon";
import { reportSummary } from "../lib/report";
import type { ReportLevel } from "../lib/report";
import { readableError } from "../lib/todo";

/** 一份预览 + 用户确认后的人员选择。 */
type EditableSection = { section: string; title: string; items: string[] };

interface PreviewRow {
  preview: ReportPreview;
  personId: string;
  sections: EditableSection[];
}

function normalizeSections(list: ReportSectionData[] | null | undefined): EditableSection[] {
  return (list ?? []).map((sec) => ({ section: sec.section, title: sec.title, items: sec.items ?? [] }));
}

interface ReportPasteProps {
  week: string;
  level: ReportLevel;
  /** 给出时按组长月报归档（level 应为 lead），week 传月份。 */
  month?: boolean;
  /**
   * 锁定的提交人（在该人行上点「粘贴」/ 选中行后 ⌘V），空字符串表示不锁定。
   * 锁定后不再出现人员下拉：从哪一行进来就归谁。
   */
  presetPersonId: string;
  /** 打开时预填的文本（选中行后 ⌘V 直接带上剪贴板内容）。 */
  initialText?: string;
  onClose: () => void;
  /** 写入成功后的回调，参数是写入的份数。 */
  onDone: (count: number) => void;
}

/**
 * 粘贴周报弹窗：大文本框 → 识别 → 预览 → 确认写入。
 *
 * 锁定某人时（从某人那一行进来）不再问「归给谁」：预览里只显示这个人、
 * 头行由服务端剥掉；解锁的多份粘贴才出现可改的人员下拉。
 */
export function ReportPaste({
  week,
  level,
  month = false,
  presetPersonId,
  initialText = "",
  onClose,
  onDone,
}: ReportPasteProps) {
  // 组长月报在服务端用 lead-month 层级归档。
  const submitLevel: string = month ? "lead-month" : level;
  const [text, setText] = useState(initialText);
  const [people, setPeople] = useState<PersonData[]>([]);
  const [rows, setRows] = useState<PreviewRow[] | null>(null);
  const [editing, setEditing] = useState<number | null>(null);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const textareaRef = useRef<HTMLTextAreaElement | null>(null);

  // 锁定时只认这个人，解锁时才需要全量名单做下拉。
  const locked = presetPersonId !== "";

  useEffect(() => {
    void ReportService.People(true)
      .then((list) => setPeople(list ?? []))
      .catch(() => setPeople([]));
  }, []);

  useEffect(() => {
    textareaRef.current?.focus();
  }, []);

  // 窗口里直接 ⌘V：剪贴板里已经是周报时自动填进来，不用再点一次「粘贴」。
  useEffect(() => {
    const onPaste = (e: ClipboardEvent) => {
      if (rows) return;
      const target = e.target as HTMLElement | null;
      if (target && target.tagName === "TEXTAREA") return;
      const data = e.clipboardData?.getData("text") ?? "";
      if (!data.trim()) return;
      e.preventDefault();
      setText((prev) => (prev.trim() ? prev : data));
    };
    window.addEventListener("paste", onPaste);
    return () => window.removeEventListener("paste", onPaste);
  }, [rows]);

  // 锁定人员的姓名（名单还没加载完时先空着）。
  const lockedName = useMemo(
    () => people.find((p) => p.id === presetPersonId)?.name ?? "",
    [people, presetPersonId],
  );

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
      const next = await Promise.all(
        previews.map(async (p) => ({
          preview: p,
          personId: p.personId || (p.hasHeader ? "" : presetPersonId),
          sections: normalizeSections(await ReportService.ParseReportSections(p.text)),
        })),
      );
      if (next.length === 0) {
        setError("没识别出周报内容");
        return;
      }
      // 录入时先展示识别结果（本周/下周/其他），允许调整分类后再写入。
      setRows(next);
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

  const submit = useCallback(
    async (list: PreviewRow[]) => {
      if (busy) return;
      const items = list
        .filter((r) => !r.preview.duplicate && r.personId)
        .map((r) => ({
          text: r.preview.text,
          person: r.personId,
          week: r.preview.week || week,
          level: submitLevel,
          sections: r.sections.map((sec) => ({ section: sec.section, items: sec.items })),
        }));
      if (items.length === 0) return;
      setBusy(true);
      setError(null);
      try {
        await ReportService.Submit(items);
        onDone(list.length);
      } catch (err) {
        setError(`写入失败：${readableError(err)}`);
      } finally {
        setBusy(false);
      }
    },
    [busy, submitLevel, week, onDone],
  );

  /**
   * 识别之后停下来问「确认写入」的唯一理由：有需要人判断的分支。
   *
   * 锁定某人、只交一份、不覆盖、不迟交、无重复、周也对得上时，确认框里
   * 没有任何新信息——直接写入更省事。其余情况（尤其是多份批量粘贴）
   * 仍然要给一眼预览。
   */
  // 停下来确认时，把那件需要人判断的事直接写出来。
  const confirmReason = useMemo(() => {
    if (!rows || rows.length === 0) return "";
    const flags: string[] = [];
    if (rows.length > 1) flags.push(`一次写入 ${rows.length} 份`);
    for (const r of rows) {
      const p = r.preview;
      const who = options.find((o) => o.id === r.personId)?.name || p.personName || "这个人";
      if (p.duplicate) flags.push("有重复，只保留最后一份");
      if (p.overwrite) flags.push(`会覆盖 ${who} 已有的周报`);
      if (p.late) flags.push("这次写入算迟交");
      if (!p.personId) flags.push("有人还没指定姓名");
      if (p.reason && !p.duplicate) flags.push(p.reason);
    }
    return [...new Set(flags)].join("；");
  }, [rows, options]);

  const confirmAction = rows?.[0]?.preview.overwrite ? "覆盖写入" : "确认写入";

  return (
    <div className="modal-mask" onClick={onClose}>
      <div className="modal report-paste" onClick={(e) => e.stopPropagation()}>
        <div className="report-paste-head">
          <h2>{locked ? `粘贴${lockedName ? ` ${lockedName} 的` : ""}周报` : "粘贴周报"}</h2>
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
                locked
                  ? "⌘V 直接粘进来。开头那行「【周报】姓名 日期」会自动去掉，不会存进正文。"
                  : "粘贴组员发来的周报，可一次贴多份。\n\n每份以「【周报】姓名 日期」开头，按头行自动认人。"
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
                  locked={locked}
                  options={options}
                  onPerson={(id) => setPerson(i, id)}
                  onEditSections={() => setEditing(i)}
                />
              ))}
            </div>
            {writable === 0 && (
              <div className="report-preview-hint">没有可写入的周报：请为每份指定人员，或跳过重复项。</div>
            )}
            {writable > 0 && confirmReason && (
              <div className="report-preview-hint">请确认：{confirmReason}</div>
            )}
            <div className="modal-buttons">
              <button type="button" className="btn" onClick={() => setRows(null)}>
                返回编辑
              </button>
              <button
                type="button"
                className="btn btn-primary"
                disabled={writable === 0 || busy}
                onClick={() => rows && void submit(rows)}
              >
                {locked ? confirmAction : `确认写入 ${writable} 份`}
              </button>
            </div>
          </>
        )}

        {error && <div className="settings-error">{error}</div>}
      </div>

      {editing !== null && rows?.[editing] && (
        <SectionEditor
          sections={rows[editing].sections}
          onClose={() => setEditing(null)}
          onSave={(sections) => {
            setRows((prev) =>
              prev ? prev.map((row, i) => (i === editing ? { ...row, sections } : row)) : prev,
            );
            setEditing(null);
          }}
        />
      )}
    </div>
  );
}

interface PreviewCardProps {
  row: PreviewRow;
  index: number;
  /** 锁定人员：只显示姓名，不给下拉，避免同一个问题问两遍。 */
  locked: boolean;
  options: PersonData[];
  onPerson: (id: string) => void;
  onEditSections: () => void;
}

/** 单份预览：序号、人员（锁定时只读）、周、覆盖/迟交/重复标记与原文摘要。 */
function PreviewCard({ row, index, locked, options, onPerson, onEditSections }: PreviewCardProps) {
  const p = row.preview;
  const personName = options.find((o) => o.id === row.personId)?.name ?? p.personName;
  return (
    <div className={`report-preview-item${p.duplicate ? " is-duplicate" : ""}`}>
      <div className="report-preview-head">
        <span className="report-preview-index">{index + 1}</span>
        {locked ? (
          <span className="report-preview-person is-locked">
            <Icon name="person" size={13} />
            {personName || "未指定"}
          </span>
        ) : (
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
        )}
        <span className="report-preview-week">{p.week || "未定周"}</span>
      </div>
      <div className="report-preview-flags">
        {p.overwrite && <span className="chip chip-due">覆盖</span>}
        {p.late && <span className="chip chip-late">迟交</span>}
        {p.duplicate && <span className="chip">重复</span>}
        {locked && p.hasHeader && <span className="chip">已去掉开头标题行</span>}
        {p.reason && <span className="report-preview-reason">{p.reason}</span>}
      </div>
      <div className="report-preview-summary">{reportSummary(p.text)}</div>
      <div className="report-preview-sections">
        {row.sections.map((sec) => (
          <span key={sec.section} className="report-preview-section-chip">
            {sec.title} <b>{sec.items.length}</b>
          </span>
        ))}
        <button type="button" className="link-btn" onClick={onEditSections}>
          调整分类
        </button>
      </div>
    </div>
  );
}


interface SectionEditorProps {
  sections: EditableSection[];
  onClose: () => void;
  onSave: (sections: EditableSection[]) => void;
}

/** 录入时的分节校正：移动条目、改文字、删条目，不调用 AI。 */
function SectionEditor({ sections, onClose, onSave }: SectionEditorProps) {
  const [draft, setDraft] = useState<EditableSection[]>(() =>
    sections.map((sec) => ({ ...sec, items: [...sec.items] })),
  );
  const options = sections.map((sec) => ({ section: sec.section, title: sec.title }));
  const updateItem = (secIndex: number, itemIndex: number, text: string) => {
    setDraft((prev) => prev.map((sec, i) => (i === secIndex
      ? { ...sec, items: sec.items.map((item, j) => (j === itemIndex ? text : item)) }
      : sec)));
  };
  const moveItem = (secIndex: number, itemIndex: number, target: string) => {
    setDraft((prev) => {
      const text = prev[secIndex]?.items[itemIndex] ?? "";
      return prev.map((sec, i) => {
        if (i === secIndex) return { ...sec, items: sec.items.filter((_, j) => j !== itemIndex) };
        if (sec.section === target) return { ...sec, items: [...sec.items, text] };
        return sec;
      });
    });
  };
  const removeItem = (secIndex: number, itemIndex: number) => {
    setDraft((prev) => prev.map((sec, i) =>
      i === secIndex ? { ...sec, items: sec.items.filter((_, j) => j !== itemIndex) } : sec));
  };
  const addItem = (secIndex: number) => {
    setDraft((prev) => prev.map((sec, i) =>
      i === secIndex ? { ...sec, items: [...sec.items, ""] } : sec));
  };
  return (
    <div
      className="modal-mask is-center is-nested"
      onMouseDown={(e) => {
        if (e.target === e.currentTarget) onClose();
      }}
      onClick={(e) => e.stopPropagation()}
    >
      <div className="modal report-section-editor" onMouseDown={(e) => e.stopPropagation()} onClick={(e) => e.stopPropagation()}>
        <div className="report-paste-head">
          <h2>调整分类</h2>
          <button type="button" className="icon-btn" onClick={onClose} title="关闭"><Icon name="close" /></button>
        </div>
        <p className="settings-desc">识别不对可以直接改：移动条目、修改文字或删除。保存后后续分拣按这里来。</p>
        <div className="report-section-columns">
          {draft.map((sec, secIndex) => (
            <section key={sec.section} className="report-section-column">
              <h3>{sec.title} <span>{sec.items.length}</span></h3>
              {sec.items.map((item, itemIndex) => (
                <div key={itemIndex} className="report-section-item">
                  <textarea
                    className="field"
                    value={item}
                    rows={2}
                    onChange={(e) => updateItem(secIndex, itemIndex, e.target.value)}
                  />
                  <div className="report-section-item-actions">
                    <select
                      className="field"
                      value={sec.section}
                      onChange={(e) => moveItem(secIndex, itemIndex, e.target.value)}
                    >
                      {options.map((opt) => (
                        <option key={opt.section} value={opt.section}>{opt.title}</option>
                      ))}
                    </select>
                    <button type="button" className="btn is-compact" onClick={() => removeItem(secIndex, itemIndex)}>删除</button>
                  </div>
                </div>
              ))}
              <button type="button" className="btn is-compact" onClick={() => addItem(secIndex)}>＋ 添加一条</button>
            </section>
          ))}
        </div>
        <div className="modal-buttons">
          <button type="button" className="btn" onClick={onClose}>取消</button>
          <button
            type="button"
            className="btn btn-primary"
            onClick={() => onSave(draft.map((sec) => ({ ...sec, items: sec.items.map((item) => item.trim()).filter(Boolean) })))}
          >
            保存分类
          </button>
        </div>
      </div>
    </div>
  );
}
