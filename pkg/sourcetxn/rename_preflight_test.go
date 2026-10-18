package sourcetxn

import (
	"errors"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPreflightRepairRenameEndpointsPropagatesUnexpectedStatErrors(t *testing.T) {
	sentinel := errors.New("lstat unavailable")
	rename := Rename{From: "old.md", To: "new.md"}
	tests := []struct {
		name      string
		sourceErr error
		targetErr error
		wantErr   error
	}{
		{name: "missing endpoints remain recoverable", sourceErr: os.ErrNotExist, targetErr: os.ErrNotExist},
		{name: "source error fails closed", sourceErr: sentinel, targetErr: os.ErrNotExist, wantErr: sentinel},
		{name: "destination error fails closed", sourceErr: os.ErrNotExist, targetErr: sentinel, wantErr: sentinel},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			calls := 0
			err := preflightRepairRenameEndpoints(rename, "source", "destination", func(string) (os.FileInfo, error) {
				calls++
				if calls == 1 {
					return nil, tt.sourceErr
				}
				return nil, tt.targetErr
			})
			if tt.wantErr == nil {
				require.NoError(t, err)
				return
			}
			assert.ErrorIs(t, err, tt.wantErr)
		})
	}
}
