import { useQueryClient } from "@tanstack/react-query";
import { useCallback, useEffect, useState } from "react";

import {
  ApiError,
  applyValidationRepairReview,
  createValidationRepairReview,
  getValidationRepairReview,
} from "../api/publicClient";
import { isJsonObject, isString, isStringArray } from "../api/parse";
import { isValidationSnapshotQuery, queryKeys } from "../api/queryKeys";
import type {
  ValidationRepairApplyResponse,
  ValidationRepairReview,
  ValidationRepairReviewApplyRequest,
} from "../api/types";

export function useValidationRepair(generation: number | null, planFingerprint?: string) {
  const queryClient = useQueryClient();
  const [review, setReview] = useState<ValidationRepairReview | null>(null);
  const [result, setResult] = useState<ValidationRepairApplyResponse | null>(null);
  const [busy, setBusy] = useState<"stage" | "apply" | "restore" | null>(null);
  const [error, setError] = useState<Error | null>(null);
  const [changed, setChanged] = useState(false);

  const [recoveryFailed, setRecoveryFailed] = useState(false);

  const [expansionRequest, setExpansionRequest] = useState<{
    generation: number;
    fingerprint: string;
    actionIds: string[];
  } | null>(null);

  const expansion =
    expansionRequest?.generation === generation && expansionRequest.fingerprint === planFingerprint
      ? expansionRequest
      : null;

  const recoverReview = useCallback(async (stored: ValidationRepairReview) => {
    setBusy("restore");
    setError(null);
    setRecoveryFailed(false);

    try {
      const next = await getValidationRepairReview(stored.id);
      setReview((current) => (current?.id === stored.id ? next : current));
    } catch (cause) {
      if (cause instanceof ApiError && (cause.status === 404 || cause.status === 410)) {
        setReview((current) =>
          current?.id === stored.id
            ? {
                ...current,
                state: "stale",
                staleReason:
                  "The server restarted or no longer has this review. Stage these fixes again.",
              }
            : current,
        );
        setChanged(true);
      } else {
        setError(normalizeError(cause));
        setRecoveryFailed(true);
      }
    } finally {
      setBusy(null);
    }
  }, []);

  useEffect(() => {
    let stored: ValidationRepairReview | null = null;

    try {
      const raw = window.sessionStorage.getItem("rhizome:validation-repair-review");
      const parsed: unknown = raw ? JSON.parse(raw) : null;

      if (isJsonObject(parsed) && isString(parsed.id) && isString(parsed.vaultIdentity)) {
        // SAFETY: this module stored a validated server review; identity fields reject unrelated data.
        stored = parsed as ValidationRepairReview;
      }
    } catch {
      setError(new Error("Saved fix review recovery data is unreadable."));
    }

    if (!stored) return;
    setReview(stored);
    void recoverReview(stored);
  }, [recoverReview]);

  useEffect(() => {
    try {
      if (review) {
        window.sessionStorage.setItem("rhizome:validation-repair-review", JSON.stringify(review));
      } else {
        window.sessionStorage.removeItem("rhizome:validation-repair-review");
      }
    } catch {
      setError(new Error("Fix review recovery could not be saved in this browser."));
    }
  }, [review]);

  useEffect(() => {
    if (!review || generation === null || review.state !== "pending") return;

    if (review.generation === generation && review.planFingerprint === planFingerprint) return;
    setReview({
      ...review,
      state: "stale",
      staleReason: "Validation changed after this review was created. Stage these fixes again.",
    });
    setChanged(true);
  }, [generation, planFingerprint, review]);

  const stage = async (actionIds: string[]) => {
    if (!generation || !planFingerprint || actionIds.length === 0) return null;
    setExpansionRequest(null);
    setBusy("stage");
    setError(null);
    setResult(null);

    try {
      const next = await createValidationRepairReview({ generation, planFingerprint, actionIds });
      setReview(next);
      setChanged(false);

      return next;
    } catch (cause) {
      if (
        cause instanceof ApiError &&
        cause.code === "REPAIR_TRANSACTION_INCOMPLETE" &&
        isJsonObject(cause.details) &&
        isStringArray(cause.details.requiredActionIds)
      ) {
        setExpansionRequest({
          generation,
          fingerprint: planFingerprint,
          actionIds: [...new Set([...actionIds, ...cause.details.requiredActionIds])],
        });

        return null;
      }

      if (cause instanceof ApiError && isJsonObject(cause.details)) {
        // SAFETY: the typed repair error envelope mirrors the success fields checked below.
        const details = cause.details as Partial<ValidationRepairApplyResponse>;

        if (details.review?.id) setReview(details.review);

        if (details.review && details.result) {
          setResult({
            review: details.review,
            result: details.result,
            execution: details.execution,
          });
        }
      }

      setError(normalizeError(cause));
      throw cause;
    } finally {
      setBusy(null);
    }
  };

  const apply = async (confirmations: ValidationRepairReviewApplyRequest["confirmations"]) => {
    if (!review) return null;
    setBusy("apply");
    setError(null);

    try {
      const next = await applyValidationRepairReview(review.id, {
        generation: review.generation,
        planFingerprint: review.planFingerprint,
        selectionFingerprint: review.selectionFingerprint,
        confirmations,
      });

      setReview(next.review);
      setResult(next);
      await queryClient.invalidateQueries({
        queryKey: queryKeys.validationAll(),
        predicate: (query) => !isValidationSnapshotQuery(query),
      });

      return next;
    } catch (cause) {
      if (cause instanceof ApiError && isJsonObject(cause.details)) {
        // SAFETY: the typed repair error envelope mirrors the success fields checked below.
        const details = cause.details as Partial<ValidationRepairApplyResponse>;

        if (details.review?.id) setReview(details.review);

        if (details.review && details.result) {
          setResult({
            review: details.review,
            result: details.result,
            execution: details.execution,
          });
        }
      }

      setError(normalizeError(cause));
      throw cause;
    } finally {
      setBusy(null);
    }
  };

  return {
    review,
    result,
    busy,
    error,
    changed,
    expansion,
    dismissExpansion: () => setExpansionRequest(null),
    recoveryFailed,
    retryRecovery: () => {
      if (review) void recoverReview(review);
    },
    stage,
    apply,
    close: () => setReview(null),
    clearError: () => setError(null),
  };
}

export type ValidationRepairController = ReturnType<typeof useValidationRepair>;

function normalizeError(cause: unknown) {
  return cause instanceof Error ? cause : new Error("The repair request failed.");
}
