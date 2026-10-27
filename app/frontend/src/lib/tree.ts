// 笔记树移动的位置计算（拖拽与键盘共用）。结果直接交给 WorkspaceService.Move，
// 其中 pos 是「去掉被移动节点后」新父节点子列表里的插入位置，-1 表示末尾，
// 与 store.Move 的语义一致。
//
// 树是「目录 + 叶子笔记」两层规则：只有目录（type=folder）能包含子节点，
// 笔记永远没有子节点。判断一律看 type 字段，不看有没有 children——空目录也是目录。

import type { TreeItem } from "../../bindings/github.com/axu4/kuibu/app";

/** 与 store.FolderType 对应：目录节点的 note type。 */
export const FOLDER_TYPE = "folder";

/** 节点是否是目录。 */
export function isFolder(item: TreeItem | null | undefined): boolean {
  return item?.type === FOLDER_TYPE;
}

/** 放置方式：放到目标前面、后面，或成为它的子节点。 */
export type DropZone = "before" | "after" | "inside";

/**
 * 把鼠标落在行上的原始放置方式换算成真正能用的方式：
 * 中间区域只有落在目录上才是 inside，落在笔记上一律按上下半区算 before/after，
 * 这样拖动时看到的提示与松手后的结果一致。
 */
export function effectiveZone(target: TreeItem, zone: DropZone, ratio: number): DropZone {
  if (zone !== "inside" || isFolder(target)) {
    return zone;
  }
  return ratio < 0.5 ? "before" : "after";
}

/** 一次移动：把 id 移到 parent 下第 pos 个位置。 */
export interface MoveTarget {
  id: string;
  parent: string;
  pos: number;
}

interface Located {
  item: TreeItem;
  parent: string;
  siblings: TreeItem[];
  index: number;
}

/** 在树里找到节点及其父节点、同级列表。 */
export function locate(items: TreeItem[], id: string, parent = ""): Located | null {
  for (let i = 0; i < items.length; i++) {
    const it = items[i];
    if (it.id === id) {
      return { item: it, parent, siblings: items, index: i };
    }
    const found = locate(it.children ?? [], id, it.id);
    if (found) return found;
  }
  return null;
}

/** target 是否是 id 本身或它的后代（放上去会成环）。 */
export function isSelfOrDescendant(items: TreeItem[], id: string, target: string): boolean {
  if (id === target) return true;
  const self = locate(items, id);
  if (!self) return false;
  return locate(self.item.children ?? [], target) !== null;
}

/** 去掉 id 后，同级列表里 targetId 的位置。 */
function indexWithout(siblings: TreeItem[], id: string, targetId: string): number {
  return siblings.filter((s) => s.id !== id).findIndex((s) => s.id === targetId);
}

/** 拖到 target 的 zone 上时的移动目标；不合法（成环、笔记当父、找不到）返回 null。 */
export function dropMove(items: TreeItem[], id: string, targetId: string, zone: DropZone): MoveTarget | null {
  if (isSelfOrDescendant(items, id, targetId)) return null;
  const target = locate(items, targetId);
  if (!target) return null;
  if (zone === "inside") {
    // 只有目录能装东西，拖到笔记中间不会变成子节点（调用方已按 effectiveZone 换算过，
    // 这里再兜一层，防止别处直接传 inside）。
    if (!isFolder(target.item)) return null;
    return { id, parent: targetId, pos: -1 };
  }
  const idx = indexWithout(target.siblings, id, targetId);
  return { id, parent: target.parent, pos: zone === "before" ? idx : idx + 1 };
}

/**
 * 键盘移动：up / down 同级移动，left 升级到父节点后面，right 成为上一个兄弟的末尾子节点。
 * 成为子节点时上一个兄弟必须是目录，否则不动（返回 null）。
 */
export function keyMove(
  items: TreeItem[],
  id: string,
  dir: "up" | "down" | "left" | "right",
): MoveTarget | null {
  const self = locate(items, id);
  if (!self) return null;
  const { siblings, index, parent } = self;
  switch (dir) {
    case "up":
      return index > 0 ? { id, parent, pos: index - 1 } : null;
    case "down":
      return index < siblings.length - 1 ? { id, parent, pos: index + 1 } : null;
    case "right": {
      if (index <= 0 || !isFolder(siblings[index - 1])) return null;
      return { id, parent: siblings[index - 1].id, pos: -1 };
    }
    case "left": {
      if (!parent) return null;
      const up = locate(items, parent);
      if (!up) return null;
      return { id, parent: up.parent, pos: up.index + 1 };
    }
  }
}
