package idalloc

import (
	"errors"
	"fmt"

	"github.com/atomicobject/rhizome/pkg/ontology"
)

func translateStrategyError(err error) error {
	switch {
	case errors.Is(err, ontology.ErrIdentifierStrategyInvalidInput):
		return fmt.Errorf("%w: %v", ErrInvalidInput, err)
	case errors.Is(err, ontology.ErrIdentifierStrategyUnsupported):
		return fmt.Errorf("%w: %v", ErrUnsupportedForType, err)
	case errors.Is(err, ontology.ErrIdentifierStrategyExhausted):
		return fmt.Errorf("%w: %v", ErrIdentifierExhausted, err)
	default:
		return err
	}
}
