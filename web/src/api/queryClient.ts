import { QueryClient, type QueryClientConfig } from "@tanstack/react-query";

import { ApiError } from "./client";

function shouldRetry(failureCount: number, error: Error): boolean {
  if (failureCount >= 1) return false;

  if (error instanceof ApiError) return error.status >= 500;

  return true;
}

export function createAppQueryClient(overrides: QueryClientConfig = {}): QueryClient {
  const defaultOptions: QueryClientConfig["defaultOptions"] = {
    queries: {
      staleTime: 30_000,
      gcTime: 5 * 60_000,
      retry:
        typeof process !== "undefined" && process.env.NODE_ENV === "test" ? false : shouldRetry,
      refetchOnWindowFocus: false,
      refetchOnReconnect: true,
      ...overrides.defaultOptions?.queries,
    },
    mutations: {
      retry: false,
      ...overrides.defaultOptions?.mutations,
    },
  };

  return new QueryClient({
    ...overrides,
    defaultOptions,
  });
}

export const appQueryClient = createAppQueryClient();
