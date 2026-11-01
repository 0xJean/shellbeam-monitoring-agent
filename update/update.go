package update

import (
	"bytes"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/blang/semver"
	goupdate "github.com/inconshreveable/go-update"
	"github.com/komari-monitor/komari-agent/dnsresolver"
)

var ErrRestartRequired = errors.New("update installed; restart required")

var (
	CurrentVersion = "1.5.13"
	VersionHash    = "unknown"
	Repo           = "0xJean/shellbeam-monitoring-agent"
)

const (
	ProductName           = "ShellBeam Monitoring Agent"
	UpstreamVersion       = "1.5.11"
	UpstreamCommit        = "c7bafb79b1a73ed9e14e2c248ae4d4163e506036"
	snapshotVersionPrefix = "Snapshot-"
	containerMarkerPath   = "/.komari-agent-container"
	githubAPIBaseURL      = "https://api.github.com"
	signatureSuffix       = ".sig"
	maxAssetBytes         = 256 * 1024 * 1024
	maxSignatureBytes     = 4 * 1024
	maxReleasesBytes      = 32 * 1024 * 1024
)

type buildTrack int

const (
	stableTrack buildTrack = iota
	snapshotTrack
)

type githubRelease struct {
	TagName     string               `json:"tag_name"`
	Name        string               `json:"name"`
	Body        string               `json:"body"`
	Draft       bool                 `json:"draft"`
	Prerelease  bool                 `json:"prerelease"`
	HTMLURL     string               `json:"html_url"`
	PublishedAt time.Time            `json:"published_at"`
	Assets      []githubReleaseAsset `json:"assets"`
}

type githubReleaseAsset struct {
	ID                 int64  `json:"id"`
	Name               string `json:"name"`
	Size               int    `json:"size"`
	BrowserDownloadURL string `json:"browser_download_url"`
}

type snapshotReleaseCandidate struct {
	TagName     string
	Name        string
	Body        string
	HTMLURL     string
	PublishedAt time.Time
	Asset       githubReleaseAsset
	Signature   *githubReleaseAsset
}

func (c snapshotReleaseCandidate) signature() (githubReleaseAsset, bool) {
	if c.Signature == nil {
		return githubReleaseAsset{}, false
	}
	return *c.Signature, true
}

// updateSource groups the external effects of an update so tests stay offline.
type updateSource struct {
	publicKey   string
	listRelease releaseLister
	download    func(url string, limit int64) ([]byte, error)
	install     func(payload []byte, cmdPath string) error
	execPath    func() (string, error)
}

type releaseLister func(owner, repo string) ([]githubRelease, error)
type containerCheck func() bool

// parseVersion 解析可能带有 v/V 前缀，以及预发布或构建元数据的版本字符串
func parseVersion(ver string) (semver.Version, error) {
	ver = strings.TrimPrefix(ver, "v")
	ver = strings.TrimPrefix(ver, "V")
	return semver.ParseTolerant(ver)
}

// needUpdate 判断是否需要更新
func needUpdate(current, latest semver.Version) bool {
	// 返回最新版本大于当前版本时需要更新
	return latest.Compare(current) > 0
}

func detectBuildTrack(version string) buildTrack {
	if strings.HasPrefix(version, snapshotVersionPrefix) {
		return snapshotTrack
	}
	return stableTrack
}

func expectedAssetName(goos, goarch string) string {
	name := fmt.Sprintf("komari-agent-%s-%s", goos, goarch)
	if goos == "windows" {
		name += ".exe"
	}
	return name
}

func findReleaseAsset(release githubRelease, assetName string) (githubReleaseAsset, bool) {
	for _, asset := range release.Assets {
		if asset.Name == assetName {
			return asset, true
		}
	}
	return githubReleaseAsset{}, false
}

func selectLatestSnapshotRelease(releases []githubRelease, assetName string) (snapshotReleaseCandidate, bool) {
	var latest snapshotReleaseCandidate
	found := false

	for _, release := range releases {
		if release.Draft || !release.Prerelease || !strings.HasPrefix(release.TagName, snapshotVersionPrefix) {
			continue
		}

		asset, ok := findReleaseAsset(release, assetName)
		if !ok {
			continue
		}

		candidate := snapshotReleaseCandidate{
			TagName:     release.TagName,
			Name:        release.Name,
			Body:        release.Body,
			HTMLURL:     release.HTMLURL,
			PublishedAt: release.PublishedAt,
			Asset:       asset,
		}
		if signature, ok := findReleaseAsset(release, assetName+signatureSuffix); ok {
			candidate.Signature = &signature
		}

		if !found ||
			candidate.PublishedAt.After(latest.PublishedAt) ||
			(candidate.PublishedAt.Equal(latest.PublishedAt) && candidate.TagName > latest.TagName) {
			latest = candidate
			found = true
		}
	}

	return latest, found
}

// selectLatestStableRelease returns the highest published release version that
// carries the platform asset. Tags that are not plain versions are ignored.
func selectLatestStableRelease(releases []githubRelease, assetName string) (githubRelease, semver.Version, bool) {
	var latest githubRelease
	var latestVersion semver.Version
	found := false

	for _, release := range releases {
		if release.Draft || release.Prerelease {
			continue
		}
		version, err := parseVersion(release.TagName)
		if err != nil || len(version.Pre) > 0 {
			continue
		}
		if _, ok := findReleaseAsset(release, assetName); !ok {
			continue
		}
		if !found || version.GT(latestVersion) {
			latest, latestVersion, found = release, version, true
		}
	}

	return latest, latestVersion, found
}

func snapshotNeedsUpdate(currentVersion string, latest snapshotReleaseCandidate) bool {
	return currentVersion != latest.TagName
}

func isContainerAgent() bool {
	_, err := os.Stat(containerMarkerPath)
	return err == nil
}

func splitRepoSlug(slug string) (string, string, error) {
	parts := strings.Split(slug, "/")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", fmt.Errorf("invalid repo slug %q, expected owner/name", slug)
	}
	return parts[0], parts[1], nil
}

func listGitHubReleases(owner, repo string) ([]githubRelease, error) {
	var releases []githubRelease

	for page := 1; ; page++ {
		endpoint := fmt.Sprintf(
			"%s/repos/%s/%s/releases?per_page=100&page=%d",
			githubAPIBaseURL,
			url.PathEscape(owner),
			url.PathEscape(repo),
			page,
		)
		req, err := http.NewRequest(http.MethodGet, endpoint, nil)
		if err != nil {
			return nil, fmt.Errorf("failed to create GitHub releases request: %w", err)
		}

		req.Header.Set("Accept", "application/vnd.github+json")
		req.Header.Set("User-Agent", "shellbeam-monitoring-agent")
		if token := os.Getenv("GITHUB_TOKEN"); token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}

		resp, err := updateHTTPClient().Do(req)
		if err != nil {
			return nil, fmt.Errorf("failed to list GitHub releases: %w", err)
		}

		if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
			body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
			_ = resp.Body.Close()
			return nil, fmt.Errorf("GitHub releases API returned status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
		}

		var pageReleases []githubRelease
		if err := json.NewDecoder(io.LimitReader(resp.Body, maxReleasesBytes)).Decode(&pageReleases); err != nil {
			_ = resp.Body.Close()
			return nil, fmt.Errorf("failed to decode GitHub releases response: %w", err)
		}
		_ = resp.Body.Close()

		releases = append(releases, pageReleases...)
		if len(pageReleases) < 100 {
			return releases, nil
		}
	}
}

func currentExecutablePath() (string, error) {
	cmdPath, err := os.Executable()
	if err != nil {
		return "", err
	}
	if runtime.GOOS == "windows" && !strings.HasSuffix(cmdPath, ".exe") {
		cmdPath += ".exe"
	}

	stat, err := os.Lstat(cmdPath)
	if err != nil {
		return "", fmt.Errorf("failed to stat %q: %w", cmdPath, err)
	}
	if stat.Mode()&os.ModeSymlink != 0 {
		resolved, err := filepath.EvalSymlinks(cmdPath)
		if err != nil {
			return "", fmt.Errorf("failed to resolve symlink %q for executable: %w", cmdPath, err)
		}
		cmdPath = resolved
	}

	return cmdPath, nil
}

// updateHTTPClient always verifies certificates: it builds its own transport, so
// ignore_unsafe_cert, which only concerns the connection to the server, cannot
// reach update traffic. HTTPS is also required across redirects.
func updateHTTPClient() *http.Client {
	return &http.Client{
		Timeout: 5 * time.Minute,
		Transport: &http.Transport{
			Proxy:               http.ProxyFromEnvironment,
			DialContext:         dnsresolver.GetDialContextWithPreference(30*time.Second, ""),
			TLSClientConfig:     &tls.Config{MinVersion: tls.VersionTLS12},
			TLSHandshakeTimeout: 15 * time.Second,
			ForceAttemptHTTP2:   true,
		},
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if req.URL.Scheme != "https" {
				return fmt.Errorf("refusing non-HTTPS redirect to %s", req.URL.Host)
			}
			if len(via) >= 10 {
				return errors.New("too many redirects")
			}
			return nil
		},
	}
}

func downloadAsset(assetURL string, limit int64) ([]byte, error) {
	return downloadWithClient(updateHTTPClient(), assetURL, limit)
}

func downloadWithClient(client *http.Client, assetURL string, limit int64) ([]byte, error) {
	parsed, err := url.Parse(assetURL)
	if err != nil {
		return nil, fmt.Errorf("invalid asset URL: %w", err)
	}
	if parsed.Scheme != "https" {
		return nil, fmt.Errorf("refusing non-HTTPS asset URL for %s", parsed.Host)
	}
	req, err := http.NewRequest(http.MethodGet, assetURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/octet-stream")
	req.Header.Set("User-Agent", "shellbeam-monitoring-agent")
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("asset download returned status %d", resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, fmt.Errorf("asset exceeds %d bytes", limit)
	}
	return data, nil
}

func installBinary(payload []byte, cmdPath string) error {
	info, err := os.Stat(cmdPath)
	if err != nil {
		return err
	}
	err = goupdate.Apply(bytes.NewReader(payload), goupdate.Options{TargetPath: cmdPath, TargetMode: info.Mode().Perm()})
	if rollbackErr := goupdate.RollbackError(err); rollbackErr != nil {
		return fmt.Errorf("update failed and rollback failed: %v (rollback: %w)", err, rollbackErr)
	}
	return err
}

// installVerifiedRelease is the only path that replaces the binary. The bytes
// that are verified are the bytes that are installed; nothing is reread.
func installVerifiedRelease(source updateSource, release githubRelease, assetName string) error {
	publicKey, err := parseUpdatePublicKey(source.publicKey)
	if err != nil {
		return err
	}
	asset, ok := findReleaseAsset(release, assetName)
	if !ok {
		return fmt.Errorf("release %s has no asset %s", release.TagName, assetName)
	}
	signatureAsset, ok := findReleaseAsset(release, assetName+signatureSuffix)
	if !ok {
		return fmt.Errorf("refusing update to %s: %w: %s", release.TagName, ErrUpdateSignature, "signature asset is missing")
	}
	signature, err := source.download(signatureAsset.BrowserDownloadURL, maxSignatureBytes)
	if err != nil {
		return fmt.Errorf("failed to download signature for %s: %w", release.TagName, err)
	}
	payload, err := source.download(asset.BrowserDownloadURL, maxAssetBytes)
	if err != nil {
		return fmt.Errorf("failed to download %s: %w", release.TagName, err)
	}
	if err := verifyReleaseAsset(publicKey, release.TagName, assetName, payload, signature); err != nil {
		return fmt.Errorf("refusing update to %s: %w", release.TagName, err)
	}
	cmdPath, err := source.execPath()
	if err != nil {
		return fmt.Errorf("failed to resolve current executable path: %w", err)
	}
	log.Printf("Signature verified; updating %s to %s\n", cmdPath, release.TagName)
	if err := source.install(payload, cmdPath); err != nil {
		return fmt.Errorf("failed to update to %s: %w", release.TagName, err)
	}
	return nil
}

func DoUpdateWorks(onRestartRequired func()) {
	ticker_ := time.NewTicker(time.Duration(6) * time.Hour)
	defer ticker_.Stop()
	runScheduledUpdates(ticker_.C, CheckAndUpdate, onRestartRequired)
}

func runScheduledUpdates(ticks <-chan time.Time, check func() error, onRestartRequired func()) {
	for range ticks {
		err := check()
		if errors.Is(err, ErrRestartRequired) {
			if onRestartRequired != nil {
				onRestartRequired()
			}
			return
		}
		if err != nil {
			log.Println("[ERROR]", err)
		}
	}
}

func checkAndUpdateStable(currentSemVer semver.Version, source updateSource) error {
	owner, repo, err := splitRepoSlug(Repo)
	if err != nil {
		return err
	}

	releases, err := source.listRelease(owner, repo)
	if err != nil {
		return fmt.Errorf("failed to check for updates: %w", err)
	}

	assetName := expectedAssetName(runtime.GOOS, runtime.GOARCH)
	latest, latestVersion, found := selectLatestStableRelease(releases, assetName)
	if !found || !needUpdate(currentSemVer, latestVersion) {
		log.Println("Current version is the latest:", CurrentVersion)
		return nil
	}

	if err := installVerifiedRelease(source, latest, assetName); err != nil {
		return err
	}
	// Default is installed as a service, so don't automatically restart
	//execPath, err := os.Executable()
	//if err != nil {
	//	return fmt.Errorf("failed to get current executable path: %v", err)
	//}

	// _, err = os.StartProcess(execPath, os.Args, &os.ProcAttr{
	// 	Files: []*os.File{os.Stdin, os.Stdout, os.Stderr},
	// })
	// if err != nil {
	// 	return fmt.Errorf("failed to restart program: %v", err)
	// }
	log.Printf("Successfully updated to version %s\n", latestVersion)
	return ErrRestartRequired
}

func checkAndUpdateSnapshot(source updateSource, isContainer containerCheck) error {
	if isContainer() {
		log.Println("Snapshot agent is running in a container; skip binary self-update. Refresh the ghcr.io image tagged 'snapshot' instead.")
		return nil
	}

	owner, repo, err := splitRepoSlug(Repo)
	if err != nil {
		return err
	}

	releases, err := source.listRelease(owner, repo)
	if err != nil {
		return err
	}

	assetName := expectedAssetName(runtime.GOOS, runtime.GOARCH)
	latest, found := selectLatestSnapshotRelease(releases, assetName)
	if !found {
		log.Printf("No suitable snapshot release asset was found for %s. Current snapshot is considered up-to-date.", assetName)
		return nil
	}

	if !snapshotNeedsUpdate(CurrentVersion, latest) {
		log.Println("Current snapshot version is the latest:", CurrentVersion)
		return nil
	}

	log.Printf("Will update from snapshot %s to %s\n", CurrentVersion, latest.TagName)
	release := githubRelease{TagName: latest.TagName, Assets: []githubReleaseAsset{latest.Asset}}
	if signature, ok := latest.signature(); ok {
		release.Assets = append(release.Assets, signature)
	}
	if err := installVerifiedRelease(source, release, assetName); err != nil {
		return err
	}

	log.Printf("Successfully updated to snapshot version %s\n", latest.TagName)
	return ErrRestartRequired
}

// 检查更新并执行自动更新
func CheckAndUpdate() error {
	if isContainerAgent() {
		log.Println("Container agent: update the ShellBeam Monitoring Agent image instead of the binary.")
		return nil
	}
	// Without a trusted key nothing can be verified, so do not even look.
	if _, err := parseUpdatePublicKey(UpdatePublicKey); err != nil {
		return fmt.Errorf("refusing to update: %w", err)
	}
	log.Println("Checking update...")

	source := updateSource{
		publicKey:   UpdatePublicKey,
		listRelease: listGitHubReleases,
		download:    downloadAsset,
		install:     installBinary,
		execPath:    currentExecutablePath,
	}

	if detectBuildTrack(CurrentVersion) == snapshotTrack {
		return checkAndUpdateSnapshot(source, isContainerAgent)
	}

	currentSemVer, err := parseVersion(CurrentVersion)
	if err != nil {
		return fmt.Errorf("failed to parse current version: %v", err)
	}

	return checkAndUpdateStable(currentSemVer, source)
}
