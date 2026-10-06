import { screen, waitFor } from "@testing-library/react";
import { expect, it } from "vitest";

import type { ValidationScope } from "../src/api/types";
import { jsonReply, type FakeFetchRequest } from "../src/test/fakeFetch";
import {
  useDisplayGroup,
  useDisplayGroups,
  useTypeDocs,
  useValidationSummaries,
  validationScopeKey,
} from "./index";
import { renderView } from "./testing";
import type { DisplayGroupMember, DisplayGroupsResponse, TypeDoc } from "./types";

function member(name: string, count: number): DisplayGroupMember {
  return {
    name,
    kind: "type",
    label: name,
    pluralLabel: `${name}s`,
    count,
    issueCount: 0,
    implementors: [],
    children: [],
  };
}

function groups(specCount: number): DisplayGroupsResponse {
  return {
    groups: [
      { name: "Delivery", members: [member("Spec", specCount), member("Effort", 2)] },
      { name: "Knowledge", members: [member("Concept", 9)] },
    ],
  };
}

function GroupMembers() {
  const { group, isLoading } = useDisplayGroup();

  if (isLoading) return <p>Loading</p>;

  if (!group) return <p>No group</p>;

  return (
    <ul>
      {group.members.map((entry) => (
        <li key={entry.name}>{`${entry.pluralLabel}: ${entry.count}`}</li>
      ))}
    </ul>
  );
}

it("reads the invocation's display group and refreshes it on index changes", async () => {
  let specCount = 3;

  const view = await renderView(async () => ({ default: GroupMembers }), {
    context: { kind: "group", group: "Delivery" },
    routes: { "GET /api/v1/display-groups": () => jsonReply(groups(specCount)) },
  });

  expect(await screen.findByText("Specs: 3")).toBeVisible();
  expect(screen.getByText("Efforts: 2")).toBeVisible();
  expect(screen.queryByText("Concepts: 9")).toBeNull();

  specCount = 4;
  await view.emit("node.changed");
  expect(await screen.findByText("Specs: 4")).toBeVisible();
});

it("reports a missing display group as null rather than loading", async () => {
  await renderView(async () => ({ default: GroupMembers }), {
    context: { kind: "group", group: "Gone" },
    routes: { "GET /api/v1/display-groups": groups(1) },
  });

  expect(await screen.findByText("No group")).toBeVisible();
});

function doc(name: string, label: string): TypeDoc {
  return { name, label, pluralLabel: `${label}s`, fields: [] };
}

function TypeLabels({ names }: { names: string[] }) {
  const { docs, isLoading, error } = useTypeDocs(names);

  if (error) return <p>{error.message}</p>;

  if (isLoading) return <p>Loading</p>;

  return <p>{names.map((name) => docs[name]?.label ?? "?").join(", ")}</p>;
}

it("loads type documentation without notes and refreshes it only on schema changes", async () => {
  let version = 1;

  const reply = ({ path }: FakeFetchRequest) => {
    const name = decodeURIComponent(path.split("/").at(-1) ?? "");

    return jsonReply({ type: doc(name, `${name} v${version}`), count: 0 });
  };

  const view = await renderView(
    async () => ({ default: () => <TypeLabels names={["Spec", "Work Item"]} /> }),
    {
      routes: {
        "GET /api/v1/ontology/types/Spec": reply,
        "GET /api/v1/ontology/types/Work%20Item": reply,
      },
    },
  );

  expect(await screen.findByText("Spec v1, Work Item v1")).toBeVisible();
  const requests = view.http.requests("GET").filter(({ path }) => path.includes("/types/"));
  expect(requests.length).toBeGreaterThan(0);
  expect(requests.every(({ query }) => query.get("notes") === "none")).toBe(true);

  // StrictMode remounts once in development, so count from here.
  const before = view.http.calls.length;
  version = 2;
  await view.emit("index.changed");
  expect(view.http.calls).toHaveLength(before);
  await view.emit("schema.invalidated");
  expect(await screen.findByText("Spec v2, Work Item v2")).toBeVisible();
});

const SCOPES: ValidationScope[] = [
  { kind: "type", key: "Spec" },
  { kind: "note", key: "docs/a.md" },
];

function IssueCounts() {
  const { summaries, generation } = useValidationSummaries(SCOPES);

  return (
    <p>
      {`generation ${generation ?? "-"}: `}
      {SCOPES.map((scope) => summaries.get(validationScopeKey(scope))?.issueCount ?? "?").join(" ")}
    </p>
  );
}

it("reads summaries for the published validation generation and follows validation changes", async () => {
  let generation = 7;

  const summaries = (request: FakeFetchRequest) => {
    // SAFETY: the kit posts the generated ValidationScopeSummaryRequest.
    const body = JSON.parse(request.body ?? "{}") as {
      generation: number;
      scopes: ValidationScope[];
    };

    return jsonReply({
      generation: body.generation,
      summaries: body.scopes.map((scope, index) => ({
        scope,
        issueCount: body.generation + index,
        affectedFileCount: 0,
        affectedNoteCount: 0,
        repairActionCount: 0,
      })),
    });
  };

  const view = await renderView(async () => ({ default: IssueCounts }), {
    routes: {
      // `generation` counts runs, one past the published snapshot while a run is in progress.
      "GET /api/v2/validate": () =>
        jsonReply({ health: "running", generation: generation + 1, snapshot: { generation } }),
      "POST /api/v1/validation/summaries": summaries,
    },
  });

  expect(await screen.findByText("generation 7: 7 8")).toBeVisible();

  generation = 8;
  await view.emit("index.changed");
  expect(screen.getByText("generation 7: 7 8")).toBeVisible();
  await view.emit("validation.invalidated");
  expect(await screen.findByText("generation 8: 8 9")).toBeVisible();
  await waitFor(() => expect(view.http.count("POST", "/api/v1/validation/summaries")).toBe(2));
});

function FirstDoc({ name }: { name: string }) {
  const { docs, error } = useTypeDocs([name]);
  const doc = docs[name];

  if (error) return <p role="alert">{error.message}</p>;

  if (!doc) return <p>Loading</p>;

  return (
    <p>{`${doc.label}/${doc.pluralLabel}: ${doc.fields.length} fields, ${doc.enums?.length ?? 0} enums`}</p>
  );
}

it("fills the parts of type documentation the server omits when empty", async () => {
  await renderView(async () => ({ default: () => <FirstDoc name="Bare" /> }), {
    // Go omits empty fields, labels, and enum values.
    routes: {
      "GET /api/v1/ontology/types/Bare": {
        type: { name: "Bare", enums: [{ name: "Stage" }] },
        count: 0,
      },
    },
  });

  expect(await screen.findByText("Bare/Bare: 0 fields, 1 enums")).toBeVisible();
});

it("fails a kit read whose response does not match its contract", async () => {
  await renderView(async () => ({ default: () => <FirstDoc name="Odd" /> }), {
    routes: {
      "GET /api/v1/ontology/types/Odd": {
        type: { name: "Odd", fields: [{ name: "stage", kind: "colour" }] },
        count: 0,
      },
    },
  });

  expect(await screen.findByRole("alert")).toHaveTextContent("type Odd.fields[0].kind");
});

it("fails display groups whose members are missing", async () => {
  const GroupError = () => {
    const { error } = useDisplayGroups();

    return error ? <p role="alert">{error.message}</p> : <p>Loading</p>;
  };

  await renderView(async () => ({ default: GroupError }), {
    routes: { "GET /api/v1/display-groups": { groups: [{ name: "Delivery" }] } },
  });

  expect(await screen.findByRole("alert")).toHaveTextContent("groups[0].members");
});

it("carries each enum value's lifecycle stage and whether the schema declared it", async () => {
  const Stages = () => {
    const { docs } = useTypeDocs(["Work"]);
    const values = docs.Work?.enums?.[0]?.values;

    if (!values) return <p>Loading</p>;

    return (
      <p>
        {values.map((value) => `${value.name}:${value.stage}:${value.stageDeclared}`).join(" ")}
      </p>
    );
  };

  await renderView(async () => ({ default: Stages }), {
    routes: {
      "GET /api/v1/ontology/types/Work": {
        type: {
          name: "Work",
          enums: [
            {
              name: "Status",
              values: [
                { name: "todo", stage: "open", stageDeclared: true },
                { name: "shipped", tone: "success", stage: "done" },
              ],
            },
          ],
        },
        count: 0,
      },
    },
  });

  expect(await screen.findByText("todo:open:true shipped:done:undefined")).toBeVisible();
});

it("carries the type profile, with omitted lists empty", async () => {
  const Profile = () => {
    const { docs } = useTypeDocs(["Work", "Part"]);

    if (!docs.Work || !docs.Part) return <p>Loading</p>;
    const profile = docs.Work.profile;

    return (
      <p>
        {[
          // oxlint-disable-next-line anti-slop/no-shape-in-symbol-names -- `shape` is the profile's API field.
          profile?.shape,
          profile?.lifecycleField,
          profile?.gapFields.join("+"),
          profile?.keyTextFields.join("+"),
          profile?.categoryFields.length,
          docs.Part.profile === undefined ? "no profile" : "profile",
        ].join(" ")}
      </p>
    );
  };

  await renderView(async () => ({ default: Profile }), {
    routes: {
      "GET /api/v1/ontology/types/Work": {
        type: {
          name: "Work",
          profile: {
            // oxlint-disable-next-line anti-slop/no-shape-in-symbol-names -- `shape` is the profile's API field.
            shape: "workflow",
            lifecycleField: "status",
            gapFields: ["owner", "risk"],
            keyTextFields: ["nextStep"],
          },
        },
        count: 0,
      },
      // Go omits the profile on section and embedded types.
      "GET /api/v1/ontology/types/Part": { type: { name: "Part" }, count: 0 },
    },
  });

  expect(await screen.findByText("workflow status owner+risk nextStep 0 no profile")).toBeVisible();
});

it("drops a type profile with a shape it does not know and keeps the rest of the documentation", async () => {
  const Odd = () => {
    const { docs, error } = useTypeDocs(["Odd"]);

    if (error) return <p role="alert">{error.message}</p>;

    if (!docs.Odd) return <p>Loading</p>;

    return (
      <p>{`${docs.Odd.label}: ${docs.Odd.profile === undefined ? "no profile" : "profile"}`}</p>
    );
  };

  // A newer server can add a shape; the view still gets the type's fields.
  await renderView(async () => ({ default: Odd }), {
    routes: {
      "GET /api/v1/ontology/types/Odd": {
        type: {
          name: "Odd",
          label: "Odd one",
          // oxlint-disable-next-line anti-slop/no-shape-in-symbol-names -- `shape` is the profile's API field.
          profile: { shape: "spiral", lifecycleField: "status" },
        },
        count: 0,
      },
    },
  });

  expect(await screen.findByText("Odd one: no profile")).toBeVisible();
  expect(screen.queryByRole("alert")).toBeNull();
});
