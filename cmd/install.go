package cmd

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

const installWelcomeMessage = `Thank you for using OCLI!
OCLI is now configured to run from anywhere.
Type 'ocli setup' in your terminal from anywhere to get started.

This window will close in 5 seconds...`

var installCmd = &cobra.Command{
	Use:   "install",
	Short: "Install ocli into your system PATH so it can be run from anywhere",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		if _, err := installSelfToOS(); err != nil {
			return err
		}
		fmt.Println("Thank you for using OCLI!")
		fmt.Println("OCLI is now configured to run from anywhere.")
		fmt.Println("Type 'ocli setup' in your terminal from anywhere to get started.")
		return nil
	},
}

func init() {
	rootCmd.AddCommand(installCmd)
}

// maybeAutoInstallAndExit checks if the binary was launched by clicking/double-clicking
// (or executed directly as a downloaded release artifact without arguments).
// If so, it installs ocli into the system PATH, displays the welcome message,
// waits 5 seconds, and exits cleanly.
func maybeAutoInstallAndExit(args []string, sleepDuration time.Duration) bool {
	if len(args) > 1 {
		return false
	}

	exePath, err := os.Executable()
	if err != nil {
		return false
	}

	standalone := isStandaloneLaunch()
	releaseBin := isReleaseBinaryName(exePath)
	notInstalledYet := !isAlreadyInstalledBinary(exePath)

	if !standalone && !releaseBin && !notInstalledYet {
		return false
	}

	_, installErr := installSelfToOS()
	if installErr != nil {
		fmt.Fprintf(os.Stderr, "Warning: could not complete system PATH setup: %v\n\n", installErr)
	}

	fmt.Println(installWelcomeMessage)
	if sleepDuration > 0 {
		time.Sleep(sleepDuration)
	}
	return true
}

// isReleaseBinaryName returns true if the executable file name looks like a downloaded
// release artifact (e.g., "ocli-windows-amd64.exe", "ocli-linux-amd64", "ocli-darwin-arm64")
// rather than the installed "ocli" / "ocli.exe".
func isReleaseBinaryName(exePath string) bool {
	base := strings.ToLower(filepath.Base(exePath))
	if runtime.GOOS == "windows" {
		return base != "ocli.exe" && strings.HasPrefix(base, "ocli")
	}
	return base != "ocli" && strings.HasPrefix(base, "ocli")
}

func copyExecutableFile(src, dst string) error {
	srcClean, err := filepath.Abs(src)
	if err != nil {
		srcClean = filepath.Clean(src)
	}
	dstClean, err := filepath.Abs(dst)
	if err != nil {
		dstClean = filepath.Clean(dst)
	}

	if runtime.GOOS == "windows" {
		if strings.EqualFold(srcClean, dstClean) {
			return nil
		}
	} else if srcClean == dstClean {
		return nil
	}

	if err := os.MkdirAll(filepath.Dir(dstClean), 0755); err != nil {
		return err
	}

	in, err := os.Open(srcClean)
	if err != nil {
		return err
	}
	defer in.Close()

	tmpDst := dstClean + ".tmp"
	out, err := os.OpenFile(tmpDst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0755)
	if err != nil {
		// Fallback to writing directly to dstClean if .tmp creation fails
		out, err = os.OpenFile(dstClean, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0755)
		if err != nil {
			return err
		}
		_, copyErr := io.Copy(out, in)
		closeErr := out.Close()
		if copyErr != nil {
			return copyErr
		}
		return closeErr
	}

	_, copyErr := io.Copy(out, in)
	closeErr := out.Close()
	if copyErr != nil {
		_ = os.Remove(tmpDst)
		return copyErr
	}
	if closeErr != nil {
		_ = os.Remove(tmpDst)
		return closeErr
	}

	_ = os.Chmod(tmpDst, 0755)
	if err := os.Rename(tmpDst, dstClean); err != nil {
		_ = os.Remove(tmpDst)
		return err
	}
	return nil
}

func containsPathDir(pathEnv, targetDir string, caseInsensitive bool) bool {
	sep := string(os.PathListSeparator)
	if caseInsensitive {
		sep = ";"
	}
	normTarget := normalizePathEntry(targetDir, caseInsensitive)
	if normTarget == "" {
		return false
	}

	for _, part := range strings.Split(pathEnv, sep) {
		if normalizePathEntry(part, caseInsensitive) == normTarget {
			return true
		}
	}
	return false
}

func appendToPathList(pathEnv, targetDir string, caseInsensitive bool) (string, bool) {
	if containsPathDir(pathEnv, targetDir, caseInsensitive) {
		return pathEnv, false
	}
	sep := string(os.PathListSeparator)
	if caseInsensitive {
		sep = ";"
	}
	trimmed := strings.TrimRight(strings.TrimSpace(pathEnv), sep)
	if trimmed == "" {
		return targetDir, true
	}
	return trimmed + sep + targetDir, true
}

func normalizePathEntry(p string, caseInsensitive bool) string {
	p = strings.TrimSpace(strings.Trim(p, `"`))
	p = strings.TrimRight(p, `/\`)
	if caseInsensitive {
		p = strings.ToLower(p)
	}
	return p
}
