package traffic

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestBillingResponseValidationAcceptsOneCompleteObject(t *testing.T) {
	for _, body := range []string{
		`{"data":[1,true,null,"str",{},[]],"usage":{"total_tokens":10}}`,
		" {\"usage\":{\"total_tokens\":10},\"data\":[]} \n\t",
	} {
		var scan usageScanner
		for _, c := range []byte(body) {
			_, _ = scan.Write([]byte{c})
		}
		require.Equal(t, `{"total_tokens":10}`, string(scan.result()))
	}
}

// BenchmarkUsageValidation separates the prior bounded extractor's cost from
// complete response validation for a 1 MiB numeric embedding payload.
func BenchmarkUsageValidation(b *testing.B) {
	data := []byte(`{"data":[` + strings.Repeat("0.12345,", (1<<20)/8) + `0],"usage":{"total_tokens":10}}`)
	for _, full := range []bool{false, true} {
		name := "extract"
		if full {
			name = "validate"
		}
		b.Run(name, func(b *testing.B) {
			b.SetBytes(int64(len(data)))
			b.ReportAllocs()
			for range b.N {
				var scan usageScanner
				if full {
					for start := 0; start < len(data); start += 32 << 10 {
						_, _ = scan.Write(data[start:min(start+(32<<10), len(data))])
					}
					if string(scan.result()) != `{"total_tokens":10}` {
						b.Fatal("validation lost the expected usage")
					}
				} else {
					for _, c := range data {
						scan.step(c)
					}
					if string(scan.usage) != `{"total_tokens":10}` {
						b.Fatal("extraction lost the expected usage")
					}
				}
			}
		})
	}
}
