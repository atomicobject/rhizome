package unifiedsearch

import (
	"bytes"
	"compress/zlib"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"
	"time"

	"github.com/atomicobject/rhizome/pkg/app/answer"
	actions "github.com/atomicobject/rhizome/pkg/app/cli"
	searchapplication "github.com/atomicobject/rhizome/pkg/app/unifiedsearch/application"
	"github.com/atomicobject/rhizome/pkg/search"
	"github.com/atomicobject/rhizome/pkg/search/semantic"
)

// Profile chooses bounded work and presentation defaults. Intent continues to
// describe the evidence requested and therefore takes precedence over profile
// preferences for precision tasks.
type Profile = searchapplication.Profile

const (
	ProfileInteractive = searchapplication.ProfileInteractive
	ProfileAgent       = searchapplication.ProfileAgent
)

const RankingPolicyVersion = "search-quality-v2"

type EffectivePolicy = searchapplication.EffectivePolicy

type EffectiveRequest struct {
	Queries      []QueryInput    `json:"queries"`
	Intent       search.Intent   `json:"intent"`
	IntentSource string          `json:"intentSource"`
	IntentScore  float64         `json:"intentScore,omitempty"`
	Profile      Profile         `json:"profile"`
	Seeds        []string        `json:"seeds,omitempty"`
	Filters      search.Filters  `json:"filters"`
	Policy       EffectivePolicy `json:"policy"`
}

type Availability = searchapplication.Availability

const (
	AvailabilityComplete    = searchapplication.AvailabilityComplete
	AvailabilityPartial     = searchapplication.AvailabilityPartial
	AvailabilityUnavailable = searchapplication.AvailabilityUnavailable
)

type CountSummary = searchapplication.CountSummary

type EvidenceEligibility = searchapplication.EvidenceEligibility

const (
	EvidencePrimary    = searchapplication.EvidencePrimary
	EvidenceSupporting = searchapplication.EvidenceSupporting
)

type RelevanceLevel = searchapplication.RelevanceLevel

const (
	RelevanceStrong  = searchapplication.RelevanceStrong
	RelevanceUseful  = searchapplication.RelevanceUseful
	RelevanceWeak    = searchapplication.RelevanceWeak
	RelevanceUnknown = searchapplication.RelevanceUnknown
)

type SourceAssessment = searchapplication.SourceAssessment
type FacetSupport = searchapplication.FacetSupport
type TargetResolution = searchapplication.TargetResolution

// ApplicationResult is the transport-neutral result contract. Adapters may
// omit presentation bodies, but may not rerank Sources or reinterpret status.
type ApplicationResult struct {
	RequestIdentity string               `json:"requestIdentity"`
	Request         EffectiveRequest     `json:"request"`
	Sources         []SourceAssessment   `json:"sources"`
	Display         []DisplayItem        `json:"-"`
	Target          TargetResolution     `json:"target"`
	Answer          answer.Response      `json:"answer"`
	Warnings        []search.Warning     `json:"warnings,omitempty"`
	Lanes           []search.LaneStatus  `json:"lanes,omitempty"`
	Availability    Availability         `json:"availability"`
	Counts          CountSummary         `json:"counts"`
	Continuation    string               `json:"continuation,omitempty"`
	IndexGeneration string               `json:"indexGeneration,omitempty"`
	PackedText      string               `json:"packedText,omitempty"`
	Timings         []search.TimingEvent `json:"timings,omitempty"`
	Duration        time.Duration        `json:"-"`
}

func ResolveEffectivePolicy(profile Profile, intent search.Intent, explicitLimit, explicitBudget, explicitMaxPerOwner int) (EffectivePolicy, error) {
	return searchapplication.ResolveEffectivePolicy(profile, intent, explicitLimit, explicitBudget, explicitMaxPerOwner)
}

func RequestIdentity(request EffectiveRequest, vaultIdentity string) (string, error) {
	membership := membershipIdentity(request)
	b, err := json.Marshal(struct {
		Request       membershipRequest `json:"request"`
		Vault         string            `json:"vault"`
		PolicyVersion string            `json:"policyVersion"`
	}{membership, strings.TrimSpace(vaultIdentity), RankingPolicyVersion})
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(b)
	return hex.EncodeToString(digest[:]), nil
}

type membershipPolicy struct {
	Profile         Profile `json:"profile"`
	CandidateWindow int     `json:"candidateWindow"`
	MaxPerOwner     int     `json:"maxPerOwner"`
}

type membershipFilters struct {
	Inputs       []actions.ListInput `json:"inputs,omitempty"`
	Expression   string              `json:"expression,omitempty"`
	Types        []string            `json:"types,omitempty"`
	Paths        []string            `json:"paths,omitempty"`
	NoteTypes    []string            `json:"noteTypes,omitempty"`
	TestsOnly    bool                `json:"testsOnly,omitempty"`
	ExcludeTests bool                `json:"excludeTests,omitempty"`
	ExactSymbols []string            `json:"exactSymbols,omitempty"`
}

type membershipRequest struct {
	Queries []QueryInput      `json:"queries"`
	Intent  search.Intent     `json:"intent"`
	Seeds   []string          `json:"seeds,omitempty"`
	Filters membershipFilters `json:"filters"`
	Policy  membershipPolicy  `json:"policy"`
}

func membershipIdentity(request EffectiveRequest) membershipRequest {
	queries := make([]QueryInput, 0, len(request.Queries))
	seen := map[string]struct{}{}
	for _, query := range request.Queries {
		query.Text = strings.TrimSpace(query.Text)
		query.Mode = strings.TrimSpace(query.Mode)
		if query.Text == "" {
			continue
		}
		key := query.Mode + "\x00" + query.Text
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		queries = append(queries, query)
	}
	sort.Slice(queries, func(i, j int) bool {
		if queries[i].Mode != queries[j].Mode {
			return queries[i].Mode < queries[j].Mode
		}
		return queries[i].Text < queries[j].Text
	})
	inputs := make([]actions.ListInput, 0, len(request.Filters.Inputs))
	inputSeen := map[string]struct{}{}
	for _, input := range request.Filters.Inputs {
		input.Property = strings.TrimSpace(input.Property)
		input.Value = strings.TrimSpace(input.Value)
		key := fmt.Sprintf("%d\x00%s\x00%s", input.Type, input.Property, input.Value)
		if _, ok := inputSeen[key]; ok {
			continue
		}
		inputSeen[key] = struct{}{}
		inputs = append(inputs, input)
	}
	sort.Slice(inputs, func(i, j int) bool {
		if inputs[i].Type != inputs[j].Type {
			return inputs[i].Type < inputs[j].Type
		}
		if inputs[i].Property != inputs[j].Property {
			return inputs[i].Property < inputs[j].Property
		}
		return inputs[i].Value < inputs[j].Value
	})
	return membershipRequest{
		Queries: queries,
		Intent:  request.Intent,
		Seeds:   normalizeIdentityStrings(request.Seeds),
		Filters: membershipFilters{Inputs: inputs, Expression: actions.CanonicalInputExpression(request.Filters.Expression), Types: normalizeIdentityStringsFold(request.Filters.Types), Paths: normalizeIdentityStrings(request.Filters.PathPrefixes), NoteTypes: normalizeIdentityStringsFold(request.Filters.NoteTypes), TestsOnly: request.Filters.TestsOnly, ExcludeTests: request.Filters.ExcludeTests, ExactSymbols: normalizeIdentityStrings(request.Filters.ExactSymbols)},
		Policy:  membershipPolicy{Profile: request.Profile, CandidateWindow: request.Policy.CandidateWindow, MaxPerOwner: request.Policy.MaxPerOwner},
	}
}

func normalizeIdentityStrings(values []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}

func normalizeIdentityStringsFold(values []string) []string {
	folded := make([]string, 0, len(values))
	for _, value := range values {
		folded = append(folded, strings.ToLower(strings.TrimSpace(value)))
	}
	return normalizeIdentityStrings(folded)
}

type ContinuationCursor struct {
	Version         int                             `json:"version"`
	RequestIdentity string                          `json:"requestIdentity"`
	IndexGeneration string                          `json:"indexGeneration"`
	WindowDigest    string                          `json:"windowDigest"`
	CandidateWindow int                             `json:"candidateWindow"`
	Offset          int                             `json:"offset"`
	QueryEmbeddings []semantic.QueryEmbeddingRecord `json:"queryEmbeddings,omitempty"`
}

var (
	ErrCursorRefreshRequired = errors.New("search continuation refresh required")
	ErrCursorInvalid         = errors.New("invalid search continuation")
	ErrCursorStale           = errors.New("stale search continuation")
)

const (
	maxEncodedContinuationBytes = 1 << 20
	// Sixteen queries with two 4096-component vectors can exceed 1 MiB of
	// JSON before compression. Keep decoding bounded while allowing that shape.
	maxDecodedContinuationBytes = 4 << 20
)

func EncodeContinuation(cursor ContinuationCursor) (string, error) {
	cursor.Version = 2
	if err := validateContinuation(cursor); err != nil {
		return "", err
	}
	b, err := json.Marshal(cursor)
	if err != nil {
		return "", err
	}
	if len(b) > maxDecodedContinuationBytes {
		return "", fmt.Errorf("%w: decoded payload exceeds 4 MiB; split the query batch", ErrCursorInvalid)
	}
	// Query vectors must survive exactly across pages. Compress the existing
	// payload rather than rounding them or requesting new provider output.
	var compressed bytes.Buffer
	w := zlib.NewWriter(&compressed)
	if _, err := w.Write(b); err != nil {
		return "", err
	}
	if err := w.Close(); err != nil {
		return "", err
	}
	if 2+base64.RawURLEncoding.EncodedLen(compressed.Len()) > maxEncodedContinuationBytes {
		return "", fmt.Errorf("%w: encoded payload too large", ErrCursorInvalid)
	}
	return "z:" + base64.RawURLEncoding.EncodeToString(compressed.Bytes()), nil
}

func DecodeContinuation(token string) (ContinuationCursor, error) {
	token = strings.TrimSpace(token)
	if len(token) > maxEncodedContinuationBytes {
		return ContinuationCursor{}, fmt.Errorf("%w: payload too large", ErrCursorInvalid)
	}
	encoded, compressed := strings.CutPrefix(token, "z:")
	b, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil {
		return ContinuationCursor{}, fmt.Errorf("%w: decode", ErrCursorInvalid)
	}
	if compressed {
		r, err := zlib.NewReader(bytes.NewReader(b))
		if err != nil {
			return ContinuationCursor{}, fmt.Errorf("%w: compression", ErrCursorInvalid)
		}
		defer r.Close()
		b, err = io.ReadAll(io.LimitReader(r, maxDecodedContinuationBytes+1))
		if err != nil || len(b) > maxDecodedContinuationBytes {
			return ContinuationCursor{}, fmt.Errorf("%w: compressed payload invalid or too large", ErrCursorInvalid)
		}
	}
	var cursor ContinuationCursor
	if err := json.Unmarshal(b, &cursor); err != nil {
		return ContinuationCursor{}, fmt.Errorf("%w: payload", ErrCursorInvalid)
	}
	if cursor.Version != 2 {
		return ContinuationCursor{}, ErrCursorRefreshRequired
	}
	if err := validateContinuation(cursor); err != nil {
		return ContinuationCursor{}, err
	}
	return cursor, nil
}

func ValidateContinuation(cursor ContinuationCursor, requestIdentity, indexGeneration, windowDigest string, effectiveCandidateWindow int) error {
	if err := ValidateContinuationRequest(cursor, requestIdentity, effectiveCandidateWindow); err != nil {
		return err
	}
	if cursor.IndexGeneration != indexGeneration || cursor.WindowDigest != windowDigest {
		return ErrCursorStale
	}
	return nil
}

func ValidateContinuationRequest(cursor ContinuationCursor, requestIdentity string, effectiveCandidateWindow int) error {
	if cursor.RequestIdentity != requestIdentity {
		return fmt.Errorf("%w: request controls changed", ErrCursorInvalid)
	}
	if cursor.CandidateWindow != effectiveCandidateWindow {
		return fmt.Errorf("%w: candidate window changed", ErrCursorInvalid)
	}
	return nil
}

func validateContinuation(cursor ContinuationCursor) error {
	if cursor.RequestIdentity == "" || cursor.IndexGeneration == "" || cursor.WindowDigest == "" || cursor.CandidateWindow <= 0 || cursor.Offset < 0 || cursor.Offset > cursor.CandidateWindow {
		return ErrCursorInvalid
	}
	if len(cursor.QueryEmbeddings) > 16 {
		return fmt.Errorf("%w: too many query embeddings", ErrCursorInvalid)
	}
	seen := map[string]struct{}{}
	for _, record := range cursor.QueryEmbeddings {
		text := strings.TrimSpace(record.Text)
		if text == "" || len(record.Code) > 4096 || len(record.Note) > 4096 {
			return fmt.Errorf("%w: invalid query embedding", ErrCursorInvalid)
		}
		if _, ok := seen[text]; ok {
			return fmt.Errorf("%w: duplicate query embedding", ErrCursorInvalid)
		}
		seen[text] = struct{}{}
	}
	return nil
}

func OrderedSourceDigest(results []SourceAssessment) string {
	h := sha256.New()
	for _, result := range results {
		_, _ = h.Write([]byte(result.Result.Handle.String()))
		_, _ = h.Write([]byte{0})
	}
	return hex.EncodeToString(h.Sum(nil))
}
