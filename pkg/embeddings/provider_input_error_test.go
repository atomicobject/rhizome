package embeddings

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestProviderInputErrorsLeaveOutagesGroupable(t *testing.T) {
	for _, status := range []int{400, 413, 422, 401, 403, 408, 429, 500, 503} {
		cause := errors.New("synthetic response")
		err := inputErrorForStatus(status, cause)
		require.ErrorIs(t, err, cause)
		require.Equal(t, status == 400 || status == 413 || status == 422, errors.Is(err, ErrInvalidInput), "status %d", status)
	}
}
