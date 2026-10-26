import { useEffect, useRef, useState } from "react";
import type { TreeItem } from "../../bindings/github.com/axu4/kuibu/app";
import { Icon } from "./Icon";
import { Menu } from "./Menu";

interface TreeProps {
  items: TreeItem[];
  selectedId: string | null;
  expanded: Set<string>;
  onToggle: (id: string) => void;
  onSelect: (id: string) => void;
  onAddChild: (parent: string) => void;
  /** 空树时的「新建笔记」。 */
  onCreateRoot: () => void;
  onRename: (item: TreeItem) => void;
  onDelete: (item: TreeItem) => void;
  /** 正在重命名的笔记 ID。 */
  renamingId: string | null;
  onRenameSubmit: (id: string, title: string) => void;
  onRenameCancel: () => void;
}

interface MenuState {
  x: number;
  y: number;
  item: TreeItem;
}

/**
 * 左侧大纲树。展开/折叠状态由 App 维护并持久化到 localStorage。
 */
export function Tree(props: TreeProps) {
  const {
    items,
    selectedId,
    expanded,
    onToggle,
    onSelect,
    onAddChild,
    onCreateRoot,
    onRename,
    onDelete,
    renamingId,
    onRenameSubmit,
    onRenameCancel,
  } = props;

  const [menu, setMenu] = useState<MenuState | null>(null);

  if (items.length === 0) {
    return (
      <div className="tree">
        <div className="tree-empty">
          <p>这里还空着。</p>
          <button type="button" className="btn btn-ghost" onClick={onCreateRoot}>
            <Icon name="plus" size={14} />
            新建笔记
          </button>
        </div>
      </div>
    );
  }

  return (
    <div className="tree">
      {items.map((item) => (
        <TreeRow
          key={item.id}
          item={item}
          depth={0}
          selectedId={selectedId}
          expanded={expanded}
          onToggle={onToggle}
          onSelect={onSelect}
          onAddChild={onAddChild}
          onRename={onRename}
          onDelete={onDelete}
          onContextMenu={(e, target) => {
            e.preventDefault();
            setMenu({ x: e.clientX, y: e.clientY, item: target });
          }}
          renamingId={renamingId}
          onRenameSubmit={onRenameSubmit}
          onRenameCancel={onRenameCancel}
        />
      ))}

      {menu && (
        <Menu
          x={menu.x}
          y={menu.y}
          onClose={() => setMenu(null)}
          items={[
            { label: "重命名", icon: "edit", onClick: () => onRename(menu.item) },
            { label: "新建子笔记", icon: "plus", onClick: () => onAddChild(menu.item.id) },
            { label: "删除…", icon: "trash", danger: true, onClick: () => onDelete(menu.item) },
          ]}
        />
      )}
    </div>
  );
}

interface TreeRowProps
  extends Omit<TreeProps, "items" | "renamingId" | "onRenameSubmit" | "onRenameCancel" | "onCreateRoot"> {
  item: TreeItem;
  depth: number;
  onContextMenu: (e: React.MouseEvent, item: TreeItem) => void;
  renamingId: string | null;
  onRenameSubmit: (id: string, title: string) => void;
  onRenameCancel: () => void;
}

function TreeRow(props: TreeRowProps) {
  const {
    item,
    depth,
    selectedId,
    expanded,
    onToggle,
    onSelect,
    onAddChild,
    onRename,
    onDelete,
    onContextMenu,
    renamingId,
    onRenameSubmit,
    onRenameCancel,
  } = props;

  const children = item.children ?? [];
  const hasChildren = children.length > 0;
  const isOpen = expanded.has(item.id);
  const isSelected = selectedId === item.id;
  const isRenaming = renamingId === item.id;

  return (
    <>
      <div
        className={`tree-row${isSelected ? " is-selected" : ""}`}
        style={{ paddingLeft: 4 + depth * 16 }}
        onClick={() => onSelect(item.id)}
        onDoubleClick={() => onRename(item)}
        onContextMenu={(e) => onContextMenu(e, item)}
      >
        <button
          type="button"
          className={`tree-caret${hasChildren ? "" : " is-hidden"}${isOpen ? " is-open" : ""}`}
          aria-label={isOpen ? "折叠" : "展开"}
          onClick={(e) => {
            e.stopPropagation();
            onToggle(item.id);
          }}
        >
          <Icon name="chevron" size={12} />
        </button>

        {isRenaming ? (
          <RenameInput
            initial={item.title}
            onSubmit={(title) => onRenameSubmit(item.id, title)}
            onCancel={onRenameCancel}
          />
        ) : (
          <>
            <span className="tree-label" title={item.title}>
              {item.title}
            </span>
            {item.orphan && (
              <span className="tree-badge" title="父笔记不存在，暂按根节点显示">
                孤儿
              </span>
            )}
            {item.cycle && (
              <span className="tree-badge" title="父子关系成环，已按根节点显示">
                环
              </span>
            )}
          </>
        )}

        {!isRenaming && (
          <span className="tree-actions">
            <button
              type="button"
              className="tree-action"
              title="更多"
              onMouseDown={(e) => e.stopPropagation()}
              onClick={(e) => {
                e.stopPropagation();
                onContextMenu(e, item);
              }}
            >
              <Icon name="more" size={14} className="icon-bold" />
            </button>
            <button
              type="button"
              className="tree-action"
              title="新建子笔记"
              onClick={(e) => {
                e.stopPropagation();
                onAddChild(item.id);
              }}
            >
              <Icon name="plus" size={14} />
            </button>
          </span>
        )}
      </div>

      {hasChildren &&
        isOpen &&
        children.map((child) => (
          <TreeRow
            key={child.id}
            item={child}
            depth={depth + 1}
            selectedId={selectedId}
            expanded={expanded}
            onToggle={onToggle}
            onSelect={onSelect}
            onAddChild={onAddChild}
            onRename={onRename}
            onDelete={onDelete}
            onContextMenu={onContextMenu}
            renamingId={renamingId}
            onRenameSubmit={onRenameSubmit}
            onRenameCancel={onRenameCancel}
          />
        ))}
    </>
  );
}

interface RenameInputProps {
  initial: string;
  onSubmit: (title: string) => void;
  onCancel: () => void;
}

function RenameInput({ initial, onSubmit, onCancel }: RenameInputProps) {
  const [value, setValue] = useState(initial);
  const ref = useRef<HTMLInputElement | null>(null);

  useEffect(() => {
    ref.current?.focus();
    ref.current?.select();
  }, []);

  return (
    <input
      ref={ref}
      className="tree-label-input"
      value={value}
      onChange={(e) => setValue(e.target.value)}
      onClick={(e) => e.stopPropagation()}
      onBlur={() => onSubmit(value)}
      onKeyDown={(e) => {
        if (e.key === "Enter") {
          e.preventDefault();
          onSubmit(value);
        } else if (e.key === "Escape") {
          e.preventDefault();
          onCancel();
        }
      }}
    />
  );
}
