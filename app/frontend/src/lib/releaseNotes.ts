// GitHub Release 说明（自动生成的 Markdown）转成可直接渲染的结构。
//
// 只处理 Release 里实际会出现的几种写法：`## 标题`、`* / - 条目`、普通段落，
// 行内去掉 `**`、`` ` ``，PR 链接显示为 #N，比较链接（Full Changelog）整行丢弃。
// 不追求完整 Markdown，避免为了一段说明引入解析库。

export type NoteBlock =
  | { kind: "heading"; text: string }
  | { kind: "list"; items: string[] }
  | { kind: "text"; text: string };

/** 把 Release Markdown 解析成标题 / 列表 / 段落块。 */
export function parseReleaseNotes(markdown: string): NoteBlock[] {
  const blocks: NoteBlock[] = [];
  for (const raw of markdown.split(/\r?\n/)) {
    const line = raw.trim();
    if (!line || /full changelog/i.test(line)) {
      continue;
    }
    const heading = /^#{1,6}\s+(.*)$/.exec(line);
    if (heading) {
      blocks.push({ kind: "heading", text: headingText(cleanInline(heading[1])) });
      continue;
    }
    const item = /^[*-]\s+(.*)$/.exec(line);
    if (item) {
      const text = cleanInline(item[1]);
      const last = blocks[blocks.length - 1];
      if (last?.kind === "list") {
        last.items.push(text);
      } else {
        blocks.push({ kind: "list", items: [text] });
      }
      continue;
    }
    blocks.push({ kind: "text", text: cleanInline(line) });
  }
  return blocks;
}

/** 常见英文标题换成中文。 */
function headingText(text: string): string {
  if (/^what'?s changed$/i.test(text)) {
    return "更新内容";
  }
  if (/^new contributors$/i.test(text)) {
    return "新贡献者";
  }
  return text;
}

/** 清理行内 Markdown：PR 链接 → #N，「by @x in」去掉，[文字](链接) → 文字。 */
function cleanInline(text: string): string {
  return text
    .replace(/\s+by\s+@[\w-]+\s+in\s+https:\/\/github\.com\/\S+\/pull\/(\d+)/g, " (#$1)")
    .replace(/https:\/\/github\.com\/\S+\/pull\/(\d+)/g, "#$1")
    .replace(/\[([^\]]+)\]\([^)]+\)/g, "$1")
    .replace(/\*\*|__|`/g, "")
    .trim();
}
