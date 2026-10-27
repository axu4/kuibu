import { useEffect, useRef, useState } from "react";
import type { TreeItem } from "../../bindings/github.com/axu4/kuibu/app";
import { Icon } from "./Icon";
import { Menu } from "./Menu";
import type { MenuItem } from "./Menu";
import { dropMove, effectiveZone, isFolder, isSelfOrDescendant } from "../lib/tree";
import type { DropZone, MoveTarget } from "../lib/tree";

/** 拖拽数据类型，只认树内拖动，避免和 Finder 拖文件混淆。 */
const DRAG_TYPE = "application/x-kuibu-note";

/** 拖拽中的放置提示。 */
interface DropHint {
  id: string;
  zone: DropZone;
}

/** 鼠标在行内的纵向比例（0 顶 1 底）。 */
function ratioOf(e: React.DragEvent<HTMLElement>): number {
  const rect = e.currentTarget.getBoundingClientRect();
  return rect.height > 0 ? (e.clientY - rect.top) / rect.height : 0.5;
}

/** 由鼠标在行内的纵向位置决定放置方式：上 1/4 前面、下 1/4 后面、中间子节点。 */
function zoneOf(ratio: number): DropZone {
  if (ratio < 0.25) return "before";
  if (ratio > 0.75) return "after";
  return "inside";
}

interface TreeProps {
  items: TreeItem[];
  selectedId: string | null;
  expanded: Set<string>;
  onToggle: (id: string) => void;
  onSelect: (id: string) => void;
  /** 在目录下新建笔记。 */
  onAddChild: (parent: string) => void;
  /** 在目录下新建子目录。 */
  onAddFolder: (parent: string) => void;
  /** 在根层级新建笔记（空树的「新建笔记」）。 */
  onCreateRoot: () => void;
  /** 在根层级新建目录。 */
  onCreateFolderRoot: () => void;
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
    onAddFolder,
    onCreateRoot,
    onCreateFolderRoot,
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
      // 目录中间 = 成为子节点；笔记中间按上下半区算前后，和松手后的结果一致。
      const target = findById(items, id);
      if (!target) {
        setHint(null);
        return;
      }
      const ratio = ratioOf(e);
      const zone = effectiveZone(target, zoneOf(ratio), ratio);
      setRootHint(false);
      if (hint?.id !== id || hint.zone !== zone) setHint({ id, zone });
    },
    onDrop: (e, id) => {
      e.preventDefault();
      e.stopPropagation();
      const target = findById(items, id);
      const ratio = ratioOf(e);
      const zone = target ? effectiveZone(target, zoneOf(ratio), ratio) : "inside";
      const move = dragId ? dropMove(items, dragId, id, zone) : null;
      setDragId(null);
      setHint(null);
      if (move) onMove(move);
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
          <button type="button" className="btn btn-ghost" onClick={onCreateFolderRoot}>
            <Icon name="folder" size={14} />
            新建目录
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
          onAddFolder={onAddFolder}
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
          items={rowMenuItems(menu.item, {
            onRename,
            onAddChild,
            onAddFolder,
            onDelete,
          })}
        />
      )}
    </div>
  );
}

/** 右键菜单的调用方回调。 */
interface RowActions {
  onRename: (item: TreeItem) => void;
  onAddChild: (parent: string) => void;
  onAddFolder: (parent: string) => void;
  onDelete: (item: TreeItem) => void;
}

/**
 * 节点的右键菜单：目录下可以新建笔记 / 子目录，笔记没有新建子项。
 */
function rowMenuItems(item: TreeItem, actions: RowActions): MenuItem[] {
  const items: MenuItem[] = [{ label: "重命名", icon: "edit", onClick: () => actions.onRename(item) }];
  if (isFolder(item)) {
    items.push(
      { label: "新建笔记", icon: "note", onClick: () => actions.onAddChild(item.id) },
      { label: "新建子目录", icon: "folder", onClick: () => actions.onAddFolder(item.id) },
    );
  }
  items.push({ label: "删除…", icon: "trash", danger: true, onClick: () => actions.onDelete(item) });
  return items;
}

/** 在树里按 ID 找节点。 */
function findById(items: TreeItem[], id: string): TreeItem | null {
  for (const item of items) {
    if (item.id === id) {
      return item;
    }
    const found = findById(item.children ?? [], id);
    if (found) {
      return found;
    }
  }
  return null;
}

interface TreeRowProps
  extends Omit<
    TreeProps,
    | "items"
    | "renamingId"
    | "onRenameSubmit"
    | "onRenameCancel"
    | "onCreateRoot"
    | "onCreateFolderRoot"
    | "onMove"
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
    onAddFolder,
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
  const folder = isFolder(item);
  const isOpen = expanded.has(item.id);
  const isSelected = selectedId === item.id;
  const isRenaming = renamingId === item.id;
  const dropZone = drag.hint?.id === item.id ? drag.hint.zone : null;
  // 只有目录有展开箭头（空目录也保留位置，保证缩进对齐）。
  const showCaret = folder;

  return (
    <>
      <div
        className={`tree-row${isSelected ? " is-selected" : ""}${
          drag.dragId === item.id ? " is-dragging" : ""
        }${dropZone ? ` is-drop-${dropZone}` : ""}${folder ? " is-folder" : ""}`}
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
          className={`tree-caret${showCaret ? "" : " is-hidden"}${isOpen ? " is-open" : ""}`}
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
            {folder && (
              <span className="tree-kind" aria-hidden>
                <Icon name={isOpen ? "folderOpen" : "folder"} size={14} />
              </span>
            )}
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
            {/* 只有目录能装子项，笔记不显示「+」。 */}
            {folder && (
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
            )}
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
            onAddFolder={onAddFolder}
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
