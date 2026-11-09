package web

import (
	"strings"
	"testing"
)

func TestCustomViewShellSkipsTailwindOnlyForExperiencesThatOptOut(t *testing.T) {
	srv := newCustomViewTestServer(t, "board.tsx", map[string]string{
		"poc/board.tsx":       "export default function Board() { return null }",
		"lean/experience.yml": "title: Lean\ntailwind: false\n",
		"lean/home.yaml":      customView("lean.home", "{kind: standalone}", "home.tsx"),
		"lean/home.tsx":       "export default function Home() { return null }",
		"site/experience.yml": "title: Site\n",
		"site/home.yaml":      customView("site.home", "{kind: standalone}", "home.tsx"),
		"site/home.tsx":       "export default function Home() { return null }",
	})
	for view, want := range map[string]bool{"lean.home": true, "site.home": false, "poc.board": false} {
		body := getCustomView(srv, "/views/"+view).Body.String()
		if got := strings.Contains(body, `boot.js" data-tailwind="false"></script>`); got != want {
			t.Errorf("%s: data-tailwind=false on the boot script = %v, want %v:\n%s", view, got, want, body)
		}
	}
}
