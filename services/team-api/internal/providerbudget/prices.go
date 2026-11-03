// Package providerbudget contains reviewed provider prices and conservative
// input-token bounds. Vendor price or context changes require another review.
package providerbudget

import (
	"math"

	"github.com/atomicobject/rhizome/pkg/teamapi/wire"
)

const (
	Voyage                     = "voyage"
	TypeSafe                   = "typesafe"
	PriceVersion               = "2026-11-03-v1"
	ReviewedAt                 = "2026-11-03"
	NanodollarsPerDollar int64 = 1_000_000_000
)

// Price records the assumptions used for one receipt, not a vendor invoice.
type Price = wire.PriceAssumption

// These ASCII prefixes are documented by Voyage's embeddings API. Their byte
// count bounds their text tokens; eight more covers per-input special tokens.
const (
	voyageQueryPrefix    = "Represent the query for retrieving supporting documents: "
	voyageDocumentPrefix = "Represent the document for retrieval: "
	voyageInputOverhead  = max(len(voyageQueryPrefix), len(voyageDocumentPrefix)) + 8
)

// Reserve holds a full accepted request, including Voyage's per-input prompt
// allowance. Jev releases and the latest alias share TypeSafe's reviewed input
// price and default context bound; the assumptions are dated above.
func Reserve(provider, model string, inputs int) (Price, int64, bool) {
	p := Price{Provider: provider, Model: model, Version: PriceVersion, ReviewedAt: ReviewedAt}
	switch {
	case provider == Voyage && model == "voyage-4-lite" && inputs > 0 && inputs <= 1000:
		p.NanodollarsPerToken = 20
		p.MaxBillableTokens = 1_000_000 + int64(inputs)*int64(voyageInputOverhead)
	case provider == TypeSafe && IsJevModel(model) && inputs > 0:
		p.NanodollarsPerToken = 42
		p.MaxBillableTokens = 65_536
	default:
		return Price{}, 0, false
	}
	n, ok := Cost(p, p.MaxBillableTokens)
	return p, n, ok
}

// ReviewedPrices returns maximum request bounds for operations reports.
func ReviewedPrices() map[string]Price {
	voyage, _, _ := Reserve(Voyage, "voyage-4-lite", 1000)
	typesafe, _, _ := Reserve(TypeSafe, "jev-latest", 1)
	return map[string]Price{Voyage: voyage, TypeSafe: typesafe}
}

// Cost converts observed billing input tokens with checked integer arithmetic.
// It permits above-bound values so the ledger can retain the hold and fence it.
func Cost(price Price, tokens int64) (int64, bool) {
	if tokens < 0 || price.NanodollarsPerToken <= 0 || tokens > math.MaxInt64/price.NanodollarsPerToken {
		return 0, false
	}
	return tokens * price.NanodollarsPerToken, true
}
