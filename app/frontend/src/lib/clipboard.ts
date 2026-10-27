import { Clipboard } from "@wailsio/runtime";

/**
 * 同时写入 HTML 与纯文本两种剪贴板格式（飞书粘贴时取 HTML）。
 * WKWebView 不支持 ClipboardItem 时退回 Wails 纯文本剪贴板。
 * 返回实际写入的格式。
 */
export async function copyRich(html: string, text: string): Promise<"html" | "text"> {
  try {
    if (typeof ClipboardItem !== "undefined" && navigator.clipboard?.write) {
      await navigator.clipboard.write([
        new ClipboardItem({
          "text/html": new Blob([html], { type: "text/html" }),
          "text/plain": new Blob([text], { type: "text/plain" }),
        }),
      ]);
      return "html";
    }
  } catch {
    // 落到纯文本。
  }
  try {
    await Clipboard.SetText(text);
  } catch {
    await navigator.clipboard.writeText(text);
  }
  return "text";
}
