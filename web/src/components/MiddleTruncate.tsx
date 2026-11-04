/**
 * Truncates in the middle so an identifier keeps its distinguishing end:
 * the head shrinks behind an ellipsis while the last `tail` characters stay.
 */
export function MiddleTruncate({
  text,
  tail = 20,
  className = "",
}: {
  text: string;
  tail?: number;
  className?: string;
}) {
  const cut = Math.max(0, text.length - tail);

  return (
    <span className={`middle-truncate ${className}`.trim()} title={text}>
      <span className="middle-truncate__head">{text.slice(0, cut)}</span>
      <span className="middle-truncate__tail">{text.slice(cut)}</span>
    </span>
  );
}
