package obsidian

import "testing"

func TestMarkdownH1TitleSemanticsMatchLegacyMetadataTitles(t *testing.T) {
	tests := []struct {
		name    string
		content string
		first   string
		single  string
	}{
		{name: "wiki display and closing hashes", content: "# [[Cache Hub|Cache Hub Docs]] ###\n", first: "Cache Hub Docs", single: "Cache Hub Docs"},
		{name: "C sharp preserves final hash", content: "# C#\n", first: "C#", single: "C#"},
		{name: "F sharp preserves final hash", content: "# F#\n", first: "F#", single: "F#"},
		{name: "indented ATX excluded", content: "  # Not legacy title\n# Actual\n", first: "Actual", single: "Actual"},
		{name: "single equals excluded", content: "Title\n=\n", first: "", single: ""},
		{name: "setext H1", content: "Title\n===\n", first: "Title", single: "Title"},
		{name: "frontmatter and fence excluded", content: "---\ntitle: no\n---\n```md\n# no\n```\n# yes\n", first: "yes", single: "yes"},
		{name: "multiple H1 has no single", content: "# One\n# Two\n", first: "One", single: ""},
		{name: "inline wikilink", content: "# [[Tech Lead Sync]] 2022-05-12\n", first: "Tech Lead Sync 2022-05-12", single: "Tech Lead Sync 2022-05-12"},
		{name: "bold wrapper", content: "# **AI in Healthcare Principles**\n", first: "AI in Healthcare Principles", single: "AI in Healthcare Principles"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := FirstMarkdownH1Title(test.content); got != test.first {
				t.Fatalf("FirstMarkdownH1Title = %q, want %q", got, test.first)
			}
			if got := SingleMarkdownH1Title(test.content); got != test.single {
				t.Fatalf("SingleMarkdownH1Title = %q, want %q", got, test.single)
			}
		})
	}
}

func TestPlainMarkdownTitle(t *testing.T) {
	tests := map[string]string{
		"[[Tech Lead Sync]] 2022-05-12":                       "Tech Lead Sync 2022-05-12",
		"Call with [[Richard Panzer|Rich]]":                   "Call with Rich",
		"See [[Note#Heading]] and [[#Local]]":                 "See Note and Local",
		"![[diagram.png]] overview":                           "diagram.png overview",
		"[Program](https://thestrangeloop.com/schedule.html)": "Program",
		"**AI in Healthcare** and *more* _here_":              "AI in Healthcare and more here",
		"***both*** ~~gone~~ ==marked==":                      "both gone marked",
		"Use `**kwargs` safely":                               "Use **kwargs safely",
		"websocket_chat_example.py":                           "websocket_chat_example.py",
		"C# and F#":                                           "C# and F#",
		"2 * 3 * 4":                                           "2 * 3 * 4",
		`\*literal\*`:                                         "*literal*",
		"  **vs**  ":                                          "vs",
	}
	for input, want := range tests {
		if got := PlainMarkdownTitle(input); got != want {
			t.Errorf("PlainMarkdownTitle(%q) = %q, want %q", input, got, want)
		}
	}
}
