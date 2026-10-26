import { useEffect, useState } from "react";
import { Crepe } from "@milkdown/crepe";
import "@milkdown/crepe/theme/common/style.css";
import "@milkdown/crepe/theme/frame.css";
import { toDisplayMarkdown, toStorageMarkdown } from "../lib/markdown";

interface EditorProps {
  /** 当前笔记 ID，用于区分笔记切换。 */
  noteId: string;
  /** 打开时的正文（Markdown 原文）。 */
  initialBody: string;
  /** 正文变化回调，参数是 Markdown 原文。 */
  onChange: (body: string) => void;
  /** 空文档时的提示文字。 */
  placeholder?: string;
}

/**
 * Milkdown（Crepe）所见即所得编辑器。
 *
 * 以 Markdown 为数据源：挂载时用 defaultValue 加载正文，之后监听
 * markdownUpdated 把内容转回 Markdown。仅在内容与初始值不同才回调，
 * 避免“只是打开笔记”就被序列化改写并写回文件。
 */
export function Editor({
  noteId,
  initialBody,
  onChange,
  placeholder = "开始写…输入 / 插入标题、列表、代码块",
}: EditorProps) {
  const [host, setHost] = useState<HTMLDivElement | null>(null);

  useEffect(() => {
    if (!host) {
      return;
    }
    const initial = toDisplayMarkdown(initialBody);
    let disposed = false;
    // 用户是否已经真正操作过编辑器。Milkdown 挂载时可能对文档做规范化
    // （补末尾段落等），此时不能把内容当成用户编辑写回文件。
    let interacted = false;
    const markInteracted = () => {
      interacted = true;
    };
    const interactionEvents = ["pointerdown", "keydown", "focusin", "paste"] as const;
    for (const name of interactionEvents) {
      host.addEventListener(name, markInteracted, true);
    }

    const crepe = new Crepe({
      root: host,
      defaultValue: initial,
      featureConfigs: {
        [Crepe.Feature.Placeholder]: { text: placeholder },
      },
    });
    let lastMarkdown = initial;

    crepe.on((listener) => {
      listener.markdownUpdated((_ctx, markdown) => {
        if (disposed || !interacted || markdown === lastMarkdown) {
          return;
        }
        lastMarkdown = markdown;
        onChange(toStorageMarkdown(markdown));
      });
    });

    crepe.create().catch((err: unknown) => {
      console.error("创建编辑器失败", err);
    });

    return () => {
      disposed = true;
      for (const name of interactionEvents) {
        host.removeEventListener(name, markInteracted, true);
      }
      void crepe.destroy();
    };
    // noteId 变化时重建编辑器；initialBody 只在挂载时使用。
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [host, noteId]);

  return <div className="editor-scroll" ref={setHost} />;
}
