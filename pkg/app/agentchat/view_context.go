package agentchat

import (
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"
)

type ViewContext struct {
	Kind   string `json:"kind"`
	Title  string `json:"title"`
	Path   string `json:"path,omitempty"`
	ViewID string `json:"viewId,omitempty"`
	Route  string `json:"route"`
	Detail string `json:"detail,omitempty"`
}

func (v *ViewContext) Validate() error {
	if v == nil {
		return nil
	}
	switch v.Kind {
	case "note", "view", "type", "group", "ontology", "notes", "section":
	default:
		return fmt.Errorf("invalid context kind")
	}
	if strings.TrimSpace(v.Title) == "" || utf8.RuneCountInString(v.Title) > 200 {
		return fmt.Errorf("context title is required and must be at most 200 runes")
	}
	if len(v.Path) > 1024 || len(v.ViewID) > 1024 {
		return fmt.Errorf("context path and viewId must be at most 1024 bytes")
	}
	if !strings.HasPrefix(v.Route, "/") || len(v.Route) > 2048 {
		return fmt.Errorf("context route must start with / and be at most 2048 bytes")
	}
	if utf8.RuneCountInString(v.Detail) > 2000 {
		return fmt.Errorf("context detail must be at most 2000 runes")
	}
	return nil
}

// SetProblems names the source of user-visible problems that each turn reports
// to the agent.
func (s *Service) SetProblems(problems func() []string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.problems = problems
}

func (s *Service) currentProblems() []string {
	s.mu.Lock()
	problems := s.problems
	s.mu.Unlock()
	if problems == nil {
		return nil
	}
	return problems()
}

// problemsBlock lists the setup failures the app is showing the user, so the
// agent can explain a banner without asking for its text.
func problemsBlock(problems []string) string {
	if len(problems) == 0 {
		return ""
	}
	block := "[Rhizome problems] The app is showing the user these problems:"
	for _, problem := range problems {
		if runes := []rune(problem); len(runes) > 2000 {
			problem = string(runes[:2000]) + "…"
		}
		block += "\n- " + strings.ReplaceAll(problem, "\n", "\n  ")
	}
	return block + "\n\n"
}

func turnText(content string, v *ViewContext, problems []string) string {
	content = problemsBlock(problems) + content
	if v == nil {
		return content
	}
	kind := []rune(v.Kind)
	kind[0] = unicode.ToUpper(kind[0])
	block := fmt.Sprintf("[Rhizome view] %s %q", string(kind), v.Title)
	if v.Path != "" {
		block += " · " + v.Path
	} else if v.ViewID != "" {
		block += " · " + v.ViewID
	}
	block += "\nRoute: " + v.Route
	if v.Detail != "" {
		block += "\nDetail: " + v.Detail
	}
	return block + "\n\n" + content
}
