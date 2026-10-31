import type { ReactNode } from "react";

// One 16px, 1.5-stroke icon set for the Tasks page.

function Icon({ children }: { children: ReactNode }) {
  return (
    <svg
      className="task-icon"
      viewBox="0 0 16 16"
      aria-hidden="true"
      fill="none"
      stroke="currentColor"
      strokeWidth="1.5"
      strokeLinecap="round"
      strokeLinejoin="round"
    >
      {children}
    </svg>
  );
}

export const ChevronIcon = () => (
  <Icon>
    <path d="m6 4 4 4-4 4" />
  </Icon>
);

export const BackIcon = () => (
  <Icon>
    <path d="M10 4 6 8l4 4" />
  </Icon>
);

export const TerminalIcon = () => (
  <Icon>
    <rect x="2" y="3" width="12" height="10" rx="1.5" />
    <path d="m5 6.5 2 1.5-2 1.5M8.5 10H11" />
  </Icon>
);

export const ToolIcon = () => (
  <Icon>
    <path d="M9.5 2.5a3 3 0 0 0-2.9 3.8L2.8 10.1a1.4 1.4 0 0 0 2 2l3.8-3.8a3 3 0 0 0 3.8-3.8l-1.8 1.8-1.6-.4-.4-1.6Z" />
  </Icon>
);

export const FileIcon = () => (
  <Icon>
    <path d="M4 2h5l3 3v9H4Z" />
    <path d="M9 2v3h3M6 9h4M6 11.5h2.5" />
  </Icon>
);

export const ThinkingIcon = () => (
  <Icon>
    <path d="M3 7.5a5 4.5 0 1 1 3 4.1L3.5 13l.5-2.4A4.3 4.3 0 0 1 3 7.5Z" />
    <path d="M6 7.5h.01M8 7.5h.01M10 7.5h.01" />
  </Icon>
);

export const LogIcon = () => (
  <Icon>
    <path d="M3 3.5h10M3 6.5h10M3 9.5h7M3 12.5h5" />
  </Icon>
);

export const RetryIcon = () => (
  <Icon>
    <path d="M13 8a5 5 0 1 1-1.5-3.5M13 2.5v3h-3" />
  </Icon>
);

export const CopyIcon = () => (
  <Icon>
    <rect x="5.5" y="5.5" width="8" height="8" rx="1.5" />
    <path d="M10.5 5.5V3.5A1 1 0 0 0 9.5 2.5h-6a1 1 0 0 0-1 1v6a1 1 0 0 0 1 1h2" />
  </Icon>
);

export const PauseIcon = () => (
  <Icon>
    <path d="M5.5 3.5v9M10.5 3.5v9" />
  </Icon>
);

export const PlayIcon = () => (
  <Icon>
    <path d="M5 3.5v9l7.5-4.5Z" />
  </Icon>
);
