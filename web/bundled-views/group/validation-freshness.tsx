import { useValidationSummaries } from "@rhizome/kit";
import { useQuery } from "@tanstack/react-query";
import { isJsonObject, isNumber, isString, type JsonValue } from "./api.ts";
import { getJSON } from "./load.ts";

/** Published validation time, matching Problems' snapshot-first selection. */
function checkedTime(value: JsonValue): Date | null {
  if (!isJsonObject(value)) return null;

  const finished = isJsonObject(value.snapshot) ? value.snapshot.finishedAt : null;

  const date = isNumber(finished)
    ? new Date(finished < 1e12 ? finished * 1000 : finished)
    : isString(value.computedAt)
      ? new Date(value.computedAt)
      : null;

  return date && !Number.isNaN(date.getTime()) ? date : null;
}

/** Keep Problems' compact age format inside the bundled-view boundary. */
export function checkedAge(date: Date, now = Date.now()): string {
  const seconds = Math.max(0, Math.round((now - date.getTime()) / 1000));

  if (seconds < 60) return "just now";
  const minutes = Math.round(seconds / 60);

  if (minutes < 60) return `${minutes}m ago`;
  const hours = Math.round(minutes / 60);

  if (hours < 48) return `${hours}h ago`;

  return date.toLocaleString(undefined, {
    month: "short",
    day: "numeric",
    hour: "numeric",
    minute: "2-digit",
  });
}

const NO_SCOPES: Parameters<typeof useValidationSummaries>[0] = [];

export function ValidationFreshness() {
  // The kit's envelope follows validation events; its published generation
  // keys this read so a new run refreshes the time.
  const { generation } = useValidationSummaries(NO_SCOPES);

  const { data } = useQuery({
    queryKey: ["group-views", "validation-freshness", generation],
    queryFn: ({ signal }) => getJSON("/api/v2/validate", signal),
  });

  const time = data === undefined ? null : checkedTime(data);

  return time ? <span className="gv-sub">Checked {checkedAge(time)}</span> : null;
}
