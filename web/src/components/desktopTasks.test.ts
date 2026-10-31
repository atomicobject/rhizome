import { describe, expect, it } from "vitest";

import type { TaskItem } from "../api/tasks";
import { desktopTasks } from "./desktopTasks";

const NOW = Date.parse("2026-10-31T12:00:00Z");

function item(id: string, state: TaskItem["state"], extra: Partial<TaskItem> = {}): TaskItem {
  return { id, task: "docs-drift", kind: "task", state, created: "", updated: "", ...extra };
}

describe("desktopTasks", () => {
  it("counts unsnoozed items that need the person and names their tasks", () => {
    const items = [
      item("1", "ready", { summary: "Three notes", reason: "unused" }),
      item("2", "due", { task: "deps", reason: "every 7d" }),
      item("3", "needs-attention"),
      item("4", "running"),
      item("5", "started"),
      item("6", "done"),
      item("7", "due", { snoozedUntil: "2026-10-31T13:00:00Z" }),
      item("8", "due", { snoozedUntil: "2026-10-31T11:00:00Z" }),
    ];

    const names = new Map([["docs-drift", "Docs drift check"]]);

    expect(desktopTasks({ paused: false, items, recent: [] }, names, NOW)).toEqual({
      needs: 4,
      items: [
        {
          id: "1",
          task: "docs-drift",
          name: "Docs drift check",
          state: "ready",
          text: "Three notes",
        },
        { id: "2", task: "deps", name: undefined, state: "due", text: "every 7d" },
        {
          id: "3",
          task: "docs-drift",
          name: "Docs drift check",
          state: "needs-attention",
          text: "",
        },
        { id: "8", task: "docs-drift", name: "Docs drift check", state: "due", text: "" },
      ],
    });
  });

  it("stays within the report Rhizome Desktop accepts", () => {
    const items = Array.from({ length: 60 }, (_, index) =>
      item(String(index), "due", { summary: "é".repeat(300) }),
    );

    const names = new Map(items.map((entry) => [entry.task, "n".repeat(300)]));
    const tasks = desktopTasks({ paused: false, items, recent: [] }, names, NOW);

    expect(tasks.needs).toBe(60);
    expect([...(tasks.items[0].name ?? "")]).toHaveLength(200);
    expect(tasks.items).toHaveLength(50);
    expect([...tasks.items[0].text]).toHaveLength(200);
    expect(tasks.items[0].text.endsWith("…")).toBe(true);
  });
});
