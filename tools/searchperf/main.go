package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	semdb "github.com/atomicobject/rhizome/pkg/anchors/sqlite"
	"github.com/atomicobject/rhizome/pkg/app/unifiedsearch"
	"github.com/atomicobject/rhizome/pkg/search/perfworkload"
	"github.com/atomicobject/rhizome/pkg/sqliteutil"
)

type report struct {
	ManifestFingerprint string                         `json:"manifestFingerprint"`
	GeneratedAt         string                         `json:"generatedAt"`
	Machine             machine                        `json:"machine"`
	Provider            perfworkload.EmbeddingManifest `json:"provider"`
	ColdDefinition      string                         `json:"coldDefinition"`
	Counts              perfworkload.FixtureCounts     `json:"counts"`
	Execution           executionIdentity              `json:"execution"`
	Samples             []perfworkload.Sample          `json:"samples"`
	Aggregates          []perfworkload.Aggregate       `json:"aggregates"`
}

type executionIdentity struct {
	GitHead                string `json:"gitHead"`
	Dirty                  bool   `json:"dirty"`
	WorkingTreeFingerprint string `json:"workingTreeFingerprint"`
	BinaryFingerprint      string `json:"binaryFingerprint"`
	IndexFingerprint       string `json:"indexFingerprint"`
	IndexBytes             int64  `json:"indexBytes"`
}

type machine struct {
	GOOS      string `json:"goos"`
	GOARCH    string `json:"goarch"`
	GoVersion string `json:"goVersion"`
	CPUs      int    `json:"cpus"`
	Hostname  string `json:"hostname"`
}

func main() {
	manifestPath := flag.String("manifest", "", "frozen workload manifest JSON")
	root := flag.String("root", "", "generated workload root")
	prepare := flag.Bool("prepare", false, "prepare the generated workload in an empty root")
	run := flag.Bool("run", false, "run the frozen measurement protocol")
	reportPath := flag.String("report", "", "report JSON output path")
	single := flag.Bool("single", false, "internal: run one cold child sample")
	profileName := flag.String("profile", "", "internal: sample profile")
	queryIndex := flag.Int("query-index", 0, "internal: sample query index")
	expectedManifestFingerprint := flag.String("manifest-fingerprint", "", "internal: expected captured manifest fingerprint")
	flag.Parse()
	if *manifestPath == "" || *root == "" {
		fail("-manifest and -root are required")
	}
	manifestBytes, err := os.ReadFile(*manifestPath)
	if err != nil {
		fail(err.Error())
	}
	manifestFingerprint := fingerprintBytes(manifestBytes)
	if *expectedManifestFingerprint != "" && manifestFingerprint != *expectedManifestFingerprint {
		fail("workload manifest changed before child measurement")
	}
	manifest, err := perfworkload.ParseManifest(manifestBytes)
	if err != nil {
		fail(err.Error())
	}
	ctx := context.Background()
	if *single {
		coldChild(ctx, *root, manifest, unifiedsearch.Profile(*profileName), *queryIndex)
		return
	}
	var counts perfworkload.FixtureCounts
	if *prepare {
		counts, err = perfworkload.Prepare(ctx, *root, manifest)
		if err != nil {
			fail(err.Error())
		}
		fmt.Fprintf(os.Stderr, "prepared owners=%d chunks=%d embeddings=%d graph_edges=%d\n", counts.Owners, counts.Chunks, counts.Embeddings, counts.GraphEdges)
	}
	if !*run {
		return
	}
	store, err := semdb.OpenReadOnlyExisting(perfworkload.IndexPath(*root), ctx, sqliteutil.Options{})
	if err != nil {
		fail(err.Error())
	}
	counts, err = perfworkload.ValidateFixture(ctx, store, manifest)
	_ = store.Close()
	if err != nil {
		fail(err.Error())
	}
	execution, err := captureExecutionIdentity(perfworkload.IndexPath(*root))
	if err != nil {
		fail(err.Error())
	}
	profiles := []unifiedsearch.Profile{unifiedsearch.ProfileInteractive, unifiedsearch.ProfileAgent}
	if err := perfworkload.ValidateQueries(ctx, *root, manifest, profiles); err != nil {
		fail(err.Error())
	}
	var samples []perfworkload.Sample
	for _, profile := range profiles {
		for _, query := range manifest.Queries {
			for _, concurrency := range manifest.Protocol.Concurrencies {
				measured, runErr := perfworkload.RunWarm(ctx, manifest, perfworkload.RunOptions{Root: *root, Profile: profile, Concurrency: concurrency, Warmups: manifest.Protocol.Warmups, Samples: manifest.Protocol.WarmSamples, Query: query})
				if runErr != nil {
					fail(runErr.Error())
				}
				samples = append(samples, measured...)
			}
			for i := 0; i < manifest.Protocol.ColdSamples; i++ {
				samples = append(samples, runColdChild(*manifestPath, *root, profile, query, queryIndexByID(manifest, query.ID), manifestFingerprint))
			}
		}
	}
	afterManifestBytes, err := os.ReadFile(*manifestPath)
	if err != nil {
		fail(err.Error())
	}
	if fingerprintBytes(afterManifestBytes) != manifestFingerprint {
		fail("workload manifest changed during measurement")
	}
	hostname, _ := os.Hostname()
	out := report{
		ManifestFingerprint: manifestFingerprint, GeneratedAt: time.Now().UTC().Format(time.RFC3339Nano),
		Machine:        machine{GOOS: runtime.GOOS, GOARCH: runtime.GOARCH, GoVersion: runtime.Version(), CPUs: runtime.NumCPU(), Hostname: hostname},
		Provider:       manifest.Embeddings,
		ColdDefinition: "one fresh searchperf process and read-only SQLite store per observation; parent wall time includes process startup, store open, query, response marshal, and process exit; OS page cache is not flushed and may remain warm",
		Counts:         counts, Execution: execution, Samples: samples, Aggregates: perfworkload.AggregateSamples(samples),
	}
	afterIndexFingerprint, _, err := fingerprintFile(perfworkload.IndexPath(*root))
	if err != nil {
		fail(err.Error())
	}
	if afterIndexFingerprint != execution.IndexFingerprint {
		fail("workload index changed during measurement")
	}
	if *reportPath == "" {
		if err := json.NewEncoder(os.Stdout).Encode(out); err != nil {
			fail(err.Error())
		}
		return
	}
	encoded, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		fail(err.Error())
	}
	if err := os.WriteFile(*reportPath, append(encoded, '\n'), 0o644); err != nil {
		fail(err.Error())
	}
}

func queryIndexByID(manifest perfworkload.Manifest, id string) int {
	for i := range manifest.Queries {
		if manifest.Queries[i].ID == id {
			return i
		}
	}
	return -1
}

func coldChild(ctx context.Context, root string, manifest perfworkload.Manifest, profile unifiedsearch.Profile, queryIndex int) {
	if queryIndex < 0 || queryIndex >= len(manifest.Queries) {
		fail("cold query index is out of bounds")
	}
	store, err := semdb.OpenReadOnlyExisting(perfworkload.IndexPath(root), ctx, sqliteutil.Options{})
	if err != nil {
		fail(err.Error())
	}
	sample := perfworkload.Measure(ctx, root, store, manifest, profile, 1, "cold", manifest.Queries[queryIndex])
	_ = store.Close()
	if err := json.NewEncoder(os.Stdout).Encode(sample); err != nil {
		fail(err.Error())
	}
}

func runColdChild(manifestPath, root string, profile unifiedsearch.Profile, query perfworkload.Query, queryIndex int, manifestFingerprint string) perfworkload.Sample {
	started := time.Now()
	cmd := exec.Command(os.Args[0], "-manifest", manifestPath, "-root", root, "-single", "-profile", string(profile), "-query-index", fmt.Sprint(queryIndex), "-manifest-fingerprint", manifestFingerprint)
	output, err := cmd.Output()
	duration := time.Since(started)
	if err != nil {
		return failedColdSample(profile, query, duration, err)
	}
	var sample perfworkload.Sample
	if err := json.Unmarshal(output, &sample); err != nil {
		return failedColdSample(profile, query, duration, err)
	}
	sample.DurationMS = float64(duration.Microseconds()) / 1000
	return sample
}

func failedColdSample(profile unifiedsearch.Profile, query perfworkload.Query, duration time.Duration, err error) perfworkload.Sample {
	return perfworkload.Sample{
		Profile: profile, Concurrency: 1, Temperature: "cold", QueryID: query.ID, LaneMode: query.LaneMode,
		EffectiveConfig: perfworkload.EffectiveConfig{Intent: query.Intent, Types: query.Types, PathPrefixes: query.PathPrefixes, UseVector: query.LaneMode == "hybrid", UseIntel: true, UseGraph: query.LaneMode == "graph", Pack: query.Pack, RequireBody: query.RequireBody},
		DurationMS:      float64(duration.Microseconds()) / 1000, Error: err.Error(),
	}
}

func captureExecutionIdentity(indexPath string) (executionIdentity, error) {
	head, err := exec.Command("git", "rev-parse", "HEAD").Output()
	if err != nil {
		return executionIdentity{}, err
	}
	diff, err := exec.Command("git", "diff", "--binary", "HEAD").Output()
	if err != nil {
		return executionIdentity{}, err
	}
	status, err := exec.Command("git", "status", "--porcelain=v1").Output()
	if err != nil {
		return executionIdentity{}, err
	}
	untracked, err := exec.Command("git", "ls-files", "--others", "--exclude-standard", "-z").Output()
	if err != nil {
		return executionIdentity{}, err
	}
	h := sha256.New()
	_, _ = h.Write(trimSpace(head))
	_, _ = h.Write([]byte{0})
	_, _ = h.Write(diff)
	for _, rel := range splitZero(untracked) {
		if filepath.Ext(rel) != ".go" && !(filepath.Ext(rel) == ".json" && strings.HasPrefix(filepath.Base(rel), "performance-workload-")) {
			continue
		}
		content, readErr := os.ReadFile(rel)
		if readErr != nil {
			return executionIdentity{}, readErr
		}
		_, _ = h.Write([]byte(rel))
		_, _ = h.Write([]byte{0})
		_, _ = h.Write(content)
	}
	binaryFingerprint, _, err := fingerprintFile(os.Args[0])
	if err != nil {
		return executionIdentity{}, err
	}
	indexFingerprint, indexBytes, err := fingerprintFile(indexPath)
	if err != nil {
		return executionIdentity{}, err
	}
	return executionIdentity{GitHead: string(trimSpace(head)), Dirty: len(status) > 0, WorkingTreeFingerprint: hex.EncodeToString(h.Sum(nil)), BinaryFingerprint: binaryFingerprint, IndexFingerprint: indexFingerprint, IndexBytes: indexBytes}, nil
}

func fingerprintBytes(data []byte) string {
	digest := sha256.Sum256(data)
	return hex.EncodeToString(digest[:])
}

func fingerprintFile(path string) (string, int64, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", 0, err
	}
	defer file.Close()
	h := sha256.New()
	size, err := io.Copy(h, file)
	if err != nil {
		return "", 0, err
	}
	return hex.EncodeToString(h.Sum(nil)), size, nil
}

func splitZero(value []byte) []string {
	var out []string
	start := 0
	for i, b := range value {
		if b != 0 {
			continue
		}
		if i > start {
			out = append(out, string(value[start:i]))
		}
		start = i + 1
	}
	return out
}

func trimSpace(value []byte) []byte {
	for len(value) > 0 && (value[len(value)-1] == '\n' || value[len(value)-1] == '\r' || value[len(value)-1] == ' ') {
		value = value[:len(value)-1]
	}
	return value
}

func fail(message string) {
	fmt.Fprintln(os.Stderr, message)
	os.Exit(1)
}
