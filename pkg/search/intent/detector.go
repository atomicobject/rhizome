package intent

import (
	"context"
	"fmt"
	"math"
	"reflect"
	"sort"
	"strings"
	"sync"

	"github.com/atomicobject/rhizome/pkg/search"
	"github.com/atomicobject/rhizome/pkg/search/embeddings"
	"github.com/atomicobject/rhizome/pkg/search/intentstore"
)

const (
	defaultMinScore  = 0.22
	defaultMinMargin = 0.03
)

type Detector struct {
	provider            embeddings.Provider
	providerFingerprint string
	exemplars           map[search.Intent][]string

	mu        sync.Mutex
	embedded  bool
	embedErr  error
	intentVec map[search.Intent][]embeddings.Embedding
}

func NewDetector(provider embeddings.Provider, providerInfo embeddings.ProviderConfig, exemplars map[search.Intent][]string) *Detector {
	return &Detector{
		provider:            provider,
		providerFingerprint: ProviderFingerprint(providerInfo),
		exemplars:           exemplars,
		intentVec:           make(map[search.Intent][]embeddings.Embedding),
	}
}

func (d *Detector) Detect(ctx context.Context, query string) (search.Intent, float64, bool, error) {
	if d == nil || d.provider == nil {
		return "", 0, false, nil
	}
	query = strings.TrimSpace(query)
	if query == "" {
		return "", 0, false, nil
	}
	var (
		queryVec embeddings.Embedding
		err      error
	)

	d.mu.Lock()
	if d.embedded {
		d.mu.Unlock()
		queryVec, err = d.embedQuery(ctx, query)
		if err != nil {
			return "", 0, false, err
		}
	} else {
		queryVec, err = d.embedExemplarsAndQueryLocked(ctx, query)
		d.mu.Unlock()
		if err != nil {
			return "", 0, false, err
		}
	}

	intentVec := d.snapshotIntentVec()

	bestIntent, bestScore, secondScore := bestIntentScores(queryVec, intentVec)
	if bestScore < defaultMinScore {
		return "", bestScore, false, nil
	}
	if (bestScore - secondScore) < defaultMinMargin {
		return "", bestScore, false, nil
	}
	return bestIntent, bestScore, true, nil
}

// DetectWithStore performs intent detection using a compatible stored exemplar
// snapshot when available, otherwise it detects with the current provider.
func (d *Detector) DetectWithStore(ctx context.Context, query string, store intentstore.SnapshotReader) (search.Intent, float64, bool, error) {
	if store == nil {
		return d.Detect(ctx, query)
	}
	if d == nil || d.provider == nil {
		return "", 0, false, nil
	}
	query = strings.TrimSpace(query)
	if query == "" {
		return "", 0, false, nil
	}

	queryVec, err := d.embedQuery(ctx, query)
	if err != nil {
		return "", 0, false, err
	}

	intentVec, err := d.intentVecFromStore(ctx, store, len(queryVec))
	if err != nil {
		// If the store isn't ready, fall back to in-memory detection.
		return d.Detect(ctx, query)
	}
	if len(intentVec) == 0 {
		return d.Detect(ctx, query)
	}

	bestIntent, bestScore, secondScore := bestIntentScores(queryVec, intentVec)
	if bestScore < defaultMinScore {
		return "", bestScore, false, nil
	}
	if (bestScore - secondScore) < defaultMinMargin {
		return "", bestScore, false, nil
	}
	return bestIntent, bestScore, true, nil
}

func (d *Detector) embedQuery(ctx context.Context, query string) (embeddings.Embedding, error) {
	vecs, err := d.provider.EmbedTexts(ctx, []string{query})
	if err != nil || len(vecs) == 0 {
		return nil, err
	}
	return vecs[0], nil
}

func (d *Detector) embedExemplarsAndQueryLocked(ctx context.Context, query string) (embeddings.Embedding, error) {
	if d.embedded {
		return nil, d.embedErr
	}
	batch := flattenExemplars(d.exemplars)
	texts := make([]string, 0, len(batch.texts)+1)
	texts = append(texts, batch.texts...)
	texts = append(texts, query)

	vecs, err := d.provider.EmbedTexts(ctx, texts)
	if err != nil {
		d.embedErr = err
		return nil, err
	}
	if len(vecs) < len(texts) {
		err = fmt.Errorf("intent exemplar embeddings: expected %d vectors, got %d", len(texts), len(vecs))
		d.embedErr = err
		return nil, err
	}

	for intent, r := range batch.ranges {
		if r.count == 0 {
			continue
		}
		start := r.start
		end := r.start + r.count
		d.intentVec[intent] = append([]embeddings.Embedding(nil), vecs[start:end]...)
	}
	d.embedErr = nil
	d.embedded = true
	return vecs[len(texts)-1], nil
}

func (d *Detector) snapshotIntentVec() map[search.Intent][]embeddings.Embedding {
	d.mu.Lock()
	defer d.mu.Unlock()
	out := make(map[search.Intent][]embeddings.Embedding, len(d.intentVec))
	for intent, vecs := range d.intentVec {
		out[intent] = vecs
	}
	return out
}

func (d *Detector) intentVecFromStore(ctx context.Context, store intentstore.SnapshotReader, dimensions int) (map[search.Intent][]embeddings.Embedding, error) {
	snapshot, err := store.IntentEmbeddingSnapshot(ctx)
	if err != nil {
		return nil, err
	}
	if d.providerFingerprint == "" || snapshot.ProviderFingerprint == "" || snapshot.ProviderFingerprint != d.providerFingerprint {
		return nil, nil
	}
	rows := snapshot.Rows
	if len(rows) == 0 {
		return nil, nil
	}
	if dimensions <= 0 {
		return nil, nil
	}
	pairs := flattenExemplarPairs(d.exemplars)
	if len(rows) != len(pairs) {
		return nil, nil
	}
	expected := make(map[string]struct{}, len(pairs))
	for _, pair := range pairs {
		expected[intentKey(string(pair.Intent), pair.Exemplar)] = struct{}{}
	}
	intentVec := make(map[search.Intent][]embeddings.Embedding)
	for _, row := range rows {
		if row.Dimensions != dimensions || len(row.Embedding) != dimensions {
			return nil, nil
		}
		key := intentKey(row.Intent, row.Exemplar)
		if _, ok := expected[key]; !ok {
			return nil, nil
		}
		delete(expected, key)
		intent := search.Intent(row.Intent)
		intentVec[intent] = append(intentVec[intent], row.Embedding)
	}
	return intentVec, nil
}

func bestIntentScores(query embeddings.Embedding, intentVec map[search.Intent][]embeddings.Embedding) (search.Intent, float64, float64) {
	var bestIntent search.Intent
	bestScore := -1.0
	secondScore := -1.0
	for intent, exemplars := range intentVec {
		score := maxCosine(query, exemplars)
		if score > bestScore {
			secondScore = bestScore
			bestScore = score
			bestIntent = intent
			continue
		}
		if score > secondScore {
			secondScore = score
		}
	}
	return bestIntent, bestScore, secondScore
}

// DetectorForProvider returns a cached detector instance keyed by provider and
// embedding configuration identity.
func DetectorForProvider(provider embeddings.Provider, providerInfo embeddings.ProviderConfig) *Detector {
	if provider == nil {
		return nil
	}
	providerPointer := providerKey(provider)
	if providerPointer == 0 {
		return NewDetector(provider, providerInfo, DefaultExemplars())
	}
	key := detectorCacheKey{providerPointer: providerPointer, providerFingerprint: ProviderFingerprint(providerInfo)}
	if v, ok := detectorCache.Load(key); ok {
		return v.(*Detector)
	}
	d := NewDetector(provider, providerInfo, DefaultExemplars())
	detectorCache.Store(key, d)
	return d
}

type detectorCacheKey struct {
	providerPointer     uintptr
	providerFingerprint string
}

var detectorCache sync.Map // detectorCacheKey -> *Detector

func providerKey(provider embeddings.Provider) uintptr {
	val := reflect.ValueOf(provider)
	if !val.IsValid() {
		return 0
	}
	for val.Kind() == reflect.Interface {
		val = val.Elem()
	}
	if val.Kind() != reflect.Ptr && val.Kind() != reflect.UnsafePointer {
		return 0
	}
	return val.Pointer()
}

func maxCosine(query embeddings.Embedding, vecs []embeddings.Embedding) float64 {
	best := -1.0
	for _, v := range vecs {
		if score := cosine(query, v); score > best {
			best = score
		}
	}
	return best
}

type exemplarRange struct {
	start int
	count int
}

type exemplarBatch struct {
	texts  []string
	ranges map[search.Intent]exemplarRange
}

func flattenExemplars(exemplars map[search.Intent][]string) exemplarBatch {
	if len(exemplars) == 0 {
		return exemplarBatch{ranges: map[search.Intent]exemplarRange{}}
	}
	intents := make([]search.Intent, 0, len(exemplars))
	for intent := range exemplars {
		intents = append(intents, intent)
	}
	sort.SliceStable(intents, func(i, j int) bool {
		return intents[i] < intents[j]
	})

	out := exemplarBatch{
		ranges: make(map[search.Intent]exemplarRange, len(intents)),
	}
	for _, intent := range intents {
		items := exemplars[intent]
		if len(items) == 0 {
			continue
		}
		out.ranges[intent] = exemplarRange{start: len(out.texts), count: len(items)}
		out.texts = append(out.texts, items...)
	}
	return out
}

func cosine(a, b embeddings.Embedding) float64 {
	if len(a) == 0 || len(b) == 0 || len(a) != len(b) {
		return 0
	}
	var dot, normA, normB float64
	for i := range a {
		af := float64(a[i])
		bf := float64(b[i])
		dot += af * bf
		normA += af * af
		normB += bf * bf
	}
	if normA == 0 || normB == 0 {
		return 0
	}
	return dot / (math.Sqrt(normA) * math.Sqrt(normB))
}
