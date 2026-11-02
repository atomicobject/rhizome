package viewconfig

import (
	"fmt"
	"math"
	"regexp"
	"strings"
)

// ParamKind is the value type of a view parameter.
type ParamKind string

const (
	ParamKindString ParamKind = "string"
	ParamKindInt    ParamKind = "int"
	ParamKindBool   ParamKind = "bool"
)

// ViewParam is one declared view parameter (SPEC-0110). Its value lives on
// the view's URL query under Name. An identity parameter is part of the
// invocation, so changing it remounts the view; any other parameter is UI
// state the view updates in place.
type ViewParam struct {
	Name     string    `json:"name" yaml:"name"`
	Kind     ParamKind `json:"kind" yaml:"kind"`
	Default  any       `json:"default,omitempty" yaml:"default,omitempty"`
	Identity bool      `json:"identity,omitempty" yaml:"identity,omitempty"`
}

var paramNamePattern = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_]*$`)

// reservedParamNames are the query keys the Notes workspace and the /views
// page already read, so a parameter cannot share their URL.
var reservedParamNames = map[string]struct{}{
	"context": {}, "hosted": {}, "preferenceSlot": {}, "viewId": {},
	"note": {}, "noteQuery": {}, "view": {}, "presentation": {}, "nodeId": {}, "kind": {}, "structural": {},
	"search": {}, "scope": {}, "noteType": {}, "folder": {}, "searchTab": {},
	"issueScopeKind": {}, "issueScopeKey": {}, "issue": {},
}

func normalizeParams(params []ViewParam) {
	for i := range params {
		params[i].Name = strings.TrimSpace(params[i].Name)
		params[i].Kind = ParamKind(strings.TrimSpace(string(params[i].Kind)))
		if value, ok := integral(params[i].Default); ok && params[i].Kind == ParamKindInt {
			params[i].Default = value
		}
	}
}

func validateParams(def ViewDefinition) []Issue {
	var issues []Issue
	seen := map[string]struct{}{}
	for i, param := range def.Params {
		field := fmt.Sprintf("params[%d]", i)
		if !paramNamePattern.MatchString(param.Name) {
			issues = append(issues, issue(def, "invalid_view_param", field+".name", "param name must match [A-Za-z][A-Za-z0-9_]*"))
		} else if _, ok := reservedParamNames[param.Name]; ok {
			issues = append(issues, issue(def, "invalid_view_param", field+".name", fmt.Sprintf("param name %q is reserved by the workspace URL", param.Name)))
		} else if _, ok := seen[param.Name]; ok {
			issues = append(issues, issue(def, "duplicate_view_param", field+".name", fmt.Sprintf("param %q is declared more than once", param.Name)))
		}
		seen[param.Name] = struct{}{}
		switch param.Kind {
		case ParamKindString, ParamKindInt, ParamKindBool:
			if param.Default != nil && !paramDefaultMatches(param.Kind, param.Default) {
				issues = append(issues, issue(def, "invalid_view_param", field+".default", fmt.Sprintf("default must be a %s value", param.Kind)))
			}
		default:
			issues = append(issues, issue(def, "invalid_view_param", field+".kind", "kind must be string, int, or bool"))
		}
	}
	return issues
}

func paramDefaultMatches(kind ParamKind, value any) bool {
	switch kind {
	case ParamKindString:
		_, ok := value.(string)
		return ok
	case ParamKindBool:
		_, ok := value.(bool)
		return ok
	default:
		_, ok := integral(value)
		return ok
	}
}

// integral reports a YAML or JSON number that holds a whole value.
func integral(value any) (int64, bool) {
	switch value := value.(type) {
	case int:
		return int64(value), true
	case int64:
		return value, true
	case uint64:
		return int64(value), value <= math.MaxInt64
	case float64:
		return int64(value), value == math.Trunc(value) && math.Abs(value) < 1<<53
	default:
		return 0, false
	}
}
