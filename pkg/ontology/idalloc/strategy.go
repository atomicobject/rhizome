package idalloc

import (
	"errors"
	"fmt"
	ontoschema "github.com/atomicobject/rhizome/pkg/ontology/schema"
)

func translateStrategyError(err error) error {
	switch {
	case errors.Is(err, ontoschema.ErrIdentifierStrategyInvalidInput):
		return fmt.Errorf("%w: %v", ErrInvalidInput, err)
	case errors.Is(err, ontoschema.ErrIdentifierStrategyUnsupported):
		return fmt.Errorf("%w: %v", ErrUnsupportedForType, err)
	case errors.Is(err, ontoschema.ErrIdentifierStrategyExhausted):
		return fmt.Errorf("%w: %v", ErrIdentifierExhausted, err)
	default:
		return err
	}
}
