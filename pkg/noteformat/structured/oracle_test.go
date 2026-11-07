package structured

import (
	"encoding/json"
	"math/big"
	"os"
	"testing"
	"time"
)

func TestPinnedScalarOracle(t *testing.T) {
	data, err := os.ReadFile("testdata/scalar-oracle.json")
	if err != nil {
		t.Fatal(err)
	}
	var oracle struct {
		Pyyaml, Profile string
		Cases           []struct{ Source, Kind, Text, Unsupported string }
	}
	if err = json.Unmarshal(data, &oracle); err != nil {
		t.Fatal(err)
	}
	if oracle.Pyyaml != "6.0.3" || oracle.Profile != ScalarProfile {
		t.Fatal("oracle pin mismatch")
	}
	for _, c := range oracle.Cases {
		t.Run(c.Source, func(t *testing.T) {
			v, diags := ParseYAML([]byte(c.Source))
			if c.Unsupported != "" {
				if v != nil || len(diags) != 1 || diags[0].Code != "yaml_scalar_unsupported" {
					t.Fatalf("expected unsupported: %v, %v", v, diags)
				}
				return
			}
			if len(diags) > 0 || v == nil {
				t.Fatalf("parse: %v", diags)
			}
			if string(v.Kind()) != c.Kind {
				t.Fatalf("kind %s, want %s", v.Kind(), c.Kind)
			}
			switch v.Kind() {
			case Number:
				got, ok := new(big.Rat).SetString(v.Text())
				want, valid := new(big.Rat).SetString(c.Text)
				if !ok || !valid || got.Cmp(want) != 0 {
					t.Fatalf("number %s, want %s", v.Text(), c.Text)
				}
			case Timestamp:
				got, e := time.Parse(time.RFC3339Nano, v.Text())
				want, e2 := time.Parse(time.RFC3339Nano, c.Text)
				_, goff := got.Zone()
				_, woff := want.Zone()
				if e != nil || e2 != nil || !got.Equal(want) || goff != woff {
					t.Fatalf("timestamp %s, want %s", v.Text(), c.Text)
				}
			default:
				if v.Text() != c.Text {
					t.Fatalf("text %q, want %q", v.Text(), c.Text)
				}
			}
		})
	}
}
