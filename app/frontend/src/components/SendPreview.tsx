import { useState } from "react";
import { Icon } from "./Icon";

/** 与 Go 侧 llm.Preview 对应的预览内容。 */
export interface SendPreviewData {
  modelName: string;
  provider: string;
  modelId: string;
  baseUrl: string;
  text: string;
  chars: number;
}

interface SendPreviewProps {
  /** 弹窗标题，例如「发送给 AI 归一周报」。 */
  title: string;
  preview: SendPreviewData;
  /** 点「确认发送」后调用；返回的 Promise 结束前按钮保持忙碌。 */
  onConfirm: () => Promise<void> | void;
  onCancel: () => void;
}

/**
 * 外发确认弹窗：把他人内容发给外部大模型前，展示目标模型与将发送的完整文本，
 * 只有点「确认发送」才发。所有外发入口（归一、组报、产品报、月报等）都复用它，
 * 不能做成可跳过的选项。
 */
export function SendPreview({ title, preview, onConfirm, onCancel }: SendPreviewProps) {
  const [sending, setSending] = useState(false);
  const [copied, setCopied] = useState(false);

  const confirm = async () => {
    setSending(true);
    try {
      await onConfirm();
    } finally {
      setSending(false);
    }
  };

  const copy = async () => {
    try {
      await navigator.clipboard.writeText(preview.text);
      setCopied(true);
      window.setTimeout(() => setCopied(false), 1500);
    } catch {
      // 复制失败时用户仍可手动选中文本。
    }
  };

  return (
    <div
      className="modal-mask is-center is-nested"
      onMouseDown={(e) => {
        e.stopPropagation();
        if (!sending) {
          onCancel();
        }
      }}
    >
      <div className="modal send-preview" onMouseDown={(e) => e.stopPropagation()}>
        <h2>{title}</h2>
        <div className="settings-desc">
          将发送到 <strong>{preview.modelName}</strong>（{preview.modelId} · {preview.baseUrl}），共{" "}
          {preview.chars} 字。请确认内容可以发给这个模型。
        </div>
        <pre className="send-preview-text">{preview.text}</pre>
        <div className="modal-buttons">
          <button type="button" className="btn" onClick={() => void copy()}>
            <Icon name="copy" size={14} />
            {copied ? "已复制" : "复制"}
          </button>
          <span className="send-preview-spacer" />
          <button type="button" className="btn" disabled={sending} onClick={onCancel}>
            取消
          </button>
          <button
            type="button"
            className="btn btn-primary"
            disabled={sending}
            onClick={() => void confirm()}
          >
            {sending ? "发送中…" : "确认发送"}
          </button>
        </div>
      </div>
    </div>
  );
}
