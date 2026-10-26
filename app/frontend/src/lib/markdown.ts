// 工作台资源 URL 与 Markdown 之间的图片路径转换。
//
// 笔记正文里的图片用相对路径 ../assets/xxx（相对 notes/ 下的笔记文件），
// 前端通过 Wails AssetServer 的 /workspace-assets/xxx 访问。编辑器里显示的
// 是改写后的 URL，保存前再改回相对路径，保证文件内容始终是 Markdown 原生
// 的相对引用。

/** 前端访问工作台资源的 URL 前缀，与 Go 侧 assetPrefix 保持一致。 */
export const ASSET_URL_PREFIX = "/workspace-assets/";

/** 笔记正文中的资源相对路径前缀。 */
const ASSET_REL_PREFIX = "../assets/";

/**
 * 把 Markdown 里的 ../assets/ 改写成前端可访问的 URL。
 * 用于把正文喂给编辑器。
 */
export function toDisplayMarkdown(markdown: string): string {
  return markdown.split(ASSET_REL_PREFIX).join(ASSET_URL_PREFIX);
}

/**
 * 把编辑器产出的 Markdown 里的资源 URL 改回 ../assets/。
 * 用于写回文件。
 */
export function toStorageMarkdown(markdown: string): string {
  return markdown.split(ASSET_URL_PREFIX).join(ASSET_REL_PREFIX);
}
