import { describe, expect, it } from "vitest";
import { formatTypeName } from "./typeNames";

describe("formatTypeName", () => {
  it("splits PascalCase into kebab-case", () => {
    expect(formatTypeName("UserStory")).toBe("user-story");
    expect(formatTypeName("ProductSpec")).toBe("product-spec");
  });

  it("handles consecutive words with shared acronyms", () => {
    expect(formatTypeName("UserStoriesSection")).toBe("user-stories-section");
    expect(formatTypeName("HTTPServer")).toBe("http-server");
    expect(formatTypeName("APIKey")).toBe("api-key");
  });

  it("preserves digits within words", () => {
    expect(formatTypeName("Spec2Reviewer")).toBe("spec2-reviewer");
  });

  it("normalizes existing whitespace/underscore boundaries", () => {
    expect(formatTypeName("user_story")).toBe("user-story");
    expect(formatTypeName("User Story")).toBe("user-story");
    expect(formatTypeName("Narrative  Section")).toBe("narrative-section");
  });

  it("returns empty for empty input", () => {
    expect(formatTypeName("")).toBe("");
  });

  it("passes through already-kebab strings", () => {
    expect(formatTypeName("user-story")).toBe("user-story");
  });
});
