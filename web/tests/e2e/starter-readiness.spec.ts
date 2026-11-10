import { expect, test } from "./fixtures";

// The HTTP owner has separate runtime regressions. This journey exercises the
// shell's visible choice, retry, and navigation path through its public contract.
test("starter recovery remains visible across navigation and reload", async ({ page }) => {
  let state = "required_input";
  const requests: unknown[] = [];
  await page.route("**/api/v1/starter-readiness**", async (route) => {
    if (route.request().method() === "POST") {
      requests.push(route.request().postDataJSON());
      state = state === "required_input" ? "failed" : "ready";
    }

    await route.fulfill({
      json: [
        {
          module: "ao-kb",
          title: "AO Knowledge Base",
          state,
          reason:
            state === "required_input"
              ? "Choose a product"
              : "uv is missing. Install uv, then retry.",
          recovery:
            state === "required_input"
              ? [
                  {
                    kind: "choice",
                    label: "Select product",
                    input: "product",
                    choices: ["alpha", "beta"],
                  },
                ]
              : [{ kind: "retry", label: "Retry setup" }],
        },
      ],
    });
  });
  await page.goto("/notes");
  const notice = page.getByRole("complementary", { name: "AO Knowledge Base setup" });
  await expect(notice).toBeVisible();
  await notice.getByRole("combobox").selectOption("beta");
  await notice.getByRole("button", { name: "Use this product" }).click();
  await expect.poll(() => requests).toEqual([{ kind: "choice", value: "beta" }]);
  await page.goto("/ontology");
  await expect(notice).toContainText("uv is missing");
  await page.reload();
  await expect(notice).toContainText("uv is missing");
  await notice.getByRole("button", { name: "Retry setup" }).click();
  await expect(notice).toBeHidden({ timeout: 10000 });
  expect(requests).toEqual([{ kind: "choice", value: "beta" }, { kind: "retry" }]);
});
