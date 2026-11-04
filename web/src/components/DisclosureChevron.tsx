/**
 * The one disclosure chevron (`.disclosure-chevron` in base.css): a 16px,
 * 1.5-stroke chevron that points right when closed and down when open. Pass
 * `open`, or let a parent `[aria-expanded="true"]` or `details[open] > summary`
 * turn it. A bare icon button around it uses `.disclosure-toggle` (24px).
 */
export function DisclosureChevron({ open, className }: { open?: boolean; className?: string }) {
  return (
    <svg
      className={className ? `disclosure-chevron ${className}` : "disclosure-chevron"}
      data-open={open}
      viewBox="0 0 16 16"
      aria-hidden="true"
      fill="none"
      stroke="currentColor"
      strokeWidth="1.5"
      strokeLinecap="round"
      strokeLinejoin="round"
    >
      <path d="m6 4 4 4-4 4" />
    </svg>
  );
}
