package init

import (
	"context"
	"encoding/json"
	"io"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/atomicobject/rhizome/internal/testkit"
	appviews "github.com/atomicobject/rhizome/pkg/app/views"
	appweb "github.com/atomicobject/rhizome/pkg/app/web"
	"github.com/atomicobject/rhizome/pkg/ontology/viewconfig"
	"github.com/atomicobject/rhizome/pkg/validate"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/atomicobject/rhizome/pkg/vault/repoconfig"
	"github.com/stretchr/testify/require"
)

// ejectedUtility is an arbitrary-value Tailwind utility that no bundled view
// or kit source uses, so the precompiled stylesheet (whose @source covers
// web/kit and the bundled starters' view folders) cannot contain it.
const ejectedUtility = "grid-cols-[repeat(7,minmax(0,1fr))]"

// SPEC-0136 R9 and US7: a team ejects the AO KB starter, extends its
// Knowledge experience in place, and init leaves the copy alone while the
// new page, the changed panel, and Tailwind all work from it.
func TestEjectedAOKBExperienceKeepsLocalPagesAndPanel(t *testing.T) {
	for _, dir := range []string{"templates/starters", filepath.Join("..", "..", "..", "..", "web", "kit")} {
		require.NoError(t, filepath.WalkDir(dir, func(path string, entry fs.DirEntry, err error) error {
			if err != nil || entry.IsDir() {
				return err
			}
			data, err := os.ReadFile(path)
			require.NoError(t, err)
			require.NotContains(t, string(data), ejectedUtility, "%s uses the utility, so the precompiled stylesheet would carry it", path)
			return nil
		}))
	}

	root := installAOKBFixture(t)
	_, err := runInit(t, RunOptions{Env: testkit.Env(t), Dir: root, Eject: "ao-kb"})
	require.NoError(t, err)
	require.Contains(t, loadConfigForTest(t, root).WorkflowTemplateManagement.Ejected, "ao-kb")

	site := ".rhizome/modules/ao-kb/views/knowledge/"
	experience := readTestFile(t, root, site+"experience.yml")
	require.Contains(t, experience, "tailwind: false\n")
	writeTestFile(t, root, site+"experience.yml", strings.Replace(experience, "tailwind: false\n", "tailwind: true\n", 1))
	panel := readTestFile(t, root, site+"panel.yaml")
	require.Contains(t, panel, "name: Knowledge\n")
	writeTestFile(t, root, site+"panel.yaml", strings.Replace(panel, "name: Knowledge\n", "name: Team knowledge\n", 1))
	writeTestFile(t, root, site+"glossary.yaml", "apiVersion: rhizome.view.v1\nid: team.glossary\nname: Glossary\nsource:\n  kind: custom\n  entry: glossary.tsx\nmount:\n  kind: group\n  group: Knowledge\n  order: 90\n")
	writeTestFile(t, root, site+"glossary.tsx", "export default function Glossary() {\n  return <div className=\""+ejectedUtility+"\">Glossary</div>;\n}\n")

	before := aoKBFiles(t, filepath.Join(root, ".rhizome", "modules", "ao-kb"))
	_, err = runInit(t, RunOptions{Env: testkit.Env(t), Dir: root})
	require.NoError(t, err)
	require.Equal(t, before, aoKBFiles(t, filepath.Join(root, ".rhizome", "modules", "ao-kb")), "init leaves the ejected module untouched")
	_, err = runInit(t, RunOptions{Env: testkit.Env(t), Dir: root, Check: true})
	require.NoError(t, err)

	result := validate.RunViews(context.Background(), validate.RunContext{
		VaultDef: obsidian.VaultDefinition{Path: root}, VaultPath: root, NoteReader: &obsidian.Note{}, MaxIssues: 100,
	})
	require.True(t, result.OK, "%+v", result.Issues)

	srv := serveEjectedVault(t, root)
	var catalog appviews.Catalog
	getJSON(t, srv, "/api/v1/views", &catalog)
	keys := map[string]string{}
	for _, entry := range catalog.Views {
		keys[entry.ID] = entry.ExperienceKey
	}
	key := keys["ao-kb.overview"]
	require.NotEmpty(t, key)
	require.Equal(t, key, keys["team.glossary"], "the new page joins the Knowledge experience")
	require.Equal(t, key, keys["ao-kb.panel"])
	want := viewconfig.Panel{Kind: viewconfig.PanelView, ViewID: "ao-kb.panel", ExperienceKey: key}
	var group *appviews.ViewTarget
	for i, target := range catalog.Targets {
		if target.Kind == viewconfig.MountKindGroup && target.Name == "Knowledge" {
			group = &catalog.Targets[i]
		}
	}
	require.NotNil(t, group)
	require.Equal(t, want, group.Panel, "the group still resolves to the ejected panel")
	var listed bool
	for _, choice := range group.Choices {
		listed = listed || choice.ViewID == "team.glossary"
	}
	require.True(t, listed, "the new page is a choice for the Knowledge group: %+v", group.Choices)

	knowledge := `{"kind":"group","group":"Knowledge"}`
	page := getInvocationJSON(t, srv, "team.glossary", knowledge)
	require.Equal(t, key, page.View.ExperienceKey)
	panelInvocation := getInvocationJSON(t, srv, "ao-kb.panel", knowledge)
	require.Equal(t, "Team knowledge", panelInvocation.View.Name, "the panel serves the ejected copy")
	require.Equal(t, key, panelInvocation.View.ExperienceKey)

	entry := get(t, srv, page.Entry)
	require.Equal(t, http.StatusOK, entry.StatusCode)
	require.Contains(t, readBody(t, entry), ejectedUtility, "the page module carries the new utility")
	shell := get(t, srv, "/views/team.glossary?hosted=1&context="+url.QueryEscape(knowledge))
	require.Equal(t, http.StatusOK, shell.StatusCode)
	body := readBody(t, shell)
	require.Contains(t, body, "boot.js")
	require.NotContains(t, body, `data-tailwind="false"`, "an experience with tailwind: true loads the compiler")
}

type ejectedInvocation struct {
	View struct {
		Name          string `json:"name"`
		ExperienceKey string `json:"experienceKey"`
	} `json:"view"`
	Entry string `json:"entry"`
}

func serveEjectedVault(t *testing.T, root string) *httptest.Server {
	t.Helper()
	env := testkit.Env(t)
	server, err := appweb.NewServer(context.Background(), appweb.Config{
		Vault:             &repoconfig.Vault{Env: env, Name: "ejected"},
		VaultPath:         root,
		VaultDef:          obsidian.VaultDefinition{Name: "ejected", Path: root, Includes: []string{"**/*.md"}},
		NoteMetadata:      testkit.NoteMetadataIndexer(t),
		RepositoryTrusted: func(string) (bool, error) { return true, nil },
	}, fstest.MapFS{"kit/v1/boot.js": &fstest.MapFile{Data: []byte("//")}})
	require.NoError(t, err)
	t.Cleanup(func() { _ = server.Close() })
	httpServer := httptest.NewServer(server.Handler())
	t.Cleanup(httpServer.Close)
	return httpServer
}

func get(t *testing.T, srv *httptest.Server, path string) *http.Response {
	t.Helper()
	resp, err := http.Get(srv.URL + path)
	require.NoError(t, err)
	t.Cleanup(func() { _ = resp.Body.Close() })
	return resp
}

func readBody(t *testing.T, resp *http.Response) string {
	t.Helper()
	data, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	return string(data)
}

func getJSON(t *testing.T, srv *httptest.Server, path string, out any) {
	t.Helper()
	resp := get(t, srv, path)
	body := readBody(t, resp)
	require.Equal(t, http.StatusOK, resp.StatusCode, body)
	require.NoError(t, json.Unmarshal([]byte(body), out), body)
}

func getInvocationJSON(t *testing.T, srv *httptest.Server, view, viewContext string) ejectedInvocation {
	t.Helper()
	var invocation ejectedInvocation
	getJSON(t, srv, "/api/v1/views/"+view+"/invocation?"+url.Values{"hosted": {"1"}, "context": {viewContext}}.Encode(), &invocation)
	require.NotEmpty(t, invocation.Entry, view)
	return invocation
}
