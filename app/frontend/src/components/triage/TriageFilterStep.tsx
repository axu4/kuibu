// 第 1 步：筛选台。左侧队列按人分组，右侧一次只呈现一条卡片。

import type { TriageData, TriageItemData, TriageProjectData } from "../../../bindings/github.com/axu4/kuibu/app";
import { Icon } from "../Icon";
import { itemsInSection, sectionTitle, shortSection, sectionsOf } from "./shared";

export interface ExcerptState {
  loading: boolean;
  body: string;
  status: string;
  error: string | null;
}

interface TriageFilterStepProps {
  data: TriageData;
  filter: string;
  current: TriageItemData | null;
  busy: boolean;
  lastAction: string | null;
  excerptOpen: boolean;
  excerpt: ExcerptState | null;
  missing: string[];
  onFilter: (section: string) => void;
  onGoto: (itemId: string) => void;
  onDecide: (decision: "accepted" | "rejected" | "skipped", projectId?: string) => void;
  onToggleExcerpt: () => void;
  onUndo: () => void;
  onOpenSettings: () => void;
}

const MARK = {
  accepted: "●",
  rejected: "✕",
  skipped: "–",
} as const;

/** 一条决策后的状态文案。 */
function decidedNote(item: TriageItemData): { text: string; tone: string } | null {
  if (!item.decision) return null;
  if (item.decision === "accepted") {
    return { text: `已放入「${item.projectName || "已删除的项目"}」 · 按项目键可改，U 撤销`, tone: "is-accept" };
  }
  if (item.decision === "rejected") {
    return { text: "已移除 · 仍留在「已移除」里，随时可恢复", tone: "is-reject" };
  }
  return { text: "已待定 · 不会自动进任何项目的草稿，预览时可以回去处理", tone: "is-skip" };
}

export function TriageFilterStep({
  data,
  filter,
  current,
  busy,
  lastAction,
  excerptOpen,
  excerpt,
  missing,
  onFilter,
  onGoto,
  onDecide,
  onToggleExcerpt,
  onUndo,
  onOpenSettings,
}: TriageFilterStepProps) {
  const items = data.items ?? [];
  const projects = data.projects ?? [];
  const sections = sectionsOf(data);
  const sectionItems = itemsInSection(items, filter);
  const sectionDone = sectionItems.filter((i) => i.decision).length;
  const globalIndex = current ? items.findIndex((i) => i.id === current.id) + 1 : 0;

  // 队列按人分组，保持服务端给出的顺序（按姓名、节内顺序）。
  const groups: { person: string; name: string; items: TriageItemData[] }[] = [];
  for (const item of sectionItems) {
    const group = groups.find((g) => g.person === item.person);
    if (group) {
      group.items.push(item);
    } else {
      groups.push({ person: item.person, name: item.name, items: [item] });
    }
  }

  const perProject = projects
    .map((p) => `${p.name} ${items.filter((i) => i.decision === "accepted" && i.project === p.id).length}`)
    .join(" · ");
  const summary =
    projects.length > 1
      ? `${perProject} · 移除 ${items.filter((i) => i.decision === "rejected").length} · 待定 ${
          items.filter((i) => i.decision === "skipped").length
        }`
      : `采纳 ${items.filter((i) => i.decision === "accepted").length} · 移除 ${
          items.filter((i) => i.decision === "rejected").length
        } · 待定 ${items.filter((i) => i.decision === "skipped").length}`;

  return (
    <div className="triage-step triage-filter-step">
      <aside className="triage-queue">
        <div className="triage-queue-head">
          <div className="triage-queue-title">
            <b>{sectionTitle(data, filter)}</b>
            <span>
              已判 {sectionDone} / {sectionItems.length}
            </span>
          </div>
          <div className="triage-section-tabs">
            {sections.map((s) => (
              <button
                key={s.section}
                type="button"
                className={`triage-section-tab${filter === s.section ? " is-on" : ""}`}
                title={`${s.title} ${s.done}/${s.total}`}
                onClick={() => onFilter(s.section)}
              >
                {shortSection(s.title)} {s.done}/{s.total}
              </button>
            ))}
          </div>
        </div>
        <div className="triage-queue-body">
          {groups.length === 0 ? (
            <div className="triage-queue-empty">这一栏没有条目。</div>
          ) : (
            groups.map((group) => (
              <div key={group.person} className="triage-queue-group">
                <div className="triage-queue-group-head">
                  <b>{group.name}</b>
                  <span>
                    {group.items.filter((i) => i.decision).length} / {group.items.length}
                  </span>
                </div>
                {group.items.map((item) => (
                  <button
                    key={item.id}
                    type="button"
                    className={`triage-queue-item${item.decision ? ` is-${item.decision}` : ""}${
                      current?.id === item.id ? " is-current" : ""
                    }`}
                    onClick={() => onGoto(item.id)}
                  >
                    <span className={`triage-queue-mark is-${item.decision || "undecided"}`}>
                      {item.decision ? MARK[item.decision as keyof typeof MARK] : "○"}
                    </span>
                    <span className="triage-queue-text">
                      <span>{item.text}</span>
                      <span className="triage-queue-sub">
                        {item.sectionTitle || sectionTitle(data, item.section)}
                        {item.domain ? ` · ${item.domain}` : ""}
                        {item.decision === "accepted" && item.projectName ? ` · ${item.projectName}` : ""}
                      </span>
                    </span>
                  </button>
                ))}
              </div>
            ))
          )}
        </div>
        <div className="triage-queue-foot">
          <div>{summary}</div>
          {missing.length > 0 && (
            <div className="triage-queue-missing">未交 / 无归一稿：{missing.join("、")}</div>
          )}
        </div>
      </aside>

      <section className="triage-focus">
        {!current ? (
          <div className="triage-empty">
            <p>这一周没有可筛选的条目。</p>
            <button type="button" className="btn" onClick={onOpenSettings}>
              项目设置
            </button>
          </div>
        ) : (
          <div className="triage-focus-scroll">
            <article className="triage-card">
              <span className="triage-card-index">
                第 {globalIndex} / {items.length} 条
              </span>
              <div className="triage-card-meta">
                <span className="triage-avatar">{current.name.charAt(0)}</span>
                <span className="triage-who">{current.name}</span>
                <span className="triage-pill">{current.sectionTitle || sectionTitle(data, current.section)}</span>
                {current.domain && <span className="triage-pill is-domain">{current.domain}</span>}
                <span className="triage-src-note">组员原文 · 按条</span>
              </div>

              {(() => {
                const note = decidedNote(current);
                return note ? <div className={`triage-decided-note ${note.tone}`}>{note.text}</div> : null;
              })()}

              <div className="triage-card-text">{current.text}</div>

              <div className="triage-excerpt-toggle">
                <button type="button" className="link-btn" onClick={onToggleExcerpt}>
                  {excerptOpen ? "收起原文" : "看原文"} <kbd>E</kbd>
                </button>
              </div>

              {excerptOpen && (
                <div className="triage-excerpt">
                  <div className="triage-excerpt-head">
                    <span>
                      {current.name} 的
                      {excerpt?.status === "raw"
                        ? "周报原文 · 按条"
                        : `归一稿${excerpt?.status === "final" ? " · 已定稿" : " · 草稿"}`}
                      {" · 只读"}
                    </span>
                    <span>当前条目高亮</span>
                  </div>
                  {excerpt?.loading ? (
                    <div className="triage-excerpt-loading">正在读取原文…</div>
                  ) : excerpt?.error ? (
                    <div className="triage-excerpt-loading">{excerpt.error}</div>
                  ) : (
                    <div className="triage-excerpt-body">
                      {(excerpt?.body ?? "").split("\n").map((line, idx) => (
                        <div
                          key={idx}
                          className={`triage-excerpt-line${line.includes(current.text) ? " is-hit" : ""}`}
                        >
                          {line || "\u00a0"}
                        </div>
                      ))}
                      {!excerpt?.body && <div className="triage-excerpt-line">（归一稿正文为空）</div>}
                    </div>
                  )}
                </div>
              )}

              <div className="triage-card-actions">
                {projects.length > 1 && <span className="triage-action-label">归入</span>}
                <span className="triage-project-chips">
                  {projects.map((project: TriageProjectData, idx: number) => {
                    const chosen = current.decision === "accepted" && current.project === project.id;
                    const single = projects.length === 1;
                    return (
                      <button
                        key={project.id}
                        type="button"
                        className={`triage-seal-btn${chosen ? " is-chosen" : ""}${single ? " is-single" : ""}`}
                        disabled={busy}
                        onClick={() => onDecide("accepted", project.id)}
                      >
                        <kbd>{single ? "↵" : String(idx + 1)}</kbd>
                        {single ? `采纳 · ${project.name}` : project.name}
                      </button>
                    );
                  })}
                </span>
                <span className="triage-secondary-actions">
                  <button
                    type="button"
                    className="triage-ghost-btn is-danger"
                    disabled={busy}
                    onClick={() => onDecide("rejected")}
                  >
                    移除 <kbd>X</kbd>
                  </button>
                  <button
                    type="button"
                    className="triage-ghost-btn"
                    disabled={busy}
                    onClick={() => onDecide("skipped")}
                  >
                    待定 <kbd>空格</kbd>
                  </button>
                </span>
              </div>

              <p className="triage-action-hint">
                {projects.length === 1
                  ? `按 ↵ 采纳并放入「${projects[0].name}」；移除和待定随时可撤销。`
                  : "点项目名或用数字键 = 采纳并归入该项目；移除和待定随时可撤销。"}
              </p>
            </article>

            <div className="triage-focus-foot">
              <div className="triage-last-action">
                <span>{lastAction ? `上次：${lastAction}` : "一条一条过，决定会立刻记下"}</span>
                <button type="button" className="link-btn" onClick={onUndo} disabled={busy}>
                  撤销 <kbd>U</kbd>
                </button>
              </div>
              <div className="triage-hints">
                <span className="triage-hint">
                  <kbd>↑</kbd>
                  <kbd>↓</kbd> 翻看
                </span>
                <span className="triage-hint">
                  <kbd>E</kbd> 原文
                </span>
                <span className="triage-hint">
                  <kbd>U</kbd> 撤销
                </span>
                <span className="triage-hint">
                  <kbd>?</kbd> 键盘
                </span>
                <span className="triage-hint">
                  <Icon name="settings" size={12} /> 项目设置
                </span>
              </div>
            </div>
          </div>
        )}
      </section>
    </div>
  );
}
