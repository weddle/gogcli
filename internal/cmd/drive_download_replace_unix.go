//go:build !windows

package cmd

import "os"

func replaceBoundedDriveDownload(tempPath, destPath string) error {
	return os.Rename(tempPath, destPath)
}
