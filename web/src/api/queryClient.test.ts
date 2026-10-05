import { describe, expect, it } from "vitest";

import { createAppQueryClient } from "./queryClient";

describe("createAppQueryClient", () => {
  it("uses the shared lifecycle defaults and disables retries in tests", () => {
    const client = createAppQueryClient();
    const defaults = client.getDefaultOptions();

    expect(defaults.queries).toMatchObject({
      staleTime: 30_000,
      gcTime: 300_000,
      retry: false,
      refetchOnWindowFocus: false,
      refetchOnReconnect: true,
    });
    expect(defaults.mutations).toMatchObject({ retry: false });
  });

  it("merges query overrides without dropping unrelated defaults", () => {
    const client = createAppQueryClient({
      defaultOptions: { queries: { staleTime: 5_000 } },
    });

    expect(client.getDefaultOptions().queries).toMatchObject({
      staleTime: 5_000,
      gcTime: 300_000,
      refetchOnWindowFocus: false,
    });
  });
});
