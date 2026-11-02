//go:build !windows

package cmd

// restrictFileACL is Windows only; elsewhere the mode bits are the control.
func restrictFileACL(string) error { return nil }
