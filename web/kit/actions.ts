// Module actions (SPEC-0131) for custom views: a read polls, a command runs once
// and refreshes every action read.
import { useMutation, useQuery, useQueryClient, type UseQueryOptions } from "@tanstack/react-query";

import { fetchJSON } from "../src/api/client";
import { isJsonObject, isString, type JsonObject } from "../src/api/parse";

/** How an action ended: `refused` is a deliberate no, `error` is a failure. */
export type ActionStatus = "ok" | "refused" | "error";

/** A refused or failed action, including an unknown action or invalid input. */
export class ActionError extends Error {
  readonly status: Exclude<ActionStatus, "ok">;
  readonly reason?: string;
  readonly stderrTail?: string;

  constructor(
    id: string,
    { status, reason, stderrTail }: Pick<ActionError, "status" | "reason" | "stderrTail">,
  ) {
    super(reason || `Action ${id} ${status === "refused" ? "was refused" : "failed"}`);
    this.name = "ActionError";
    this.status = status;
    this.reason = reason;
    this.stderrTail = stderrTail;
  }
}

const POLL_MS = 15_000;

/** Run an action by `<module>/<name>` id; resolves to its ok result. */
export async function runAction<TResult = unknown>(
  id: string,
  input: JsonObject = {},
  signal?: AbortSignal,
): Promise<TResult> {
  const [module, name, ...rest] = id.split("/");

  if (!module || !name || rest.length)
    throw new ActionError(id, {
      status: "error",
      reason: `Invalid action id ${JSON.stringify(id)}: expected <module>/<name>`,
    });

  let envelope: unknown;

  try {
    envelope = await fetchJSON<unknown>(
      `/api/v1/actions/${encodeURIComponent(module)}/${encodeURIComponent(name)}`,
      {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify(input),
        signal,
      },
    );
  } catch (error) {
    if (error instanceof DOMException && error.name === "AbortError") throw error;

    // HTTP 400 and 404 carry the server's reason; a network failure its own.
    throw new ActionError(id, {
      status: "error",
      reason: error instanceof Error ? error.message : String(error),
    });
  }

  if (!isJsonObject(envelope) || !["ok", "refused", "error"].includes(String(envelope.status)))
    throw new ActionError(id, { status: "error", reason: "Invalid action response" });

  if (envelope.status === "ok") {
    // SAFETY: TResult is the caller's description of its own action's result.
    return envelope.result as TResult;
  }

  throw new ActionError(id, {
    status: envelope.status === "refused" ? "refused" : "error",
    reason: isString(envelope.reason) ? envelope.reason : undefined,
    stderrTail: isString(envelope.stderrTail) ? envelope.stderrTail : undefined,
  });
}

/**
 * Read an action's result. It polls every 15 s while the page is visible, and
 * a refused action is not retried.
 */
export function useActionQuery<TResult = unknown>(
  id: string,
  input: JsonObject = {},
  options?: Omit<UseQueryOptions<TResult, ActionError>, "queryKey" | "queryFn">,
) {
  return useQuery<TResult, ActionError>({
    refetchInterval: POLL_MS,
    // TanStack pauses the interval while the document is hidden.
    refetchIntervalInBackground: false,
    retry: (failures, error) => error.status !== "refused" && failures < 1,
    ...options,
    queryKey: ["action", id, input],
    queryFn: ({ signal }) => runAction<TResult>(id, input, signal),
  });
}

/** Run an action on demand; on ok, every `useActionQuery` refetches. */
export function useActionCommand<TResult = unknown, TInput extends JsonObject = JsonObject>(
  id: string,
) {
  const queryClient = useQueryClient();

  return useMutation<TResult, ActionError, TInput>({
    mutationFn: (input) => runAction<TResult>(id, input),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: ["action"] }),
  });
}
