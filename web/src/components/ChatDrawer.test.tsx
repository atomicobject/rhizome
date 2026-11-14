import { fireEvent, screen, waitFor, within } from "@testing-library/react";
import { beforeEach, describe, expect, it } from "vitest";

import type { AgentEvent, AgentSessionResponse } from "../api/types";
import { withFakeEventSource } from "../test/fakeEventSource";
import { withFakeFetch } from "../test/fakeFetch";
import { renderWithQueryClient as render } from "../test/renderWithQueryClient";
import { readyStatus, session, settingsWith } from "./AgentWorkspace.test-support";
import { ChatDrawer } from "./ChatDrawer";

const SESSION_KEY = "rhizome:chat:session:v1";

function event(id: number, type: string, result: AgentEvent["result"] = {}): AgentEvent {
  return {
    id,
    sessionId: "agt-1",
    type,
    result,
    createdAt: `2026-11-14T12:00:0${id}Z`,
  };
}

const thread: AgentSessionResponse = {
  session: session(),
  messages: [
    {
      id: 1,
      sessionId: "agt-1",
      role: "user",
      content: "Which types have no records?",
      context: { kind: "ontology", title: "Atlas", route: "/ontology" },
      createdAt: "2026-11-14T12:00:00Z",
    },
  ],
  events: [
    event(2, "tool-call", { turnId: "t1", itemId: "i1", name: "mcp__rhizome_page__page_text" }),
    event(3, "file-change", { turnId: "t1", itemId: "i2", paths: ["notes/types/spec.md"] }),
    event(4, "approval-requested", {
      turnId: "t1",
      requestId: "r1",
      name: "Delete 3 notes",
      reason: "Deleting content needs your approval.",
    }),
  ],
};

describe("Chat in the right pane", () => {
  const http = withFakeFetch();
  const sources = withFakeEventSource();

  beforeEach(() => {
    window.history.replaceState({}, "", "/ontology");
    window.localStorage.clear();
    http.json("GET", "/api/agent/settings", {
      ...settingsWith(readyStatus),
      settings: {
        ...settingsWith(readyStatus).settings,
        harnesses: {
          codex: { model: "gpt-standard", permissionMode: "auto" },
          claude: { permissionMode: "auto" },
        },
      },
    });
  });

  it("starts a session with the first message and sends what the user is looking at", async () => {
    http
      .json("POST", "/api/agent/sessions", { session: session() })
      .json("GET", "/api/agent/sessions/agt-1", { session: session(), messages: [], events: [] })
      .json("POST", "/api/agent/sessions/agt-1/messages", {
        session: session({ turnRunning: true }),
      });
    render(<ChatDrawer />);

    const pane = screen.getByRole("complementary", { name: "Chat" });
    expect(await within(pane).findByText(/Codex · GPT Standard/)).toBeVisible();
    expect(within(pane).getByText("Auto")).toBeVisible();

    const composer = within(pane).getByRole("textbox", { name: "Message" });
    fireEvent.change(composer, { target: { value: "Explain this graph" } });
    fireEvent.keyDown(composer, { key: "Enter" });

    await waitFor(() => expect(http.count("POST", "/api/agent/sessions/agt-1/messages")).toBe(1));
    const sent = http.requests("POST", "/api/agent/sessions/agt-1/messages")[0];
    expect(JSON.parse(sent?.body ?? "{}")).toEqual({
      content: "Explain this graph",
      context: {
        kind: "ontology",
        title: "Atlas",
        route: "/ontology",
        detail: "All types and how they relate",
      },
    });
    expect(window.localStorage.getItem(SESSION_KEY)).toBe("agt-1");
    expect(await within(pane).findByText("Explain this graph")).toBeVisible();
    expect(within(pane).getByRole("button", { name: "Stop" })).toBeVisible();
  });

  it("shows each agent action as one line and asks only for escalations", async () => {
    window.localStorage.setItem(SESSION_KEY, "agt-1");
    http
      .json("GET", "/api/agent/sessions/agt-1", thread)
      .json("POST", "/api/agent/sessions/agt-1/approvals/r1", { responded: true });
    render(<ChatDrawer />);

    const pane = screen.getByRole("complementary", { name: "Chat" });
    expect(await within(pane).findByText("Which types have no records?")).toBeVisible();
    expect(within(pane).getByText("Read the page")).toBeVisible();
    expect(within(pane).getByText("Edited spec.md").closest("[title]")).toHaveAttribute(
      "title",
      "notes/types/spec.md\nAllowed by auto mode",
    );

    const approval = within(pane).getByRole("region", { name: "Approval requested" });
    expect(within(approval).getByText("Delete 3 notes")).toBeVisible();
    fireEvent.click(within(approval).getByRole("button", { name: "Allow" }));
    await waitFor(() =>
      expect(http.count("POST", "/api/agent/sessions/agt-1/approvals/r1")).toBe(1),
    );

    sources
      .latest()
      .emitJSON(
        event(5, "approval-decision", { turnId: "t1", requestId: "r1", status: "allow" }),
        "agent_event",
      );
    expect(await within(pane).findByText("Allowed: Delete 3 notes")).toBeVisible();
    expect(within(pane).queryByRole("region", { name: "Approval requested" })).toBeNull();
  });

  it("answers the agent's page_text request with the section's text, not the chat's", async () => {
    window.localStorage.setItem(SESSION_KEY, "agt-1");
    http
      .json("GET", "/api/agent/sessions/agt-1", { session: session(), messages: [], events: [] })
      .json("POST", "/api/agent/sessions/agt-1/page-relay/req-1", { ok: true });

    const page = document.createElement("div");
    page.className = "app-shell__main";
    page.textContent = "Ontology Atlas 10 types";
    document.body.append(page);

    try {
      render(<ChatDrawer />);
      await waitFor(() => expect(sources.instances.length).toBeGreaterThan(0));
      sources.latest().emitJSON(
        {
          id: 0,
          sessionId: "agt-1",
          type: "page-request",
          result: { requestId: "req-1" },
          createdAt: "2026-11-14T12:00:00Z",
        },
        "agent_event",
      );

      await waitFor(() =>
        expect(http.count("POST", "/api/agent/sessions/agt-1/page-relay/req-1")).toBe(1),
      );
      const reply = http.requests("POST", "/api/agent/sessions/agt-1/page-relay/req-1")[0];
      expect(JSON.parse(reply?.body ?? "{}")).toEqual({
        title: "Atlas",
        route: "/ontology",
        text: "Ontology Atlas 10 types",
      });
      // The request is not part of the conversation.
      expect(screen.queryByText(/page-request/)).toBeNull();
    } finally {
      page.remove();
    }
  });

  it("offers setup instead of a composer when no coding agent is installed", async () => {
    http.json("GET", "/api/agent/settings", settingsWith({ ...readyStatus, installed: false }));
    render(<ChatDrawer />);

    expect(await screen.findByRole("link", { name: "Set one up on the Agent page" })).toBeVisible();
    expect(screen.getByRole("textbox", { name: "Message" })).toBeDisabled();
  });
});

describe("a view context", () => {
  it("fits the server's bounds, dropping identifiers too long to keep whole", async () => {
    const { boundedContext } = await import("./chat/viewContext");

    const bounded = boundedContext({
      kind: "note",
      title: "é".repeat(300),
      path: `notes/${"é".repeat(600)}.md`,
      route: `/notes?note=${"a".repeat(3000)}`,
      detail: "d".repeat(2500),
    });

    expect(Array.from(bounded.title)).toHaveLength(200);
    expect(bounded.route).toHaveLength(2048);
    expect(bounded).not.toHaveProperty("path");
    expect(bounded.detail).toHaveLength(2000);
  });
});
