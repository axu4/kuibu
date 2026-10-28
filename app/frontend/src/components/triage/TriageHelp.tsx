// 周报三步流程的键盘说明。

import { Icon } from "../Icon";

interface TriageHelpProps {
  onClose: () => void;
}

const ROWS: { label: string; keys: string[] }[] = [
  { label: "放进第 N 个项目的候选", keys: ["1", "2", "3", "4"] },
  { label: "只有一个项目时直接采纳", keys: ["↵"] },
  { label: "移除（留在「已移除」可恢复）", keys: ["X", "⌫"] },
  { label: "待定，先不做决定", keys: ["空格"] },
  { label: "上一条 / 下一条", keys: ["↑", "↓", "K", "J"] },
  { label: "展开 / 收起组员原文", keys: ["E"] },
  { label: "撤销上一步", keys: ["U", "⌘Z"] },
  { label: "保存当前项目草稿", keys: ["⌘S"] },
  { label: "关闭 / 返回上一步", keys: ["Esc"] },
  { label: "打开键盘说明", keys: ["?"] },
];

export function TriageHelp({ onClose }: TriageHelpProps) {
  return (
    <div className="modal-mask is-center" onMouseDown={onClose}>
      <div className="modal triage-help-modal" onMouseDown={(e) => e.stopPropagation()}>
        <div className="triage-help-head">
          <h2>键盘</h2>
          <button type="button" className="icon-btn" title="关闭" onClick={onClose}>
            <Icon name="close" />
          </button>
        </div>
        <p className="settings-desc">
          第 1 步：先过「本周总结」，再过「下周计划」，一条按一次键；第 2 步可以直接拖动条目换项目；
          第 3 步按 ⌘S 保存当前项目草稿。快捷键只在流程里生效，不抢编辑器输入。
        </p>
        <div className="triage-keyrows">
          {ROWS.map((row) => (
            <div key={row.label} className="triage-keyrow">
              <span>{row.label}</span>
              <span className="triage-keys">
                {row.keys.map((key) => (
                  <kbd key={key}>{key}</kbd>
                ))}
              </span>
            </div>
          ))}
        </div>
        <p className="settings-desc">
          鼠标：点项目名 = 采纳；点左侧任意条目 = 跳过去改判；第 2 步拖动条目 = 换项目 / 待定 / 移除；
          点步骤条 = 回到某一步。
        </p>
        <div className="modal-buttons">
          <button type="button" className="btn" onClick={onClose}>
            知道了
          </button>
        </div>
      </div>
    </div>
  );
}
