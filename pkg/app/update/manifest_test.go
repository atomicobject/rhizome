package update

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

const testSHA256 = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

func TestFetchManifestResolvesLatestReleaseAssetsAndChecksums(t *testing.T) {
	var metadataRequests atomic.Int64
	var checksumRequests atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/releases/latest":
			metadataRequests.Add(1)
			require.Equal(t, "application/vnd.github+json", r.Header.Get("Accept"))
			fmt.Fprint(w, releaseJSON(r, "v0.39.0", false, false, ArtifactObjectName("darwin", "arm64")))
		case "/assets/checksums.txt":
			checksumRequests.Add(1)
			fmt.Fprintf(w, "%s  %s\n", testSHA256, ArtifactObjectName("darwin", "arm64"))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)

	manifest, err := FetchManifest(server.Client(), server.URL+"/releases/latest")

	require.NoError(t, err)
	require.Equal(t, "v0.39.0", manifest.Version)
	artifact, err := manifest.ArtifactFor("darwin/arm64")
	require.NoError(t, err)
	require.Equal(t, testSHA256, artifact.SHA256)
	require.Equal(t, server.URL+"/assets/"+ArtifactObjectName("darwin", "arm64"), artifact.URL)
	require.EqualValues(t, 1, metadataRequests.Load())
	require.EqualValues(t, 1, checksumRequests.Load())
}

func TestFetchVersionManifestUsesTagEndpointAndAllowsMatchingPrerelease(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/releases/tags/v0.40.0-rc.1":
			fmt.Fprint(w, releaseJSON(r, "v0.40.0-rc.1", false, true, ArtifactObjectName("linux", "amd64")))
		case "/assets/checksums.txt":
			fmt.Fprintf(w, "%s  %s\n", testSHA256, ArtifactObjectName("linux", "amd64"))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)

	manifest, err := FetchVersionManifest(server.Client(), server.URL+"/releases/latest", "0.40.0-rc.1")

	require.NoError(t, err)
	require.Equal(t, "v0.40.0-rc.1", manifest.Version)
}

func TestFetchReleaseRejectsUnsafeOrIncompleteMetadata(t *testing.T) {
	tests := []struct {
		name       string
		tag        string
		draft      bool
		prerelease bool
		checksums  string
		want       string
	}{
		{name: "draft", tag: "v1.0.0", draft: true, checksums: testSHA256 + "  " + ArtifactObjectName("darwin", "arm64") + "\n", want: "is a draft"},
		{name: "prerelease latest", tag: "v1.0.0-rc.1", prerelease: true, checksums: testSHA256 + "  " + ArtifactObjectName("darwin", "arm64") + "\n", want: "is a prerelease"},
		{name: "missing checksum", tag: "v1.0.0", checksums: testSHA256 + "  unrelated.tar.gz\n", want: "asset/checksum mismatch"},
		{name: "malformed checksum", tag: "v1.0.0", checksums: "bad  " + ArtifactObjectName("darwin", "arm64") + "\n", want: "invalid SHA256"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/releases/latest":
					fmt.Fprint(w, releaseJSON(r, test.tag, test.draft, test.prerelease, ArtifactObjectName("darwin", "arm64")))
				case "/assets/checksums.txt":
					fmt.Fprint(w, test.checksums)
				default:
					http.NotFound(w, r)
				}
			}))
			t.Cleanup(server.Close)

			_, err := FetchManifest(server.Client(), server.URL+"/releases/latest")
			require.ErrorContains(t, err, test.want)
		})
	}
}

func TestFetchVersionManifestRejectsMismatchedTag(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, releaseJSON(r, "v2.0.0", false, false, ArtifactObjectName("darwin", "arm64")))
	}))
	t.Cleanup(server.Close)

	_, err := FetchVersionManifest(server.Client(), server.URL+"/releases/latest", "v1.0.0")
	require.ErrorContains(t, err, "does not match requested v1.0.0")
}

func TestFetchManifestUsesClientTimeout(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		time.Sleep(100 * time.Millisecond)
	}))
	t.Cleanup(server.Close)
	client := &http.Client{Timeout: 10 * time.Millisecond}

	_, err := FetchManifest(client, server.URL+"/releases/latest")
	require.ErrorContains(t, err, "Client.Timeout")
}

func TestDecodeChecksumsRejectsDuplicates(t *testing.T) {
	_, err := decodeChecksums(strings.NewReader(fmt.Sprintf("%s  asset\n%s *asset\n", testSHA256, testSHA256)))
	require.ErrorContains(t, err, "duplicate checksum")
}

func TestArtifactObjectNameUsesGitHubReleaseShape(t *testing.T) {
	require.Equal(t, "rhizome-darwin-arm64.tar.gz", ArtifactObjectName("darwin", "arm64"))
}

func releaseJSON(r *http.Request, tag string, draft, prerelease bool, archiveName string) string {
	base := "http://" + r.Host
	return fmt.Sprintf(`{"tag_name":%q,"draft":%t,"prerelease":%t,"assets":[{"name":%q,"browser_download_url":%q},{"name":"checksums.txt","browser_download_url":%q}]}`,
		tag, draft, prerelease, archiveName, base+"/assets/"+archiveName, base+"/assets/checksums.txt")
}
