import { useCallback, useEffect, useState } from "react";
import { ReportService } from "../../bindings/github.com/axu4/kuibu/app";
import type { GroupInfoData, PersonData } from "../../bindings/github.com/axu4/kuibu/app";
import { readableError } from "../lib/todo";

interface EditorState {
  id: string;
  name: string;
  leadId: string;
  members: string[];
}

interface GroupManagerProps {
  /** 嵌入设置页时去掉外层页面留白。 */
  embedded?: boolean;
  onManagePeople?: () => void;
}

/** 组管理：组是唯一组织单位；没有额外的汇总组。 */
export function GroupManager({ embedded = false, onManagePeople }: GroupManagerProps) {
  const [groups, setGroups] = useState<GroupInfoData[]>([]);
  const [people, setPeople] = useState<PersonData[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [editor, setEditor] = useState<EditorState | null>(null);
  const [busy, setBusy] = useState(false);
  const [pendingDelete, setPendingDelete] = useState<GroupInfoData | null>(null);

  const load = useCallback(async () => {
    setLoading(true);
    try {
      const [gs, ps] = await Promise.all([ReportService.GroupList(), ReportService.People(false)]);
      setGroups(gs ?? []);
      setPeople((ps ?? []).filter((p) => p.active));
      setError(null);
    } catch (err) {
      setError(readableError(err));
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    void load();
  }, [load]);

  const openNew = () =>
    setEditor({ id: "", name: "", leadId: people[0]?.id ?? "", members: [] });
  const openEdit = (g: GroupInfoData) =>
    setEditor({ id: g.id, name: g.name, leadId: g.leadId || people[0]?.id || "", members: (g.members ?? []).map((m) => m.id) });

  const save = async () => {
    if (!editor) return;
    const name = editor.name.trim();
    if (!name) {
      setError("组名不能为空");
      return;
    }
    setBusy(true);
    try {
      if (editor.id) {
        setGroups((await ReportService.UpdateGroup(editor.id, name, editor.leadId, editor.members)) ?? []);
      } else {
        const created = (await ReportService.AddGroup(name, editor.leadId)) ?? [];
        const g = created.find((x) => x.name === name);
        if (g) {
          setGroups((await ReportService.UpdateGroup(g.id, name, editor.leadId, editor.members)) ?? created);
        } else {
          setGroups(created);
        }
      }
      setEditor(null);
      setError(null);
    } catch (err) {
      setError(readableError(err));
    } finally {
      setBusy(false);
    }
  };

  const remove = async () => {
    if (!pendingDelete) return;
    setBusy(true);
    try {
      setGroups((await ReportService.DeleteGroup(pendingDelete.id)) ?? []);
      setPendingDelete(null);
      setError(null);
    } catch (err) {
      setError(readableError(err));
    } finally {
      setBusy(false);
    }
  };

  if (loading) return <div className="gm-page"><div className="lib-empty">正在读取组…</div></div>;

  return (
    <div className={`gm-page${embedded ? " is-embedded" : ""}`}>
      <div className="lib-inner">
        <div className="lib-head">
          <div>
            <div className="lib-eyebrow">Groups · CRUD</div>
            <h1>组管理</h1>
            <p>组是唯一的组织单位：名字、组长、成员都可以改。组报定稿后自动进入周报库。</p>
          </div>
          <div className="lib-head-actions">
            {onManagePeople && (
              <button type="button" className="btn" onClick={onManagePeople}>
                管理名单
              </button>
            )}
            <button type="button" className="btn btn-primary" onClick={openNew}>＋ 新建组</button>
          </div>
        </div>

        {error && <div className="settings-error">{error}</div>}

        <div className="gm-grid">
          {groups.map((g) => (
            <article key={g.id} className="gm-card">
              <div className="gm-card-head">
                <span className="gm-mark">{g.name.slice(0, 1)}</span>
                <div><h3>{g.name}</h3><p>组长 · {g.leadName || "未指定"}</p></div>
              </div>
              <div className="gm-meta">
                <div><span>成员</span><b>{(g.members ?? []).length} 人</b></div>
                <div><span>本周定稿</span><b>—</b></div>
              </div>
              <div className="gm-members">
                {(g.members ?? []).slice(0, 5).map((m) => <i key={m.id} title={m.name}>{m.name.slice(0, 1)}</i>)}
                {(g.members ?? []).length > 5 && <i className="more">+{(g.members ?? []).length - 5}</i>}
                {(g.members ?? []).length === 0 && <span className="muted">还没有成员</span>}
              </div>
              <div className="gm-actions">
                <button type="button" className="btn is-compact" onClick={() => openEdit(g)}>编辑</button>
                <button type="button" className="btn btn-danger is-compact" onClick={() => setPendingDelete(g)}>删除</button>
              </div>
            </article>
          ))}
        </div>

        <div className="gm-note">
          <b>定稿即归档</b>
          <span>组报定稿后直接进入周报库，不需要再交给任何角色或组。删除组不会删除已归档的周报。</span>
        </div>
      </div>

      {editor && (
        <div className="modal-mask is-center" onMouseDown={() => setEditor(null)}>
          <div className="modal gm-editor" onMouseDown={(e) => e.stopPropagation()}>
            <div className="lib-preview-head">
              <div>
                <div className="lib-eyebrow">{editor.id ? "Edit group" : "New group"}</div>
                <h2>{editor.id ? "编辑组" : "新建组"}</h2>
                <p>组是唯一组织单位，组报定稿后自动进入周报库。</p>
              </div>
              <button type="button" className="icon-btn" onClick={() => setEditor(null)} title="关闭">✕</button>
            </div>
            <div className="gm-form">
              <label><span>组名</span><input className="field" value={editor.name} onChange={(e) => setEditor({ ...editor, name: e.target.value })} placeholder="例如：框架组" /></label>
              <label><span>组长</span>
                <select className="field" value={editor.leadId} onChange={(e) => setEditor({ ...editor, leadId: e.target.value })}>
                  <option value="">未指定</option>
                  {people.map((p) => <option key={p.id} value={p.id}>{p.name}</option>)}
                </select>
              </label>
            </div>
            <div className="gm-picks">
              <span>成员（可多选）</span>
              <div className="gm-pick-grid">
                {people.map((p) => (
                  <label key={p.id} className="gm-pick">
                    <input
                      type="checkbox"
                      checked={editor.members.includes(p.id)}
                      onChange={(e) =>
                        setEditor({
                          ...editor,
                          members: e.target.checked
                            ? [...editor.members, p.id]
                            : editor.members.filter((id) => id !== p.id),
                        })
                      }
                    />
                    {p.name}
                  </label>
                ))}
              </div>
            </div>
            <div className="modal-buttons">
              <button type="button" className="btn" onClick={() => setEditor(null)}>取消</button>
              <button type="button" className="btn btn-primary" disabled={busy} onClick={() => void save()}>保存</button>
            </div>
          </div>
        </div>
      )}

      {pendingDelete && (
        <div className="modal-mask is-center" onMouseDown={() => setPendingDelete(null)}>
          <div className="modal" onMouseDown={(e) => e.stopPropagation()}>
            <div className="lib-preview-head">
              <div><div className="lib-eyebrow">Delete group</div><h2>删除「{pendingDelete.name}」？</h2><p>只有没有在职成员的组才能删除；已归档的周报不会删除。</p></div>
              <button type="button" className="icon-btn" onClick={() => setPendingDelete(null)}>✕</button>
            </div>
            <div className="modal-buttons">
              <button type="button" className="btn" onClick={() => setPendingDelete(null)}>取消</button>
              <button type="button" className="btn btn-danger" disabled={busy} onClick={() => void remove()}>删除组</button>
            </div>
          </div>
        </div>
      )}
    </div>
  );
}
