import { describe, expect, it } from "vitest";

import { displayTitle, enumLabel, humanizeName } from "./labels";

describe("humanizeName", () => {
  it("sentence-cases camelCase and PascalCase identifiers", () => {
    expect(humanizeName("nextStep")).toBe("Next step");
    expect(humanizeName("potentialValue")).toBe("Potential value");
    expect(humanizeName("evidenceAsOf")).toBe("Evidence as of");
    expect(humanizeName("CoachingSession")).toBe("Coaching session");
  });

  it("keeps acronyms", () => {
    expect(humanizeName("IPOpportunity")).toBe("IP opportunity");
    expect(humanizeName("HTTPServer")).toBe("HTTP server");
  });

  it("reads enum and snake_case values", () => {
    expect(humanizeName("pursuing")).toBe("Pursuing");
    expect(humanizeName("in_progress")).toBe("In progress");
    expect(humanizeName("ready-for-decision")).toBe("Ready for decision");
  });

  it("drops field source prefixes", () => {
    expect(humanizeName("frontmatter.clientRef")).toBe("Client ref");
  });

  it("leaves authored labels alone", () => {
    expect(humanizeName("Affected people or teams")).toBe("Affected people or teams");
    expect(humanizeName("1:1 Sync")).toBe("1:1 Sync");
    expect(humanizeName("")).toBe("");
    expect(humanizeName(undefined)).toBe("");
  });
});

describe("displayTitle", () => {
  it("unwraps wikilinks, keeping aliases", () => {
    expect(displayTitle("[[Tech Lead Sync]] 2022-05-12")).toBe("Tech Lead Sync 2022-05-12");
    expect(displayTitle("Call with [[Notes/Mike Levy|Mike]]")).toBe("Call with Mike");
    expect(displayTitle("See [[Plan#Goals]]")).toBe("See Plan");
  });

  it("strips emphasis, code, links, and heading marks", () => {
    expect(displayTitle("**AI in Healthcare Principles**")).toBe("AI in Healthcare Principles");
    expect(displayTitle("# A *quick* `rzm` [guide](https://x.dev)")).toBe("A quick rzm guide");
  });

  it("keeps ordinary punctuation and identifiers", () => {
    expect(displayTitle('"Code is a means of communication"')).toBe(
      '"Code is a means of communication"',
    );
    expect(displayTitle("snake_case_name notes")).toBe("snake_case_name notes");
    expect(displayTitle("2 * 3 = 6")).toBe("2 * 3 = 6");
  });
});

describe("enumLabel", () => {
  it("prefers a schema label and humanizes an echoed value", () => {
    expect(enumLabel("ready", "Ready for decision")).toBe("Ready for decision");
    expect(enumLabel("active", "active")).toBe("Active");
    expect(enumLabel("in_progress")).toBe("In progress");
  });
});
