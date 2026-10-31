package cmd

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestSaveAutoDiscoveryConfigIsPrivateAndAtomic(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX permission bits")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "auto-discovery.json")
	if err := os.WriteFile(path, []byte("{}"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := saveAutoDiscoveryConfigTo(path, &AutoDiscoveryConfig{UUID: "u", Token: "t"}); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0600 {
		t.Fatalf("mode = %o, want 600", info.Mode().Perm())
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("temporary file left behind: %v", entries)
	}
	loaded, err := loadAutoDiscoveryConfigFrom(path)
	if err != nil || loaded == nil || loaded.Token != "t" {
		t.Fatalf("loaded = %+v, err = %v", loaded, err)
	}
}

func TestSaveAutoDiscoveryConfigReplacesSymlinkInsteadOfFollowing(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlinks require privileges")
	}
	dir := t.TempDir()
	target := filepath.Join(dir, "target")
	if err := os.WriteFile(target, []byte("keep"), 0644); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "auto-discovery.json")
	if err := os.Symlink(target, path); err != nil {
		t.Fatal(err)
	}
	if err := saveAutoDiscoveryConfigTo(path, &AutoDiscoveryConfig{Token: "t"}); err != nil {
		t.Fatal(err)
	}
	if data, _ := os.ReadFile(target); string(data) != "keep" {
		t.Fatalf("symlink target was overwritten: %q", data)
	}
}

func TestLoadAutoDiscoveryConfigTightensExistingFile(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX permission bits")
	}
	path := filepath.Join(t.TempDir(), "auto-discovery.json")
	if err := os.WriteFile(path, []byte(`{"uuid":"u","token":"t"}`), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0644); err != nil {
		t.Fatal(err)
	}
	loaded, err := loadAutoDiscoveryConfigFrom(path)
	if err != nil || loaded == nil || loaded.Token != "t" {
		t.Fatalf("loaded = %+v, err = %v", loaded, err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0600 {
		t.Fatalf("mode = %o, want 600", info.Mode().Perm())
	}
}

func TestLoadAutoDiscoveryConfigMissingFile(t *testing.T) {
	loaded, err := loadAutoDiscoveryConfigFrom(filepath.Join(t.TempDir(), "absent.json"))
	if err != nil || loaded != nil {
		t.Fatalf("loaded = %+v, err = %v", loaded, err)
	}
}
