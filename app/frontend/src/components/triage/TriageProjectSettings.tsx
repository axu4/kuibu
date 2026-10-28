// 项目设置：报送项目新增 / 改名 / 删除（最多 4 个，删除前确认）。

import { useCallback, useEffect, useState } from "react";
import { TriageService } from "../../../bindings/github.com/axu4/kuibu/app";
import type { TriageItemData, TriageProjectData } from "../../../bindings/github.com/axu4/kuibu/app";
import { readableError } from "../../lib/todo";
import { Icon } from "../Icon";

const MAX_PROJECTS = 4;

interface TriageProjectSettingsProps {
  items: TriageItemData[];
  onClose: () => void;
  /** 项目增删改之后通知流程重新读取筛选记录与草稿状态。 */
  onChanged: () => void;
  onToast: (text: string) => void;
}

export function TriageProjectSettings({
  items,
  onClose,
  onChanged,
  onToast,
}: TriageProjectSettingsProps) {
  const [projects, setProjects] = useState<TriageProjectData[] | null>(null);
  const [names, setNames] = useState<Record<string, string>>({});
  const [error, setError] = useState<string | null>(null);
  const [newName, setNewName] = useState("");
  const [busy, setBusy] = useState(false);
  const [pendingDelete, setPendingDelete] = useState<{
    id: string;
    name: string;
    count: number;
  } | null>(null);

  const applyList = useCallback((list: TriageProjectData[]) => {
    setProjects(list);
    setNames(Object.fromEntries(list.map((p) => [p.id, p.name])));
  }, []);

  const load = useCallback(async () => {
    try {
      const list = (await TriageService.Projects()) ?? [];
      applyList(list);
      setError(null);
    } catch (err) {
      setError(readableError(err));
    }
  }, [applyList]);

  useEffect(() => {
    void load();
  }, [load]);

  const candidatesOf = (projectId: string) =>
    items.filter((i) => i.decision === "accepted" && i.project === projectId).length;

  const commitRename = async (project: TriageProjectData) => {
    const value = (names[project.id] ?? "").trim();
    if (!value) {
      setNames((prev) => ({ ...prev, [project.id]: project.name }));
      onToast("项目名不能为空");
      return;
    }
    if (value === project.name) return;
    setBusy(true);
    try {
      const list = (await TriageService.RenameProject(project.id, value)) ?? [];
      applyList(list);
      onToast(`已改名为「${value}」`);
      onChanged();
    } catch (err) {
      setNames((prev) => ({ ...prev, [project.id]: project.name }));
      onToast(`改名失败：${readableError(err)}`);
    } finally {
      setBusy(false);
    }
  };

  const add = async () => {
    const name = newName.trim();
    if (!name) {
      onToast("先输入项目名");
      return;
    }
    setBusy(true);
    try {
      const list = (await TriageService.AddProject(name)) ?? [];
      applyList(list);
      setNewName("");
      onToast(`已添加「${name}」`);
      onChanged();
    } catch (err) {
      onToast(`添加失败：${readableError(err)}`);
    } finally {
      setBusy(false);
    }
  };

  const remove = async () => {
    if (!pendingDelete) return;
    const target = pendingDelete;
    setBusy(true);
    try {
      const list = (await TriageService.RemoveProject(target.id)) ?? [];
      applyList(list);
      setPendingDelete(null);
      onToast(
        target.count > 0
          ? `已删除「${target.name}」，${target.count} 条已分到它的条目退回待定`
          : `已删除「${target.name}」`,
      );
      onChanged();
    } catch (err) {
      onToast(`删除失败：${readableError(err)}`);
    } finally {
      setBusy(false);
    }
  };

  return (
    <div className="modal-mask is-center" onMouseDown={onClose}>
      <div
        className="modal triage-settings-modal"
        onMouseDown={(e) => e.stopPropagation()}
        role="dialog"
        aria-label="报送项目设置"
      >
        <div className="triage-settings-head">
          <div>
            <h2>报送项目</h2>
            <p>筛选时每条事项归入一个项目；第 3 步每个项目单独一份草稿、单独保存。</p>
          </div>
          <button type="button" className="icon-btn" title="关闭" onClick={onClose}>
            <Icon name="close" />
          </button>
        </div>

        <div className="triage-settings-body">
          {error && <div className="settings-error">{error}</div>}
          {!projects ? (
            !error && <div className="triage-settings-loading">正在读取项目…</div>
          ) : (
            <>
              {projects.map((project, idx) => (
                <div key={project.id} className="triage-proj-row">
                  <span className="triage-proj-idx">{idx + 1}</span>
                  <input
                    className="field triage-proj-input"
                    value={names[project.id] ?? project.name}
                    maxLength={20}
                    spellCheck={false}
                    disabled={busy}
                    onChange={(e) =>
                      setNames((prev) => ({ ...prev, [project.id]: e.target.value }))
                    }
                    onBlur={() => void commitRename(project)}
                    onKeyDown={(e) => {
                      if (e.key === "Enter") {
                        e.currentTarget.blur();
                      }
                    }}
                  />
                  <button
                    type="button"
                    className="icon-btn is-danger"
                    title="删除项目（已分到它的条目退回待定）"
                    disabled={busy}
                    onClick={() =>
                      setPendingDelete({
                        id: project.id,
                        name: project.name,
                        count: candidatesOf(project.id),
                      })
                    }
                  >
                    <Icon name="close" size={14} />
                  </button>
                </div>
              ))}

              {projects.length < MAX_PROJECTS ? (
                <div className="triage-proj-add">
                  <input
                    className="field"
                    placeholder="新项目名称"
                    value={newName}
                    maxLength={20}
                    spellCheck={false}
                    disabled={busy}
                    onChange={(e) => setNewName(e.target.value)}
                    onKeyDown={(e) => {
                      if (e.key === "Enter") void add();
                    }}
                  />
                  <button
                    type="button"
                    className="btn"
                    disabled={busy || !newName.trim()}
                    onClick={() => void add()}
                  >
                    <Icon name="plus" size={14} />
                    添加项目
                  </button>
                </div>
              ) : (
                <div className="triage-note-box">最多 4 个报送项目。</div>
              )}

              <div className="triage-note-box">
                <b>只配置 1 个项目时：</b>筛选界面不出现项目选择，按 ↵ 直接采纳；第 3 步只有一份草稿。
              </div>
              <div className="triage-note-box">
                <b>删除项目不会静默改归属：</b>已经分到该项目的条目会退回「待定」，需要重新过一遍。
              </div>
              <div className="triage-note-box">
                <b>编号只是快捷键：</b>项目名可随时改，快捷键按当前顺序重新编号。
              </div>
            </>
          )}
        </div>

        <div className="modal-buttons">
          <button type="button" className="btn" onClick={onClose}>
            关闭
          </button>
        </div>
      </div>

      {pendingDelete && (
        <div
          className="modal-mask is-center is-nested"
          onMouseDown={(e) => {
            e.stopPropagation();
            setPendingDelete(null);
          }}
        >
          <div className="modal" onMouseDown={(e) => e.stopPropagation()}>
            <h2>删除「{pendingDelete.name}」？</h2>
            <p>
              已分到该项目的 {pendingDelete.count} 条会退回「待定」，草稿文件保留但不再显示；
              不会静默改到别的项目。
            </p>
            <div className="modal-buttons">
              <button type="button" className="btn" onClick={() => setPendingDelete(null)}>
                取消
              </button>
              <button
                type="button"
                className="btn btn-danger"
                disabled={busy}
                onClick={() => void remove()}
              >
                删除项目
              </button>
            </div>
          </div>
        </div>
      )}
    </div>
  );
}
