package notemeta

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"math/rand"
	"path/filepath"
	"testing"

	semdb "github.com/atomicobject/rhizome/pkg/store"
	"github.com/atomicobject/rhizome/pkg/testutil/sqlitefixture"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/require"
)

func TestRandomizedDeltasEqualFreshSnapshots(t *testing.T) {
	for _, seed := range []int64{7, 29, 103} {
		t.Run(fmt.Sprint(seed), func(t *testing.T) {
			ctx := context.Background()
			random := rand.New(rand.NewSource(seed))
			store, err := sqlitefixture.Open(filepath.Join(t.TempDir(), "incremental.sqlite"))
			require.NoError(t, err)
			defer store.Close()
			reader := fakeNoteReader{notes: map[string]string{"source.md": "[[Target]] [[ALPHA]] [[Café]]"}}
			vault := obsidian.VaultDefinition{Path: t.TempDir()}
			indexer := testIndexer(t)
			_, err = indexer.EnsureIndexed(ctx, vault, reader, store)
			require.NoError(t, err)
			names := []string{"a/Target.md", "b/target.md", "Target.md", "Café.md", "Cafe\u0301.md", "other.md"}
			bodies := []string{"# Plain\n", "---\naliases: [Alpha]\ntags: [planning]\n---\n[[Target]] #work", "---\naliases: [ALPHA, Café]\n---\n[Target](Target.md)", "---\naliases: [\n---\n[[other]]", "~~~\n[[Target]] #hidden\n~~~\n[[Café]]", "---\nsummary: {text: nested, count: 3}\n---\n# Title\n"}
			for step := 0; step < 32; step++ {
				name := names[random.Intn(len(names))]
				var changed, deleted []string
				switch random.Intn(4) {
				case 0:
					if _, ok := reader.notes[name]; ok {
						delete(reader.notes, name)
						deleted = []string{name}
					}
				case 1:
					destination := names[random.Intn(len(names))]
					if body, ok := reader.notes[name]; ok && destination != name {
						delete(reader.notes, name)
						reader.notes[destination] = body
						deleted = []string{name}
						changed = []string{destination}
					}
				default:
					reader.notes[name] = bodies[random.Intn(len(bodies))]
					changed = []string{name}
				}
				require.NoError(t, indexer.SyncPaths(ctx, vault, reader, store, changed, deleted), "seed %d step %d", seed, step)
				fresh, err := sqlitefixture.Open(filepath.Join(t.TempDir(), "fresh.sqlite"))
				require.NoError(t, err)
				_, err = indexer.EnsureIndexed(ctx, vault, reader, fresh)
				require.NoError(t, err)
				require.Equal(t, noteMetadataStateShape(t, fresh), noteMetadataStateShape(t, store), "seed %d step %d", seed, step)
				require.NoError(t, fresh.Close())
			}
		})
	}
}

func noteMetadataStateShape(t *testing.T, store *semdb.Store) map[string][]string {
	t.Helper()
	queries := map[string]string{
		"notes":       `SELECT path,title,content_hash,indexer_version,mtime,size,format_id FROM notes WHERE indexed_at>0 ORDER BY path`,
		"state":       `SELECT notes_hash,raw_notes_hash,ready FROM note_metadata_state`,
		"projection":  `SELECT n.path,p.provider_version,p.projection_version,p.source_content_hash,p.status,p.diagnostic_code,p.diagnostic_detail FROM note_projection_state p JOIN notes n ON n.id=p.note_id ORDER BY n.path`,
		"properties":  `SELECT n.path,k.property_name,p.source,p.value_text,p.value_norm,p.value_kind,p.is_list,p.list_ordinal FROM note_property_values p JOIN notes n ON n.id=p.note_id JOIN property_keys k ON k.property_id=p.property_id ORDER BY n.path,k.property_name,p.source,p.list_ordinal,p.value_text`,
		"tags":        `SELECT n.path,t.tag_norm FROM note_tags t JOIN notes n ON n.id=t.note_id ORDER BY n.path,t.tag_norm`,
		"targets":     `SELECT n.path,t.target_kind,t.target_text,t.target_norm,t.ordinal FROM note_fragment_targets t JOIN notes n ON n.id=t.note_id ORDER BY n.path,t.target_kind,t.ordinal,t.target_norm`,
		"diagnostics": `SELECT n.path,d.ordinal,d.code,d.category,d.message,d.range_present,d.start_byte,d.end_byte,d.blocking,d.affected_operation FROM note_projection_diagnostics d JOIN notes n ON n.id=d.note_id ORDER BY n.path,d.ordinal`,
		"regions":     `SELECT n.path,r.ordinal,r.origin,r.region_kind,r.region_text,r.media_type,r.range_present,r.start_byte,r.end_byte FROM note_search_regions r JOIN notes n ON n.id=r.note_id ORDER BY n.path,r.ordinal`,
		"inputs":      `SELECT n.path,i.lookup_key FROM note_link_inputs i JOIN notes n ON n.id=i.note_id ORDER BY n.path,i.lookup_key`,
		"payloads":    `SELECT n.path,p.payload_json FROM note_projection_payloads p JOIN notes n ON n.id=p.note_id ORDER BY n.path`,
		"edges":       `SELECT src_path,dst_path,kind,confidence,confidence_score,source_location,link_text FROM graph_doc_edges WHERE kind IN ('wikilink','mdlink') OR kind GLOB 'note_link:*' ORDER BY src_path,dst_path,kind`,
	}
	out := map[string][]string{}
	for name, query := range queries {
		out[name] = queryShape(t, store.DB(), query)
	}
	return out
}

func queryShape(t *testing.T, db *sql.DB, query string) []string {
	t.Helper()
	rows, err := db.QueryContext(context.Background(), query)
	require.NoError(t, err)
	defer rows.Close()
	columns, err := rows.Columns()
	require.NoError(t, err)
	var out []string
	for rows.Next() {
		values := make([]any, len(columns))
		dest := make([]any, len(columns))
		for i := range values {
			dest[i] = &values[i]
		}
		require.NoError(t, rows.Scan(dest...))
		raw, err := json.Marshal(values)
		require.NoError(t, err)
		out = append(out, string(raw))
	}
	require.NoError(t, rows.Err())
	return out
}
