package structured

import (
	"reflect"
	"strings"
	"testing"
)

func TestDiagnostics(t *testing.T) {
	cases := []struct {
		code, input string
		json        bool
	}{
		{"yaml_parse_error", "a: [", false},
		{"yaml_parse_error", "\xff", false},
		{"json_parse_error", `{"a":`, true},
		{"json_parse_error", `{} null`, true},
		{"json_parse_error", `[1,]`, true},
		{"json_parse_error", "\xff", true},
		{"json_parse_error", `"\ud800"`, true},
		{"json_parse_error", `"\udc00"`, true},
		{"json_parse_error", `{"\ud800":1}`, true},
		{"yaml_tag_unsupported", "a: !custom hello", false},
		{"yaml_tag_unsupported", "a: !!str hello", false},
		{"yaml_anchor_unsupported", "a: &x hello", false},
		{"yaml_alias_unsupported", "a: &x [*x]", false},
		{"yaml_merge_key_unsupported", "<<: {}", false},
		{"yaml_duplicate_key", "a: 1\na: 2", false},
		{"yaml_duplicate_key", "01: a\n1: b", false},
		{"yaml_duplicate_key", "1: a\n\"1\": b", false},
		{"json_duplicate_key", `{"a":1,"\u0061":2}`, true},
		{"yaml_complex_key_unsupported", "? [a,b]\n: c", false},
		{"yaml_complex_key_unsupported", "? {a: b}\n: c", false},
		{"yaml_key_type_unsupported", "true: a", false},
		{"yaml_key_type_unsupported", "null: a", false},
		{"yaml_key_type_unsupported", "1.0: a", false},
		{"yaml_multiple_documents", "a: 1\n---\nb: 2", false},
		{"yaml_multiple_documents", "a: 1\n---\n", false},
		{"yaml_scalar_unsupported", "a: .nan", false},
	}
	for _, c := range cases {
		t.Run(c.code+"/"+c.input, func(t *testing.T) {
			parse := ParseYAML
			if c.json {
				parse = ParseJSON
			}
			v, d := parse([]byte(c.input))
			if v != nil {
				t.Fatal("partial tree returned")
			}
			for _, diag := range d {
				if diag.Code == c.code {
					if strings.Contains(c.code, "duplicate") && (len(diag.Related) != 1 || diag.Start.Offset <= diag.Related[0].Offset) {
						t.Fatalf("duplicate locations: %+v", diag)
					}
					return
				}
			}
			t.Fatalf("missing %s: %+v", c.code, d)
		})
	}
}

func TestOrderedImmutableTree(t *testing.T) {
	for _, parse := range []func([]byte) (*Value, []Diagnostic){ParseYAML, ParseJSON} {
		data := []byte(`{"z":null,"a/b~":["",[],{},true,123456789012345678901234567890]}`)
		root, d := parse(data)
		if len(d) > 0 {
			t.Fatal(d)
		}
		entries := root.Entries()
		if entries[0].Key != "z" || entries[1].Key != "a/b~" {
			t.Fatal(entries)
		}
		null, ok := root.Lookup("z")
		if !ok || null.Kind() != Null {
			t.Fatal("present null lost")
		}
		if _, ok = root.Lookup("absent"); ok {
			t.Fatal("missing became present")
		}
		items := entries[1].Value.Items()
		for i, kind := range []Kind{String, Array, Object, Bool, Number} {
			if items[i].Kind() != kind {
				t.Fatal(items[i])
			}
		}
		if items[0].Address() != "/a~1b~0/0" || items[4].Text() != "123456789012345678901234567890" {
			t.Fatal(items)
		}
		entries[0].Key = "changed"
		items[0] = nil
		data[0] = '!'
		if root.Entries()[0].Key != "z" || root.Entries()[1].Value.Items()[0] == nil {
			t.Fatal("mutable tree")
		}
	}
}

func TestYAMLLocationsAndQuotedMerge(t *testing.T) {
	data := []byte("\xef\xbb\xbf\"é\": [null, \"x\"]\r\n\"<<\": {}\r\n")
	root, d := ParseYAML(data)
	if len(d) > 0 {
		t.Fatal(d)
	}
	items := root.Entries()[0].Value.Items()
	for i, want := range []Position{{1, 7, 10}, {1, 13, 16}} {
		if items[i].Start() != want {
			t.Fatalf("got %+v want %+v", items[i].Start(), want)
		}
	}
	second := root.Entries()[1]
	if second.Start != (Position{2, 1, 22}) {
		t.Fatalf("second: %+v", second.Start)
	}
	if second.Key != "<<" || second.Value.Kind() != Object {
		t.Fatal(second)
	}
}

func TestTraversalDocumentOrder(t *testing.T) {
	root, d := ParseYAML([]byte("b: [1, {c: 2}]\na: 3"))
	if len(d) > 0 {
		t.Fatal(d)
	}
	var got []string
	var walk func(*Value)
	walk = func(v *Value) {
		got = append(got, v.Address())
		for _, i := range v.Items() {
			walk(i)
		}
		for _, e := range v.Entries() {
			walk(e.Value)
		}
	}
	walk(root)
	want := []string{"", "/b", "/b/0", "/b/1", "/b/1/c", "/a"}
	if !reflect.DeepEqual(got, want) {
		t.Fatal(got)
	}
}

func TestJSONUnicodeAndLocations(t *testing.T) {
	root, d := ParseJSON([]byte("{\"é\": \"\\ud83d\\ude00\",\r\n\"z\":null}"))
	if len(d) > 0 {
		t.Fatal(d)
	}
	e := root.Entries()
	if e[0].Value.Text() != "😀" || e[0].Value.Start() != (Position{1, 7, 7}) || e[1].Start != (Position{2, 1, 24}) {
		t.Fatalf("%+v, %+v", e, e[0].Value.Start())
	}
}

func TestYAMLUnicodeLineBreaks(t *testing.T) {
	for _, sep := range []string{"\u0085", "\u2028", "\u2029"} {
		data := []byte("a: 1" + sep + "b: 2")
		root, d := ParseYAML(data)
		if len(d) > 0 {
			t.Fatal(d)
		}
		want := Position{2, 1, len("a: 1" + sep)}
		if got := root.Entries()[1].Start; got != want {
			t.Fatalf("%q: %+v want %+v", sep, got, want)
		}
	}
}

func TestReturnedHandlesCannotOverwriteChildren(t *testing.T) {
	root, d := ParseJSON([]byte(`{"a":[null]}`))
	if len(d) > 0 {
		t.Fatal(d)
	}
	entries := root.Entries()
	items := entries[0].Value.Items()
	*items[0] = Value{}
	*entries[0].Value = Value{}
	found, ok := root.Lookup("a")
	if !ok {
		t.Fatal("missing a")
	}
	*found = Value{}
	still, ok := root.Lookup("a")
	if !ok || still.Kind() != Array || still.Items()[0].Kind() != Null {
		t.Fatal("child overwritten through returned handle")
	}
}
