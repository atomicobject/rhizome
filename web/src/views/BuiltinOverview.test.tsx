import { act, renderHook, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";

import { withFakeFetch } from "../test/fakeFetch";
import { clearPreferenceStores } from "../viewPreferences/store";
import { preferenceServer } from "../viewPreferences/testServer";
import { useGraphDisplay } from "./BuiltinOverview";
import { hostPreferenceScope, type ViewRuntimeProps } from "./ViewHost";

const props: ViewRuntimeProps = {
  view: { id: "builtin:overview", name: "Overview" },
  choice: { id: "builtin:overview", name: "Overview", renderer: "overview" },
  context: { kind: "type", type: "Spec" },
  active: true,
  services: {
    session: null,
    vaultKey: "vault",
    onOpenNote: vi.fn(),
    onStageOps: async () => undefined,
  },
};

afterEach(() => clearPreferenceStores());

describe("Overview display toggles", () => {
  const http = withFakeFetch();

  it("forgets a toggle switched back to its default, so later defaults apply", async () => {
    const server = preferenceServer(http);
    const scope = hostPreferenceScope(props);
    const { result } = renderHook(() => useGraphDisplay(props));
    const stored = () => server.read(scope).values["overview.display"];

    act(() => result.current.onChange({ showEdgeLabels: false, showProblems: true }));
    await waitFor(() => expect(stored()).toEqual({ showEdgeLabels: false, showProblems: true }));

    act(() => result.current.onChange({ showEdgeLabels: true }));
    await waitFor(() => expect(stored()).toEqual({ showProblems: true }));

    act(() => result.current.onChange({ showProblems: false }));
    await waitFor(() => expect(server.read(scope).values).not.toHaveProperty("overview.display"));
    expect(result.current.display).toEqual({
      scaleByAuthority: true,
      showEdgeLabels: true,
      showProblems: false,
    });
  });
});
