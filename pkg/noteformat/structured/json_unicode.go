package structured

import (
	"fmt"
	"strconv"
)

// encoding/json replaces unpaired UTF-16 escapes with U+FFFD. Refuse that
// irreversible substitution while allowing authored replacement characters.
func checkJSONSurrogates(raw []byte) error {
	for i := 1; i < len(raw)-1; i++ {
		if raw[i] != '\\' {
			continue
		}
		i++
		if raw[i] != 'u' {
			continue
		}
		code, _ := strconv.ParseUint(string(raw[i+1:i+5]), 16, 16)
		i += 4
		if code >= 0xdc00 && code <= 0xdfff {
			return fmt.Errorf("unpaired UTF-16 surrogate")
		}
		if code < 0xd800 || code > 0xdbff {
			continue
		}
		if i+6 >= len(raw) || raw[i+1] != '\\' || raw[i+2] != 'u' {
			return fmt.Errorf("unpaired UTF-16 surrogate")
		}
		low, _ := strconv.ParseUint(string(raw[i+3:i+7]), 16, 16)
		if low < 0xdc00 || low > 0xdfff {
			return fmt.Errorf("unpaired UTF-16 surrogate")
		}
		i += 6
	}
	return nil
}
