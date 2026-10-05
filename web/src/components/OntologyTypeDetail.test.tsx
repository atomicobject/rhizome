import { fireEvent, screen, waitFor } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import type { OntologyAtlasResponse, OntologyTypeResponse } from "../api/types";
import { withFakeFetch } from "../test/fakeFetch";
import { renderWithQueryClient as render } from "../test/renderWithQueryClient";
import { OntologyTypeDetail } from "./OntologyTypeDetail";

const http = withFakeFetch();

describe("OntologyTypeDetail", () => {
  it("shows a reverse relation and its authored source field", async () => {
    const response: OntologyTypeResponse = {
      count: 1,
      type: {
        name: "Opportunity",
        fields: [
          {
            name: "evidence",
            kind: "reverse",
            typeName: "Evidence",
            reverseField: "opportunities",
            list: true,
          },
        ],
      },
    };

    http.json("GET", "/api/v1/ontology/types/Opportunity", response);
    render(
      <OntologyTypeDetail
        name="Opportunity"
        atlas={null}
        onBack={() => {}}
        onSelectType={() => {}}
      />,
    );

    expect(await screen.findByRole("heading", { name: "Typed relations" })).toBeVisible();
    expect(screen.getByRole("button", { name: /Evidence\[\]/ })).toBeVisible();
    fireEvent.click(screen.getByRole("tab", { name: "Schema" }));
    expect(screen.getByText("@reverse")).toBeVisible();
    expect(screen.getByText('"opportunities"')).toBeVisible();
  });

  it("shows typed relations and previews example links by canonical identity", async () => {
    const response: OntologyTypeResponse = {
      count: 2,
      issueCount: 0,
      type: {
        name: "SpecLike",
        fields: [
          {
            name: "summary",
            kind: "text",
            required: true,
            list: false,
          },
          {
            name: "requirements",
            kind: "link",
            typeName: "ReferenceDoc",
            direction: "outgoing",
            required: false,
            list: true,
            description: "Related references",
          },
          {
            name: "stories",
            kind: "section",
            typeName: "UserStory",
            direction: "contains",
            required: false,
            list: true,
            description: "Embedded stories",
          },
        ],
      },
      notes: [
        {
          ref: {
            notePath: "specs/100-demo/spec.md",
            fragment: "item-17",
            kind: "EMBEDDED",
            structuralFingerprint: "criterion-fingerprint",
          },
          path: "specs/100-demo/spec.md#item-17",
          title: "Demo Spec",
          relationCount: 3,
          hasIssues: false,
        },
      ],
    };

    http.json("GET", "/api/v1/ontology/types/SpecLike", response);
    http.json("GET", "/api/v1/nodes/preview", {
      ref: "specs/100-demo/spec.md#struct:criterion-fingerprint",
      path: "specs/100-demo/spec.md",
      title: "Demo Spec preview",
      format: "markdown",
      fragmentResolved: true,
      fields: [],
      hasIssues: false,
    });

    render(
      <OntologyTypeDetail name="SpecLike" atlas={null} onBack={() => {}} onSelectType={() => {}} />,
    );

    expect(await screen.findByRole("heading", { name: "Typed relations" })).toBeVisible();
    expect(screen.getByRole("button", { name: /ReferenceDoc\[\]/ })).toBeVisible();
    expect(screen.getByRole("button", { name: /UserStory\[\]/ })).toBeVisible();
    expect(screen.queryByText("requirements")).not.toBeNull();
    expect(screen.queryByText("stories")).not.toBeNull();

    const exampleLink = screen.getByRole("link", { name: "Demo Spec" });
    expect(exampleLink).toHaveAttribute(
      "href",
      "/notes?note=specs%2F100-demo%2Fspec.md#struct%3Acriterion-fingerprint",
    );
    fireEvent.focus(exampleLink);
    await waitFor(() =>
      expect(http.requests("GET", "/api/v1/nodes/preview")[0]?.query.get("ref")).toBe(
        "specs/100-demo/spec.md#struct:criterion-fingerprint",
      ),
    );
    expect(
      await screen.findByRole("region", { name: "Preview of Demo Spec preview" }),
    ).toBeVisible();
    expect(screen.getByRole("link", { name: "See all 2 →" })).toHaveAttribute(
      "href",
      "/notes/spec-like",
    );
  });

  it("renders implementors and inbound references on an interface page", async () => {
    const response: OntologyTypeResponse = {
      count: 5,
      issueCount: 0,
      type: {
        name: "SpecLike",
        role: "interface",
        description: "Any spec-shaped note.",
        fields: [],
      },
      notes: [],
    };

    http.json("GET", "/api/v1/ontology/types/SpecLike", response);

    const atlas: OntologyAtlasResponse = {
      schemaPresent: true,
      types: [
        {
          count: 3,
          type: {
            name: "ProcessSpec",
            description: "Workflow spec.",
            implements: ["SpecLike"],
            fields: [],
          },
        },
        {
          count: 2,
          type: {
            name: "ProductSpec",
            description: "Product spec.",
            implements: ["SpecLike"],
            fields: [],
          },
        },
        {
          count: 4,
          type: {
            name: "ArchitectureDecision",
            fields: [
              {
                name: "refines",
                kind: "link",
                typeName: "SpecLike",
                direction: "outgoing",
                required: false,
                list: false,
                description: "Upstream spec",
              },
            ],
          },
        },
      ],
      interfaces: [
        {
          name: "SpecLike",
          fields: [],
        },
      ],
    };

    render(
      <OntologyTypeDetail
        name="SpecLike"
        atlas={atlas}
        onBack={() => {}}
        onSelectType={() => {}}
      />,
    );

    expect(await screen.findByRole("heading", { name: "Implementors" })).toBeVisible();
    expect(screen.getByRole("button", { name: /ProcessSpec/ })).toBeVisible();
    expect(screen.getByRole("button", { name: /ProductSpec/ })).toBeVisible();

    expect(screen.getByRole("heading", { name: "Referenced by" })).toBeVisible();
    expect(screen.getByRole("button", { name: /ArchitectureDecision/ })).toBeVisible();
    expect(screen.getByText("refines")).toBeVisible();
  });
});
