// 待办界面的纯函数工具：日期展示、状态文案与本地偏好。
//
// 截止日期的语义（"今天/明天/周X"）统一在这里算，避免散落在组件里。

/** localStorage 里保存主视图（笔记/待办）的键。 */
export const VIEW_KEY = "kuibu.view";

/** localStorage 里保存随手记小窗模式的键。 */
export const QUICKNOTE_MODE_KEY = "kuibu.quicknote.mode";

/** 主视图。 */
export type MainView = "notes" | "todo" | "report";

/** 读取上次的主视图，默认笔记。 */
export function loadMainView(): MainView {
  try {
    const value = localStorage.getItem(VIEW_KEY);
    return value === "todo" || value === "report" ? value : "notes";
  } catch {
    return "notes";
  }
}

/** 保存主视图。 */
export function saveMainView(view: MainView) {
  try {
    localStorage.setItem(VIEW_KEY, view);
  } catch {
    // 忽略本地存储不可用。
  }
}

/** 状态的中文文案。 */
export function statusLabel(status: string): string {
  switch (status) {
    case "doing":
      return "进行中";
    case "done":
      return "已完成";
    case "canceled":
      return "已取消";
    default:
      return "未开始";
  }
}

/** 状态在界面上的弱化样式名。 */
export function statusClass(status: string): string {
  if (status === "done" || status === "canceled") {
    return " is-closed";
  }
  if (status === "doing") {
    return " is-doing";
  }
  return "";
}

/** 把 YYYY-MM-DD 解析成本地时间（避免时区偏移）。 */
function parseDate(date: string): Date | null {
  const m = /^(\d{4})-(\d{2})-(\d{2})$/.exec(date);
  if (!m) {
    return null;
  }
  return new Date(Number(m[1]), Number(m[2]) - 1, Number(m[3]));
}

/** 两个日期相差的天数（按自然日）。 */
function dayDiff(from: Date, to: Date): number {
  const a = new Date(from.getFullYear(), from.getMonth(), from.getDate()).getTime();
  const b = new Date(to.getFullYear(), to.getMonth(), to.getDate()).getTime();
  return Math.round((b - a) / 86400000);
}

const WEEKDAYS = ["周日", "周一", "周二", "周三", "周四", "周五", "周六"];

/**
 * 把截止日期格式化成列表里的短文案：
 * 今天 / 明天 / 昨天 / 周X（7 天内）/ MM-DD（今年）/ YYYY-MM-DD。
 * 有时间时追加 HH:MM。
 */
export function formatDue(due: string, now: Date = new Date()): string {
  if (!due) {
    return "";
  }
  const datePart = due.slice(0, 10);
  const timePart = due.length > 10 ? due.slice(11, 16) : "";
  const date = parseDate(datePart);
  if (!date) {
    return due;
  }
  const diff = dayDiff(now, date);
  let label: string;
  if (diff === 0) {
    label = "今天";
  } else if (diff === 1) {
    label = "明天";
  } else if (diff === -1) {
    label = "昨天";
  } else if (diff > 1 && diff <= 6) {
    label = WEEKDAYS[date.getDay()];
  } else if (diff < 0 && diff >= -6) {
    label = WEEKDAYS[date.getDay()];
  } else if (date.getFullYear() === now.getFullYear()) {
    label = `${pad(date.getMonth() + 1)}-${pad(date.getDate())}`;
  } else {
    label = `${date.getFullYear()}-${pad(date.getMonth() + 1)}-${pad(date.getDate())}`;
  }
  return timePart ? `${label} ${timePart}` : label;
}

/** 详情面板里显示的完整截止：YYYY-MM-DD 周X HH:MM。 */
export function formatDueLong(due: string): string {
  if (!due) {
    return "";
  }
  const date = parseDate(due.slice(0, 10));
  const time = due.length > 10 ? due.slice(11, 16) : "";
  if (!date) {
    return due;
  }
  const base = `${date.getFullYear()}-${pad(date.getMonth() + 1)}-${pad(date.getDate())} ${WEEKDAYS[date.getDay()]}`;
  return time ? `${base} ${time}` : base;
}

/** 两位补零。 */
function pad(n: number): string {
  return n < 10 ? `0${n}` : String(n);
}

/** 把链接显示成「域名+路径」，未填标题时使用。 */
export function linkDisplay(url: string, title: string): string {
  if (title) {
    return title;
  }
  try {
    const parsed = new URL(url);
    const path = parsed.pathname === "/" ? "" : parsed.pathname;
    return `${parsed.host}${path}${parsed.search}`;
  } catch {
    return url;
  }
}

/** 把绑定调用抛出的错误整理成一行可读文本。 */
export function readableError(err: unknown): string {
  const text = err instanceof Error ? err.message : String(err);
  return text.replace(/^Error:\s*/, "");
}
