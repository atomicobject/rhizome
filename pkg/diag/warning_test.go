package diag

import (
	"encoding/json"
	"testing"
)

func TestWarningJSONContract(t *testing.T) {
	for _, test := range []struct {
		warning Warning
		want    string
	}{
		{Warning{Code: "index_unavailable", Message: "Index is unavailable."}, `{"code":"index_unavailable","message":"Index is unavailable."}`},
		{Warning{Code: "invalid_path", Message: "Path is outside the vault.", Path: "../outside.md"}, `{"code":"invalid_path","message":"Path is outside the vault.","path":"../outside.md"}`},
		{Warning{}, `{"code":"","message":""}`},
	} {
		got, err := json.Marshal(test.warning)
		if err != nil {
			t.Fatal(err)
		}
		if string(got) != test.want {
			t.Fatalf("got %s, want %s", got, test.want)
		}
	}
}
