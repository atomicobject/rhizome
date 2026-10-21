import { act, fireEvent, screen, waitFor } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

import { renderWithQueryClient as render } from "../test/renderWithQueryClient";
import { deferredReply, jsonReply, withFakeFetch } from "../test/fakeFetch";
import { withFakeEventSource } from "../test/fakeEventSource";
import { queryKeys } from "../api/queryKeys";
import { VaultInvalidationBridge } from "../query/VaultInvalidationBridge";
import { ConfiguredTableCellContent } from "./ConfiguredTableCell";

describe("wikilinks in string cells", () => {
  const http = withFakeFetch();
  const events = withFakeEventSource();

  it.each(["index.changed", "reconnect"])(
    "refreshes a lookup still loading on %s",
    async (event) => {
      const pending = deferredReply<{ matches: { path: string; title: string }[] }>();
      let requests = 0;
      http.on("GET", "/api/v1/search/notes", () => {
        requests += 1;

        return requests === 1
          ? pending.promise
          : jsonReply({ matches: [{ path: "Notes/New.md", title: "New" }] });
      });
      render(
        <>
          <VaultInvalidationBridge />
          <ConfiguredTableCellContent
            row={{
              ref: { notePath: "Notes/Source.md", kind: "NOTE" },
              fields: { about: "[[New]]" },
            }}
            field="about"
            columnIndex={1}
            capability={undefined}
            onOpenRelation={vi.fn()}
          />
        </>,
      );
      await waitFor(() => expect(requests).toBe(1));
      const source = events.latest();
      act(() => source.open());
      act(() => {
        if (event === "reconnect") source.open();
        else source.emit("", event);
      });
      const link = await screen.findByRole("link", { name: "New" });
      expect(link).toHaveAttribute("href", "/notes?note=Notes%2FNew.md");
      expect(http.requests("GET", "/api/v1/search/notes")[0].signal?.aborted).toBe(true);
      await act(async () => pending.resolve({ matches: [] }));
      expect(link).toHaveAttribute("href", "/notes?note=Notes%2FNew.md");
    },
  );

  it("resolves a previously missing note after the vault changes", async () => {
    http.json("GET", "/api/v1/search/notes", { matches: [] });

    const { queryClient } = render(
      <ConfiguredTableCellContent
        row={{
          ref: { notePath: "Notes/Perspective.md", kind: "NOTE" },
          fields: { about: "[[New note]]" },
        }}
        field="about"
        columnIndex={1}
        capability={undefined}
        onOpenRelation={vi.fn()}
      />,
    );

    await screen.findByTitle("Could not resolve New note");
    http.json("GET", "/api/v1/search/notes", {
      matches: [{ path: "Notes/New note.md", title: "New note" }],
    });
    await act(async () => {
      await queryClient.invalidateQueries({ queryKey: queryKeys.all });
    });
    expect(await screen.findByRole("link", { name: "New note" })).toHaveAttribute(
      "href",
      "/notes?note=Notes%2FNew+note.md",
    );
  });

  it("shares repeated link lookups across cells and refetches once after invalidation", async () => {
    http.json("GET", "/api/v1/search/notes", {
      matches: [{ path: "Notes/Shared.md", title: "Shared" }],
    });

    const { queryClient } = render(
      <>
        {Array.from({ length: 20 }, (_, index) => (
          <ConfiguredTableCellContent
            key={index}
            row={{
              ref: { notePath: `Notes/Source ${index}.md`, kind: "NOTE" },
              fields: { about: "[[Shared]]" },
            }}
            field="about"
            columnIndex={1}
            capability={undefined}
            onOpenRelation={vi.fn()}
          />
        ))}
      </>,
    );

    expect(await screen.findAllByRole("link", { name: "Shared" })).toHaveLength(20);
    expect(http.count("GET", "/api/v1/search/notes")).toBe(1);
    await act(async () => {
      await queryClient.invalidateQueries({ queryKey: queryKeys.all });
    });
    expect(http.count("GET", "/api/v1/search/notes")).toBe(2);
  });

  it("keeps local heading links scoped to each source note", async () => {
    render(
      <>
        {["Notes/First.md", "Notes/Second.md"].map((notePath) => (
          <ConfiguredTableCellContent
            key={notePath}
            row={{ ref: { notePath, kind: "NOTE" }, fields: { about: "[[#Details]]" } }}
            field="about"
            columnIndex={1}
            capability={undefined}
            onOpenRelation={vi.fn()}
          />
        ))}
      </>,
    );
    await waitFor(() =>
      expect(
        screen.getAllByRole("link", { name: "#Details" }).map((link) => link.getAttribute("href")),
      ).toEqual(["/notes?note=Notes%2FFirst.md#Details", "/notes?note=Notes%2FSecond.md#Details"]),
    );
  });

  it.each([false, true])("opens every link with editable=%s", async (editable) => {
    http.json("GET", "/api/v1/search/notes", {
      matches: [
        { path: "Notes/First note.md", title: "First note" },
        { path: "Notes/Second note.md", title: "Second note" },
      ],
    });
    const onOpenRelation = vi.fn();
    const onStageOps = vi.fn();
    const onRowClick = vi.fn();
    render(
      <div onClick={onRowClick}>
        <ConfiguredTableCellContent
          row={{
            ref: { notePath: "Notes/Perspective.md", kind: "NOTE" },
            path: "Notes/Perspective.md",
            fields: { about: ["[[First note|First]]", "[[Second note#Details|Second]]"] },
          }}
          field="about"
          columnIndex={1}
          capability={{
            key: "about",
            label: "About",
            valueKind: "string",
            sortable: true,
            groupable: false,
            edit: editable
              ? { kind: "text", field: "about", operation: "setField", list: true }
              : undefined,
          }}
          onStageOps={onStageOps}
          onOpenRelation={onOpenRelation}
        />
      </div>,
    );

    const first = await screen.findByRole("link", { name: "First" });
    const second = await screen.findByRole("link", { name: "Second" });
    expect(first.closest("button")).toBeNull();
    expect(first.getAttribute("href")).toContain("note=Notes%2FFirst+note.md");
    fireEvent.click(first);
    await waitFor(() =>
      expect(onOpenRelation).toHaveBeenLastCalledWith(
        expect.objectContaining({ path: "Notes/First note.md" }),
        "activate",
      ),
    );
    fireEvent.click(second, { metaKey: true });
    await waitFor(() =>
      expect(onOpenRelation).toHaveBeenLastCalledWith(
        expect.objectContaining({ path: "Notes/Second note.md#Details" }),
        "beside",
      ),
    );
    expect(onRowClick).not.toHaveBeenCalled();
    expect(onStageOps).not.toHaveBeenCalled();

    if (editable) {
      fireEvent.click(screen.getByRole("button", { name: "Edit About" }));
      expect(screen.queryByRole("link", { name: "First" })).toBeNull();
      const editor = screen.getByRole("textbox");
      expect(editor).toHaveValue("[[First note|First]]\n[[Second note#Details|Second]]");
      fireEvent.blur(editor);
      expect(await screen.findByRole("link", { name: "First" })).toBeVisible();
      expect(onStageOps).not.toHaveBeenCalled();
    }
  });

  it("preserves surrounding text and leaves code and HTML inert", async () => {
    http.json("GET", "/api/v1/search/notes", {
      matches: [{ path: "Notes/First note.md", title: "First note" }],
    });
    render(
      <ConfiguredTableCellContent
        row={{
          ref: { notePath: "Notes/Perspective.md", kind: "NOTE" },
          path: "Notes/Perspective.md",
          fields: {
            about:
              "See [[First note]], `[[literal]]`, *emphasis*, ![diagram](x.png) and <script>alert(1)</script>.",
          },
        }}
        field="about"
        columnIndex={1}
        capability={{
          key: "about",
          label: "About",
          valueKind: "string",
          sortable: true,
          groupable: false,
        }}
        onOpenRelation={vi.fn()}
      />,
    );
    expect(await screen.findByRole("link", { name: "First note" })).toBeVisible();
    expect(screen.getAllByRole("link")).toHaveLength(1);
    expect(document.body).toHaveTextContent(
      "See First note, `[[literal]]`, *emphasis*, ![diagram](x.png) and <script>alert(1)</script>.",
    );
    expect(document.querySelector("script")).toBeNull();
  });

  it.each([
    ["./Sibling", "Notes/Sibling.md", "/notes?note=Notes%2FSibling.md"],
    ["#Details", "Notes/Perspective.md#Details", "/notes?note=Notes%2FPerspective.md#Details"],
  ])("resolves %s from the source note", async (target, expected, href) => {
    http.json("GET", "/api/v1/search/notes", { matches: [] });
    const onOpenRelation = vi.fn();
    render(
      <ConfiguredTableCellContent
        row={{
          ref: { notePath: "Notes/Perspective.md", kind: "NOTE" },
          fields: { about: `[[${target}|Follow]]` },
        }}
        field="about"
        columnIndex={1}
        capability={undefined}
        onOpenRelation={onOpenRelation}
      />,
    );
    const link = await screen.findByRole("link", { name: "Follow" });
    expect(link.getAttribute("href")).toBe(href);
    fireEvent.click(link);
    await waitFor(() =>
      expect(onOpenRelation).toHaveBeenCalledWith(
        expect.objectContaining({ path: expected }),
        "activate",
      ),
    );
  });
});
