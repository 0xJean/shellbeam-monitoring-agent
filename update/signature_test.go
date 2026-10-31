package update

import (
	"crypto/ed25519"
	"encoding/base64"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/blang/semver"
	pkg_flags "github.com/komari-monitor/komari-agent/cmd/flags"
)

type testSigner struct {
	public  string
	private ed25519.PrivateKey
}

// The key exists only for this test run.
func newTestSigner(t *testing.T) testSigner {
	t.Helper()
	public, private, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	return testSigner{public: base64.StdEncoding.EncodeToString(public), private: private}
}

func (s testSigner) sign(tag, asset string, payload []byte) []byte {
	return []byte(base64.StdEncoding.EncodeToString(ed25519.Sign(s.private, signedUpdateMessage(tag, asset, payload))) + "\n")
}

type fakeSource struct {
	updateSource
	files     map[string][]byte
	installed []byte
}

func newFakeSource(key string, releases ...githubRelease) *fakeSource {
	f := &fakeSource{files: map[string][]byte{}}
	f.updateSource = updateSource{
		publicKey:   key,
		listRelease: func(string, string) ([]githubRelease, error) { return releases, nil },
		download: func(url string, limit int64) ([]byte, error) {
			data, ok := f.files[url]
			if !ok {
				return nil, errors.New("unexpected download " + url)
			}
			return data, nil
		},
		install: func(payload []byte, _ string) error {
			f.installed = payload
			return nil
		},
		execPath: func() (string, error) { return "/tmp/agent", nil },
	}
	return f
}

func signedRelease(t *testing.T, f *fakeSource, s testSigner, tag string, payload []byte) githubRelease {
	t.Helper()
	asset := expectedAssetName(runtime.GOOS, runtime.GOARCH)
	release := testRelease(tag, false, false, time.Now(), asset, asset+signatureSuffix)
	f.files[release.Assets[0].BrowserDownloadURL] = payload
	f.files[release.Assets[1].BrowserDownloadURL] = s.sign(tag, asset, payload)
	return release
}

func TestStableUpdateInstallsVerifiedAsset(t *testing.T) {
	signer := newTestSigner(t)
	f := newFakeSource(signer.public)
	payload := []byte("new binary")
	release := signedRelease(t, f, signer, "v1.2.4", payload)
	f.listRelease = func(owner, repo string) ([]githubRelease, error) {
		if owner+"/"+repo != Repo {
			t.Fatalf("repo = %s/%s, want %s", owner, repo, Repo)
		}
		return []githubRelease{release}, nil
	}

	err := checkAndUpdateStable(semver.MustParse("1.2.3"), f.updateSource)
	if !errors.Is(err, ErrRestartRequired) {
		t.Fatalf("error = %v, want ErrRestartRequired", err)
	}
	if string(f.installed) != string(payload) {
		t.Fatalf("installed = %q", f.installed)
	}
}

func TestStableUpdateSkipsWhenCurrent(t *testing.T) {
	signer := newTestSigner(t)
	f := newFakeSource(signer.public)
	release := signedRelease(t, f, signer, "v1.2.3", []byte("same"))
	f.listRelease = func(string, string) ([]githubRelease, error) { return []githubRelease{release}, nil }

	if err := checkAndUpdateStable(semver.MustParse("1.2.3"), f.updateSource); err != nil {
		t.Fatal(err)
	}
	if f.installed != nil {
		t.Fatal("installed although already current")
	}
}

func TestUpdateRefusals(t *testing.T) {
	asset := expectedAssetName(runtime.GOOS, runtime.GOARCH)
	signer := newTestSigner(t)
	other := newTestSigner(t)
	payload := []byte("new binary")

	tests := []struct {
		name   string
		key    string
		mutate func(f *fakeSource, release *githubRelease)
		want   error
	}{
		{name: "no key embedded", key: "", want: ErrUpdateKeyMissing},
		{name: "invalid key", key: "not base64!", want: ErrUpdateKeyInvalid},
		{name: "short key", key: base64.StdEncoding.EncodeToString([]byte("short")), want: ErrUpdateKeyInvalid},
		{name: "missing signature", key: signer.public, want: ErrUpdateSignature, mutate: func(f *fakeSource, r *githubRelease) {
			r.Assets = r.Assets[:1]
		}},
		{name: "wrong key", key: other.public, want: ErrUpdateSignature},
		{name: "malformed signature", key: signer.public, want: ErrUpdateSignature, mutate: func(f *fakeSource, r *githubRelease) {
			f.files[r.Assets[1].BrowserDownloadURL] = []byte("garbage")
		}},
		{name: "tampered asset", key: signer.public, want: ErrUpdateSignature, mutate: func(f *fakeSource, r *githubRelease) {
			f.files[r.Assets[0].BrowserDownloadURL] = []byte("tampered")
		}},
		{name: "signature from an older release", key: signer.public, want: ErrUpdateSignature, mutate: func(f *fakeSource, r *githubRelease) {
			f.files[r.Assets[1].BrowserDownloadURL] = signer.sign("v1.0.0", asset, payload)
		}},
		{name: "signature for another platform", key: signer.public, want: ErrUpdateSignature, mutate: func(f *fakeSource, r *githubRelease) {
			f.files[r.Assets[1].BrowserDownloadURL] = signer.sign(r.TagName, "komari-agent-foreign-arch", payload)
		}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newFakeSource(tt.key)
			release := signedRelease(t, f, signer, "v1.2.4", payload)
			if tt.mutate != nil {
				tt.mutate(f, &release)
			}
			f.listRelease = func(string, string) ([]githubRelease, error) { return []githubRelease{release}, nil }

			err := checkAndUpdateStable(semver.MustParse("1.2.3"), f.updateSource)
			if !errors.Is(err, tt.want) {
				t.Fatalf("error = %v, want %v", err, tt.want)
			}
			if f.installed != nil {
				t.Fatal("binary was replaced despite the refusal")
			}
		})
	}
}

func TestSnapshotUpdateRequiresSignature(t *testing.T) {
	oldVersion := CurrentVersion
	CurrentVersion = "Snapshot-2607061100"
	t.Cleanup(func() { CurrentVersion = oldVersion })

	signer := newTestSigner(t)
	asset := expectedAssetName(runtime.GOOS, runtime.GOARCH)
	payload := []byte("snapshot")

	f := newFakeSource(signer.public)
	signed := testRelease("Snapshot-2607061200", true, false, time.Now(), asset, asset+signatureSuffix)
	f.files[signed.Assets[0].BrowserDownloadURL] = payload
	f.files[signed.Assets[1].BrowserDownloadURL] = signer.sign(signed.TagName, asset, payload)
	f.listRelease = func(string, string) ([]githubRelease, error) { return []githubRelease{signed}, nil }
	if err := checkAndUpdateSnapshot(f.updateSource, func() bool { return false }); !errors.Is(err, ErrRestartRequired) {
		t.Fatalf("signed snapshot error = %v", err)
	}

	f = newFakeSource(signer.public)
	unsigned := testRelease("Snapshot-2607061200", true, false, time.Now(), asset)
	f.files[unsigned.Assets[0].BrowserDownloadURL] = payload
	f.listRelease = func(string, string) ([]githubRelease, error) { return []githubRelease{unsigned}, nil }
	if err := checkAndUpdateSnapshot(f.updateSource, func() bool { return false }); !errors.Is(err, ErrUpdateSignature) {
		t.Fatalf("unsigned snapshot error = %v", err)
	}
	if f.installed != nil {
		t.Fatal("unsigned snapshot was installed")
	}
}

func TestCheckAndUpdateRefusesWithoutKeyBeforeAnyRequest(t *testing.T) {
	previous := UpdatePublicKey
	UpdatePublicKey = ""
	t.Cleanup(func() { UpdatePublicKey = previous })
	if isContainerAgent() {
		t.Skip("container agents never self-update")
	}
	if err := CheckAndUpdate(); !errors.Is(err, ErrUpdateKeyMissing) {
		t.Fatalf("error = %v, want ErrUpdateKeyMissing", err)
	}
}

func TestSelectLatestStableRelease(t *testing.T) {
	asset := "komari-agent-linux-amd64"
	now := time.Now()
	releases := []githubRelease{
		testRelease("v1.5.9", false, false, now, asset),
		testRelease("v1.5.20", false, false, now.Add(-time.Hour), asset),
		testRelease("v1.5.30", false, true, now, asset),
		testRelease("v1.5.31", true, false, now, asset),
		testRelease("v1.5.32", false, false, now, "komari-agent-linux-arm64"),
		testRelease("v1.5.33-rc.1", false, false, now, asset),
		testRelease("Snapshot-2607061200", false, false, now, asset),
	}
	got, version, ok := selectLatestStableRelease(releases, asset)
	if !ok || got.TagName != "v1.5.20" || version.String() != "1.5.20" {
		t.Fatalf("selected %q %s %v", got.TagName, version, ok)
	}
}

func TestUpdateClientIgnoresUnsafeCertSetting(t *testing.T) {
	previous := *pkg_flags.GlobalConfig
	pkg_flags.GlobalConfig.IgnoreUnsafeCert = true
	t.Cleanup(func() { *pkg_flags.GlobalConfig = previous })

	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("unverified"))
	}))
	defer server.Close()

	_, err := downloadWithClient(updateHTTPClient(), server.URL, 1024)
	if err == nil {
		t.Fatal("update client accepted a certificate it cannot verify")
	}
	if !strings.Contains(err.Error(), "certificate") {
		t.Fatalf("error = %v, want a certificate failure", err)
	}
}

func TestDownloadRefusesPlaintext(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Error("plaintext URL was requested")
	}))
	defer server.Close()
	if _, err := downloadWithClient(server.Client(), server.URL, 1024); err == nil {
		t.Fatal("plaintext asset URL was accepted")
	}
}

func TestDownloadRefusesPlaintextRedirect(t *testing.T) {
	plain := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Error("redirect target was requested")
	}))
	defer plain.Close()
	secure := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, plain.URL, http.StatusFound)
	}))
	defer secure.Close()

	client := updateHTTPClient()
	client.Transport = secure.Client().Transport
	if _, err := downloadWithClient(client, secure.URL, 1024); err == nil {
		t.Fatal("plaintext redirect was followed")
	}
}

func TestDownloadEnforcesSizeLimit(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(make([]byte, 2048))
	}))
	defer server.Close()
	if _, err := downloadWithClient(server.Client(), server.URL, 1024); err == nil {
		t.Fatal("oversized asset was accepted")
	}
}

func TestInstallBinaryReplacesFileAndKeepsMode(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX permission bits")
	}
	path := filepath.Join(t.TempDir(), "agent")
	writeExecutable(t, path, "old")
	if err := installBinary([]byte("new"), path); err != nil {
		t.Fatal(err)
	}
	assertFile(t, path, "new", 0700)
}
