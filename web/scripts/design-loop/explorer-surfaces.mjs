export const EXPLORER_SURFACES = [
  { id: "explorer", title: "Explorer (graph)", url: "/explorer" },
  {
    id: "explorer-search",
    title: "Explorer · search",
    url: "/explorer",
    act: async (p) => {
      const search = p.getByRole("searchbox", { name: "Search notes and code" });
      await search.fill("search");
      await search.press("Enter");
      await p.locator(".explorer-bar__matches").waitFor();
    },
  },
  {
    id: "c-explorer-folder",
    title: "Explorer · folder expanded (pkg/app)",
    url: "/explorer",
    act: async (p) => {
      console.log(
        "Explorer first view:",
        await p.getByRole("img", { name: /^Graph of / }).getAttribute("aria-label"),
      );
      const started = performance.now();
      await p.locator(".explorer-rail").getByRole("button", { name: "app", exact: true }).click();
      await p.locator('[data-graph-ready="true"]').waitFor();
      console.log("Explorer folder click to ready:", Math.round(performance.now() - started), "ms");
    },
  },
  {
    id: "c-explorer-deep",
    title: "Explorer · deep folder after two expansions (pkg/app, then pkg/app/cli)",
    url: "/explorer",
    act: async (p) => {
      const rail = p.locator(".explorer-rail");
      await rail.getByRole("button", { name: "app", exact: true }).click();
      await p.locator('[data-graph-ready="true"]').waitFor();
      await rail
        .getByRole("button", { name: "app", exact: true })
        .locator("..")
        .getByRole("button", { name: "cli", exact: true })
        .click();
      await p.locator('[data-graph-ready="true"]').waitFor();
    },
  },
  {
    id: "c-explorer-file",
    title: "Explorer · code file focused with its neighbours",
    url: "/explorer?file=pkg%2Fsearch%2Fplanner%2Fplanner.go",
    act: async (p) => {
      await p.locator('[data-graph-ready="true"]').waitFor();
    },
  },
];
