package ontology

import (
	"errors"
	"math"
	"strconv"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestIdentifierFormat_DateTimeCodecRoundTrip(t *testing.T) {
	f := &IdentifierFormat{Strategy: IdentifierStrategyDateTime, Prefix: "EFF", Separator: "-"}
	codec, err := f.StrategyContract()
	require.NoError(t, err)

	stamp := "2026-08-05-14-32"
	base, err := codec.Format(IdentifierValue{Strategy: IdentifierStrategyDateTime, DateTimeStamp: stamp, Ordinal: 1})
	require.NoError(t, err)
	require.Equal(t, "EFF-2026-08-05-14-32", base)

	disambiguated, err := codec.Format(IdentifierValue{Strategy: IdentifierStrategyDateTime, DateTimeStamp: stamp, Ordinal: 2})
	require.NoError(t, err)
	require.Equal(t, "EFF-2026-08-05-14-32-2", disambiguated)

	parsed, ok := codec.Parse(disambiguated)
	require.True(t, ok)
	family, err := codec.ReservationFamily(parsed)
	require.NoError(t, err)
	require.Equal(t, IdentifierReservationFamily("EFF-2026-08-05-14-32"), family)
	require.Equal(t, 2, parsed.Ordinal)
	require.Equal(t, stamp, parsed.DateTimeStamp)
}

func TestIdentifierFormat_DateTimeCodecRejectsInvalidGrammar(t *testing.T) {
	f := &IdentifierFormat{Strategy: IdentifierStrategyDateTime, Prefix: "EFF", Separator: "-"}
	codec, err := f.StrategyContract()
	require.NoError(t, err)

	for _, value := range []string{
		"",
		"EFF-2026-08-05-14",
		"EFF-2026-02-29-14-32",
		"EFF-2026-13-05-14-32",
		"EFF-2026-08-05-24-00",
		"EFF-2026-08-05-14-60",
		"EFF-2026-08-05-14-32-1",
		"EFF-2026-08-05-14-32-02",
		"EFF-2026-08-05-14-32-2-2",
	} {
		_, ok := codec.Parse(value)
		require.False(t, ok, "case %q should not parse", value)
	}
}

func TestIdentifierFormat_DateTimeReplacementUsesCanonicalFamily(t *testing.T) {
	f := &IdentifierFormat{Strategy: IdentifierStrategyDateTime, Prefix: "EFF", Separator: "-"}
	codec, err := f.StrategyContract()
	require.NoError(t, err)
	parsed, ok := codec.Parse("EFF-2026-08-05-14-32-2")
	require.True(t, ok)

	replacement, err := codec.Replacement(parsed, IdentifierReservationInventory{Reservations: []IdentifierReservation{
		{Value: "eff-2026-08-05-14-32"},
		{Value: "EFF-2026-08-05-14-32-2"},
		{Value: "EFF-2026-08-05-14-32-3"},
	}})
	require.NoError(t, err)
	formatted, err := codec.Format(replacement)
	require.NoError(t, err)
	require.Equal(t, "EFF-2026-08-05-14-32-4", formatted)
}

func TestIdentifierFormat_SequentialContractPreservesMaxPlusOne(t *testing.T) {
	f := &IdentifierFormat{Strategy: IdentifierStrategySequential, Prefix: "SPEC", Separator: "-", Pad: 4}
	codec, err := f.StrategyContract()
	require.NoError(t, err)

	parsed, ok := codec.Parse("SPEC-0007")
	require.True(t, ok)
	require.Equal(t, 7, parsed.Number)

	replacement, err := codec.Replacement(parsed, IdentifierReservationInventory{Reservations: []IdentifierReservation{
		{Value: "SPEC-0002"},
		{Value: "SPEC-0009"},
		{Value: "off-pattern"},
	}})
	require.NoError(t, err)
	formatted, err := codec.Format(replacement)
	require.NoError(t, err)
	require.Equal(t, "SPEC-0010", formatted)
}

func TestIdentifierFormat_SequentialContractOwnsAllocation(t *testing.T) {
	f := &IdentifierFormat{Strategy: IdentifierStrategySequential, Prefix: "SPEC", Separator: "-", Pad: 4}
	strategy, err := f.StrategyContract()
	require.NoError(t, err)
	request := IdentifierAllocationRequest{Count: 2}
	require.NoError(t, strategy.ValidateAllocationRequest(request))

	reservations := []IdentifierReservation{
		{Value: "SPEC-0007", Owner: "specs/seven.md"},
		{Value: "legacy", Owner: "specs/legacy.md"},
		{Value: "SPEC-0002", Owner: "specs/two.md"},
	}
	original := append([]IdentifierReservation(nil), reservations...)
	result, err := strategy.Allocate(request, IdentifierReservationInventory{Reservations: reservations})
	require.NoError(t, err)
	require.Equal(t, original, reservations, "strategy allocation must not mutate frozen inventory")
	var ids []string
	for _, allocation := range result.Values {
		id, err := strategy.Format(allocation.Value)
		require.NoError(t, err)
		ids = append(ids, id)
	}
	require.Equal(t, []string{"SPEC-0008", "SPEC-0009"}, ids)
	require.Equal(t, []IdentifierReservation{
		{Value: "SPEC-0002", Owner: "specs/two.md"},
		{Value: "SPEC-0007", Owner: "specs/seven.md"},
	}, result.MatchedReservations)
	require.Equal(t, []IdentifierReservation{{Value: "legacy", Owner: "specs/legacy.md"}}, result.SkippedReservations)
	require.NotNil(t, result.HighWatermark)
	require.Equal(t, 7, result.HighWatermark.Value.Number)
	require.Equal(t, "specs/seven.md", result.HighWatermark.Reservation.Owner)
}

func TestIdentifierFormat_StrategiesOwnRequestValidation(t *testing.T) {
	sequential, err := (&IdentifierFormat{Strategy: IdentifierStrategySequential, Prefix: "SPEC", Separator: "-", Pad: 4}).StrategyContract()
	require.NoError(t, err)
	err = sequential.ValidateAllocationRequest(IdentifierAllocationRequest{
		Count: 1,
		Paths: []string{"specs/2026-08-05-14-32-example.md"},
	})
	require.Error(t, err)
	require.True(t, errors.Is(err, ErrIdentifierStrategyInvalidInput))

	datetime, err := (&IdentifierFormat{Strategy: IdentifierStrategyDateTime, Prefix: "EFF", Separator: "-"}).StrategyContract()
	require.NoError(t, err)
	require.NoError(t, datetime.ValidateAllocationRequest(IdentifierAllocationRequest{
		Count: 1,
		Paths: []string{"efforts/2026-08-05-14-32-example.md"},
	}))

	for _, request := range []IdentifierAllocationRequest{
		{Count: 1},
		{Count: 2, Paths: []string{"efforts/2026-08-05-14-32-example.md"}},
		{Count: 1, Paths: []string{"efforts/not-a-timestamp.md"}},
		{Count: 1, Paths: []string{"/efforts/2026-08-05-14-32-example.md"}},
		{Count: 1, Paths: []string{"efforts/../2026-08-05-14-32-example.md"}},
		{Count: 1, Paths: []string{"../2026-08-05-14-32-example.md"}},
		{Count: 1, Paths: []string{"C:/efforts/2026-08-05-14-32-example.md"}},
		{Count: 1, Paths: []string{"C:efforts/2026-08-05-14-32-example.md"}},
	} {
		err = datetime.ValidateAllocationRequest(request)
		require.ErrorIs(t, err, ErrIdentifierStrategyInvalidInput, "request: %#v", request)
	}
}

func TestIdentifierFormat_DateTimeAllocationPreservesPathOrderAndReservesEarlierValues(t *testing.T) {
	strategy, err := (&IdentifierFormat{Strategy: IdentifierStrategyDateTime, Prefix: "EFF", Separator: "-"}).StrategyContract()
	require.NoError(t, err)
	request := IdentifierAllocationRequest{Count: 1, Paths: []string{
		"efforts/2026-08-05-14-32-first.md",
		"efforts/2026-08-05-14-33-only.md",
		"efforts/2026-08-05-14-32-second.md",
	}}
	inventory := IdentifierReservationInventory{Reservations: []IdentifierReservation{
		{Value: "EFF-2026-08-05-14-32", Owner: "efforts/existing.md"},
		{Value: "EFF-2026-08-05-14-32-3", Owner: "efforts/reserved-by-alias.md"},
		{Value: "legacy", Owner: "efforts/legacy.md"},
	}}

	result, err := strategy.Allocate(request, inventory)
	require.NoError(t, err)
	require.Equal(t, []IdentifierAllocationValue{
		{Path: request.Paths[0], Value: IdentifierValue{Strategy: IdentifierStrategyDateTime, DateTimeStamp: "2026-08-05-14-32", Ordinal: 2}},
		{Path: request.Paths[1], Value: IdentifierValue{Strategy: IdentifierStrategyDateTime, DateTimeStamp: "2026-08-05-14-33", Ordinal: 1}},
		{Path: request.Paths[2], Value: IdentifierValue{Strategy: IdentifierStrategyDateTime, DateTimeStamp: "2026-08-05-14-32", Ordinal: 4}},
	}, result.Values)
	require.Equal(t, inventory.Reservations[:2], result.MatchedReservations)
	require.Equal(t, inventory.Reservations[2:], result.SkippedReservations)
}

func TestIdentifierFormat_SequentialContractRejectsExhaustedAllocation(t *testing.T) {
	strategy, err := (&IdentifierFormat{Strategy: IdentifierStrategySequential, Prefix: "SPEC", Separator: "-", Pad: 4}).StrategyContract()
	require.NoError(t, err)
	maxID := "SPEC-" + strconv.Itoa(math.MaxInt)
	inventory := IdentifierReservationInventory{Reservations: []IdentifierReservation{{Value: maxID, Owner: "specs/max.md"}}}

	_, err = strategy.Allocate(IdentifierAllocationRequest{Count: 1}, inventory)
	require.ErrorIs(t, err, ErrIdentifierStrategyExhausted)
	_, err = strategy.Replacement(IdentifierValue{Strategy: IdentifierStrategySequential, Number: math.MaxInt}, inventory)
	require.ErrorIs(t, err, ErrIdentifierStrategyExhausted)
}

func TestIdentifierFormat_SequentialContractRejectsNegativeValue(t *testing.T) {
	strategy, err := (&IdentifierFormat{Strategy: IdentifierStrategySequential, Prefix: "SPEC", Separator: "-", Pad: 4}).StrategyContract()
	require.NoError(t, err)

	_, err = strategy.Format(IdentifierValue{Strategy: IdentifierStrategySequential, Number: -1})
	require.ErrorIs(t, err, ErrIdentifierStrategyInvalidInput)
}

func TestIdentifierFormat_FormatPadsAndOverflows(t *testing.T) {
	f := &IdentifierFormat{Prefix: "EFF", Separator: "-", Pad: 4}
	require.Equal(t, "EFF-0001", f.Format(1))
	require.Equal(t, "EFF-0042", f.Format(42))
	// Numbers wider than the pad still render at their natural width so
	// existing ids that outgrew the original pad keep parsing.
	require.Equal(t, "EFF-12345", f.Format(12345))
}

func TestIdentifierFormat_ParseRoundTrip(t *testing.T) {
	f := &IdentifierFormat{Prefix: "SPEC", Separator: "-", Pad: 4}
	n, ok := f.Parse("SPEC-0007")
	require.True(t, ok)
	require.Equal(t, 7, n)

	// Wider-than-pad suffixes still parse.
	n, ok = f.Parse("SPEC-12345")
	require.True(t, ok)
	require.Equal(t, 12345, n)
}

func TestIdentifierFormat_ParseRejectsOffPattern(t *testing.T) {
	f := &IdentifierFormat{Prefix: "EFF", Separator: "-", Pad: 4}
	cases := []string{
		"",
		"EFF-",
		"EFF_0001",
		"OTHER-0001",
		"EFF-0001a",
		"EFF--0001",
		"EFF-+0001",
	}
	for _, c := range cases {
		_, ok := f.Parse(c)
		require.False(t, ok, "case %q should not parse", c)
	}
}

func TestIdentifierFormat_ParseUsesCaseFoldedSemanticPrefix(t *testing.T) {
	f := &IdentifierFormat{Prefix: "EFF", Separator: "-", Pad: 4}
	n, ok := f.Parse("eff-0001")
	require.True(t, ok)
	require.Equal(t, 1, n)
}

func TestIdentifierFormat_ParseTrimsSurroundingWhitespace(t *testing.T) {
	f := &IdentifierFormat{Prefix: "EFF", Separator: "-", Pad: 4}
	for _, c := range []string{"EFF-0001 ", "  EFF-0001", "\tEFF-0001\n"} {
		n, ok := f.Parse(c)
		require.True(t, ok, "case %q should parse after trim", c)
		require.Equal(t, 1, n)
	}
}

func TestIdentifierFormat_NilSafe(t *testing.T) {
	var f *IdentifierFormat
	require.Equal(t, "", f.Format(1))
	_, ok := f.Parse("EFF-0001")
	require.False(t, ok)
}
