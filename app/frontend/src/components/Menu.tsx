import { useEffect, useLayoutEffect, useRef, useState } from "react";
import { Icon } from "./Icon";
import type { IconName } from "./Icon";

export interface MenuItem {
  label: string;
  icon?: IconName;
  danger?: boolean;
  /** 置灰不可点；title 里写原因。 */
  disabled?: boolean;
  title?: string;
  onClick: () => void;
}

interface MenuProps {
  x: number;
  y: number;
  items: MenuItem[];
  onClose: () => void;
}

/**
 * 浮动菜单：右键菜单和「更多」按钮共用。点外面、失焦或按 Esc 关闭，
 * 贴近窗口边缘时自动往回收，避免被裁掉。
 */
export function Menu({ x, y, items, onClose }: MenuProps) {
  const ref = useRef<HTMLDivElement | null>(null);
  const [pos, setPos] = useState({ left: x, top: y });

  useLayoutEffect(() => {
    const el = ref.current;
    if (!el) {
      return;
    }
    const rect = el.getBoundingClientRect();
    setPos({
      left: Math.min(x, window.innerWidth - rect.width - 8),
      top: Math.min(y, window.innerHeight - rect.height - 8),
    });
  }, [x, y]);

  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "Escape") {
        onClose();
      }
    };
    window.addEventListener("mousedown", onClose);
    window.addEventListener("blur", onClose);
    window.addEventListener("keydown", onKey);
    return () => {
      window.removeEventListener("mousedown", onClose);
      window.removeEventListener("blur", onClose);
      window.removeEventListener("keydown", onKey);
    };
  }, [onClose]);

  return (
    <div
      ref={ref}
      className="menu"
      role="menu"
      style={pos}
      onMouseDown={(e) => e.stopPropagation()}
    >
      {items.map((item) => (
        <button
          key={item.label}
          type="button"
          role="menuitem"
          className={item.danger ? "is-danger" : ""}
          disabled={item.disabled}
          title={item.title}
          onClick={() => {
            onClose();
            item.onClick();
          }}
        >
          {item.icon && <Icon name={item.icon} size={14} />}
          {item.label}
        </button>
      ))}
    </div>
  );
}

/** 工具栏里的「…」按钮，点开在按钮下方弹出菜单。 */
export function MoreMenu({ items, title = "更多" }: { items: MenuItem[]; title?: string }) {
  const [anchor, setAnchor] = useState<{ x: number; y: number } | null>(null);

  return (
    <>
      <button
        type="button"
        className={`icon-btn${anchor ? " is-on" : ""}`}
        title={title}
        onMouseDown={(e) => e.stopPropagation()}
        onClick={(e) => {
          if (anchor) {
            setAnchor(null);
            return;
          }
          const rect = e.currentTarget.getBoundingClientRect();
          setAnchor({ x: rect.right - 168, y: rect.bottom + 4 });
        }}
      >
        <Icon name="more" className="icon-bold" />
      </button>
      {anchor && <Menu x={anchor.x} y={anchor.y} items={items} onClose={() => setAnchor(null)} />}
    </>
  );
}
