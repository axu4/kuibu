// 周报界面的纯函数工具：周展示、周报日、状态文案与本地偏好。

/** localStorage 里保存周报 tab（框架组长 / SE）的键。 */
export const REPORT_LEVEL_KEY = "kuibu.report.level";

/** localStorage 里保存右侧详情面板宽度的键。 */
export const REPORT_DETAIL_WIDTH_KEY = "kuibu.report.detailWidth";

/** 详情面板默认宽度与可拖动范围（像素）。 */
export const REPORT_DETAIL_DEFAULT_WIDTH = 420;
export const REPORT_DETAIL_MIN_WIDTH = 360;
export const REPORT_DETAIL_MAX_WIDTH = 960;

/** 读取上次的详情面板宽度，越界或读不到时用默认值。 */
export function loadReportDetailWidth(): number {
  try {
    const raw = Number(localStorage.getItem(REPORT_DETAIL_WIDTH_KEY));
    if (Number.isFinite(raw) && raw >= REPORT_DETAIL_MIN_WIDTH && raw <= REPORT_DETAIL_MAX_WIDTH) {
      return raw;
    }
  } catch {
    // 忽略本地存储不可用。
  }
  return REPORT_DETAIL_DEFAULT_WIDTH;
}

/** 保存详情面板宽度。 */
export function saveReportDetailWidth(width: number) {
  try {
    localStorage.setItem(REPORT_DETAIL_WIDTH_KEY, String(Math.round(width)));
  } catch {
    // 忽略本地存储不可用。
  }
}

/** 周报层级。 */
export type ReportLevel = "member" | "lead";

/** 读取上次选择的周报层级，默认框架组长（member）。 */
export function loadReportLevel(): ReportLevel {
  try {
    return localStorage.getItem(REPORT_LEVEL_KEY) === "lead" ? "lead" : "member";
  } catch {
    return "member";
  }
}

/** 保存周报层级。 */
export function saveReportLevel(level: ReportLevel) {
  try {
    localStorage.setItem(REPORT_LEVEL_KEY, level);
  } catch {
    // 忽略本地存储不可用。
  }
}

const WEEKDAY_NAMES = ["周一", "周二", "周三", "周四", "周五", "周六", "周日"];

/** 周报日（1=周一 … 7=周日）的中文名。 */
export function weekdayLabel(n: number): string {
  if (n < 1 || n > 7) {
    return "";
  }
  return WEEKDAY_NAMES[n - 1];
}

/** 层级的中文名。 */
export function levelLabel(level: ReportLevel): string {
  return level === "lead" ? "SE" : "框架组长";
}

/** 把 YYYY-MM-DD 转成 MM/DD。 */
function monthDay(date: string): string {
  const m = /^(\d{4})-(\d{2})-(\d{2})$/.exec(date);
  if (!m) {
    return date;
  }
  return `${Number(m[2])}/${Number(m[3])}`;
}

/** 把周信息拼成「2026-W44 · 10/26–11/01」。 */
export function weekRangeLabel(start: string, end: string): string {
  if (!start || !end) {
    return "";
  }
  return `${monthDay(start)}–${monthDay(end)}`;
}

/** 把 RFC3339 提交时间显示成「周四 18:20」（本地时区）。 */
export function formatSubmittedAt(value: string): string {
  if (!value) {
    return "";
  }
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) {
    return "";
  }
  const hm = `${String(date.getHours()).padStart(2, "0")}:${String(date.getMinutes()).padStart(2, "0")}`;
  return `${WEEKDAY_NAMES[date.getDay()]} ${hm}`;
}

/** 组名展示：空组名统一显示为「未分组」。 */
export function groupLabel(group: string): string {
  return group.trim() || "未分组";
}

/**
 * 一个组的完成进度百分比（0~100），按「应交」算。
 * 已交与迟交都算交上来了；应交为 0 时返回 0。
 */
export function groupProgressPercent(expected: number, submitted: number, late: number): number {
  if (expected <= 0) {
    return 0;
  }
  const done = Math.max(0, Math.min(expected, submitted + late));
  return Math.round((done / expected) * 100);
}

/** 提交状态的中文文案。 */
export function reportStatusLabel(status: string): string {
  switch (status) {
    case "submitted":
      return "已交";
    case "late":
      return "迟交";
    default:
      return "未交";
  }
}

/** 取原文的前几行摘要。 */
export function reportSummary(text: string, lines = 3): string {
  const list = text.split("\n");
  const picked: string[] = [];
  for (let i = 0; i < list.length && picked.length < lines; i++) {
    const line = list[i].trim();
    if (line) {
      picked.push(line);
    }
  }
  return picked.join(" · ") || "（空）";
}

/** 把「别名 / 产品线」输入框里的逗号分隔文本解析成列表。 */
export function parseListInput(raw: string): string[] {
  const out: string[] = [];
  const seen = new Set<string>();
  for (const part of raw.split(/[,，]/)) {
    const item = part.trim();
    if (!item) {
      continue;
    }
    const key = item.toLowerCase();
    if (seen.has(key)) {
      continue;
    }
    seen.add(key);
    out.push(item);
  }
  return out;
}

/** 把列表拼接回逗号分隔文本。 */
export function joinListInput(list: string[] | null | undefined): string {
  return (list ?? []).join("，");
}

/** 今天的本地日期 YYYY-MM-DD。 */
export function todayStr(): string {
  const d = new Date();
  return `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, "0")}-${String(d.getDate()).padStart(2, "0")}`;
}

/** 把 YYYY-MM-DD 解析成本地当天 00:00；格式不对返回 null。 */
function parseDay(value: string): Date | null {
  const m = /^(\d{4})-(\d{2})-(\d{2})$/.exec(value);
  if (!m) {
    return null;
  }
  return new Date(Number(m[1]), Number(m[2]) - 1, Number(m[3]));
}

/** 剩余时长的中文描述：不足一天时用「N 小时 M 分」或「N 分钟」。 */
function humanize(diff: number): string {
  const minute = 60 * 1000;
  const hour = 60 * minute;
  if (diff >= hour) {
    const h = Math.floor(diff / hour);
    const m = Math.floor((diff % hour) / minute);
    return m > 0 ? `${h} 小时 ${m} 分` : `${h} 小时`;
  }
  return `${Math.max(1, Math.floor(diff / minute))} 分钟`;
}

/**
 * 截止提醒：截止点 = 周报日当天 24:00。返回「还有 N 天截止」「剩 3 小时」
 * 「已过截止」这类文案与是否已过截止；起始日期不可解析时返回 null。
 */
export function deadlineInfo(
  start: string,
  reportDay: number,
  now = new Date(),
): { overdue: boolean; text: string } | null {
  const monday = parseDay(start);
  if (!monday) {
    return null;
  }
  const day = reportDay >= 1 && reportDay <= 7 ? reportDay : 7;
  const deadline = new Date(monday.getFullYear(), monday.getMonth(), monday.getDate() + day);
  const diff = deadline.getTime() - now.getTime();
  if (diff <= 0) {
    const days = Math.floor(-diff / (24 * 60 * 60 * 1000));
    return { overdue: true, text: days >= 1 ? `已过截止 ${days} 天` : "已过截止" };
  }
  const days = Math.floor(diff / (24 * 60 * 60 * 1000));
  if (days >= 1) {
    return { overdue: false, text: `还有 ${days} 天截止` };
  }
  return { overdue: false, text: `剩 ${humanize(diff)}` };
}

/**
 * 一行右侧的时间说明：交了的显示提交时间，没交的过了截止线显示「逾期」。
 * 截止前没交不加字，因为「未交」本身已经说明了。
 */
export function rowHint(status: string, submittedAt: string, deadlineOverdue: boolean): string {
  if (status === "missing") {
    return deadlineOverdue ? "逾期" : "";
  }
  return formatSubmittedAt(submittedAt);
}
