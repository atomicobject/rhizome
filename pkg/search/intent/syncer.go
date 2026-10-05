package intent

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"slices"
	"sort"
	"strings"

	"github.com/atomicobject/rhizome/pkg/search"
	"github.com/atomicobject/rhizome/pkg/search/embeddings"
	"github.com/atomicobject/rhizome/pkg/search/intentstore"
)

type EmbeddingNode interface {
	Submit(ctx context.Context, phase string, texts []string) ([]embeddings.Embedding, error)
}

type SyncOptions struct {
	Node         EmbeddingNode
	Phase        string
	ProviderInfo embeddings.ProviderConfig
}

const providerFingerprintVersion = "intent-embedding-provider-v1"

// ProviderFingerprint identifies provider settings that can change embedding
// values. Operational settings and credentials do not affect cache identity.
func ProviderFingerprint(info embeddings.ProviderConfig) string {
	provider := strings.ToLower(strings.TrimSpace(info.Provider))
	if provider == "" {
		return ""
	}
	h := sha256.New()
	for _, part := range []string{
		providerFingerprintVersion,
		provider,
		strings.TrimSpace(info.Model),
		strings.TrimRight(strings.TrimSpace(info.Endpoint), "/"),
		fmt.Sprintf("%d", info.Dimensions),
	} {
		_, _ = fmt.Fprintf(h, "%d:%s\n", len(part), part)
	}
	return providerFingerprintVersion + ":" + hex.EncodeToString(h.Sum(nil))
}

// SyncEmbeddings embeds exemplar texts and stores them in the intent embedding table.
func SyncEmbeddings(ctx context.Context, provider embeddings.Provider, store intentstore.Store, exemplars map[search.Intent][]string) error {
	return SyncEmbeddingsWithOptions(ctx, provider, store, exemplars, SyncOptions{})
}

func SyncEmbeddingsWithOptions(ctx context.Context, provider embeddings.Provider, store intentstore.Store, exemplars map[search.Intent][]string, opts SyncOptions) error {
	if provider == nil || store == nil {
		return nil
	}
	if len(exemplars) == 0 {
		return nil
	}

	exemplarPairs := flattenExemplarPairs(exemplars)
	if len(exemplarPairs) == 0 {
		return nil
	}

	snapshot, err := store.IntentEmbeddingSnapshot(ctx)
	if err != nil {
		return err
	}

	providerFingerprint := ProviderFingerprint(opts.ProviderInfo)
	providerDim := provider.Dimensions()
	dim := providerDim
	existingMap := make(map[string]intentstore.EmbeddingRecord, len(snapshot.Rows))
	canReuse := snapshot.ProviderFingerprint != "" && snapshot.ProviderFingerprint == providerFingerprint
	if canReuse {
		storedDim, ok := consistentSnapshotDimensions(snapshot.Rows)
		if !ok || (dim > 0 && storedDim != dim) {
			canReuse = false
		} else if dim <= 0 {
			dim = storedDim
		}
	}
	if canReuse {
		for _, row := range snapshot.Rows {
			key := intentKey(row.Intent, row.Exemplar)
			if _, exists := existingMap[key]; exists {
				canReuse = false
				break
			}
			existingMap[key] = row
		}
	}
	if !canReuse {
		clear(existingMap)
		dim = providerDim
	}

	missing := make([]exemplarPair, 0, len(exemplarPairs))
	for _, pair := range exemplarPairs {
		if _, ok := existingMap[intentKey(string(pair.Intent), pair.Exemplar)]; ok {
			continue
		}
		missing = append(missing, pair)
	}
	if len(missing) == 0 && len(snapshot.Rows) == len(exemplarPairs) {
		return nil
	}

	generated := make(map[string]intentstore.EmbeddingRecord, len(missing))
	if len(missing) > 0 {
		embed := provider.EmbedTexts
		batchSize := len(missing)
		if opts.Node != nil {
			phase := opts.Phase
			if phase == "" {
				phase = "sync_intent_embeddings"
			}
			embed = func(ctx context.Context, texts []string) ([]embeddings.Embedding, error) {
				return opts.Node.Submit(ctx, phase, texts)
			}
		} else {
			batchSize = providerBatchSize(provider)
		}
		for batch := range slices.Chunk(missing, batchSize) {
			vecs, err := embed(ctx, exemplarTexts(batch))
			if err != nil {
				return fmt.Errorf("intent exemplar embeddings: %w", err)
			}
			if len(vecs) != len(batch) {
				return fmt.Errorf("intent exemplar embeddings: expected %d vectors, got %d", len(batch), len(vecs))
			}
			dim, err = appendGeneratedIntentRows(generated, batch, vecs, dim)
			if err != nil {
				return err
			}
		}
	}

	rows := make([]intentstore.EmbeddingRecord, 0, len(exemplarPairs))
	for _, pair := range exemplarPairs {
		key := intentKey(string(pair.Intent), pair.Exemplar)
		if row, ok := existingMap[key]; ok {
			rows = append(rows, row)
			continue
		}
		rows = append(rows, generated[key])
	}
	return store.ReplaceIntentEmbeddingSnapshot(ctx, intentstore.Snapshot{
		ProviderFingerprint: providerFingerprint,
		Rows:                rows,
	})
}

type defaultBatchSizeProvider interface {
	DefaultBatchSize() int
}

func providerBatchSize(provider embeddings.Provider) int {
	if p, ok := provider.(defaultBatchSizeProvider); ok {
		if v := p.DefaultBatchSize(); v > 0 {
			return v
		}
	}
	return embeddings.DefaultBatchSize
}

func exemplarTexts(pairs []exemplarPair) []string {
	texts := make([]string, 0, len(pairs))
	for _, pair := range pairs {
		texts = append(texts, pair.Exemplar)
	}
	return texts
}

func appendGeneratedIntentRows(rows map[string]intentstore.EmbeddingRecord, pairs []exemplarPair, vecs []embeddings.Embedding, dim int) (int, error) {
	for i, pair := range pairs {
		vecDim := len(vecs[i])
		if vecDim <= 0 {
			return 0, fmt.Errorf("intent exemplar embeddings: exemplar %q returned an empty vector", pair.Exemplar)
		}
		if dim <= 0 {
			dim = vecDim
		}
		if vecDim != dim {
			return 0, fmt.Errorf("intent exemplar embeddings: exemplar %q returned %d dimensions, expected %d", pair.Exemplar, vecDim, dim)
		}
		row := intentstore.EmbeddingRecord{
			Intent:     string(pair.Intent),
			Exemplar:   pair.Exemplar,
			Embedding:  vecs[i],
			Dimensions: dim,
		}
		rows[intentKey(row.Intent, row.Exemplar)] = row
	}
	return dim, nil
}

func consistentSnapshotDimensions(rows []intentstore.EmbeddingRecord) (int, bool) {
	if len(rows) == 0 {
		return 0, false
	}
	dim := rows[0].Dimensions
	if dim <= 0 || len(rows[0].Embedding) != dim {
		return 0, false
	}
	for _, row := range rows[1:] {
		if row.Dimensions != dim || len(row.Embedding) != dim {
			return 0, false
		}
	}
	return dim, true
}

type exemplarPair struct {
	Intent   search.Intent
	Exemplar string
}

func flattenExemplarPairs(exemplars map[search.Intent][]string) []exemplarPair {
	if len(exemplars) == 0 {
		return nil
	}
	out := make([]exemplarPair, 0, len(exemplars))
	seen := make(map[string]struct{})
	for intent, items := range exemplars {
		for _, ex := range items {
			ex = strings.TrimSpace(ex)
			if ex == "" {
				continue
			}
			key := intentKey(string(intent), ex)
			if _, duplicate := seen[key]; duplicate {
				continue
			}
			seen[key] = struct{}{}
			out = append(out, exemplarPair{Intent: intent, Exemplar: ex})
		}
	}
	if len(out) == 0 {
		return nil
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Intent == out[j].Intent {
			return out[i].Exemplar < out[j].Exemplar
		}
		return out[i].Intent < out[j].Intent
	})
	return out
}

func intentKey(intent, exemplar string) string {
	return intent + "\x00" + exemplar
}
