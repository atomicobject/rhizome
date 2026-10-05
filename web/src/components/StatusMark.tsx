export type StatusTone = "neutral" | "info" | "progress" | "success" | "warning" | "risk" | "muted";

type Props = {
  tone?: string;
  label: string;
};

export function StatusMark({ tone, label }: Props) {
  return (
    <span className={`status-mark status-mark--${normalizeStatusTone(tone)}`}>
      <span className="status-mark__shape" aria-hidden="true" />
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
