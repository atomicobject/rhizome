import { DisclosureIcon as DisclosureChevron } from "@rhizome/kit";
import { useState, type ReactNode } from "react";

export function SignalExamples({ label, children }: { label: string; children: ReactNode }) {
  const [open, setOpen] = useState(false);

  return (
    <>
      <button
        type="button"
        className="gv-signal-toggle disclosure-toggle"
        aria-expanded={open}
        aria-label={`${open ? "Hide" : "Show"} ${label}`}
        onClick={() => setOpen(!open)}
      >
        <DisclosureChevron open={open} />
        {open ? "Hide" : "Show"} {label}
      </button>
      {open && children}
    </>
  );
}

type SignalAction = { label: string; run: () => void };

export function SignalRow({
  count,
  severity,
  text,
  action,
  children,
}: {
  count: ReactNode;
  severity: "risk" | "warning" | "ok" | "context";
  text: ReactNode;
  action?: SignalAction | null;
  children?: ReactNode;
}) {
  return (
    <li className="gv-sig" data-severity={severity}>
      <span className="gv-sig-count">{count}</span>
      <span className="gv-sig-text">{text}</span>
      {action ? (
        <button type="button" className="gv-link gv-sig-act" onClick={action.run}>
          {action.label}
        </button>
      ) : (
        <span />
      )}
      {children && <div className="gv-sig-detail">{children}</div>}
    </li>
  );
}
