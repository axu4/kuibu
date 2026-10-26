// 界面里用到的线性图标，统一 16px 视框、1.5 描边，颜色跟随 currentColor。

const PATHS = {
  note: "M4 2.75h5.5L12.25 5.5v7.75H4z M9.25 2.75v3h3 M6 8.25h4 M6 10.75h3",
  todo: "M2.75 8.25l2.5 2.5L10.5 5.5 M9 3.5H13.25 M11.5 8.25h1.75 M7.5 12.75h5.75",
  plus: "M8 3.25v9.5 M3.25 8h9.5",
  more: "M3.5 8h.01 M8 8h.01 M12.5 8h.01",
  chevron: "M6 4l4 4-4 4",
  chevronDown: "M4 6l4 4 4-4",
  close: "M4 4l8 8 M12 4l-8 8",
  search: "M7 11.75a4.75 4.75 0 1 0 0-9.5 4.75 4.75 0 0 0 0 9.5z M10.5 10.5l3 3",
  settings:
    "M8 10a2 2 0 1 0 0-4 2 2 0 0 0 0 4z M8 1.75v1.5 M8 12.75v1.5 M1.75 8h1.5 M12.75 8h1.5 M3.6 3.6l1.05 1.05 M11.35 11.35l1.05 1.05 M3.6 12.4l1.05-1.05 M11.35 4.65l1.05-1.05",
  folder: "M2.25 4.25c0-.55.45-1 1-1h3l1.5 1.5h5c.55 0 1 .45 1 1v6c0 .55-.45 1-1 1h-9.5c-.55 0-1-.45-1-1z",
  trash: "M3 4.5h10 M6.25 4.5V3h3.5v1.5 M4.5 4.5l.5 8.75h6l.5-8.75",
  edit: "M10.5 2.75l2.75 2.75L6 12.75H3.25V10z",
  code: "M5.5 4.5L2 8l3.5 3.5 M10.5 4.5L14 8l-3.5 3.5",
  calendar: "M3 4h10v9H3z M3 6.75h10 M5.5 2.5v2.5 M10.5 2.5v2.5",
  tag: "M2.75 2.75h5l5.5 5.5-5 5-5.5-5.5z M5.5 5.5h.01",
  link: "M7 9a2.5 2.5 0 0 0 3.5 0l2-2a2.5 2.5 0 0 0-3.5-3.5l-.75.75 M9 7a2.5 2.5 0 0 0-3.5 0l-2 2A2.5 2.5 0 0 0 7 12.5l.75-.75",
  paperclip: "M12.5 7.5l-4.75 4.75a3 3 0 0 1-4.25-4.25L8.25 3.25a2 2 0 0 1 2.75 2.75L6.5 10.5a1 1 0 0 1-1.5-1.5L9 5",
  flag: "M3.5 13.5V2.75 M3.5 3h8l-1.5 2.75L11.5 8.5h-8",
  bolt: "M9 1.75L3.75 9h4l-1 5.25L12.25 7h-4z",
  inbox: "M2.25 9.25l1.5-6h8.5l1.5 6v3.5h-11.5z M2.25 9.25h3.25l.75 1.5h3.5l.75-1.5h3.25",
  sun: "M8 10.75a2.75 2.75 0 1 0 0-5.5 2.75 2.75 0 0 0 0 5.5z M8 1.5v1.25 M8 13.25v1.25 M1.5 8h1.25 M13.25 8h1.25 M3.4 3.4l.9.9 M11.7 11.7l.9.9 M3.4 12.6l.9-.9 M11.7 4.3l.9-.9",
  week: "M3 4h10v9H3z M3 6.75h10 M5.5 2.5v2.5 M10.5 2.5v2.5 M5.5 9.25h5",
  list: "M5.75 4.5h7.5 M5.75 8h7.5 M5.75 11.5h7.5 M2.75 4.5h.01 M2.75 8h.01 M2.75 11.5h.01",
  check: "M3.5 8.25l3 3 6-6.5",
  checkCircle: "M8 14a6 6 0 1 0 0-12 6 6 0 0 0 0 12z M5.5 8.25l1.75 1.75 3.25-3.5",
  external: "M9.5 2.75h3.75V6.5 M13.25 2.75L7.5 8.5 M11.5 9.5v3.75h-8.75V4.5H6.5",
  file: "M4 2.75h5.5L12.25 5.5v7.75H4z M9.25 2.75v3h3",
  arrowUp: "M8 13V3 M4 7l4-4 4 4",
  refresh: "M13.5 8a5.5 5.5 0 1 1-1.5-4 M13.5 1.75V5.5H10.75",
  download: "M8 2.5v7 M4.5 7L8 10.5 11.5 7 M2.75 13.25h10.5",
} as const;

export type IconName = keyof typeof PATHS;

interface IconProps {
  name: IconName;
  size?: number;
  className?: string;
}

export function Icon({ name, size = 16, className }: IconProps) {
  return (
    <svg
      className={`icon${className ? ` ${className}` : ""}`}
      width={size}
      height={size}
      viewBox="0 0 16 16"
      fill="none"
      stroke="currentColor"
      strokeWidth={1.5}
      strokeLinecap="round"
      strokeLinejoin="round"
      aria-hidden="true"
    >
      <path d={PATHS[name]} />
    </svg>
  );
}
