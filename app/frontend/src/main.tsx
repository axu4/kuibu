import React from "react";
import ReactDOM from "react-dom/client";
import "./styles.css";

/**
 * 同一个 Vite 构建，按 URL 决定渲染主窗口还是随手记小窗。
 *
 * 小窗用 "/?view=quicknote" 打开；主窗口是 "/"。开发模式下 URL 可能
 * 带 dev server 的查询参数，所以只认 view 参数本身。
 */
function pickView(): "app" | "quicknote" {
  const view = new URLSearchParams(window.location.search).get("view");
  return view === "quicknote" ? "quicknote" : "app";
}

const isQuickNote = pickView() === "quicknote";
if (isQuickNote) {
  document.body.classList.add("quicknote-body");
}

// 按窗口只加载需要的代码：随手记小窗不加载主窗口（及其编辑器、周报等）的代码。
const load = isQuickNote
  ? import("./QuickNote").then((m) => m.default)
  : import("./App").then((m) => m.default);

void load.then((Root) => {
  ReactDOM.createRoot(document.getElementById("root") as HTMLElement).render(
    <React.StrictMode>
      <Root />
    </React.StrictMode>,
  );
});
