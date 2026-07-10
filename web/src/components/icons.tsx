import type { ReactNode } from "react";

// Line-art icon set matching the Paper design. All use currentColor so they inherit
// the surrounding text color (active/inactive nav states, buttons, etc.).
type P = { size?: number; className?: string; strokeWidth?: number };

function Svg({ size = 16, viewBox, className, children }: P & { viewBox: string; children: ReactNode }) {
  return (
    <svg
      width={size}
      height={size}
      viewBox={viewBox}
      fill="none"
      stroke="currentColor"
      strokeLinecap="round"
      strokeLinejoin="round"
      className={`shrink-0 ${className ?? ""}`}
    >
      {children}
    </svg>
  );
}

export const IconDashboard = (p: P) => (
  <Svg {...p} viewBox="0 0 18 18">
    <g strokeWidth={p.strokeWidth ?? 1.4}>
      <rect x="2.5" y="2.5" width="5.5" height="5.5" rx="1" />
      <rect x="2.5" y="10.5" width="5.5" height="5" rx="1" />
      <rect x="10.5" y="2.5" width="5" height="5" rx="1" />
      <rect x="10.5" y="9.5" width="5" height="6" rx="1" />
    </g>
  </Svg>
);

export const IconTarget = (p: P) => (
  <Svg {...p} viewBox="0 0 18 18">
    <circle cx="9" cy="9" r="6.25" strokeWidth={p.strokeWidth ?? 1.5} />
    <circle cx="9" cy="9" r="2" fill="currentColor" stroke="none" />
  </Svg>
);

export const IconFlag = (p: P) => (
  <Svg {...p} viewBox="0 0 18 18">
    <g strokeWidth={p.strokeWidth ?? 1.4}>
      <path d="M4.5 2.5V15.5" />
      <path d="M4.5 3.5H13.5L11.5 6.5L13.5 9.5H4.5" />
    </g>
  </Svg>
);

export const IconTag = (p: P) => (
  <Svg {...p} viewBox="0 0 18 18">
    <path d="M8.7 2.7L15.3 9.3L9.3 15.3L2.7 8.7V2.7H8.7Z" strokeWidth={p.strokeWidth ?? 1.4} />
    <circle cx="6" cy="6" r="1.1" fill="currentColor" stroke="none" />
  </Svg>
);

export const IconEye = (p: P) => (
  <Svg viewBox="0 0 24 24" {...p}>
    <path strokeWidth={p.strokeWidth ?? 1.8} d="M2.5 12S6 5.5 12 5.5 21.5 12 21.5 12 18 18.5 12 18.5 2.5 12 2.5 12Z" />
    <circle cx="12" cy="12" r="3" strokeWidth={p.strokeWidth ?? 1.8} />
  </Svg>
);

export const IconSearch = (p: P) => (
  <Svg {...p} viewBox="0 0 16 16">
    <circle cx="7" cy="7" r="4.5" strokeWidth={p.strokeWidth ?? 1.4} />
    <path d="M10.5 10.5L14 14" strokeWidth={p.strokeWidth ?? 1.4} />
  </Svg>
);

export const IconPlus = (p: P) => (
  <Svg {...p} viewBox="0 0 16 16">
    <path d="M8 3V13M3 8H13" strokeWidth={p.strokeWidth ?? 1.7} />
  </Svg>
);

export const IconChevronDown = (p: P) => (
  <Svg {...p} viewBox="0 0 14 14">
    <path d="M4 5.5L7 8.5L10 5.5" strokeWidth={p.strokeWidth ?? 1.5} />
  </Svg>
);

export const IconChevronRight = (p: P) => (
  <Svg {...p} viewBox="0 0 14 14">
    <path d="M5.5 3L9.5 7L5.5 11" strokeWidth={p.strokeWidth ?? 1.4} />
  </Svg>
);

export const IconKebab = (p: P) => (
  <Svg {...p} viewBox="0 0 16 16">
    <g fill="currentColor" stroke="none">
      <circle cx="3.5" cy="8" r="1.2" />
      <circle cx="8" cy="8" r="1.2" />
      <circle cx="12.5" cy="8" r="1.2" />
    </g>
  </Svg>
);

export const IconPencil = (p: P) => (
  <Svg {...p} viewBox="0 0 14 14">
    <path d="M9.5 2.2L11.8 4.5L5 11.3L2.2 11.8L2.7 9L9.5 2.2Z" strokeWidth={p.strokeWidth ?? 1.3} />
  </Svg>
);

export const IconArrowDown = (p: P) => (
  <Svg {...p} viewBox="0 0 14 14">
    <path d="M7 3V11M3.5 7.5L7 11L10.5 7.5" strokeWidth={p.strokeWidth ?? 1.5} />
  </Svg>
);

export const IconLabelLines = (p: P) => (
  <Svg {...p} viewBox="0 0 14 14">
    <path d="M2 3.5h10M4 7h6M5.5 10.5h3" strokeWidth={p.strokeWidth ?? 1.3} />
  </Svg>
);

export const IconMilestone = (p: P) => (
  <Svg {...p} viewBox="0 0 16 16">
    <g strokeWidth={p.strokeWidth ?? 1.3}>
      <path d="M4 2.5V14" />
      <path d="M4 3.2H12L10.3 5.8L12 8.4H4" />
    </g>
  </Svg>
);

export const IconBranch = (p: P) => (
  <Svg {...p} viewBox="0 0 14 14">
    <g strokeWidth={p.strokeWidth ?? 1.4}>
      <path d="M4 2.5V11.5" />
      <circle cx="4" cy="2.5" r="1.5" />
      <circle cx="4" cy="11.5" r="1.5" />
      <path d="M10 11.5V6.5C10 5 9 4.5 7.5 4.5H4" />
      <circle cx="10" cy="11.5" r="1.5" />
    </g>
  </Svg>
);

export const IconCommit = (p: P) => (
  <Svg {...p} viewBox="0 0 14 14">
    <circle cx="7" cy="7" r="2.4" strokeWidth={p.strokeWidth ?? 1.4} />
    <path d="M2 7H4.6M9.4 7H12" strokeWidth={p.strokeWidth ?? 1.4} />
  </Svg>
);

export const IconLogout = (p: P) => (
  <Svg {...p} viewBox="0 0 18 18">
    <g strokeWidth={p.strokeWidth ?? 1.4}>
      <path d="M8 2.5H4C3.2 2.5 2.5 3.2 2.5 4V14C2.5 14.8 3.2 15.5 4 15.5H8" />
      <path d="M11.5 5.5L15 9L11.5 12.5" />
      <path d="M6.5 9H15" />
    </g>
  </Svg>
);

export const IconBoard = (p: P) => (
  <Svg {...p} viewBox="0 0 18 18">
    <g strokeWidth={p.strokeWidth ?? 1.4}>
      <rect x="2.5" y="2.5" width="3.6" height="13" rx="1" />
      <rect x="7.2" y="2.5" width="3.6" height="9" rx="1" />
      <rect x="11.9" y="2.5" width="3.6" height="11" rx="1" />
    </g>
  </Svg>
);

export const IconSun = (p: P) => (
  <Svg {...p} viewBox="0 0 18 18">
    <circle cx="9" cy="9" r="3.2" strokeWidth={p.strokeWidth ?? 1.4} />
    <path
      d="M9 1.5v2M9 14.5v2M16.5 9h-2M3.5 9h-2M14.3 3.7l-1.4 1.4M5.1 12.9l-1.4 1.4M14.3 14.3l-1.4-1.4M5.1 5.1L3.7 3.7"
      strokeWidth={p.strokeWidth ?? 1.3}
    />
  </Svg>
);

export const IconMoon = (p: P) => (
  <Svg {...p} viewBox="0 0 18 18">
    <path d="M15 10.5A6 6 0 1 1 7.5 3a4.8 4.8 0 0 0 7.5 7.5Z" strokeWidth={p.strokeWidth ?? 1.4} />
  </Svg>
);

export const IconGear = (p: P) => (
  <Svg {...p} viewBox="0 0 18 18">
    <circle cx="9" cy="9" r="2.4" strokeWidth={p.strokeWidth ?? 1.4} />
    <path
      d="M9 1.8v2M9 14.2v2M16.2 9h-2M3.8 9h-2M14.1 3.9l-1.4 1.4M5.3 12.7l-1.4 1.4M14.1 14.1l-1.4-1.4M5.3 5.3L3.9 3.9"
      strokeWidth={p.strokeWidth ?? 1.3}
    />
  </Svg>
);

export const IconMark = (p: P) => (
  <Svg {...p} viewBox="0 0 20 20">
    <circle cx="10" cy="10" r="6.25" strokeWidth={p.strokeWidth ?? 1.5} />
    <path d="M10 1.5V6M10 14v4.5M1.5 10H6M14 10h4.5" strokeWidth={p.strokeWidth ?? 1.5} />
  </Svg>
);
