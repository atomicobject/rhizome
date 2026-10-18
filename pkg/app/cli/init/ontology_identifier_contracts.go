package init

import (
	"fmt"
	"sort"
	"strings"

	"github.com/vektah/gqlparser/v2/ast"
)

// ontologyFingerprint ignores identifier allocation contracts: a separate
// identifier migration may change them without that counting as an edit, and
// a refresh carries them forward (see preserveAdoptedIdentifierStrategies).
func ontologyFingerprint(content []byte) string {
	contracts, err := parseIdentifierAllocationContracts("ontology", content)
	if err != nil || len(contracts) == 0 {
		return contentFingerprint(content)
	}
	spans := make([]identifierDirectiveSpan, 0, len(contracts))
	for _, contract := range contracts {
		// Only contracts a refresh carries forward may change without counting
		// as an edit.
		if contract.allocation.protected() {
			spans = append(spans, contract.span)
		}
	}
	if len(spans) == 0 {
		return contentFingerprint(content)
	}
	sort.Slice(spans, func(i, j int) bool { return spans[i].start > spans[j].start })
	runes := []rune(string(content))
	for _, span := range spans {
		if !validRuneRange(span.start, span.end, len(runes)) {
			return contentFingerprint(content)
		}
		runes = append(append(append([]rune(nil), runes[:span.start]...), []rune("@identifier")...), runes[span.end:]...)
	}
	return contentFingerprint([]byte(string(runes)))
}

type identifierAllocationContract struct {
	preferred bool
	strategy  string
	prefix    string
	separator string
	pad       int

	preferredLiteral string
	strategyLiteral  string
	prefixLiteral    string
	separatorLiteral string
	padLiteral       string
}

func (c identifierAllocationContract) enabled() bool {
	return c.prefix != ""
}

func (c identifierAllocationContract) protected() bool {
	return c.preferred && c.enabled()
}

func (c identifierAllocationContract) equal(other identifierAllocationContract) bool {
	return c.preferred == other.preferred &&
		c.strategy == other.strategy &&
		c.prefix == other.prefix &&
		c.separator == other.separator &&
		c.pad == other.pad
}

type identifierDirectiveContract struct {
	allocation identifierAllocationContract
	interfaces []string // Implemented interfaces that declare this identifier field.
	fieldName  string
	directive  *ast.Directive
	span       identifierDirectiveSpan
}

type identifierDirectiveSpan struct {
	start      int
	end        int
	hasComment bool
}

type identifierContractEdit struct {
	start       int
	end         int
	key         string
	replacement string
}

// preserveAdoptedIdentifierStrategies carries already-adopted identifier
// allocation contracts across an upstream ontology refresh. Init has refresh
// authority, not rekey authority; changing strategy, namespace, or sequential
// padding requires a separate migration path that can account for existing
// identifiers and references.
func preserveAdoptedIdentifierStrategies(existing, upstream []byte) ([]byte, []string, error) {
	current, err := parseIdentifierAllocationContracts("installed ontology", existing)
	if err != nil {
		return nil, nil, err
	}
	next, err := parseIdentifierAllocationContracts("starter ontology", upstream)
	if err != nil {
		return nil, nil, err
	}

	if err := inheritNewIdentifierSiblingContracts(current, next); err != nil {
		return nil, nil, err
	}

	upstreamRunes := []rune(string(upstream))
	var edits []identifierContractEdit
	for key, installed := range current {
		if !installed.allocation.protected() {
			continue
		}
		candidate, ok := next[key]
		if !ok {
			return nil, nil, fmt.Errorf("preserve %s identifier allocation contract: starter ontology no longer declares the matching @identifier field", key)
		}
		if candidate.allocation.equal(installed.allocation) {
			continue
		}
		if candidate.span.hasComment {
			return nil, nil, fmt.Errorf("preserve %s identifier allocation contract: comments inside @identifier cannot be rewritten safely", key)
		}
		replacement, renderErr := renderIdentifierDirective(upstreamRunes, candidate.directive, installed.allocation)
		if renderErr != nil {
			return nil, nil, fmt.Errorf("preserve %s identifier allocation contract: %w", key, renderErr)
		}
		edits = append(edits, identifierContractEdit{
			start:       candidate.span.start,
			end:         candidate.span.end,
			key:         key,
			replacement: replacement,
		})
	}
	if len(edits) == 0 {
		return upstream, nil, nil
	}

	// Offsets come from the parser's rune positions. Rewrite from the end so an
	// earlier directive's length change cannot invalidate a later edit.
	sort.Slice(edits, func(i, j int) bool { return edits[i].start > edits[j].start })
	preserved := make([]string, 0, len(edits))
	for _, edit := range edits {
		upstreamRunes = append(append(append([]rune(nil), upstreamRunes[:edit.start]...), []rune(edit.replacement)...), upstreamRunes[edit.end:]...)
		preserved = append(preserved, edit.key)
	}
	rewritten := []byte(string(upstreamRunes))
	verified, err := parseIdentifierAllocationContracts("preserved starter ontology", rewritten)
	if err != nil {
		return nil, nil, err
	}
	for _, key := range preserved {
		candidate, ok := verified[key]
		if !ok || !candidate.allocation.equal(current[key].allocation) {
			return nil, nil, fmt.Errorf("preserve %s identifier allocation contract: rewritten contract does not match installed ontology", key)
		}
	}
	if err := validateIdentifierAllocationNamespaces(verified); err != nil {
		return nil, nil, err
	}
	sort.Strings(preserved)
	return rewritten, preserved, nil
}

// New concrete implementations join their existing siblings' adopted allocation
// contract. Match both the shared interface field and the upstream contract;
// coincidental namespace reuse alone does not authorize a strategy change.
func inheritNewIdentifierSiblingContracts(current, next map[string]identifierDirectiveContract) error {
	additions := map[string]identifierDirectiveContract{}
	for key, candidate := range next {
		if _, exists := current[key]; exists {
			continue
		}
		var inherited *identifierAllocationContract
		for siblingKey, installed := range current {
			upstreamSibling, exists := next[siblingKey]
			if !exists || !installed.allocation.protected() || candidate.fieldName != upstreamSibling.fieldName || !candidate.allocation.equal(upstreamSibling.allocation) {
				continue
			}
			shared := false
			for _, iface := range candidate.interfaces {
				for _, other := range upstreamSibling.interfaces {
					if iface == other {
						shared = true
					}
				}
			}
			if !shared {
				continue
			}
			if inherited != nil && !inherited.equal(installed.allocation) {
				return fmt.Errorf("preserve %s identifier allocation contract: existing interface siblings have conflicting allocation contracts", key)
			}
			adopted := installed.allocation
			inherited = &adopted
		}
		if inherited != nil {
			candidate.allocation = *inherited
			additions[key] = candidate
		}
	}
	for key, contract := range additions {
		current[key] = contract
	}
	return nil
}

func validIdentifierPrefix(prefix string) bool {
	if len(prefix) < 1 || len(prefix) > 16 || prefix[0] < 'A' || prefix[0] > 'Z' {
		return false
	}
	for index := 1; index < len(prefix); index++ {
		char := prefix[index]
		if (char < 'A' || char > 'Z') && (char < '0' || char > '9') {
			return false
		}
	}
	return true
}

func renderIdentifierDirective(content []rune, directive *ast.Directive, installed identifierAllocationContract) (string, error) {
	arguments := make([]string, 0, len(directive.Arguments)+4)
	for _, argument := range directive.Arguments {
		switch argument.Name {
		case "preferred", "strategy", "prefix", "separator", "pad":
			continue
		}
		if argument.Value == nil || argument.Value.Position == nil || !validRuneRange(argument.Value.Position.Start, argument.Value.Position.End, len(content)) {
			return "", fmt.Errorf("argument %q has no valid source position", argument.Name)
		}
		switch argument.Value.Kind {
		case ast.IntValue, ast.FloatValue, ast.StringValue, ast.BlockValue, ast.BooleanValue, ast.NullValue, ast.EnumValue:
		default:
			return "", fmt.Errorf("argument %q uses a composite value that cannot be preserved safely", argument.Name)
		}
		literal := string(content[argument.Value.Position.Start:argument.Value.Position.End])
		arguments = append(arguments, argument.Name+": "+literal)
	}
	arguments = append(arguments,
		"preferred: "+installed.preferredLiteral,
		"strategy: "+installed.strategyLiteral,
		"prefix: "+installed.prefixLiteral,
		"separator: "+installed.separatorLiteral,
	)
	if installed.strategy == "SEQUENTIAL" {
		arguments = append(arguments, "pad: "+installed.padLiteral)
	}
	return "@identifier(" + strings.Join(arguments, ", ") + ")", nil
}

func validRuneRange(start, end, size int) bool {
	return start >= 0 && end > start && end <= size
}

func validateIdentifierAllocationNamespaces(contracts map[string]identifierDirectiveContract) error {
	type namespaceOwner struct {
		key      string
		contract identifierAllocationContract
	}
	owners := map[string]namespaceOwner{}
	for key, directive := range contracts {
		contract := directive.allocation
		if !contract.protected() {
			continue
		}
		namespace := strings.ToLower(contract.prefix + contract.separator)
		prior, ok := owners[namespace]
		if !ok {
			owners[namespace] = namespaceOwner{key: key, contract: contract}
			continue
		}
		compatible := prior.contract.strategy == contract.strategy
		if compatible && contract.strategy == "SEQUENTIAL" {
			compatible = prior.contract.pad == contract.pad
		}
		if !compatible {
			return fmt.Errorf("preserve identifier allocation contracts: namespace %q conflicts between %s and %s", contract.prefix+contract.separator, prior.key, key)
		}
	}
	return nil
}
