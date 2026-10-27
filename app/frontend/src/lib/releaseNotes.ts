// 把 GitHub Release 的说明（Markdown）整理成设置里能直接显示的几类行。
//
// 只处理 GitHub 自动生成说明里会出现的写法：## 标题、* / - 列表、**加粗**、
// 「by @user in https://github.com/…/pull/N」和 Full Changelog 链接。
// 其他内容按普通文字显示，不追求完整的 Markdown 渲染。

export type NoteLine =
  | { kind: "heading"; text: string }
  | { kind: "item"; text: string }
  | { kind: "text"; text: string };

/** 标题里常见的英文换成中文。 */
const HEADINGS: Record<string, string> = {
  "What's Changed": "更新内容",
  "New Contributors": "新贡献者",
};

/** 去掉行内 Markdown 记号，并把 PR / 对比链接缩短。 */
function cleanInline(text: string): string {
  return (
    text
      // 「by @axu4 in https://github.com/o/r/pull/20」→「#20」
      .replace(/\s+by\s+@\S+\s+in\s+https:\/\/github\.com\/[^/\s]+\/[^/\s]+\/pull\/(\d+)/g, "（#$1）")
      // 剩下的 PR 链接也缩成 #N。
      .replace(/https:\/\/github\.com\/[^/\s]+\/[^/\s]+\/pull\/(\d+)/g, "#$1")
      // 对比链接只留版本区间，如 v0.3.0…v0.4.0。
      .replace(/https:\/\/github\.com\/[^/\s]+\/[^/\s]+\/compare\/(\S+?)\.\.\.(\S+)/g, "$1 → $2")
      .replace(/\*\*(.+?)\*\*/g, "$1")
      .replace(/`([^`]+)`/g, "$1")
      .trim()
  );
}

export function parseReleaseNotes(notes: string): NoteLine[] {
  const lines: NoteLine[] = [];
  for (const raw of notes.replace(/\r\n/g, "\n").split("\n")) {
    const line = raw.trim();
    if (!line) {
      continue;
    }
    const heading = /^#{1,6}\s+(.*)$/.exec(line);
    if (heading) {
      const text = cleanInline(heading[1]);
      lines.push({ kind: "heading", text: HEADINGS[text] ?? text });
      continue;
    }
    const item = /^[*-]\s+(.*)$/.exec(line);
    if (item) {
      lines.push({ kind: "item", text: cleanInline(item[1]) });
      continue;
    }
    const text = cleanInline(line).replace(/^Full Changelog:\s*/, "完整变更：");
    lines.push({ kind: "text", text });
  }
  return lines;
}
