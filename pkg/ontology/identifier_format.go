package ontology

import (
	"errors"
	"fmt"
	"math"
	"path"
	"sort"
	"strconv"
	"strings"
	"time"
)

const identifierDateTimeLayout = "2006-01-02-15-04"

// IdentifierValue is the strategy-neutral result of parsing, or the input to
// formatting, one schema-declared identifier.
type IdentifierValue struct {
	Strategy      IdentifierStrategy
	Number        int
	DateTimeStamp string
	Ordinal       int
}

type IdentifierReservationFamily string

var (
	ErrIdentifierStrategyInvalidInput = errors.New("identifier strategy invalid input")
	ErrIdentifierStrategyUnsupported  = errors.New("identifier strategy allocation unsupported")
	ErrIdentifierStrategyExhausted    = errors.New("identifier strategy exhausted")
)

// IdentifierAllocationRequest is the strategy-owned portion of an allocation
// request. Repository/type resolution and inventory loading stay in idalloc.
type IdentifierAllocationRequest struct {
	Count int
	Paths []string
}

// IdentifierReservation is one reserved value supplied by shared allocation
// orchestration. Owner is opaque strategy metadata used for deterministic
// diagnostics; strategies decide whether Value belongs to their grammar.
type IdentifierReservation struct {
	Value string
	Owner string
}

// IdentifierReservationInventory is the frozen claim set a strategy allocates
// against. Future collectors may add preferred values and aliases without
// changing strategy dispatch.
type IdentifierReservationInventory struct {
	Reservations []IdentifierReservation
}

type IdentifierObservation struct {
	Reservation IdentifierReservation
	Value       IdentifierValue
}

// IdentifierAllocationValue correlates a strategy value with its prospective
// path. Callers format Value through the same strategy contract.
type IdentifierAllocationValue struct {
	Path  string
	Value IdentifierValue
}

// IdentifierAllocationResult is the pure strategy result. Shared idalloc
// orchestration adapts it to CLI/GraphQL diagnostics and owner counts.
type IdentifierAllocationResult struct {
	Values              []IdentifierAllocationValue
	MatchedReservations []IdentifierReservation
	SkippedReservations []IdentifierReservation
	HighWatermark       *IdentifierObservation
}

// IdentifierStrategyContract owns identifier syntax and replacement within a
// reservation family. Pool discovery and collision-keeper selection remain
// outside the strategy.
type IdentifierStrategyContract interface {
	Strategy() IdentifierStrategy
	Parse(value string) (IdentifierValue, bool)
	Format(value IdentifierValue) (string, error)
	ReservationFamily(value IdentifierValue) (IdentifierReservationFamily, error)
	ValidateAllocationRequest(request IdentifierAllocationRequest) error
	Allocate(request IdentifierAllocationRequest, inventory IdentifierReservationInventory) (IdentifierAllocationResult, error)
	Replacement(current IdentifierValue, inventory IdentifierReservationInventory) (IdentifierValue, error)
}

// NormalizeIdentifierSemanticValue returns the comparison identity shared by
// ontology resolution, validation, and reconciliation. Authored spelling is
// preserved separately wherever exact source edits are required.
func NormalizeIdentifierSemanticValue(value string) string {
	value = strings.TrimSpace(value)
	value = strings.TrimPrefix(value, "^")
	return strings.ToLower(strings.TrimSpace(value))
}

// StrategyContract returns the syntax/replacement implementation selected by
// this compiled identifier format.
func (f *IdentifierFormat) StrategyContract() (IdentifierStrategyContract, error) {
	if f == nil {
		return nil, fmt.Errorf("identifier format is required")
	}
	switch f.Strategy {
	case "", IdentifierStrategySequential:
		return sequentialIdentifierContract{format: *f}, nil
	case IdentifierStrategyDateTime:
		return dateTimeIdentifierContract{format: *f}, nil
	default:
		return nil, fmt.Errorf("unsupported identifier strategy %q", f.Strategy)
	}
}

// Format preserves the original sequential formatting API. DATETIME requires
// a timestamp and must use StrategyContract.Format instead.
func (f *IdentifierFormat) Format(n int) string {
	if f == nil || (f.Strategy != "" && f.Strategy != IdentifierStrategySequential) {
		return ""
	}
	value, err := (sequentialIdentifierContract{format: *f}).Format(IdentifierValue{Number: n})
	if err != nil {
		return ""
	}
	return value
}

// Parse preserves the original sequential numeric parsing API. Strategy-aware
// callers use StrategyContract.Parse.
func (f *IdentifierFormat) Parse(value string) (int, bool) {
	if f == nil || (f.Strategy != "" && f.Strategy != IdentifierStrategySequential) {
		return 0, false
	}
	parsed, ok := (sequentialIdentifierContract{format: *f}).Parse(value)
	return parsed.Number, ok
}

type sequentialIdentifierContract struct {
	format IdentifierFormat
}

func (sequentialIdentifierContract) Strategy() IdentifierStrategy {
	return IdentifierStrategySequential
}

func (c sequentialIdentifierContract) ValidateAllocationRequest(request IdentifierAllocationRequest) error {
	if len(request.Paths) > 0 {
		return fmt.Errorf("%w: prospective paths are only supported for filename-derived identifier strategies", ErrIdentifierStrategyInvalidInput)
	}
	return nil
}

func (c sequentialIdentifierContract) Allocate(request IdentifierAllocationRequest, inventory IdentifierReservationInventory) (IdentifierAllocationResult, error) {
	if err := c.ValidateAllocationRequest(request); err != nil {
		return IdentifierAllocationResult{}, err
	}
	count := request.Count
	if count < 1 {
		count = 1
	}
	reservations := append([]IdentifierReservation(nil), inventory.Reservations...)
	sort.Slice(reservations, func(i, j int) bool {
		left := NormalizeIdentifierSemanticValue(reservations[i].Value)
		right := NormalizeIdentifierSemanticValue(reservations[j].Value)
		if left != right {
			return left < right
		}
		return reservations[i].Owner < reservations[j].Owner
	})

	result := IdentifierAllocationResult{}
	max := -1
	for _, reservation := range reservations {
		parsed, ok := c.Parse(reservation.Value)
		if !ok {
			result.SkippedReservations = append(result.SkippedReservations, reservation)
			continue
		}
		result.MatchedReservations = append(result.MatchedReservations, reservation)
		if parsed.Number > max {
			max = parsed.Number
			result.HighWatermark = &IdentifierObservation{Reservation: reservation, Value: parsed}
		}
	}
	if max < 0 {
		max = 0
		result.HighWatermark = nil
	}
	if count > math.MaxInt-max {
		return IdentifierAllocationResult{}, fmt.Errorf("%w: SEQUENTIAL identifier range ends at %d", ErrIdentifierStrategyExhausted, math.MaxInt)
	}
	result.Values = make([]IdentifierAllocationValue, 0, count)
	for offset := 1; offset <= count; offset++ {
		result.Values = append(result.Values, IdentifierAllocationValue{
			Value: IdentifierValue{Strategy: IdentifierStrategySequential, Number: max + offset},
		})
	}
	return result, nil
}

func (c sequentialIdentifierContract) Parse(value string) (IdentifierValue, bool) {
	value = strings.TrimSpace(value)
	head := c.format.Prefix + c.format.Separator
	if len(value) < len(head) || !strings.EqualFold(value[:len(head)], head) {
		return IdentifierValue{}, false
	}
	suffix := value[len(head):]
	if suffix == "" {
		return IdentifierValue{}, false
	}
	for _, r := range suffix {
		if r < '0' || r > '9' {
			return IdentifierValue{}, false
		}
	}
	n, err := strconv.Atoi(suffix)
	if err != nil || n < 0 {
		return IdentifierValue{}, false
	}
	return IdentifierValue{Strategy: IdentifierStrategySequential, Number: n}, true
}

func (c sequentialIdentifierContract) Format(value IdentifierValue) (string, error) {
	if value.Strategy != "" && value.Strategy != IdentifierStrategySequential {
		return "", fmt.Errorf("SEQUENTIAL strategy cannot format %s value", value.Strategy)
	}
	if value.Number < 0 {
		return "", fmt.Errorf("%w: SEQUENTIAL identifier number cannot be negative", ErrIdentifierStrategyInvalidInput)
	}
	pad := c.format.Pad
	if pad < 1 {
		pad = 4
	}
	return fmt.Sprintf("%s%s%0*d", c.format.Prefix, c.format.Separator, pad, value.Number), nil
}

func (c sequentialIdentifierContract) ReservationFamily(value IdentifierValue) (IdentifierReservationFamily, error) {
	if value.Strategy != "" && value.Strategy != IdentifierStrategySequential {
		return "", fmt.Errorf("SEQUENTIAL strategy cannot derive a family for %s value", value.Strategy)
	}
	return IdentifierReservationFamily(c.format.Prefix + c.format.Separator), nil
}

func (c sequentialIdentifierContract) Replacement(_ IdentifierValue, inventory IdentifierReservationInventory) (IdentifierValue, error) {
	max := 0
	for _, reservation := range inventory.Reservations {
		if parsed, ok := c.Parse(reservation.Value); ok && parsed.Number > max {
			max = parsed.Number
		}
	}
	if max == math.MaxInt {
		return IdentifierValue{}, fmt.Errorf("%w: SEQUENTIAL identifier range ends at %d", ErrIdentifierStrategyExhausted, math.MaxInt)
	}
	return IdentifierValue{Strategy: IdentifierStrategySequential, Number: max + 1}, nil
}

type dateTimeIdentifierContract struct {
	format IdentifierFormat
}

func (dateTimeIdentifierContract) Strategy() IdentifierStrategy {
	return IdentifierStrategyDateTime
}

func (dateTimeIdentifierContract) ValidateAllocationRequest(request IdentifierAllocationRequest) error {
	if request.Count > 1 {
		return fmt.Errorf("%w: DATETIME allocation accepts one identifier per path and does not support count greater than one", ErrIdentifierStrategyInvalidInput)
	}
	if len(request.Paths) == 0 {
		return fmt.Errorf("%w: DATETIME allocation requires at least one prospective vault-relative path", ErrIdentifierStrategyInvalidInput)
	}
	for _, prospectivePath := range request.Paths {
		if _, err := dateTimeStampFromProspectivePath(prospectivePath); err != nil {
			return err
		}
	}
	return nil
}

func (c dateTimeIdentifierContract) Allocate(request IdentifierAllocationRequest, inventory IdentifierReservationInventory) (IdentifierAllocationResult, error) {
	if err := c.ValidateAllocationRequest(request); err != nil {
		return IdentifierAllocationResult{}, err
	}

	result := IdentifierAllocationResult{
		Values: make([]IdentifierAllocationValue, 0, len(request.Paths)),
	}
	reservedByFamily := make(map[IdentifierReservationFamily]map[int]struct{})
	for _, reservation := range inventory.Reservations {
		parsed, ok := c.Parse(reservation.Value)
		if !ok {
			result.SkippedReservations = append(result.SkippedReservations, reservation)
			continue
		}
		family, err := c.ReservationFamily(parsed)
		if err != nil {
			result.SkippedReservations = append(result.SkippedReservations, reservation)
			continue
		}
		result.MatchedReservations = append(result.MatchedReservations, reservation)
		if reservedByFamily[family] == nil {
			reservedByFamily[family] = make(map[int]struct{})
		}
		reservedByFamily[family][parsed.Ordinal] = struct{}{}
	}

	for _, prospectivePath := range request.Paths {
		stamp, err := dateTimeStampFromProspectivePath(prospectivePath)
		if err != nil {
			return IdentifierAllocationResult{}, err
		}
		value := IdentifierValue{Strategy: IdentifierStrategyDateTime, DateTimeStamp: stamp, Ordinal: 1}
		family, err := c.ReservationFamily(value)
		if err != nil {
			return IdentifierAllocationResult{}, err
		}
		if reservedByFamily[family] == nil {
			reservedByFamily[family] = make(map[int]struct{})
		}
		ordinal := 1
		if _, exists := reservedByFamily[family][ordinal]; exists {
			for ordinal = 2; ; ordinal++ {
				if _, exists := reservedByFamily[family][ordinal]; !exists {
					break
				}
			}
		}
		reservedByFamily[family][ordinal] = struct{}{}
		value.Ordinal = ordinal
		result.Values = append(result.Values, IdentifierAllocationValue{Path: prospectivePath, Value: value})
	}
	return result, nil
}

func dateTimeStampFromProspectivePath(prospectivePath string) (string, error) {
	if prospectivePath == "" || strings.TrimSpace(prospectivePath) != prospectivePath {
		return "", fmt.Errorf("%w: DATETIME prospective path must be a non-empty canonical vault-relative path", ErrIdentifierStrategyInvalidInput)
	}
	if strings.Contains(prospectivePath, `\`) || path.IsAbs(prospectivePath) || hasWindowsVolumePrefix(prospectivePath) || path.Clean(prospectivePath) != prospectivePath || prospectivePath == "." || prospectivePath == ".." || strings.HasPrefix(prospectivePath, "../") || strings.HasSuffix(prospectivePath, "/") {
		return "", fmt.Errorf("%w: DATETIME prospective path %q must be canonical and vault-relative", ErrIdentifierStrategyInvalidInput, prospectivePath)
	}
	basename := path.Base(prospectivePath)
	if len(basename) < len(identifierDateTimeLayout) {
		return "", fmt.Errorf("%w: DATETIME prospective path %q must begin its basename with YYYY-MM-DD-HH-MM", ErrIdentifierStrategyInvalidInput, prospectivePath)
	}
	stampText := basename[:len(identifierDateTimeLayout)]
	stamp, err := time.Parse(identifierDateTimeLayout, stampText)
	if err != nil || stamp.Format(identifierDateTimeLayout) != stampText {
		return "", fmt.Errorf("%w: DATETIME prospective path %q has an invalid leading calendar-minute stamp", ErrIdentifierStrategyInvalidInput, prospectivePath)
	}
	if len(basename) > len(identifierDateTimeLayout) {
		next := basename[len(identifierDateTimeLayout)]
		if next != '-' && next != '.' {
			return "", fmt.Errorf("%w: DATETIME prospective path %q must delimit its leading calendar-minute stamp", ErrIdentifierStrategyInvalidInput, prospectivePath)
		}
	}
	return stampText, nil
}

func hasWindowsVolumePrefix(value string) bool {
	if len(value) < 2 || value[1] != ':' {
		return false
	}
	return (value[0] >= 'a' && value[0] <= 'z') || (value[0] >= 'A' && value[0] <= 'Z')
}

func (c dateTimeIdentifierContract) Parse(value string) (IdentifierValue, bool) {
	value = strings.TrimSpace(value)
	head := c.format.Prefix + c.format.Separator
	if len(value) < len(head) || !strings.EqualFold(value[:len(head)], head) {
		return IdentifierValue{}, false
	}
	remainder := value[len(head):]
	if len(remainder) < len(identifierDateTimeLayout) {
		return IdentifierValue{}, false
	}
	stampText := remainder[:len(identifierDateTimeLayout)]
	stamp, err := time.Parse(identifierDateTimeLayout, stampText)
	if err != nil || stamp.Format(identifierDateTimeLayout) != stampText {
		return IdentifierValue{}, false
	}
	ordinal := 1
	suffix := remainder[len(identifierDateTimeLayout):]
	if suffix != "" {
		if !strings.HasPrefix(suffix, "-") || len(suffix) < 2 {
			return IdentifierValue{}, false
		}
		digits := suffix[1:]
		if digits[0] == '0' {
			return IdentifierValue{}, false
		}
		for _, r := range digits {
			if r < '0' || r > '9' {
				return IdentifierValue{}, false
			}
		}
		ordinal, err = strconv.Atoi(digits)
		if err != nil || ordinal < 2 {
			return IdentifierValue{}, false
		}
	}
	return IdentifierValue{
		Strategy:      IdentifierStrategyDateTime,
		DateTimeStamp: stampText,
		Ordinal:       ordinal,
	}, true
}

func (c dateTimeIdentifierContract) Format(value IdentifierValue) (string, error) {
	if value.Strategy != "" && value.Strategy != IdentifierStrategyDateTime {
		return "", fmt.Errorf("DATETIME strategy cannot format %s value", value.Strategy)
	}
	stamp, err := time.Parse(identifierDateTimeLayout, value.DateTimeStamp)
	if err != nil || stamp.Format(identifierDateTimeLayout) != value.DateTimeStamp {
		return "", fmt.Errorf("DATETIME identifier requires a canonical calendar-minute stamp")
	}
	ordinal := value.Ordinal
	if ordinal == 0 {
		ordinal = 1
	}
	if ordinal < 1 {
		return "", fmt.Errorf("DATETIME collision ordinal must be one or at least two")
	}
	base := c.format.Prefix + c.format.Separator + value.DateTimeStamp
	if ordinal == 1 {
		return base, nil
	}
	return base + "-" + strconv.Itoa(ordinal), nil
}

func (c dateTimeIdentifierContract) ReservationFamily(value IdentifierValue) (IdentifierReservationFamily, error) {
	if value.Strategy != "" && value.Strategy != IdentifierStrategyDateTime {
		return "", fmt.Errorf("DATETIME strategy cannot derive a family for %s value", value.Strategy)
	}
	stamp, err := time.Parse(identifierDateTimeLayout, value.DateTimeStamp)
	if err != nil || stamp.Format(identifierDateTimeLayout) != value.DateTimeStamp {
		return "", fmt.Errorf("DATETIME reservation family requires a canonical calendar-minute stamp")
	}
	return IdentifierReservationFamily(c.format.Prefix + c.format.Separator + value.DateTimeStamp), nil
}

func (c dateTimeIdentifierContract) Replacement(current IdentifierValue, inventory IdentifierReservationInventory) (IdentifierValue, error) {
	family, err := c.ReservationFamily(current)
	if err != nil {
		return IdentifierValue{}, err
	}
	reservedOrdinals := map[int]struct{}{}
	for _, reservation := range inventory.Reservations {
		parsed, ok := c.Parse(reservation.Value)
		if !ok {
			continue
		}
		candidateFamily, err := c.ReservationFamily(parsed)
		if err != nil || candidateFamily != family {
			continue
		}
		ordinal := parsed.Ordinal
		if ordinal == 0 {
			ordinal = 1
		}
		reservedOrdinals[ordinal] = struct{}{}
	}
	for ordinal := 2; ; ordinal++ {
		if _, exists := reservedOrdinals[ordinal]; !exists {
			return IdentifierValue{
				Strategy:      IdentifierStrategyDateTime,
				DateTimeStamp: current.DateTimeStamp,
				Ordinal:       ordinal,
			}, nil
		}
	}
}
