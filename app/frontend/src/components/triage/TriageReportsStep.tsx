// 第 3 步：分别整理各项目组报。左侧项目列表 + 右侧 Markdown 编辑器，
// 每份草稿独立保存、定稿、导出；切换项目前先 flush 当前项目的未保存内容。

import {
  forwardRef,
  useCallback,
  useEffect,
  useImperativeHandle,
  useMemo,
  useRef,
  useState,
} from "react";
import { ReportService } from "../../../bindings/github.com/axu4/kuibu/app";
import type {
  GroupData,
  ProjectGroupList,
  TriageItemData,
  TriageProjectData,
} from "../../../bindings/github.com/axu4/kuibu/app";
import { copyRich } from "../../lib/clipboard";
import { readableError } from "../../lib/todo";
import { Editor } from "../LazyEditor";
import { DeliveryBar, deliveryShort } from "../DeliveryBar";
import { Icon } from "../Icon";
import { formatClock, normalizeBody } from "./shared";

const SAVE_DEBOUNCE_MS = 800;

interface DraftState {
  data: GroupData | null;
  body: string;
  savedBody: string;
  loading: boolean;
  error: string | null;
  saving: "saved" | "saving" | "dirty";
}

function emptyDraft(): DraftState {
  return { data: null, body: "", savedBody: "", loading: false, error: null, saving: "saved" };
}

export interface TriageReportsHandle {
  /** 保存所有项目的未保存修改；任一失败返回 false。 */
  flush: () => Promise<boolean>;
}

interface TriageReportsStepProps {
  week: string;
  group: string;
  projects: TriageProjectData[];
  rows: ProjectGroupList[];
  items: TriageItemData[];
  initialProject: string | null;
  onToast: (text: string) => void;
  onDirtyChange: (count: number) => void;
}

export const TriageReportsStep = forwardRef<TriageReportsHandle, TriageReportsStepProps>(
  function TriageReportsStep(
    { week, group, projects, rows, items, initialProject, onToast, onDirtyChange },
    ref,
  ) {
    const [active, setActive] = useState<string | null>(() => {
      if (initialProject && projects.some((p) => p.id === initialProject)) return initialProject;
      return projects[0]?.id ?? null;
    });
    const [drafts, setDrafts] = useState<Record<string, DraftState>>({});
    const draftsRef = useRef<Record<string, DraftState>>({});
    const timers = useRef<Map<string, ReturnType<typeof setTimeout>>>(new Map());
    const regenArmed = useRef<string | null>(null);

    const patchDraft = useCallback((projectId: string, patch: (state: DraftState) => DraftState) => {
      setDrafts((prev) => {
        const next = { ...prev, [projectId]: patch(prev[projectId] ?? emptyDraft()) };
        draftsRef.current = next;
        return next;
      });
    }, []);

    const clearTimer = useCallback((projectId: string) => {
      const timer = timers.current.get(projectId);
      if (timer) {
        clearTimeout(timer);
        timers.current.delete(projectId);
      }
    }, []);

    // 项目被删掉或改名后，保证 active 仍然有效。
    useEffect(() => {
      const ids = projects.map((p) => p.id);
      if (ids.length === 0) {
        if (active !== null) setActive(null);
        return;
      }
      if (!active || !ids.includes(active)) {
        setActive(initialProject && ids.includes(initialProject) ? initialProject : ids[0]);
      }
    }, [projects, active, initialProject]);

    // 第一次切换到某个项目时读它的草稿；已加载过的保留内存里的未保存正文。
    useEffect(() => {
      if (!active) return;
      const existing = draftsRef.current[active];
      if (existing && (existing.loading || existing.data)) return;
      let alive = true;
      patchDraft(active, (state) => ({ ...state, loading: true, error: null }));
      void ReportService.ProjectGroup(week, group, active)
        .then((data) => {
          if (!alive) return;
          patchDraft(active, () => ({
            data,
            body: data.body,
            savedBody: data.body,
            loading: false,
            error: null,
            saving: "saved",
          }));
        })
        .catch((err) => {
          if (!alive) return;
          patchDraft(active, (state) => ({ ...state, loading: false, error: readableError(err) }));
        });
      return () => {
        alive = false;
      };
    }, [active, week, group, patchDraft]);

    // 卸载时清掉防抖计时器；父层在离开第 3 步前会先 flush。
    useEffect(
      () => () => {
        for (const timer of timers.current.values()) clearTimeout(timer);
        timers.current.clear();
      },
      [],
    );

    const flushProject = useCallback(
      async (projectId: string): Promise<boolean> => {
        clearTimer(projectId);
        const state = draftsRef.current[projectId];
        if (!state || !state.data || state.data.status === "final") return true;
        if (normalizeBody(state.body) === state.savedBody) return true;
        patchDraft(projectId, (current) => ({ ...current, saving: "saving" }));
        try {
          const data = await ReportService.SaveProjectGroup(week, group, projectId, state.body);
          patchDraft(projectId, (current) => ({
            ...current,
            data,
            savedBody: data.body,
            saving: normalizeBody(current.body) === data.body ? "saved" : "dirty",
          }));
          return true;
        } catch (err) {
          patchDraft(projectId, (current) => ({ ...current, saving: "dirty" }));
          onToast(`保存失败：${readableError(err)}`);
          return false;
        }
      },
      [clearTimer, week, group, onToast, patchDraft],
    );

    const flushAll = useCallback(async (): Promise<boolean> => {
      const pending = Object.keys(draftsRef.current).filter((projectId) => {
        const state = draftsRef.current[projectId];
        return (
          state?.data &&
          state.data.status !== "final" &&
          normalizeBody(state.body) !== state.savedBody
        );
      });
      let ok = true;
      for (const projectId of pending) {
        if (!(await flushProject(projectId))) ok = false;
      }
      return ok;
    }, [flushProject]);

    useImperativeHandle(ref, () => ({ flush: flushAll }), [flushAll]);

    const onChange = useCallback(
      (projectId: string, body: string) => {
        patchDraft(projectId, (state) => {
          if (!state.data || state.data.status === "final") return state;
          return {
            ...state,
            body,
            saving: normalizeBody(body) === state.savedBody ? "saved" : "dirty",
          };
        });
        clearTimer(projectId);
        const timer = setTimeout(() => {
          timers.current.delete(projectId);
          void flushProject(projectId);
        }, SAVE_DEBOUNCE_MS);
        timers.current.set(projectId, timer);
      },
      [clearTimer, flushProject, patchDraft],
    );

    const dirty = useMemo(
      () =>
        Object.values(drafts).filter(
          (state) =>
            state.data &&
            state.data.status !== "final" &&
            normalizeBody(state.body) !== state.savedBody,
        ).length,
      [drafts],
    );

    useEffect(() => {
      onDirtyChange(dirty);
    }, [dirty, onDirtyChange]);

    const rowMap = useMemo(() => {
      const map = new Map<string, ProjectGroupList>();
      for (const row of rows) map.set(row.project, row);
      return map;
    }, [rows]);

    const statusOf = (projectId: string): { text: string; cls: string } => {
      const row = rowMap.get(projectId);
      const state = drafts[projectId];
      const data = state?.data;
      // 还没切到这个项目时先按左侧列表的状态展示，避免显示成「未生成」。
      if (!data) {
        if (!row?.exists) return { text: "未生成", cls: "is-none" };
        if (row.status === "final") return { text: "已定稿", cls: "is-final" };
        if (row.sourceChanged) return { text: "候选已更新", cls: "is-stale" };
        return { text: "草稿", cls: "is-saved" };
      }
      if (
        state &&
        data.status !== "final" &&
        normalizeBody(state.body) !== state.savedBody
      ) {
        return { text: "未保存", cls: "is-dirty" };
      }
      if (data.status === "final") return { text: "已定稿", cls: "is-final" };
      if (data.sourceChanged || row?.sourceChanged) {
        return { text: "候选已更新", cls: "is-stale" };
      }
      if (!data.exists) return { text: "未生成", cls: "is-none" };
      const clock = formatClock(row?.editedAt);
      return { text: `已保存${clock ? ` · ${clock}` : ""}`, cls: "is-saved" };
    };

    const activeProject = projects.find((p) => p.id === active) ?? null;
    const activeState = active ? drafts[active] : undefined;
    const activeData = activeState?.data ?? null;
    const activeStatus = statusOf(active ?? "");
    const activeItems = active
      ? items.filter((i) => i.decision === "accepted" && i.project === active)
      : [];
    const sourceCount = new Set(activeItems.map((i) => i.person)).size;
    const isFinal = activeData?.status === "final";
    const candidates = (projectId: string) =>
      items.filter((i) => i.decision === "accepted" && i.project === projectId).length;

    const saveActive = async () => {
      if (!active) return;
      if (await flushProject(active)) onToast("已保存当前项目草稿");
    };

    const switchProject = async (projectId: string) => {
      if (projectId === active) return;
      if (active && !(await flushProject(active))) return;
      setActive(projectId);
    };

    const toggleFinal = async () => {
      if (!active || !activeData) {
        onToast("还没有这个项目的草稿");
        return;
      }
      if (!(await flushProject(active))) return;
      const current = draftsRef.current[active]?.data;
      if (!current) return;
      try {
        const next =
          current.status === "final"
            ? await ReportService.UnfinalizeProjectGroup(week, group, active)
            : await ReportService.FinalizeProjectGroup(week, group, active);
        patchDraft(active, () => ({
          data: next,
          body: next.body,
          savedBody: next.body,
          loading: false,
          error: null,
          saving: "saved",
        }));
        onToast(current.status === "final" ? "已取消定稿" : "已定稿，取消定稿后才能再改");
      } catch (err) {
        onToast(`操作失败：${readableError(err)}`);
      }
    };

    const regenerate = async () => {
      if (!active) return;
      const state = draftsRef.current[active];
      if (state?.data?.status === "final") {
        onToast("已定稿，先取消定稿再重新生成");
        return;
      }
      const dirty = Boolean(
        state?.data &&
          normalizeBody(state.body) !== state.savedBody &&
          state.data.status !== "final",
      );
      const needsConfirm = Boolean(state?.data?.humanEdited) || dirty;
      if (needsConfirm && regenArmed.current !== active) {
        regenArmed.current = active;
        window.setTimeout(() => {
          if (regenArmed.current === active) regenArmed.current = null;
        }, 3000);
        onToast("再点一次：用当前候选覆盖这份草稿，人工修改会丢");
        return;
      }
      regenArmed.current = null;
      try {
        const data = await ReportService.AssembleProjectGroup(
          week,
          group,
          active,
          Boolean(state?.data?.humanEdited),
          false,
        );
        patchDraft(active, () => ({
          data,
          body: data.body,
          savedBody: data.body,
          loading: false,
          error: null,
          saving: "saved",
        }));
        onToast(state?.data?.exists ? "已按当前候选重新生成" : "已生成草稿");
      } catch (err) {
        onToast(`重新生成失败：${readableError(err)}`);
      }
    };

    const exportMd = async () => {
      if (!active) return;
      if (!(await flushProject(active))) return;
      try {
        const path = await ReportService.ExportProjectGroup(week, group, active);
        if (path) onToast(`已导出到 ${path}`);
      } catch (err) {
        onToast(`导出失败：${readableError(err)}`);
      }
    };

    const copyFeishu = async () => {
      if (!active) return;
      if (!(await flushProject(active))) return;
      try {
        const content = await ReportService.ExportProjectGroupClipboard(week, group, active);
        const mode = await copyRich(content.html, content.text);
        onToast(
          mode === "html"
            ? "已复制，粘贴到飞书即可（保留标题、列表格式）"
            : "当前环境不支持富文本剪贴板，已复制为纯文本",
        );
      } catch (err) {
        onToast(`复制失败：${readableError(err)}`);
      }
    };

    const [marking, setMarking] = useState(false);
    const markDelivered = async (delivered: boolean) => {
      if (!active) return;
      setMarking(true);
      try {
        const data = await ReportService.MarkProjectDelivered(week, group, active, delivered);
        patchDraft(active, (state) => ({ ...state, data }));
        onToast(delivered ? `已标记交给 ${data.delivery?.se || "SE"}` : "已撤销已交标记");
      } catch (err) {
        onToast(`操作失败：${readableError(err)}`);
      } finally {
        setMarking(false);
      }
    };

    const stale = Boolean(activeData?.sourceChanged || (active && rowMap.get(active)?.sourceChanged));

    if (projects.length === 0) {
      return (
        <div className="triage-empty">
          <p>还没有报送项目，先在「项目设置」里添加。</p>
        </div>
      );
    }

    return (
      <div className="triage-step triage-reports-step">
        <aside className="triage-report-list">
          <div className="triage-rl-head">
            <b>{projects.length} 个项目</b>
            <span>每份独立保存</span>
          </div>
          <div className="triage-rl-body">
            {projects.map((project) => {
              const state = statusOf(project.id);
              return (
                <button
                  key={project.id}
                  type="button"
                  className={`triage-rl-item${project.id === active ? " is-active" : ""}`}
                  onClick={() => void switchProject(project.id)}
                >
                  <span className="triage-rl-name">
                    {project.name}
                    <span className="triage-rl-count">{candidates(project.id)} 条</span>
                  </span>
                  <span className={`triage-rl-status ${state.cls}`}>{state.text}</span>
                  <span className="triage-rl-to">
                    {deliveryShort(drafts[project.id]?.data?.delivery ?? rowMap.get(project.id)?.delivery)}
                  </span>
                </button>
              );
            })}
          </div>
          <div className="triage-rl-foot">草稿是 Markdown，各自一份；定稿后只读。</div>
        </aside>

        <section className="triage-report-editor">
          {!activeProject || !active ? (
            <div className="triage-empty">
              <p>先在项目设置里添加报送项目。</p>
            </div>
          ) : (
            <>
              <div className="triage-re-head">
                <div className="triage-re-head-main">
                  <div className="triage-eyebrow">第 3 步 · 各项目组报</div>
                  <h2>
                    {activeProject.name} · {week}
                  </h2>
                  <div className="triage-re-meta">
                    {activeItems.length} 条候选
                    {sourceCount > 0 ? ` · 来源 ${sourceCount} 位组员` : ""} ·{" "}
                    <span className={`triage-rl-status ${activeStatus.cls}`}>{activeStatus.text}</span>
                  </div>
                </div>
                <div className="triage-re-actions">
                  <button
                    type="button"
                    className="btn btn-primary"
                    disabled={!activeData || isFinal || !activeState || activeState.saving === "saved"}
                    onClick={() => void saveActive()}
                  >
                    保存 <kbd>⌘S</kbd>
                  </button>
                  <button
                    type="button"
                    className="btn"
                    disabled={!activeData?.exists}
                    onClick={() => void toggleFinal()}
                  >
                    {isFinal ? "取消定稿" : "定稿"}
                  </button>
                  <button type="button" className="btn btn-ghost" onClick={() => void exportMd()}>
                    导出
                  </button>
                  <button type="button" className="btn btn-ghost" onClick={() => void copyFeishu()}>
                    复制飞书
                  </button>
                </div>
              </div>

              {activeData?.exists && (
                <DeliveryBar
                  delivery={activeData.delivery ?? rowMap.get(active)?.delivery}
                  projectName={activeProject.name}
                  isFinal={isFinal}
                  busy={marking}
                  onCopy={() => void copyFeishu()}
                  onMark={(d) => void markDelivered(d)}
                />
              )}

              {stale && (
                <div className="triage-re-banner">
                  <span>
                    {isFinal
                      ? "筛选结果有更新；这份已定稿，如需重做先取消定稿。"
                      : "筛选结果有更新，这份草稿基于旧候选。"}
                  </span>
                  {!isFinal && (
                    <button type="button" className="link-btn" onClick={() => void regenerate()}>
                      按当前候选重新生成
                    </button>
                  )}
                </div>
              )}

              {activeState?.loading ? (
                <div className="triage-empty">正在读取草稿…</div>
              ) : activeState?.error ? (
                <div className="triage-empty">{activeState.error}</div>
              ) : !activeData || !activeData.exists ? (
                <div className="triage-re-empty">
                  <span>还没有为这个项目生成草稿。</span>
                  <button type="button" className="btn btn-primary" onClick={() => void regenerate()}>
                    <Icon name="bolt" size={14} />
                    生成草稿
                  </button>
                </div>
              ) : isFinal ? (
                <>
                  <div className="settings-hint">已定稿，只读。需要修改请先「取消定稿」。</div>
                  <pre className="report-body triage-readonly-body">{activeData.body}</pre>
                </>
              ) : (
                <div className="triage-editor">
                  <Editor
                    key={`triage-report-${week}-${active}-${activeData.generatedAt}`}
                    noteId={`triage/group/${week}/${active}`}
                    initialBody={activeData.body}
                    onChange={(body) => onChange(active, body)}
                    placeholder="项目组报正文"
                  />
                </div>
              )}

              <div className="triage-re-foot">
                <span>每个项目单独一份草稿，独立保存，互不影响。</span>
                <span>
                  {isFinal
                    ? "已定稿，取消定稿后才能改。"
                    : activeState && normalizeBody(activeState.body) !== activeState.savedBody
                      ? "当前有未保存修改；按 ⌘S 保存。"
                      : "编辑后按 ⌘S 或点保存。"}
                </span>
              </div>
            </>
          )}
        </section>
      </div>
    );
  },
);
