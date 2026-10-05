import type { TypeDoc } from "@rhizome/kit";
import { fireEvent, screen, within } from "@testing-library/react";
import { expect, it } from "vitest";

import { jsonReply } from "../../src/test/fakeFetch";
import { RECORDS_DATA, TYPE_DOCS } from "./__fixtures__/groups.ts";
import { groupRoutes, renderGroupView } from "./__fixtures__/harness.tsx";
import { isJsonObject, type JsonValue } from "./api.ts";

const loadSections = () => import("./sections.tsx");

const loadTrace = () => import("./trace.tsx");

function withSummary(doc: TypeDoc, name: string, kind: "scalar" | "section"): TypeDoc {
  return {
    ...doc,
    summaryField: name,
    profile: doc.profile ? { ...doc.profile, summaryField: name } : undefined,
    fields: [
      ...doc.fields.filter((field) => field.name !== "summary"),
      {
        name,
        kind,
        typeName: kind === "section" ? "Section" : "String",
        list: false,
        display: { role: "SUMMARY" },
      },
    ],
  };
}

function summaryRows(rows: JsonValue | undefined, field: string, value: JsonValue) {
  if (!Array.isArray(rows)) throw new Error("fixture rows missing");

  return rows.map((row) => (isJsonObject(row) ? { ...row, [field]: value } : row));
}

for (const kind of ["scalar", "section"] as const) {
  for (const view of ["Sections", "Trace"] as const) {
    it(`${view} renders each interface implementor's own ${kind} and section summaries`, async () => {
      const storyText = `Story ${kind} explanation.`;
      const bugText = "Bug section explanation.";

      const work: TypeDoc = {
        ...TYPE_DOCS.Work,
        summaryField: undefined,
        profile: { ...TYPE_DOCS.Work.profile, summaryField: undefined },
        fields: TYPE_DOCS.Work.fields.filter((field) => field.name !== "summary"),
      };

      const routes = groupRoutes({
        "GET /api/v1/ontology/types/Work": () => jsonReply({ type: work, count: 6 }),
        "GET /api/v1/ontology/types/Story": () =>
          jsonReply({ type: withSummary(TYPE_DOCS.Story, "synopsis", kind), count: 4 }),
        "GET /api/v1/ontology/types/Bug": () =>
          jsonReply({ type: withSummary(TYPE_DOCS.Bug, "abstract", "section"), count: 2 }),
        "POST /api/v1/graphql": () =>
          jsonReply({
            data: {
              ...RECORDS_DATA,
              Story: summaryRows(
                RECORDS_DATA.Story,
                "synopsis",
                kind === "section" ? { content: storyText } : storyText,
              ),
              Bug: summaryRows(RECORDS_DATA.Bug, "abstract", { content: bugText }),
            },
          }),
      });

      await renderGroupView(view === "Sections" ? loadSections : loadTrace, { routes });

      if (view === "Trace") {
        fireEvent.click(await screen.findByRole("radio", { name: /^Work items/ }));
      }

      for (const [title, summary] of [
        ["Checkout redesign", storyText],
        ["Tax rounding", bugText],
      ]) {
        const row = (await screen.findByRole("button", { name: title })).closest("tr");

        if (!row) throw new Error(`missing row for ${title}`);
        expect(within(row).getByText(summary)).toBeVisible();
      }
    });
  }
}
