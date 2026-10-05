package update

import (
	"bufio"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"runtime"
	"strings"
	"time"
)

const checksumsAssetName = "checksums.txt"

// DefaultReleasesURL is the GitHub-release-shaped API root. Internal release
// builds replace it through -ldflags with a private mirror; the mirror location
// never enters source (see docs/RELEASING.md).
var DefaultReleasesURL = "https://api.github.com/repos/atomicobject/rhizome/releases"

func defaultManifestURL() string { return DefaultReleasesURL + "/latest" }

var defaultHTTPClient = &http.Client{Timeout: 60 * time.Second}

// Artifact describes a downloadable Rhizome release archive.
type Artifact struct {
	URL    string
	SHA256 string
}

// Manifest is the updater's resolved view of one GitHub Release.
type Manifest struct {
	Version   string
	Artifacts map[string]Artifact
}

type githubRelease struct {
	TagName    string        `json:"tag_name"`
	Draft      bool          `json:"draft"`
	Prerelease bool          `json:"prerelease"`
	Assets     []githubAsset `json:"assets"`
}

type githubAsset struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
}

// PlatformKey returns the manifest key for an OS/architecture pair.
func PlatformKey(goos, goarch string) string {
	return goos + "/" + goarch
}

// CurrentPlatformKey returns the manifest key for this binary.
func CurrentPlatformKey() string {
	return PlatformKey(runtime.GOOS, runtime.GOARCH)
}

// ArtifactObjectName returns the GitHub Release asset name for a platform.
func ArtifactObjectName(goos, goarch string) string {
	return fmt.Sprintf("rhizome-%s-%s.tar.gz", goos, goarch)
}

// NormalizeVersion keeps release tags and config pins consistently v-prefixed.
func NormalizeVersion(version string) string {
	version = strings.TrimSpace(version)
	if version == "" || version == "latest" {
		return version
	}
	if strings.HasPrefix(version, "v") {
		return version
	}
	return "v" + version
}

// FetchManifest resolves the latest stable public GitHub Release and its
// checksums. manifestURL is retained as the caller-facing override name while
// it now identifies a GitHub Release API endpoint.
func FetchManifest(client *http.Client, manifestURL string) (Manifest, error) {
	if strings.TrimSpace(manifestURL) == "" {
		manifestURL = defaultManifestURL()
	}
	return fetchRelease(client, manifestURL, "", false)
}

// FetchVersionManifest resolves one exact GitHub Release tag. Explicit pins
// may select prereleases, but draft releases are never installable.
func FetchVersionManifest(client *http.Client, latestURL, version string) (Manifest, error) {
	version = NormalizeVersion(version)
	if version == "" || version == "latest" {
		return Manifest{}, fmt.Errorf("exact release version is required")
	}
	return fetchRelease(client, versionReleaseURL(latestURL, version), version, true)
}

func fetchRelease(client *http.Client, releaseURL, expectedVersion string, allowPrerelease bool) (Manifest, error) {
	if client == nil {
		client = defaultHTTPClient
	}
	var release githubRelease
	if err := getJSON(client, releaseURL, &release); err != nil {
		return Manifest{}, fmt.Errorf("fetch GitHub release: %w", err)
	}
	version := NormalizeVersion(release.TagName)
	if version == "" || version == "latest" {
		return Manifest{}, fmt.Errorf("GitHub release missing tag_name")
	}
	if release.Draft {
		return Manifest{}, fmt.Errorf("GitHub release %s is a draft", version)
	}
	if release.Prerelease && !allowPrerelease {
		return Manifest{}, fmt.Errorf("latest GitHub release %s is a prerelease", version)
	}
	if expectedVersion != "" && version != NormalizeVersion(expectedVersion) {
		return Manifest{}, fmt.Errorf("GitHub release tag %s does not match requested %s", version, NormalizeVersion(expectedVersion))
	}

	assets := make(map[string]string, len(release.Assets))
	for _, asset := range release.Assets {
		name := strings.TrimSpace(asset.Name)
		downloadURL := strings.TrimSpace(asset.BrowserDownloadURL)
		if name == "" || downloadURL == "" {
			continue
		}
		if _, exists := assets[name]; exists {
			return Manifest{}, fmt.Errorf("GitHub release %s has duplicate asset %s", version, name)
		}
		assets[name] = downloadURL
	}
	checksumsURL, ok := assets[checksumsAssetName]
	if !ok {
		return Manifest{}, fmt.Errorf("GitHub release %s missing %s", version, checksumsAssetName)
	}
	checksums, err := fetchChecksums(client, checksumsURL)
	if err != nil {
		return Manifest{}, fmt.Errorf("fetch %s for %s: %w", checksumsAssetName, version, err)
	}

	artifacts := make(map[string]Artifact)
	for _, platform := range []struct{ goos, goarch string }{
		{"darwin", "amd64"},
		{"darwin", "arm64"},
		{"linux", "amd64"},
		{"linux", "arm64"},
		{"windows", "amd64"},
	} {
		name := ArtifactObjectName(platform.goos, platform.goarch)
		downloadURL, assetExists := assets[name]
		checksum, checksumExists := checksums[name]
		if assetExists != checksumExists {
			return Manifest{}, fmt.Errorf("GitHub release %s asset/checksum mismatch for %s", version, name)
		}
		if assetExists {
			artifacts[PlatformKey(platform.goos, platform.goarch)] = Artifact{URL: downloadURL, SHA256: checksum}
		}
	}
	if len(artifacts) == 0 {
		return Manifest{}, fmt.Errorf("GitHub release %s has no Rhizome platform archives", version)
	}
	return Manifest{Version: version, Artifacts: artifacts}, nil
}

func getJSON(client *http.Client, endpoint string, target any) error {
	req, err := http.NewRequest(http.MethodGet, endpoint, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "rhizome-updater")
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("%s", resp.Status)
	}
	decoder := json.NewDecoder(io.LimitReader(resp.Body, 4<<20))
	if err := decoder.Decode(target); err != nil {
		return fmt.Errorf("decode response: %w", err)
	}
	return nil
}

func fetchChecksums(client *http.Client, endpoint string) (map[string]string, error) {
	req, err := http.NewRequest(http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/octet-stream")
	req.Header.Set("User-Agent", "rhizome-updater")
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("%s", resp.Status)
	}
	return decodeChecksums(io.LimitReader(resp.Body, 1<<20))
}

func decodeChecksums(r io.Reader) (map[string]string, error) {
	checksums := make(map[string]string)
	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) != 2 {
			return nil, fmt.Errorf("invalid checksum line %q", scanner.Text())
		}
		sum := strings.ToLower(fields[0])
		if decoded, err := hex.DecodeString(sum); err != nil || len(decoded) != 32 {
			return nil, fmt.Errorf("invalid SHA256 for %s", fields[1])
		}
		name := strings.TrimPrefix(fields[1], "*")
		if _, exists := checksums[name]; exists {
			return nil, fmt.Errorf("duplicate checksum for %s", name)
		}
		checksums[name] = sum
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	if len(checksums) == 0 {
		return nil, fmt.Errorf("checksum file is empty")
	}
	return checksums, nil
}

// ArtifactFor returns the release artifact for a platform key.
func (m Manifest) ArtifactFor(platform string) (Artifact, error) {
	artifact, ok := m.Artifacts[platform]
	if !ok {
		return Artifact{}, fmt.Errorf("release has no artifact for %s", platform)
	}
	if strings.TrimSpace(artifact.URL) == "" {
		return Artifact{}, fmt.Errorf("release artifact for %s missing url", platform)
	}
	if strings.TrimSpace(artifact.SHA256) == "" {
		return Artifact{}, fmt.Errorf("release artifact for %s missing sha256", platform)
	}
	return artifact, nil
}

func versionReleaseURL(latestURL, version string) string {
	if strings.TrimSpace(latestURL) == "" {
		latestURL = defaultManifestURL()
	}
	parsed, err := url.Parse(latestURL)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return DefaultReleasesURL + "/tags/" + url.PathEscape(NormalizeVersion(version))
	}
	path := strings.TrimSuffix(parsed.Path, "/")
	if strings.HasSuffix(path, "/latest") {
		path = strings.TrimSuffix(path, "/latest")
	} else if !strings.HasSuffix(path, "/releases") {
		return DefaultReleasesURL + "/tags/" + url.PathEscape(NormalizeVersion(version))
	}
	parsed.Path = strings.TrimSuffix(path, "/") + "/tags/" + NormalizeVersion(version)
	parsed.RawPath = ""
	parsed.RawQuery = ""
	parsed.Fragment = ""
	return parsed.String()
}
