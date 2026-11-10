//go:build !windows

package cmd

import (
	"os"
	"path/filepath"
	"strings"
)

func unixInstallDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".local", "bin"), nil
}

func isAlreadyInstalledBinary(exePath string) bool {
	if filepath.Base(exePath) != "ocli" {
		return false
	}
	if _, err := os.Stat("/usr/local/bin/ocli"); err == nil {
		return true
	}
	if installDir, err := unixInstallDir(); err == nil {
		if _, err := os.Stat(filepath.Join(installDir, "ocli")); err == nil {
			return true
		}
	}
	return false
}

func isStandaloneLaunch() bool {
	return false
}

func installSelfToOS() (string, error) {
	srcExe, err := os.Executable()
	if err != nil {
		return "", err
	}
	if resolved, err := filepath.EvalSymlinks(srcExe); err == nil {
		srcExe = resolved
	}

	// Prefer /usr/local/bin/ocli if writable without sudo
	usrLocalBin := "/usr/local/bin/ocli"
	if err := copyExecutableFile(srcExe, usrLocalBin); err == nil {
		return usrLocalBin, nil
	}

	installDir, err := unixInstallDir()
	if err != nil {
		return "", err
	}
	dstExe := filepath.Join(installDir, "ocli")
	if err := copyExecutableFile(srcExe, dstExe); err != nil {
		return "", err
	}

	ensureUnixShellPath(installDir)
	return dstExe, nil
}

func ensureUnixShellPath(dir string) {
	if containsPathDir(os.Getenv("PATH"), dir, false) {
		return
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return
	}
	exportLine := "\nexport PATH=\"$HOME/.local/bin:$PATH\"\n"
	for _, rc := range []string{".zshrc", ".bashrc", ".profile"} {
		rcPath := filepath.Join(home, rc)
		data, err := os.ReadFile(rcPath)
		if err != nil {
			continue
		}
		if strings.Contains(string(data), ".local/bin") {
			continue
		}
		f, err := os.OpenFile(rcPath, os.O_APPEND|os.O_WRONLY, 0644)
		if err == nil {
			_, _ = f.WriteString(exportLine)
			_ = f.Close()
		}
	}
}
