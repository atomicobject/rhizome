import { spawnSync } from "node:child_process";
import fs from "node:fs";
import os from "node:os";
import path from "node:path";

const repoRoot = process.env.RHIZOME_REPO_ROOT;

const port = process.env.RHIZOME_E2E_PORT || "4173";

if (!repoRoot) {
  throw new Error("RHIZOME_REPO_ROOT is required");
}

const fixtureSrc = path.join(repoRoot, "testdata", "integration", "python-app", "vault");

const fixtureRoot = fs.mkdtempSync(path.join(os.tmpdir(), "rhizome-web-e2e-"));

fs.cpSync(fixtureSrc, fixtureRoot, { recursive: true });

fs.copyFileSync(
  path.join(repoRoot, "web", "tests", "e2e", "fixtures", "mermaid-note.md"),
  path.join(fixtureRoot, "notes", "mermaid-note.md"),
);

const htmlFixture = path.join(repoRoot, "testdata", "integration", "html-notes");

fs.cpSync(path.join(htmlFixture, "reports"), path.join(fixtureRoot, "reports"), {
  recursive: true,
});

fs.appendFileSync(
  path.join(fixtureRoot, ".rhizome", "config.yml"),
  '\nnotes:\n  includes: ["**/*.md", "**/*.html", "**/*.htm"]\n  links: all\n',
);

fs.appendFileSync(
  path.join(fixtureRoot, ".rhizome", "ontology", "schema.graphql"),
  '\n\ntype Report @node(paths: ["reports/typed-report.html", "reports/prototype.html"]) {\n  title: String!\n  status: String\n  tags: [String!]\n}\n',
);

// Each overlay adds notes, optional .rhizome files, and schema types.
for (const overlay of ["unified-views", "group-views", "type-views"]) {
  const overlayRoot = path.join(repoRoot, "web", "tests", "e2e", "fixtures", overlay);

  for (const directory of ["notes", ".rhizome"]) {
    if (!fs.existsSync(path.join(overlayRoot, directory))) continue;
    fs.cpSync(path.join(overlayRoot, directory), path.join(fixtureRoot, directory), {
      recursive: true,
    });
  }

  fs.appendFileSync(
    path.join(fixtureRoot, ".rhizome", "ontology", "schema.graphql"),
    fs.readFileSync(path.join(overlayRoot, "schema.graphql"), "utf8"),
  );
}

const goEnv = {
  GOCACHE: process.env.GOCACHE || path.join(repoRoot, ".gocache"),
  GOMODCACHE: process.env.GOMODCACHE || path.join(repoRoot, ".gomodcache"),
  GOTMPDIR: process.env.GOTMPDIR || path.join(repoRoot, ".gotmp"),
  RZM_SKIP_REPO_DELEGATE: "1",
};

for (const dir of Object.values(goEnv)) {
  fs.mkdirSync(dir, { recursive: true });
}

const run = (cmd, args, cwd = repoRoot) => {
  const result = spawnSync(cmd, args, {
    cwd,
    env: { ...process.env, ...goEnv },
    stdio: "inherit",
  });

  if (result.status !== 0) {
    process.exit(result.status ?? 1);
  }
};

run("npm", ["run", "build"], path.join(repoRoot, "web"));

run("go", ["run", ".", "index", "--vault", fixtureRoot]);

run("go", ["run", ".", "serve", "--host", "127.0.0.1", "--port", port, "--vault", fixtureRoot]);
