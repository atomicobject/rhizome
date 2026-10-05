package search

import (
	"github.com/atomicobject/rhizome/pkg/app/cli"
)

func ParseFilters(args []string) (Filters, error) {
	inputs, expr, err := actions.ParseInputsWithExpression(args)
	if err != nil {
		return Filters{}, err
	}
	return Filters{Inputs: inputs, Expression: expr}, nil
}
