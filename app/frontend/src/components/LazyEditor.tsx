import { Suspense, lazy } from "react";
import type { EditorProps } from "./Editor";

// Milkdown（Crepe）体积很大，第一次打开编辑器时才加载，首屏与随手记小窗不带它。
const EditorImpl = lazy(() => import("./Editor").then((m) => ({ default: m.Editor })));

/** 懒加载的编辑器；加载期间显示同尺寸的空白编辑区，避免跳动。 */
export function Editor(props: EditorProps) {
  return (
    <Suspense fallback={<div className="editor-scroll editor-loading" aria-busy="true" />}>
      <EditorImpl {...props} />
    </Suspense>
  );
}
