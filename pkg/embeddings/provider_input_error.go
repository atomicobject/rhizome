package embeddings

import "errors"

// ErrInvalidInput identifies a response that may depend on one request member.
var ErrInvalidInput = errors.New("invalid embedding input")

func inputErrorForStatus(status int, err error) error {
	if status == 400 || status == 413 || status == 422 {
		return errors.Join(ErrInvalidInput, err)
	}
	return err
}
