import { useEffect, useRef, useState } from "react";
import type { TreeItem } from "../../bindings/github.com/axu4/kuibu/app";
import { Icon } from "./Icon";
import { Menu } from "./Menu";
import { dropMove, isSelfOrDescendant } from "../lib/tree";
import type { DropZone, MoveTarget } from "../lib/tree";

/** 拖拽数据类型，只认树内拖动，避免和 Finder 拖文件混淆。 */
const DRAG_TYPE = "application/x-kuibu-note";

/** 拖拽中的放置提示。 */
interface DropHint {
  id: string;
  zone: DropZone;
}

/** 由鼠标在行内的纵向位置决定放置方式：上 1/4 前面、下 1/4 后面、中间子节点。 */
function zoneOf(e: React.DragEvent<HTMLElement>): DropZone {
  const rect = e.currentTarget.getBoundingClientRect();
  const y = (e.clientY - rect.top) / rect.height;
  if (y < 0.25) return "before";
  if (y > 0.75) return "after";
  return "inside";
}

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
  /** 拖拽移动笔记。 */
  onMove: (target: MoveTarget) => void;
}

/** 行级拖拽回调，由 Tree 统一处理。 */
interface DragHandlers {
  dragId: string | null;
  hint: DropHint | null;
  onDragStart: (id: string) => void;
  onDragOver: (e: React.DragEvent<HTMLElement>, id: string) => void;
  onDrop: (e: React.DragEvent<HTMLElement>, id: string) => void;
  onDragEnd: () => void;
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
    onMove,
  } = props;

  const [menu, setMenu] = useState<MenuState | null>(null);
  const [dragId, setDragId] = useState<string | null>(null);
  const [hint, setHint] = useState<DropHint | null>(null);
  const [rootHint, setRootHint] = useState(false);

  const drag: DragHandlers = {
    dragId,
    hint,
    onDragStart: (id) => setDragId(id),
    onDragOver: (e, id) => {
      if (!dragId) return;
      e.stopPropagation();
      // 拖到自己或后代上不接受，与后端成环规则一致。
      if (isSelfOrDescendant(items, dragId, id)) {
        setHint(null);
        return;
      }
      e.preventDefault();
      e.dataTransfer.dropEffect = "move";
      const zone = zoneOf(e);
      setRootHint(false);
      if (hint?.id !== id || hint.zone !== zone) setHint({ id, zone });
    },
    onDrop: (e, id) => {
      e.preventDefault();
      e.stopPropagation();
      const target = dragId ? dropMove(items, dragId, id, zoneOf(e)) : null;
      setDragId(null);
      setHint(null);
      if (target) onMove(target);
    },
    onDragEnd: () => {
      setDragId(null);
      setHint(null);
      setRootHint(false);
    },
  };

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
    <div
      className={`tree${rootHint ? " is-root-drop" : ""}`}
      onDragOver={(e) => {
        // 拖到行之间以外的空白处 = 移到根层级末尾。
        if (!dragId) return;
        e.preventDefault();
        setHint(null);
        setRootHint(true);
      }}
      onDragLeave={(e) => {
        if (e.currentTarget === e.target) setRootHint(false);
      }}
      onDrop={(e) => {
        e.preventDefault();
        const id = dragId;
        setDragId(null);
        setRootHint(false);
        if (id) onMove({ id, parent: "", pos: -1 });
      }}
    >
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
          drag={drag}
        />
      ))}
      <div className="tree-root-drop" aria-hidden />

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
  extends Omit<
    TreeProps,
    "items" | "renamingId" | "onRenameSubmit" | "onRenameCancel" | "onCreateRoot" | "onMove"
  > {
  drag: DragHandlers;
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
    drag,
  } = props;

  const children = item.children ?? [];
  const hasChildren = children.length > 0;
  const isOpen = expanded.has(item.id);
  const isSelected = selectedId === item.id;
  const isRenaming = renamingId === item.id;
  const dropZone = drag.hint?.id === item.id ? drag.hint.zone : null;

  return (
    <>
      <div
        className={`tree-row${isSelected ? " is-selected" : ""}${
          drag.dragId === item.id ? " is-dragging" : ""
        }${dropZone ? ` is-drop-${dropZone}` : ""}`}
        style={{ paddingLeft: 4 + depth * 16 }}
        draggable={!isRenaming}
        onDragStart={(e) => {
          e.dataTransfer.setData(DRAG_TYPE, item.id);
          e.dataTransfer.effectAllowed = "move";
          drag.onDragStart(item.id);
        }}
        onDragOver={(e) => drag.onDragOver(e, item.id)}
        onDrop={(e) => drag.onDrop(e, item.id)}
        onDragEnd={drag.onDragEnd}
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
            drag={drag}
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
