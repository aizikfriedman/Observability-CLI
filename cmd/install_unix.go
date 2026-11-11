//go:build !windows

package cmd

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
)

func unixInstallDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".local", "bin"), nil
}

func isWindowsDoubleClick() bool {
	return false
}

func installSelfToOS(in io.Reader, out io.Writer) (string, error) {
	srcExe, err := os.Executable()
	if err != nil {
		return "", err
	}
	if resolved, err := filepath.EvalSymlinks(srcExe); err == nil {
		srcExe = resolved
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}

	installDir := filepath.Join(home, ".local", "bin")
	dstExe := filepath.Join(installDir, "ocli")
	if err := copyExecutableFile(srcExe, dstExe); err != nil {
		return "", err
	}

	if err := promptAndConfigureUnixPath(home, installDir, os.Getenv("PATH"), in, out); err != nil {
		return dstExe, err
	}

	return dstExe, nil
}

func uninstallSelfFromOS(out io.Writer) error {
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}

	installDir := filepath.Join(home, ".local", "bin")
	dstExe := filepath.Join(installDir, "ocli")
	if err := os.Remove(dstExe); err != nil && !os.IsNotExist(err) {
		return err
	} else if err == nil {
		fmt.Fprintf(out, "Removed %s\n", dstExe)
	}

	cleanUnixShellProfiles(home)
	return nil
}
