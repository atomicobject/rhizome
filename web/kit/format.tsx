/**
 * The first word every label shares, with its trailing space, so a group page
 * can drop it ("Delivery specs" and "Delivery efforts" share "Delivery ").
 * Empty unless there are at least two labels and each has more after the word.
 */
export function sharedLabelPrefix(labels: readonly string[]): string {
  if (labels.length < 2) return "";
  const first = labels[0].split(" ")[0];

  return labels.every((label) => label.startsWith(`${first} `) && label.length > first.length + 1)
    ? `${first} `
    : "";
}

const capitalize = (text: string) => text.charAt(0).toUpperCase() + text.slice(1);

/**
 * A type's schema label, plural when `plural` is set or `count` is not 1, with
 * a shared `prefix` dropped when the remainder is not empty.
 */
export function typeLabel(
  labels: { label: string; pluralLabel: string },
  options: { count?: number; plural?: boolean; prefix?: string } = {},
): string {
  const plural = options.plural ?? (options.count !== undefined && options.count !== 1);
  const label = plural ? labels.pluralLabel : labels.label;
  const prefix = options.prefix ?? "";

  return prefix && label.startsWith(prefix) && label.length > prefix.length
    ? capitalize(label.slice(prefix.length))
    : label;
}

const MINUTE = 60_000;

const HOUR = 60 * MINUTE;

const DAY = 24 * HOUR;

function compactAge(time: Date, now: Date) {
  const age = Math.max(0, now.getTime() - time.getTime());

  if (age < MINUTE) return { text: "now", long: "just now" };

  const spoken = new Intl.RelativeTimeFormat(undefined, { numeric: "auto" });

  const unit = (size: number, suffix: string, name: Intl.RelativeTimeFormatUnit) => {
    const amount = Math.round(age / size);

    return { text: `${amount}${suffix}`, long: spoken.format(-amount, name) };
  };

  if (age < HOUR) return unit(MINUTE, "m", "minute");

  if (age < 48 * HOUR) return unit(HOUR, "h", "hour");

  if (age < 60 * DAY) return unit(DAY, "d", "day");

  const text = time.toLocaleDateString(undefined, {
    month: "short",
    day: "numeric",
    year: time.getFullYear() === now.getFullYear() ? undefined : "numeric",
  });

  return { text, long: text };
}

/**
 * How long ago `value` was, compactly ("5m", "3h", "12d", then a date). Screen
 * readers hear the age and the exact time, which is also the tooltip. It does
 * not tick: the age is computed on each render, and views re-render as vault
 * events refresh their queries. Renders nothing for an invalid time.
 */
export function RelativeTime({
  value,
  now,
  className,
}: {
  value: string | number | Date;
  /** The comparison time; defaults to the time of render. */
  now?: Date;
  className?: string;
}) {
  const time = new Date(value);

  if (Number.isNaN(time.getTime())) return null;
  const { text, long } = compactAge(time, now ?? new Date());
  const exact = time.toLocaleString(undefined, { dateStyle: "medium", timeStyle: "short" });

  return (
    <time dateTime={time.toISOString()} title={exact} className={className}>
      <span aria-hidden="true">{text}</span>
      <span className="sr-only">{`${long}, ${exact}`}</span>
    </time>
  );
}
