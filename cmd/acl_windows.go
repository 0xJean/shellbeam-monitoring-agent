//go:build windows

package cmd

import "golang.org/x/sys/windows"

// privateFileSDDL grants full access to SYSTEM, Administrators and the file's
// owner only, and blocks ACEs inherited from the directory.
const privateFileSDDL = "D:P(A;;FA;;;SY)(A;;FA;;;BA)(A;;FA;;;OW)"

func restrictFileACL(path string) error {
	descriptor, err := windows.SecurityDescriptorFromString(privateFileSDDL)
	if err != nil {
		return err
	}
	dacl, _, err := descriptor.DACL()
	if err != nil {
		return err
	}
	return windows.SetNamedSecurityInfo(path, windows.SE_FILE_OBJECT,
		windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION,
		nil, nil, dacl, nil)
}
