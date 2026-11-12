export type StatusTone = "neutral" | "info" | "progress" | "success" | "warning" | "risk" | "muted";

type Props = {
  tone?: string;
  label: string;
};

export function StatusMark({ tone, label }: Props) {
  return (
    <span className={`status-mark status-mark--${statusWordTone(label, tone)}`}>
      <StatusIcon tone={statusWordTone(label, tone)} />
      <span className="status-mark__label">{label}</span>
    </span>
  );
}

/** A clean validation result; lifecycle success keeps its filled dot. */
export function ValidationCheck({ label }: { label: string }) {
  return (
    <span
      className="status-mark"
      style={{ display: "inline-flex", alignItems: "center", gap: 6, fontWeight: "inherit" }}
    >
      <svg
        aria-hidden="true"
        viewBox="0 0 16 16"
        width="16"
        height="16"
        fill="none"
        stroke="var(--success)"
        strokeWidth="1.25"
        style={{ flexShrink: 0 }}
      >
        <circle cx="8" cy="8" r="6.75" />
        <path d="m4.5 8 2.25 2.25 4.75-4.75" strokeLinecap="round" strokeLinejoin="round" />
      </svg>
      <span className="status-mark__label">{label}</span>
    </span>
  );
}

export function normalizeStatusTone(tone: string | undefined): StatusTone {
  switch (tone) {
    case "info":
    case "progress":
    case "success":
    case "warning":
    case "risk":
    case "muted":
      return tone;
    default:
      return "neutral";
  }
}

/** Status words keep one mark across schema tones and lifecycle positions. */
export function statusWordTone(word: string, fallback?: string): StatusTone {
  switch (word.trim().toLowerCase()) {
    case "active":
    case "in progress":
      return "progress";
    case "complete":
    case "completed":
    case "done":
    case "satisfied":
    case "success":
      return "success";
    case "unsaved":
    case "unsaved changes":
      return "warning";
    case "failed":
    case "failure":
    case "risk":
    case "blocked":
      return "risk";
    case "draft":
    case "planned":
    case "proposed":
      return "neutral";
    case "archived":
    case "dropped":
    case "cancelled":
      return "muted";
    default:
      return fallback === "danger" ? "risk" : normalizeStatusTone(fallback);
  }
}

const TONE_COLOR: Record<StatusTone, string> = {
  neutral: "var(--text-faint)",
  info: "var(--info)",
  progress: "var(--info)",
  success: "var(--success)",
  warning: "var(--warning-ink)",
  risk: "var(--accent)",
  muted: "var(--text-faint)",
};

// The host's 9px shape set (DESIGN.md, Status Marks): a hollow ring, a
// half-filled ring for progress, a filled dot for success, a triangle for a
// warning, a disc with an exclamation cut out for risk (never the brand
// diamond), and a dashed ring for a muted value.
function ToneMark({ tone }: { tone: StatusTone }) {
  switch (tone) {
    case "warning":
      return <path d="M4.5 0.5L8.8 8.3H0.2Z" fill="currentColor" />;
    case "risk":
      // A disc with an exclamation cut out, as the host draws it (base.css).
      return (
        <path
          transform="scale(0.75)"
          fillRule="evenodd"
          d="M6 0a6 6 0 1 0 0 12A6 6 0 0 0 6 0ZM5.25 2.5h1.5v4.75h-1.5ZM5.25 8.25h1.5v1.5h-1.5Z"
          fill="currentColor"
        />
      );
    case "success":
      return <circle cx="4.5" cy="4.5" r="4.5" fill="currentColor" />;
    case "info":
    case "progress":
      return (
        <>
          <circle cx="4.5" cy="4.5" r="3.75" fill="none" stroke="currentColor" strokeWidth="1.5" />
          <path d="M4.5 0.75A3.75 3.75 0 0 0 4.5 8.25Z" fill="currentColor" />
        </>
      );
    default:
      return (
        <circle
          cx="4.5"
          cy="4.5"
          r="3.75"
          fill="none"
          stroke="currentColor"
          strokeWidth="1.5"
          strokeDasharray={tone === "muted" ? "1.8 1.4" : undefined}
        />
      );
  }
}

export function StatusIcon({ tone }: { tone: StatusTone }) {
  return (
    <svg
      aria-hidden="true"
      viewBox="0 0 9 9"
      width="9"
      height="9"
      style={{ color: TONE_COLOR[tone], flexShrink: 0 }}
    >
      <ToneMark tone={tone} />
    </svg>
  );
}
