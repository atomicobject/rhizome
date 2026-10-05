package obsidian

import (
	"reflect"
	"testing"
)

func TestExtractInlineProperties(t *testing.T) {
	content := `---
title: Test
tags: [a]
---

Office:: [[AORD]]
Notes:: Some text
Invalid: line

Archetype:: [[Explorer]]
Office:: [[AOGR]]
`
	props := ExtractInlineProperties(content)
	if len(props["Office"]) != 2 || props["Office"][0] != "[[AORD]]" || props["Office"][1] != "[[AOGR]]" {
		t.Fatalf("unexpected office props: %+v", props["Office"])
	}
	if len(props["Archetype"]) != 1 || props["Archetype"][0] != "[[Explorer]]" {
		t.Fatalf("unexpected archetype props: %+v", props["Archetype"])
	}
	if _, ok := props["Invalid"]; ok {
		t.Fatalf("invalid line should not be parsed")
	}
}

func TestExtractInlinePropertiesSkipsBulletsAndCode(t *testing.T) {
	content := "Notes:: Good\n- Activity:: Should be ignored\n* Win:: Ignored\n+ Gratitude:: Ignored\n1. Numbered:: Ignored\n| Table:: Ignored\n![:clap:: should-be-ignored\n* ![:pray:][image184]4:+1:: also ignore\n> Quote:: Ignored\n```" + "\nCode:: Ignore\n```" + "\nValidKey_1:: Kept\n"
	props := ExtractInlineProperties(content)
	if !reflect.DeepEqual(props, map[string][]string{"Notes": {"Good"}, "ValidKey_1": {"Kept"}}) {
		t.Fatalf("unexpected inline properties: %#v", props)
	}

}

func TestExtractInlinePropertiesRequiresNoSpacesInKey(t *testing.T) {
	content := "White :: Neutral and objective\nRed :: Emotion\nBlue::Good\n"
	props := ExtractInlineProperties(content)
	if _, ok := props["White"]; ok {
		t.Fatalf("key with spaces before :: should be ignored")
	}
	if _, ok := props["Red"]; ok {
		t.Fatalf("key with spaces before :: should be ignored")
	}
	if val, ok := props["Blue"]; !ok || len(val) != 1 || val[0] != "Good" {
		t.Fatalf("expected Blue to be parsed, got %+v", props["Blue"])
	}
}

func TestExtractInlinePropertiesPackedSingleParagraph(t *testing.T) {
	content := `id:: SPEC-0013.US1 summary:: A manager can review aggregate fleet health and open a detail view of carts that need
attention. status:: ready estimate:: M`

	props := ExtractInlineProperties(content)
	if val := props["id"]; len(val) != 1 || val[0] != "SPEC-0013.US1" {
		t.Fatalf("expected id to be parsed, got %+v", val)
	}
	if val := props["summary"]; len(val) != 1 || val[0] != "A manager can review aggregate fleet health and open a detail view of carts that need attention." {
		t.Fatalf("expected summary to include wrapped continuation, got %+v", val)
	}
	if val := props["status"]; len(val) != 1 || val[0] != "ready" {
		t.Fatalf("expected status to be parsed, got %+v", val)
	}
	if val := props["estimate"]; len(val) != 1 || val[0] != "M" {
		t.Fatalf("expected estimate to be parsed, got %+v", val)
	}
}

func TestExtractInlinePropertiesPackedSingleParagraphKeepsNamespaceLikeValueText(t *testing.T) {
	content := `summary:: Uses std::vector container semantics for parity. status:: ready`

	props := ExtractInlineProperties(content)
	if val := props["summary"]; len(val) != 1 || val[0] != "Uses std::vector container semantics for parity." {
		t.Fatalf("expected summary to keep namespace-like text, got %+v", val)
	}
	if val := props["status"]; len(val) != 1 || val[0] != "ready" {
		t.Fatalf("expected status to be parsed, got %+v", val)
	}
	if _, ok := props["std"]; ok {
		t.Fatalf("namespace token should not become a property: %+v", props)
	}
}

func TestExtractInlinePropertyOccurrencesPreservesAuthoredOrder(t *testing.T) {
	content := "second:: two first:: one\nthird:: three\n"
	got := ExtractInlinePropertyOccurrences(content)
	if len(got) != 3 || got[0] != (InlineProperty{Key: "second", Value: "two"}) || got[1] != (InlineProperty{Key: "first", Value: "one"}) || got[2] != (InlineProperty{Key: "third", Value: "three"}) {
		t.Fatalf("occurrences = %#v", got)
	}
}
