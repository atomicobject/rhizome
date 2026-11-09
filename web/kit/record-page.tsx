// The record-page shell (SPEC-0136 R7): one structure for an experience's
// record pages, so they share layout and states without sharing a look. Its
// title row follows the host's view header (title, then facts, then tools on
// the right) on the same tokens. It sets structure and spacing only and
// fetches nothing: the page passes its own reads' status.
import { useId, type ReactNode } from "react";

import { SectionBody, type SectionStatus } from "./layout";
import { cn } from "./lib/utils";

export type RecordPageProps = {
  /** The record's kind, usually a `RecordChip` or `StatusMark`, before the title. */
  kind?: ReactNode;
  /** Shown at once, before any read finishes: the page's subject is already known. */
  title: ReactNode;
  /** One facts line under the title, usually `<Facts>`. */
  facts?: ReactNode;
  /** Controls at the right of the title row. */
  actions?: ReactNode;
  /** The body's reads, from `sectionStatus(...)`: a loading line, a failure with retry, or the body. */
  status?: SectionStatus;
  /** Names the body's data in its loading and failure lines. */
  what?: string;
  /**
   * Set when the body shows less than the whole scope, such as a capped read
   * (`typedRootCoverage`, `truncation`): a notice above the body says what is missing.
   */
  partial?: ReactNode;
  /** A side column beside the body on wide frames and below it on narrow ones. */
  aside?: ReactNode;
  className?: string;
  children?: ReactNode | (() => ReactNode);
};

/** A record page: header slots, a body with loading, partial, and error states, and an optional aside. */
export function RecordPage({
  kind,
  title,
  facts,
  actions,
  status,
  what = "this record",
  partial,
  aside,
  className,
  children,
}: RecordPageProps) {
  const titleId = useId();

  return (
    <article aria-labelledby={titleId} className={cn("flex min-w-0 flex-col", className)}>
      <header className="flex min-w-0 flex-col gap-1.5 border-b border-[var(--border)] pb-3">
        <div className="flex min-h-9 min-w-0 items-center gap-[var(--sp-4)]">
          {kind && <span className="flex flex-none items-center">{kind}</span>}
          <h1
            id={titleId}
            className="m-0 min-w-0 truncate font-[family-name:var(--font-sans)] text-[length:var(--fs-xl)] leading-[1.2] font-bold tracking-[-0.01em] text-[var(--text)]"
          >
            {title}
          </h1>
          {actions && <div className="ml-auto flex flex-none items-center gap-2">{actions}</div>}
        </div>
        {facts}
      </header>
      {partial && (
        <p
          role="status"
          data-record-partial=""
          className="m-0 mt-3 text-[length:var(--fs-sm)] text-[var(--text-muted)]"
        >
          {partial}
        </p>
      )}
      <div
        className={cn(
          "mt-5 grid min-w-0 gap-6",
          aside && "md:grid-cols-[minmax(0,1fr)_minmax(200px,280px)]",
        )}
      >
        <div className="min-w-0">
          <SectionBody status={status} what={what}>
            {children}
          </SectionBody>
        </div>
        {aside && <aside className="min-w-0">{aside}</aside>}
      </div>
    </article>
  );
}
