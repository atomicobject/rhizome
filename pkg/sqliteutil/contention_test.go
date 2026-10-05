package sqliteutil

import (
	"errors"
	"fmt"
	"testing"

	"github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/require"
)

func TestIsBusyOrLocked(t *testing.T) {
	for _, tc := range []struct {
		name string
		err  error
		want bool
	}{
		{"nil", nil, false},
		{"busy", sqlite3.Error{Code: sqlite3.ErrBusy}, true},
		{"locked", sqlite3.Error{Code: sqlite3.ErrLocked}, true},
		{"wrapped busy", fmt.Errorf("open index: %w", sqlite3.Error{Code: sqlite3.ErrBusy}), true},
		{"wrapped locked", fmt.Errorf("read index: %w", sqlite3.Error{Code: sqlite3.ErrLocked}), true},
		{"busy snapshot", sqlite3.Error{Code: sqlite3.ErrBusy, ExtendedCode: sqlite3.ErrBusySnapshot}, true},
		{"message only", errors.New("database is locked"), false},
		{"permanent", sqlite3.Error{Code: sqlite3.ErrCorrupt}, false},
	} {
		t.Run(tc.name, func(t *testing.T) { require.Equal(t, tc.want, IsBusyOrLocked(tc.err)) })
	}
}
