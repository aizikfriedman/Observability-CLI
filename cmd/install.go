package cmd

import (
	"bufio"
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

const unixPathExportLine = `export PATH="$HOME/.local/bin:$PATH"`

var installCmd = &cobra.Command{
	Use:   "install",
	Short: "Install ocli into your user PATH so it can be run from anywhere",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		dst, err := installSelfToOS(os.Stdin, os.Stdout)
		if err != nil {
			return err
		}
		fmt.Printf("Installed ocli to %s\n", dst)
		fmt.Println("Thank you for using OCLI!")
		fmt.Println("Type 'ocli setup' in your terminal from anywhere to get started.")
		return nil
	},
}

var uninstallCmd = &cobra.Command{
	Use:   "uninstall",
	Short: "Remove ocli binary from your system PATH and clean up PATH configuration",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := uninstallSelfFromOS(os.Stdout); err != nil {
			return err
		}
		fmt.Println("OCLI has been uninstalled from your system.")
		return nil
	},
}

func init() {
	rootCmd.AddCommand(installCmd)
	rootCmd.AddCommand(uninstallCmd)
}

// maybeWindowsDoubleClickInstall performs automatic installation ONLY when the binary
// was double-clicked in Windows Explorer. Running `ocli` without arguments in any
// terminal on Linux, macOS, or Windows will return false and simply print Help (Exit 0).
func maybeWindowsDoubleClickInstall(args []string, sleepDuration time.Duration) bool {
	if len(args) > 1 || !isWindowsDoubleClick() {
		return false
	}

	if _, installErr := installSelfToOS(os.Stdin, os.Stdout); installErr != nil {
		fmt.Fprintf(os.Stderr, "Warning: could not complete system PATH setup: %v\n\n", installErr)
	}

	fmt.Println(installWelcomeMessage)
	if sleepDuration > 0 {
		time.Sleep(sleepDuration)
	}
	return true
}

// promptAndConfigureUnixPath checks if installDir (~/.local/bin) is in pathEnv.
// If not, it asks the user for explicit [Y/n] consent before appending export PATH to shell rc files.
func promptAndConfigureUnixPath(homeDir, installDir, pathEnv string, in io.Reader, out io.Writer) error {
	if containsPathDir(pathEnv, installDir, false) {
		return nil
	}

	fmt.Fprintf(out, "\nDirectory %s is not in your PATH.\n", installDir)
	fmt.Fprintf(out, "Would you like ocli to add '%s' to your shell profile (~/.zshrc / ~/.bashrc / ~/.profile)? [Y/n]: ", unixPathExportLine)

	reader := bufio.NewReader(in)
	line, err := reader.ReadString('\n')
	if err != nil && err != io.EOF {
		return err
	}

	ans := strings.ToLower(strings.TrimSpace(line))
	if ans != "" && ans != "y" && ans != "yes" {
		fmt.Fprintf(out, "Skipped modifying shell profile. Add the following line manually to your shell profile:\n  %s\n", unixPathExportLine)
		return nil
	}

	rcFiles := []string{".zshrc", ".bashrc", ".profile"}
	updatedAny := false
	for _, rc := range rcFiles {
		rcPath := filepath.Join(homeDir, rc)
		data, readErr := os.ReadFile(rcPath)
		if readErr != nil {
			continue
		}
		if strings.Contains(string(data), ".local/bin") {
			updatedAny = true
			continue
		}
		f, openErr := os.OpenFile(rcPath, os.O_APPEND|os.O_WRONLY, 0644)
		if openErr == nil {
			_, _ = f.WriteString("\n" + unixPathExportLine + "\n")
			_ = f.Close()
			fmt.Fprintf(out, "Updated %s\n", rcPath)
			updatedAny = true
		}
	}

	// If none of .zshrc/.bashrc/.profile existed yet, create .profile
	if !updatedAny {
		profilePath := filepath.Join(homeDir, ".profile")
		if writeErr := os.WriteFile(profilePath, []byte(unixPathExportLine+"\n"), 0644); writeErr == nil {
			fmt.Fprintf(out, "Created %s\n", profilePath)
		}
	}
	return nil
}

// cleanUnixShellProfiles removes the ocli PATH export line from ~/.zshrc, ~/.bashrc, and ~/.profile.
func cleanUnixShellProfiles(homeDir string) {
	for _, rc := range []string{".zshrc", ".bashrc", ".profile"} {
		rcPath := filepath.Join(homeDir, rc)
		data, err := os.ReadFile(rcPath)
		if err != nil {
			continue
		}
		content := string(data)
		if !strings.Contains(content, unixPathExportLine) {
			continue
		}
		lines := strings.Split(content, "\n")
		var kept []string
		for _, l := range lines {
			if strings.TrimSpace(l) == unixPathExportLine {
				continue
			}
			kept = append(kept, l)
		}
		_ = os.WriteFile(rcPath, []byte(strings.Join(kept, "\n")), 0644)
	}
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

func removeFromPathList(pathEnv, targetDir string, caseInsensitive bool) (string, bool) {
	sep := string(os.PathListSeparator)
	if caseInsensitive {
		sep = ";"
	}
	normTarget := normalizePathEntry(targetDir, caseInsensitive)
	if normTarget == "" {
		return pathEnv, false
	}

	parts := strings.Split(pathEnv, sep)
	var kept []string
	removed := false
	for _, part := range parts {
		if normalizePathEntry(part, caseInsensitive) == normTarget {
			removed = true
			continue
		}
		if strings.TrimSpace(part) != "" {
			kept = append(kept, part)
		}
	}
	if !removed {
		return pathEnv, false
	}
	return strings.Join(kept, sep), true
}

func normalizePathEntry(p string, caseInsensitive bool) string {
	p = strings.TrimSpace(strings.Trim(p, `"`))
	p = strings.TrimRight(p, `/\`)
	if caseInsensitive {
		p = strings.ToLower(p)
	}
	return p
}
