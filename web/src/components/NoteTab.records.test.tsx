import { waitFor } from "@testing-library/react";
import { expect, it, vi } from "vitest";
import { isString } from "../api/parse";
import { graphQLRequestBody, jsonReply, withFakeFetch } from "../test/fakeFetch";
import { withFakeEventSource } from "../test/fakeEventSource";
import { nativeRecordNode } from "../test/nativeRecordFixture";
import { renderWithQueryClient } from "../test/renderWithQueryClient";
import { NoteTab } from "./NoteTab";

const http = withFakeFetch();

withFakeEventSource();

it("uses a record's display title for locator-only tabs and the host title for host tabs", async () => {
  http.onGraphQL("PublicNodeDetail", (request) => {
    const ref = graphQLRequestBody(request).variables?.ref;

    return jsonReply({ data: { node: nativeRecordNode(isString(ref) ? ref : "services.yaml") } });
  });
  const onTitle = vi.fn();

  const tab = {
    id: "note:services.yaml",
    kind: "note" as const,
    path: "services.yaml",
    fragment: "record:v1:booking",
    dirty: false,
  };

  const props = {
    active: true,
    editSession: null,
    editing: false,
    onOpen: vi.fn(),
    onTitle,
    registerContext: vi.fn(),
    onStageOps: async () => {},
  };

  const { rerender } = renderWithQueryClient(
    <NoteTab {...props} tab={tab} anchor={tab.fragment} />,
  );

  await waitFor(() => expect(onTitle).toHaveBeenLastCalledWith(tab.id, "Booking service"));
  rerender(<NoteTab {...props} tab={{ ...tab, fragment: undefined }} anchor={null} />);
  await waitFor(() => expect(onTitle).toHaveBeenLastCalledWith(tab.id, "services"));
});
