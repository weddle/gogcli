//go:build windows

package cmd

import "golang.org/x/sys/windows"

func replaceBoundedDriveDownload(tempPath, destPath string) error {
	// Both paths are created in the same directory. Windows.Rename uses
	// MoveFileEx with MOVEFILE_REPLACE_EXISTING, preserving the atomic
	// replacement semantics that os.Rename provides on Unix.
	return windows.Rename(tempPath, destPath)
}
