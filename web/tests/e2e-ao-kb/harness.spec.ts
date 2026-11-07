import { expect, test } from "@playwright/test";

// The starter installs, indexes, and projects the AO fixture; journey specs build on this.
test("the AO KB fixture projects as typed knowledge", async ({ request }) => {
  const response = await request.post("/api/v1/graphql", {
    data: {
      query:
        "{ assertion(first: 50) { aoId } conflictRegistry(first: 1) { groups { entries { title } } } }",
    },
  });

  expect(response.ok()).toBeTruthy();
  const { data } = await response.json();
  expect(data.assertion).toHaveLength(22);
  expect(data.conflictRegistry[0].groups[0].entries).toHaveLength(2);
});
