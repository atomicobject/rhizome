import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";

import { getPublicValidate, refreshPublicValidation } from "../api/client";
import { isValidationSnapshotQuery, queryKeys } from "../api/queryKeys";

export function useValidationRefresh() {
  const client = useQueryClient();

  const refresh = useMutation({
    mutationFn: refreshPublicValidation,
    onSuccess: () =>
      client.invalidateQueries({
        queryKey: queryKeys.validationAll(),
        predicate: (query) => !isValidationSnapshotQuery(query),
      }),
  });

  const result = useQuery({
    queryKey: queryKeys.validation(),
    queryFn: ({ signal }) => getPublicValidate({ signal }),
    enabled: refresh.isSuccess,
    refetchInterval: (query) => {
      const value = query.state.data;

      return refresh.isSuccess && (!value || value.refreshPending || value.status === "running")
        ? 1000
        : false;
    },
  });

  const waiting =
    refresh.isSuccess &&
    !result.isError &&
    (!result.data || result.data.refreshPending || result.data.status === "running");

  return {
    refresh: () => refresh.mutate(),
    busy: refresh.isPending || waiting,
    error: refresh.error ?? (refresh.isSuccess ? result.error : null),
  };
}
