import { useCallback, useEffect, useMemo, useState } from "react";
import { ReportService } from "../../bindings/github.com/axu4/kuibu/app";
import type { PersonData, PersonInput } from "../../bindings/github.com/axu4/kuibu/app";
import { Icon } from "./Icon";
import { MoreMenu } from "./Menu";
import { joinListInput, parseListInput } from "../lib/report";
import { readableError } from "../lib/todo";

interface PeopleManagerProps {
  onClose: () => void;
}

type FormState =
  | { kind: "none" }
  | { kind: "add" }
  | { kind: "edit"; person: PersonData }
  | { kind: "move"; person: PersonData };

function sortPeople(list: PersonData[]): PersonData[] {
  return [...list].sort((a, b) => a.name.localeCompare(b.name, "zh-Hans-CN"));
}

/** 名单管理弹窗：在职/离职分组，支持新增、编辑、转组与设为离职/恢复。 */
export function PeopleManager({ onClose }: PeopleManagerProps) {
  const [people, setPeople] = useState<PersonData[]>([]);
  const [groups, setGroups] = useState<string[]>([]);
  const [lines, setLines] = useState<string[]>([]);
  const [showInactive, setShowInactive] = useState(false);
  const [form, setForm] = useState<FormState>({ kind: "none" });
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const load = useCallback(async () => {
    setBusy(true);
    try {
      const [list, g, l] = await Promise.all([
        ReportService.People(true),
        ReportService.Groups(),
        ReportService.ProductLines(),
      ]);
      setPeople(list ?? []);
      setGroups(g ?? []);
      setLines(l ?? []);
      setError(null);
    } catch (err) {
      setError(`读取名单失败：${readableError(err)}`);
    } finally {
      setBusy(false);
    }
  }, []);

  useEffect(() => {
    void load();
  }, [load]);

  const active = useMemo(() => sortPeople(people.filter((p) => p.active)), [people]);
  const inactive = useMemo(() => sortPeople(people.filter((p) => !p.active)), [people]);

  const submitPerson = useCallback(
    async (values: PersonInput) => {
      setBusy(true);
      setError(null);
      const editing = form.kind === "edit";
      try {
        if (editing) {
          await ReportService.UpdatePerson(form.person.id, values);
        } else {
          await ReportService.CreatePerson(values);
        }
        setForm({ kind: "none" });
        await load();
      } catch (err) {
        setError(`${editing ? "修改" : "新增"}失败：${readableError(err)}`);
      } finally {
        setBusy(false);
      }
    },
    [form, load],
  );

  const submitMove = useCallback(
    async (group: string, productLines: string[], from: string) => {
      if (form.kind !== "move") return;
      setBusy(true);
      setError(null);
      try {
        await ReportService.MovePerson(form.person.id, group, productLines, from);
        setForm({ kind: "none" });
        await load();
      } catch (err) {
        setError(`转组失败：${readableError(err)}`);
      } finally {
        setBusy(false);
      }
    },
    [form, load],
  );

  const toggleActive = useCallback(
    async (person: PersonData) => {
      setBusy(true);
      setError(null);
      try {
        await ReportService.SetPersonActive(person.id, !person.active);
        await load();
      } catch (err) {
        setError(`更新状态失败：${readableError(err)}`);
      } finally {
        setBusy(false);
      }
    },
    [load],
  );

  return (
    <div className="modal-mask" onClick={onClose}>
      <div className="modal people-modal" onClick={(e) => e.stopPropagation()}>
        <datalist id="report-group-list">
          {groups.map((g) => (
            <option key={g} value={g} />
          ))}
        </datalist>
        <datalist id="report-line-list">
          {lines.map((l) => (
            <option key={l} value={l} />
          ))}
        </datalist>

        <div className="people-head">
          <h2>管理名单</h2>
          <button type="button" className="icon-btn" title="关闭" onClick={onClose}>
            <Icon name="close" />
          </button>
        </div>

        {form.kind === "move" ? (
          <MoveForm
            person={form.person}
            busy={busy}
            onCancel={() => setForm({ kind: "none" })}
            onSubmit={submitMove}
          />
        ) : form.kind !== "none" ? (
          <PersonForm
            person={form.kind === "edit" ? form.person : null}
            busy={busy}
            onCancel={() => setForm({ kind: "none" })}
            onSubmit={submitPerson}
          />
        ) : (
          <>
            <div className="people-toolbar">
              <span className="people-count">{active.length} 名在职</span>
              <button type="button" className="btn btn-primary" onClick={() => setForm({ kind: "add" })}>
                <Icon name="plus" size={14} />
                新增组员
              </button>
            </div>

            <div className="people-list">
              {active.map((p) => (
                <PersonRow
                  key={p.id}
                  person={p}
                  onEdit={() => setForm({ kind: "edit", person: p })}
                  onMove={() => setForm({ kind: "move", person: p })}
                  onToggleActive={() => void toggleActive(p)}
                />
              ))}
              {active.length === 0 && <div className="people-empty">还没有在职组员。</div>}
            </div>

            {inactive.length > 0 && (
              <div className="people-inactive">
                <button
                  type="button"
                  className="people-inactive-toggle"
                  onClick={() => setShowInactive((v) => !v)}
                >
                  <Icon name="chevron" size={12} className={showInactive ? "is-open" : ""} />
                  离职 {inactive.length}
                </button>
                {showInactive && (
                  <div className="people-list">
                    {inactive.map((p) => (
                      <PersonRow
                        key={p.id}
                        person={p}
                        onEdit={() => setForm({ kind: "edit", person: p })}
                        onMove={() => setForm({ kind: "move", person: p })}
                        onToggleActive={() => void toggleActive(p)}
                      />
                    ))}
                  </div>
                )}
              </div>
            )}
          </>
        )}

        {error && <div className="settings-error">{error}</div>}
      </div>
    </div>
  );
}

interface PersonRowProps {
  person: PersonData;
  onEdit: () => void;
  onMove: () => void;
  onToggleActive: () => void;
}

function PersonRow({ person, onEdit, onMove, onToggleActive }: PersonRowProps) {
  const role = person.role === "lead" ? "组长" : "组员";
  const lines = person.productLines ?? [];
  return (
    <div className="people-row">
      <div className="people-row-main">
        <span className="people-name">{person.name}</span>
        <span className="people-meta">
          {person.group && <span>{person.group}</span>}
          {lines.length > 0 && <span>{lines.join(" / ")}</span>}
          <span className="people-role">{role}</span>
        </span>
      </div>
      <MoreMenu
        items={[
          { label: "编辑", icon: "edit", onClick: onEdit },
          { label: "转组", icon: "flag", onClick: onMove },
          {
            label: person.active ? "设为离职" : "恢复在职",
            icon: person.active ? "close" : "check",
            onClick: onToggleActive,
          },
        ]}
      />
    </div>
  );
}

interface PersonFormProps {
  person: PersonData | null;
  busy: boolean;
  onCancel: () => void;
  onSubmit: (values: PersonInput) => void;
}

function PersonForm({ person, busy, onCancel, onSubmit }: PersonFormProps) {
  const [name, setName] = useState(person?.name ?? "");
  const [aliases, setAliases] = useState(joinListInput(person?.aliases));
  const [group, setGroup] = useState(person?.group ?? "");
  const [products, setProducts] = useState(joinListInput(person?.productLines));
  const [role, setRole] = useState<"member" | "lead">(person?.role === "lead" ? "lead" : "member");

  const submit = () => {
    const trimmed = name.trim();
    if (!trimmed) return;
    onSubmit({
      name: trimmed,
      aliases: parseListInput(aliases),
      group: group.trim(),
      productLines: parseListInput(products),
      role,
    });
  };

  return (
    <div className="person-form">
      <div className="person-form-grid">
        <label className="field-label">
          姓名
          <input
            className="field"
            value={name}
            placeholder="必填"
            autoFocus
            onChange={(e) => setName(e.target.value)}
          />
        </label>
        <label className="field-label">
          别名
          <input
            className="field"
            value={aliases}
            placeholder="逗号分隔，用于识别"
            onChange={(e) => setAliases(e.target.value)}
          />
        </label>
        <label className="field-label">
          组
          <input
            className="field"
            list="report-group-list"
            value={group}
            onChange={(e) => setGroup(e.target.value)}
          />
        </label>
        <label className="field-label">
          产品线
          <input
            className="field"
            list="report-line-list"
            value={products}
            placeholder="逗号分隔，可多个"
            onChange={(e) => setProducts(e.target.value)}
          />
        </label>
      </div>

      <div className="person-form-role">
        <span className="field-label">角色</span>
        <div className="segmented">
          <button
            type="button"
            className={`segmented-btn${role === "member" ? " is-active" : ""}`}
            onClick={() => setRole("member")}
          >
            组员
          </button>
          <button
            type="button"
            className={`segmented-btn${role === "lead" ? " is-active" : ""}`}
            onClick={() => setRole("lead")}
          >
            组长
          </button>
        </div>
      </div>

      <div className="modal-buttons">
        <button type="button" className="btn" onClick={onCancel}>
          取消
        </button>
        <button
          type="button"
          className="btn btn-primary"
          disabled={busy || !name.trim()}
          onClick={submit}
        >
          {person ? "保存" : "添加"}
        </button>
      </div>
    </div>
  );
}

interface MoveFormProps {
  person: PersonData;
  busy: boolean;
  onCancel: () => void;
  onSubmit: (group: string, productLines: string[], from: string) => void;
}

function MoveForm({ person, busy, onCancel, onSubmit }: MoveFormProps) {
  const [group, setGroup] = useState(person.group ?? "");
  const [products, setProducts] = useState(joinListInput(person.productLines));
  const today = new Date();
  const todayStr = `${today.getFullYear()}-${pad(today.getMonth() + 1)}-${pad(today.getDate())}`;
  const [from, setFrom] = useState(todayStr);

  return (
    <div className="person-form">
      <div className="person-form-title">把「{person.name}」转到</div>
      <div className="person-form-grid">
        <label className="field-label">
          新组
          <input
            className="field"
            list="report-group-list"
            value={group}
            autoFocus
            onChange={(e) => setGroup(e.target.value)}
          />
        </label>
        <label className="field-label">
          产品线
          <input
            className="field"
            list="report-line-list"
            value={products}
            placeholder="逗号分隔，可多个"
            onChange={(e) => setProducts(e.target.value)}
          />
        </label>
        <label className="field-label">
          生效日期
          <input
            className="field"
            type="date"
            value={from}
            onChange={(e) => setFrom(e.target.value)}
          />
        </label>
      </div>

      <div className="modal-buttons">
        <button type="button" className="btn" onClick={onCancel}>
          取消
        </button>
        <button
          type="button"
          className="btn btn-primary"
          disabled={busy || !group.trim()}
          onClick={() => onSubmit(group.trim(), parseListInput(products), from)}
        >
          转组
        </button>
      </div>
    </div>
  );
}

function pad(n: number): string {
  return n < 10 ? `0${n}` : String(n);
}
