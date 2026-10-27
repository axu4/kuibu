import { useEffect, useState } from "react";
import { Crepe } from "@milkdown/crepe";
import { insert } from "@milkdown/kit/utils";
import { Events } from "@wailsio/runtime";
import "@milkdown/crepe/theme/common/style.css";
import "@milkdown/crepe/theme/frame.css";
import { WorkspaceService } from "../../bindings/github.com/axu4/kuibu/app";
import { toDisplayMarkdown, toStorageMarkdown } from "../lib/markdown";

/** 拖进笔记后 Go 侧发来的文件。 */
interface DroppedAsset {
  name: string;
  rel: string;
  image: boolean;
}

/** 把粘贴 / 上传的文件存进工作台 assets/，返回编辑器里显示用的 URL。 */
async function uploadToAssets(file: File): Promise<string> {
  const buf = new Uint8Array(await file.arrayBuffer());
  let bin = "";
  for (let i = 0; i < buf.length; i += 0x8000) {
    bin += String.fromCharCode(...buf.subarray(i, i + 0x8000));
  }
  const ext = (file.type.split("/")[1] || "png").replace("jpeg", "jpg");
  const name = file.name && file.name.includes(".") ? file.name : `image.${ext}`;
  const rel = await WorkspaceService.SaveAsset(name, btoa(bin));
  return toDisplayMarkdown(rel);
}

/** 把 ../assets/xxx 转成编辑器显示用的 URL，并转义 Markdown 链接里的括号与空格。 */
function displayURL(rel: string): string {
  return toDisplayMarkdown(rel).replace(/[ ()]/g, (c) => encodeURIComponent(c));
}

interface EditorProps {
  /** 当前笔记 ID，用于区分笔记切换。 */
  noteId: string;
  /** 打开时的正文（Markdown 原文）。 */
  initialBody: string;
  /** 正文变化回调，参数是 Markdown 原文。 */
  onChange: (body: string) => void;
  /** 空文档时的提示文字。 */
  placeholder?: string;
  /**
   * 作为 Finder 文件拖放目标：拖入的文件由 Go 侧存进 assets/，再在光标处插入
   * 图片或链接。待办详情里不开（那里拖放是加附件）。
   */
  fileDrop?: boolean;
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
  fileDrop = false,
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
        // 粘贴 / 拖入图片与图片块「上传」都存进 assets/（默认会用 blob: 临时地址）。
        [Crepe.Feature.ImageBlock]: {
          onUpload: uploadToAssets,
          inlineOnUpload: uploadToAssets,
          blockOnUpload: uploadToAssets,
        },
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

    // Finder 拖入的文件：Go 侧已存进 assets/，这里在光标处插入。
    const offDrop = fileDrop
      ? Events.On("note:assets-dropped", (event) => {
          const assets = (event.data ?? []) as DroppedAsset[];
          if (disposed || assets.length === 0) return;
          const md = assets
            .map((a) => `${a.image ? "!" : ""}[${a.name.replace(/[[\]]/g, "")}](${displayURL(a.rel)})`)
            .join("\n\n");
          interacted = true;
          crepe.editor.action(insert(md));
        })
      : null;

    return () => {
      disposed = true;
      offDrop?.();
      for (const name of interactionEvents) {
        host.removeEventListener(name, markInteracted, true);
      }
      void crepe.destroy();
    };
    // noteId 变化时重建编辑器；initialBody 只在挂载时使用。
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [host, noteId, fileDrop]);

  return fileDrop ? (
    <div className="editor-scroll" ref={setHost} data-file-drop-target="true" data-note-drop="1" />
  ) : (
    <div className="editor-scroll" ref={setHost} />
  );
}
