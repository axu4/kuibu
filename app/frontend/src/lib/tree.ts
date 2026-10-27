// 笔记树移动的位置计算（拖拽与键盘共用）。结果直接交给 WorkspaceService.Move，
// 其中 pos 是「去掉被移动节点后」新父节点子列表里的插入位置，-1 表示末尾，
// 与 store.Move 的语义一致。

import type { TreeItem } from "../../bindings/github.com/axu4/kuibu/app";

/** 放置方式：放到目标前面、后面，或成为它的子节点。 */
export type DropZone = "before" | "after" | "inside";

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

/** 拖到 target 的 zone 上时的移动目标；不合法（成环、找不到）返回 null。 */
export function dropMove(items: TreeItem[], id: string, targetId: string, zone: DropZone): MoveTarget | null {
  if (isSelfOrDescendant(items, id, targetId)) return null;
  const target = locate(items, targetId);
  if (!target) return null;
  if (zone === "inside") {
    return { id, parent: targetId, pos: -1 };
  }
  const idx = indexWithout(target.siblings, id, targetId);
  return { id, parent: target.parent, pos: zone === "before" ? idx : idx + 1 };
}

/** 键盘移动：up / down 同级移动，left 升级到父节点后面，right 成为上一个兄弟的末尾子节点。 */
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
    case "right":
      return index > 0 ? { id, parent: siblings[index - 1].id, pos: -1 } : null;
    case "left": {
      if (!parent) return null;
      const up = locate(items, parent);
      if (!up) return null;
      return { id, parent: up.parent, pos: up.index + 1 };
    }
  }
}
