// 周报三步流程的纯工具函数：分节推进、候选统计、候选稿排版。
// 只做展示与派生，不发起服务调用。

import type {
  TriageData,
  TriageItemData,
  TriageSectionData,
} from "../../../bindings/github.com/axu4/kuibu/app";

/** 第 1 步三个 tab 的短名。 */
export function shortSection(title: string): string {
  if (title.includes("总结") || title.includes("完成")) return "总结";
  if (title.includes("计划")) return "计划";
  if (title.includes("其他") || title.includes("风险") || title.includes("求助")) return "其他";
  return "其他";
}

export function sectionsOf(data: TriageData | null): TriageSectionData[] {
  return data?.sections ?? [];
}

export function sectionTitle(data: TriageData | null, section: string): string {
  return (
    sectionsOf(data).find((s) => s.section === section)?.title ??
    (section === "本周完成" ? "本周总结" : section)
  );
}

export function itemsInSection(items: TriageItemData[], section: string): TriageItemData[] {
  return items.filter((i) => i.section === section);
}

export function acceptedItems(items: TriageItemData[], projectId: string): TriageItemData[] {
  return items.filter((i) => i.decision === "accepted" && i.project === projectId);
}

export function findItem(items: TriageItemData[], id: string | null): TriageItemData | null {
  if (!id) return null;
  return items.find((i) => i.id === id) ?? null;
}

/** 本节内下一条未判条目（从 afterId 之后绕回开头）。 */
export function nextUndecidedInSection(
  items: TriageItemData[],
  section: string,
  afterId: string,
): TriageItemData | null {
  const list = itemsInSection(items, section);
  const start = Math.max(0, list.findIndex((i) => i.id === afterId) + 1);
  for (let k = 0; k < list.length; k++) {
    const item = list[(start + k) % list.length];
    if (!item.decision) return item;
  }
  return null;
}

/** 从 fromSection 之后找下一节还有未判条目的节。 */
export function nextSectionWithUndecided(data: TriageData | null, fromSection: string): string | null {
  const sections = sectionsOf(data);
  const start = sections.findIndex((s) => s.section === fromSection);
  for (let k = 1; k <= sections.length; k++) {
    const sec = sections[(start + k) % sections.length];
    if (sec.undecided > 0) return sec.section;
  }
  return null;
}

/** 进入一节时的落点：第一条未判；都判过则回到该节第一条。 */
export function entryItemOfSection(
  items: TriageItemData[],
  section: string,
): TriageItemData | null {
  const list = itemsInSection(items, section);
  return list.find((i) => !i.decision) ?? list[0] ?? null;
}

export function defaultSection(data: TriageData | null): string {
  const sections = sectionsOf(data);
  return sections.find((s) => s.undecided > 0)?.section ?? sections[0]?.section ?? "本周完成";
}

export function defaultItem(data: TriageData | null): TriageItemData | null {
  if (!data) return null;
  const items = data.items ?? [];
  const section = defaultSection(data);
  return entryItemOfSection(items, section) ?? items[0] ?? null;
}

/** 某项目当前候选条数。 */
export function candidateCount(items: TriageItemData[], projectId: string): number {
  return acceptedItems(items, projectId).length;
}

/** 候选稿：只按「栏目 → 条目」排版，不带姓名。 */
export function buildProjectReport(data: TriageData, projectId: string): string {
  const items = data.items ?? [];
  let out = "";
  for (const section of sectionsOf(data)) {
    const list = acceptedItems(items, projectId).filter((i) => i.section === section.section);
    if (list.length === 0) continue;
    out += `## ${section.title}\n`;
    for (const item of list) {
      out += `- ${item.text}\n`;
    }
    out += "\n";
  }
  return out.trim() ? `${out.trim()}\n` : "本周没有归入该项目的条目。\n";
}

/** 和 Go 侧 trimBlankLines 一致的正文规范化，用来判断是否有未保存修改。 */
export function normalizeBody(body: string): string {
  const lines = body.split("\n");
  let start = 0;
  let end = lines.length;
  while (start < end && lines[start].trim() === "") start++;
  while (end > start && lines[end - 1].trim() === "") end--;
  return lines.slice(start, end).join("\n");
}

export type MarkdownBlock =
  | { kind: "h2"; text: string }
  | { kind: "li"; text: string }
  | { kind: "p"; text: string };

/** 只读预览用的极简 Markdown：标题、列表、普通段落。 */
export function markdownBlocks(body: string): MarkdownBlock[] {
  const blocks: MarkdownBlock[] = [];
  for (const line of body.split("\n")) {
    if (/^##\s+/.test(line)) {
      blocks.push({ kind: "h2", text: line.replace(/^##\s+/, "") });
    } else if (/^-\s+/.test(line)) {
      blocks.push({ kind: "li", text: line.replace(/^-\s+/, "") });
    } else if (line.trim() !== "") {
      blocks.push({ kind: "p", text: line });
    }
  }
  return blocks;
}

/** RFC3339 时间只显示时分。 */
export function formatClock(iso: string | null | undefined): string {
  if (!iso) return "";
  const date = new Date(iso);
  if (Number.isNaN(date.getTime())) return "";
  return `${String(date.getHours()).padStart(2, "0")}:${String(date.getMinutes()).padStart(2, "0")}`;
}
