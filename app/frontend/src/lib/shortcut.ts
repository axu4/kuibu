// 快捷键相关的纯函数：Wails 加速键 ↔ macOS 展示符号、
// 浏览器 KeyboardEvent ↔ Wails 加速键。
//
// 这里的规则必须和 Go 侧 app/shortcut.go 保持一致：
// 至少一个修饰键、拒绝纯修饰键、主键是单字符或命名键。

/** 修饰键的规范名（与 Wails 加速键语法一致）。 */
export type Modifier = "Ctrl" | "Alt" | "Shift" | "Cmd";

/** 修饰键的展示顺序与符号：⌃⌥⇧⌘。 */
const MODIFIER_ORDER: Array<{ name: Modifier; symbol: string }> = [
  { name: "Ctrl", symbol: "⌃" },
  { name: "Alt", symbol: "⌥" },
  { name: "Shift", symbol: "⇧" },
  { name: "Cmd", symbol: "⌘" },
];

/** 可接受的修饰键别名（小写）。 */
const MODIFIER_ALIASES: Record<string, Modifier> = {
  ctrl: "Ctrl",
  control: "Ctrl",
  alt: "Alt",
  option: "Alt",
  opt: "Alt",
  shift: "Shift",
  cmd: "Cmd",
  command: "Cmd",
  super: "Cmd",
  win: "Cmd",
  cmdorctrl: "Cmd",
};

/** 命名键：canonical 是 Wails 可解析的写法，display 是展示名。 */
const NAMED_KEYS: Record<string, { canonical: string; display: string }> = {
  backspace: { canonical: "Backspace", display: "Backspace" },
  tab: { canonical: "Tab", display: "Tab" },
  return: { canonical: "Enter", display: "Enter" },
  enter: { canonical: "Enter", display: "Enter" },
  escape: { canonical: "Escape", display: "Esc" },
  left: { canonical: "Left", display: "←" },
  right: { canonical: "Right", display: "→" },
  up: { canonical: "Up", display: "↑" },
  down: { canonical: "Down", display: "↓" },
  space: { canonical: "Space", display: "Space" },
  delete: { canonical: "Delete", display: "Delete" },
  "forward delete": { canonical: "Forward delete", display: "Delete" },
  home: { canonical: "Home", display: "Home" },
  end: { canonical: "End", display: "End" },
  "page up": { canonical: "Page up", display: "PageUp" },
  "page down": { canonical: "Page down", display: "PageDown" },
  numlock: { canonical: "NumLock", display: "NumLock" },
  plus: { canonical: "Plus", display: "+" },
};

/** 查找命名键（含 F1~F35）。 */
function lookupNamedKey(key: string): { canonical: string; display: string } | null {
  const lower = key.trim().toLowerCase();
  const named = NAMED_KEYS[lower];
  if (named) {
    return named;
  }
  const fn = /^f([1-9]|[12]\d|3[0-5])$/.exec(lower);
  if (fn) {
    const name = `F${fn[1]}`;
    return { canonical: name, display: name };
  }
  return null;
}

/** 查找修饰键别名。 */
function lookupModifier(name: string): Modifier | null {
  return MODIFIER_ALIASES[name.trim().toLowerCase()] ?? null;
}

/** 规范化结果。 */
export interface NormalizedShortcut {
  /** 规范化的 Wails 加速键，例如 "Ctrl+Alt+N"。 */
  accelerator: string;
  /** macOS 符号形式，例如 "⌃⌥N"。 */
  display: string;
  /** 主键展示名（不含修饰键），例如 "N"、"Space"。 */
  key: string;
  /** 修饰键集合。 */
  modifiers: Modifier[];
}

/**
 * 校验并规范化加速键。非法时返回 { error }。
 *
 * 与 Go 侧 normalizeShortcut 行为一致：修饰键按 Ctrl、Alt、Shift、Cmd
 * 排序，单字符主键大写，命名键用规范名。
 */
export function normalizeShortcut(
  accel: string,
): NormalizedShortcut | { error: string } {
  const trimmed = accel.trim();
  if (!trimmed) {
    return { error: "快捷键不能为空" };
  }

  const parts = trimmed.split("+");
  if (parts.length < 2) {
    return { error: "至少需要一个修饰键（⌃⌥⇧⌘）" };
  }

  const seen = new Set<Modifier>();
  for (const part of parts.slice(0, -1)) {
    const modifier = lookupModifier(part);
    if (!modifier) {
      return { error: `无法识别的修饰键「${part.trim()}」` };
    }
    seen.add(modifier);
  }
  if (seen.size === 0) {
    return { error: "至少需要一个修饰键（⌃⌥⇧⌘）" };
  }

  const rawKey = parts[parts.length - 1].trim();
  if (!rawKey) {
    return { error: "缺少主键" };
  }
  if (lookupModifier(rawKey)) {
    return { error: "纯修饰键不能作为快捷键" };
  }

  let key: string;
  const named = lookupNamedKey(rawKey);
  if (named) {
    key = named.canonical;
  } else {
    const chars = [...rawKey];
    if (chars.length !== 1 || chars[0] < " " || chars[0] === "\x7f") {
      return { error: `无法识别的按键「${rawKey}」` };
    }
    key = rawKey.toUpperCase();
  }

  const modifiers = MODIFIER_ORDER.map((m) => m.name).filter((m) => seen.has(m));
  const accelerator = [...modifiers, key].join("+");
  return {
    accelerator,
    display: shortcutDisplay(accelerator),
    key: displayKey(key),
    modifiers,
  };
}

/** 主键的展示名。 */
function displayKey(key: string): string {
  const named = lookupNamedKey(key);
  if (named) {
    return named.display;
  }
  return [...key].length === 1 ? key.toUpperCase() : key;
}

/**
 * 把 Wails 加速键转成 macOS 符号形式（⌃⌥⇧⌘ + 键名）。
 * 非法输入原样返回，方便界面显示用户的输入。
 */
export function shortcutDisplay(accel: string): string {
  const trimmed = accel.trim();
  if (!trimmed) {
    return "";
  }
  const parts = trimmed.split("+");
  if (parts.length < 2) {
    return trimmed;
  }

  const seen = new Set<Modifier>();
  for (const part of parts.slice(0, -1)) {
    const modifier = lookupModifier(part);
    if (!modifier) {
      return trimmed;
    }
    seen.add(modifier);
  }

  const key = displayKey(parts[parts.length - 1].trim());
  const symbols = MODIFIER_ORDER.filter((m) => seen.has(m.name))
    .map((m) => m.symbol)
    .join("");
  return `${symbols}${key}`;
}

/** 修饰键符号表，供设置面板单独展示。 */
export const MODIFIER_SYMBOLS = MODIFIER_ORDER;

/** 只按修饰键时的占位（录制中提示用户继续按键）。 */
export function modifiersOnlyDisplay(e: KeyboardEvent): string {
  const seen = new Set<Modifier>();
  if (e.ctrlKey) seen.add("Ctrl");
  if (e.altKey) seen.add("Alt");
  if (e.shiftKey) seen.add("Shift");
  if (e.metaKey) seen.add("Cmd");
  return MODIFIER_ORDER.filter((m) => seen.has(m.name))
    .map((m) => m.symbol)
    .join("");
}

/**
 * 把浏览器 KeyboardEvent 转成 Wails 加速键。
 *
 * 返回 null 表示这次按键不是有效组合（只有修饰键、或者没有修饰键）。
 */
export function keyboardEventToAccelerator(e: KeyboardEvent): string | null {
  const modifiers: Modifier[] = [];
  if (e.ctrlKey) modifiers.push("Ctrl");
  if (e.altKey) modifiers.push("Alt");
  if (e.shiftKey) modifiers.push("Shift");
  if (e.metaKey) modifiers.push("Cmd");
  if (modifiers.length === 0) {
    return null;
  }

  const key = eventKeyToWailsKey(e);
  if (!key) {
    return null;
  }
  const ordered = MODIFIER_ORDER.map((m) => m.name).filter((m) =>
    modifiers.includes(m),
  );
  return [...ordered, key].join("+");
}

/** 把 event.key 映射成 Wails 的主键名。 */
function eventKeyToWailsKey(e: KeyboardEvent): string | null {
  const key = e.key;

  // 单字符（字母、数字、符号）直接用大写形式。
  if ([...key].length === 1) {
    // 空格是命名键。
    if (key === " ") {
      return "Space";
    }
    if (key === "+") {
      return "Plus";
    }
    return key.toUpperCase();
  }

  switch (key) {
    case " ":
      return "Space";
    case "Enter":
      return "Enter";
    case "Escape":
      return "Escape";
    case "Tab":
      return "Tab";
    case "Backspace":
      return "Backspace";
    case "Delete":
      return "Delete";
    case "ArrowLeft":
      return "Left";
    case "ArrowRight":
      return "Right";
    case "ArrowUp":
      return "Up";
    case "ArrowDown":
      return "Down";
    case "Home":
      return "Home";
    case "End":
      return "End";
    case "PageUp":
      return "Page up";
    case "PageDown":
      return "Page down";
    default:
      break;
  }

  // F1 ~ F35。
  const fn = /^F([1-9]|[12]\d|3[0-5])$/.exec(key);
  if (fn) {
    return `F${fn[1]}`;
  }
  // 纯修饰键：等待用户按下真正的主键。
  return null;
}
