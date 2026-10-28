// 第 2 步：项目分拣预览。每个项目一个池子，可 HTML5 拖动改归口；
// 点条目回到第 1 步定位，点「报告预览」看只读候选稿（不带姓名）。

import { useEffect, useMemo, useRef, useState } from "react";
import type { DragEvent } from "react";
import type {
  ProjectGroupList,
  TriageData,
  TriageItemData,
} from "../../../bindings/github.com/axu4/kuibu/app";
import { Icon } from "../Icon";
import {
  acceptedItems,
  buildProjectReport,
  candidateCount,
  markdownBlocks,
  sectionsOf,
} from "./shared";

interface TriagePreviewStepProps {
  data: TriageData;
  rows: ProjectGroupList[];
  busy: boolean;
  onMove: (itemId: string, decision: "accepted" | "rejected" | "skipped", projectId?: string) => void;
  onGoto: (itemId: string) => void;
  onStartReports: (projectId?: string) => void;
}

interface PoolItemProps {
  item: TriageItemData;
  extra?: string;
  onDragStart: (event: DragEvent, itemId: string) => void;
  onDragEnd: () => void;
  onClick: (itemId: string) => void;
}

function PoolItem({ item, extra = "", onDragStart, onDragEnd, onClick }: PoolItemProps) {
  return (
    <button
      type="button"
      className={`triage-pool-item${extra}`}
      draggable
      onDragStart={(event) => onDragStart(event, item.id)}
      onDragEnd={onDragEnd}
      onClick={() => onClick(item.id)}
      title="拖动换项目；点击回到第 1 步"
    >
      <span className="triage-drag-grip" aria-hidden>
        ⠿
      </span>
      <span className="triage-pool-source">{item.name}</span>
      <span className="triage-pool-text">{item.text}</span>
    </button>
  );
}

export function TriagePreviewStep({
  data,
  rows,
  busy,
  onMove,
  onGoto,
  onStartReports,
}: TriagePreviewStepProps) {
  const projects = data.projects ?? [];
  const items = data.items ?? [];
  const sections = sectionsOf(data);
  const [over, setOver] = useState<string | null>(null);
  const [previewProject, setPreviewProject] = useState<string | null>(null);
  const dragItem = useRef<string | null>(null);
  const dragEndedAt = useRef(0);

  // 预览弹层打开时 Esc 先关弹层，不要让全局键盘把流程退回第 1 步。
  useEffect(() => {
    if (!previewProject) return;
    const onKey = (event: KeyboardEvent) => {
      if (event.key === "Escape") {
        event.preventDefault();
        event.stopImmediatePropagation();
        setPreviewProject(null);
      }
    };
    window.addEventListener("keydown", onKey, true);
    return () => window.removeEventListener("keydown", onKey, true);
  }, [previewProject]);

  const accepted = items.filter((i) => i.decision === "accepted");
  const rejected = items.filter((i) => i.decision === "rejected");
  const skipped = items.filter((i) => i.decision === "skipped");
  const drafts = rows.filter((r) => r.exists).length;
  const stale = rows.filter((r) => r.sourceChanged).length;

  const preview = useMemo(() => {
    if (!previewProject) return null;
    const project = projects.find((p) => p.id === previewProject) ?? null;
    if (!project) return null;
    const list = acceptedItems(items, project.id);
    return {
      project,
      count: list.length,
      sourceCount: new Set(list.map((i) => i.person)).size,
      blocks: markdownBlocks(buildProjectReport(data, project.id)),
    };
  }, [data, items, previewProject, projects]);

  const startDrag = (event: DragEvent, itemId: string) => {
    dragItem.current = itemId;
    setOver(null);
    if (event.dataTransfer) {
      event.dataTransfer.effectAllowed = "move";
      event.dataTransfer.setData("text/plain", itemId);
    }
  };

  const endDrag = () => {
    dragItem.current = null;
    setOver(null);
    dragEndedAt.current = Date.now();
  };

  const clickPoolItem = (itemId: string) => {
    // 刚拖完的 click 不当作“回到第 1 步”。
    if (Date.now() - dragEndedAt.current < 300) return;
    onGoto(itemId);
  };

  const poolHandlers = (
    key: string,
    decision: "accepted" | "rejected" | "skipped",
    projectId?: string,
  ) => ({
    onDragOver: (event: DragEvent) => {
      if (!dragItem.current) return;
      event.preventDefault();
      if (event.dataTransfer) event.dataTransfer.dropEffect = "move";
      if (over !== key) setOver(key);
    },
    onDragLeave: (event: DragEvent) => {
      if (!event.currentTarget.contains(event.relatedTarget as Node | null)) {
        setOver((current) => (current === key ? null : current));
      }
    },
    onDrop: (event: DragEvent) => {
      event.preventDefault();
      const itemId = dragItem.current ?? event.dataTransfer?.getData("text/plain") ?? null;
      dragItem.current = null;
      setOver(null);
      dragEndedAt.current = Date.now();
      if (itemId) onMove(itemId, decision, projectId);
    },
  });

  const sectionBlock = (projectId: string) => {
    const list = acceptedItems(items, projectId);
    return sections.map((section) => {
      const inSection = list.filter((i) => i.section === section.section);
      if (inSection.length === 0) return null;
      return (
        <div key={section.section} className="triage-pool-section">
          <h4>
            {section.title}
            <span>{inSection.length} 条</span>
          </h4>
          {inSection.map((item) => (
            <PoolItem
              key={item.id}
              item={item}
              onDragStart={startDrag}
              onDragEnd={endDrag}
              onClick={clickPoolItem}
            />
          ))}
        </div>
      );
    });
  };

  return (
    <div className="triage-step triage-preview-step">
      <div className="triage-preview-inner">
        <div className="triage-eyebrow">
          第 2 步 · {data.week} {data.group ? `· ${data.group}` : ""}
        </div>
        <h1 className="triage-h1">项目分拣预览</h1>
        <p className="triage-lede">
          {items.length} 条已过完：{accepted.length} 条进入候选（按项目分开），{rejected.length} 条移除，
          {skipped.length} 条待定。这里可以<b>直接拖动条目</b>，在项目 / 待定 / 已移除之间调整；
          点条目则回到第 1 步改判。确认后进入第 3 步，按项目分别生成独立草稿。
        </p>

        <div className="triage-summary-stats">
          <div className="triage-sumcard is-accept">
            <b>{accepted.length}</b>
            <span>条候选 · 按项目分开</span>
          </div>
          <div className="triage-sumcard is-reject">
            <b>{rejected.length}</b>
            <span>条已移除 · 可恢复</span>
          </div>
          <div className="triage-sumcard is-skip">
            <b>{skipped.length}</b>
            <span>条待定 · 不带入</span>
          </div>
          <div className="triage-sumcard">
            <b>{drafts}</b>
            <span>份已有草稿</span>
          </div>
        </div>

        <div className="triage-summary-grid">
          {projects.map((project) => {
            const count = candidateCount(items, project.id);
            const key = `p:${project.id}`;
            return (
              <div
                key={project.id}
                className={`triage-pool${over === key ? " is-drop-target" : ""}`}
                {...poolHandlers(key, "accepted", project.id)}
              >
                <div className="triage-pool-head">
                  <h3>{project.name}</h3>
                  <span className="triage-pool-count">{count} 条</span>
                </div>
                <div className="triage-pool-meta">
                  这些条目会进入该项目的独立草稿 ·{" "}
                  <button type="button" className="link-btn" onClick={() => setPreviewProject(project.id)}>
                    报告预览
                  </button>
                </div>
                {count === 0 ? (
                  <div className="triage-pool-empty">还没有归入这个项目的条目。</div>
                ) : (
                  sectionBlock(project.id)
                )}
              </div>
            );
          })}

          <div
            className={`triage-pool is-removed${over === "rejected" ? " is-drop-target" : ""}`}
            {...poolHandlers("rejected", "rejected")}
          >
            <div className="triage-pool-head">
              <h3>已移除</h3>
              <span className="triage-pool-count">{rejected.length} 条</span>
            </div>
            <div className="triage-pool-meta">拖到这里 = 移除；点条目回第 1 步</div>
            {rejected.length === 0 ? (
              <div className="triage-pool-empty">没有移除的条目。</div>
            ) : (
              rejected.map((item) => (
                <PoolItem
                  key={item.id}
                  item={item}
                  extra=" is-rejected"
                  onDragStart={startDrag}
                  onDragEnd={endDrag}
                  onClick={clickPoolItem}
                />
              ))
            )}
          </div>

          <div
            className={`triage-pool is-pending${over === "skipped" ? " is-drop-target" : ""}`}
            {...poolHandlers("skipped", "skipped")}
          >
            <div className="triage-pool-head">
              <h3>待定</h3>
              <span className="triage-pool-count">{skipped.length} 条</span>
            </div>
            <div className="triage-pool-meta">拖到这里 = 待定；不会自动进任何项目</div>
            {skipped.length === 0 ? (
              <div className="triage-pool-empty">没有待定的条目。</div>
            ) : (
              skipped.map((item) => (
                <PoolItem
                  key={item.id}
                  item={item}
                  onDragStart={startDrag}
                  onDragEnd={endDrag}
                  onClick={clickPoolItem}
                />
              ))
            )}
          </div>
        </div>

        {stale > 0 && (
          <div className="triage-note">
            筛选结果改过了：已有 {stale} 份草稿基于旧候选，进入第 3 步后可逐份重新生成。
          </div>
        )}
        {skipped.length > 0 && (
          <div className="triage-note">{skipped.length} 条待定没有进入任何项目的候选。</div>
        )}
        <p className="triage-tip">
          拖动只在项目之间调整归口，不改草稿内容；已有草稿的项目会标「候选已更新」。下一步会为每个项目生成一份独立草稿；
          草稿已在的项目不会覆盖。
        </p>
      </div>

      {preview && (
        <div className="modal-mask is-center" onMouseDown={() => setPreviewProject(null)}>
          <div className="modal triage-preview-modal" onMouseDown={(e) => e.stopPropagation()}>
            <div className="triage-preview-head">
              <div>
                <div className="triage-eyebrow">候选稿预览 · 未定稿</div>
                <h2>
                  {preview.project.name} · {data.week}
                </h2>
                <p className="triage-preview-sub">
                  {preview.count} 条候选 · 来源 {preview.sourceCount} 位组员 · 只按「栏目 → 条目」排版，不调 AI、不改措辞；
                  正文不含姓名。
                </p>
              </div>
              <button type="button" className="icon-btn" title="关闭" onClick={() => setPreviewProject(null)}>
                <Icon name="close" />
              </button>
            </div>
            <div className="triage-md-preview">
              {preview.count === 0 ? (
                <p>本周没有归入该项目的条目。</p>
              ) : (
                preview.blocks.map((block, idx) =>
                  block.kind === "h2" ? (
                    <h2 key={idx}>{block.text}</h2>
                  ) : block.kind === "li" ? (
                    <div key={idx} className="triage-md-li">
                      {block.text}
                    </div>
                  ) : (
                    <p key={idx}>{block.text}</p>
                  ),
                )
              )}
            </div>
            <div className="modal-buttons">
              <button type="button" className="btn" onClick={() => setPreviewProject(null)}>
                关闭
              </button>
              <button
                type="button"
                className="btn btn-primary"
                disabled={busy || preview.count === 0}
                onClick={() => {
                  const id = preview.project.id;
                  setPreviewProject(null);
                  onStartReports(id);
                }}
              >
                去整理这份
              </button>
            </div>
          </div>
        </div>
      )}
    </div>
  );
}
