//go:build windows

package cmd

import (
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/sys/windows"
)

func TestSaveAutoDiscoveryConfigSetsPrivateACL(t *testing.T) {
	path := filepath.Join(t.TempDir(), "auto-discovery.json")
	if err := saveAutoDiscoveryConfigTo(path, &AutoDiscoveryConfig{UUID: "u", Token: "t"}); err != nil {
		t.Fatal(err)
	}
	descriptor, err := windows.GetNamedSecurityInfo(path, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION)
	if err != nil {
		t.Fatal(err)
	}
	sddl := descriptor.String()
	if !strings.Contains(sddl, "D:P") {
		t.Fatalf("DACL still inherits: %s", sddl)
	}
	for _, group := range []string{";BU)", ";WD)", ";AU)"} {
		if strings.Contains(sddl, group) {
			t.Fatalf("DACL grants %s: %s", group, sddl)
		}
	}
}
