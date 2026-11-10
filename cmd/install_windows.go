//go:build windows

package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"unsafe"

	"github.com/inconshreveable/mousetrap"
	"golang.org/x/sys/windows/registry"
)

func windowsInstallDir() (string, error) {
	localAppData := os.Getenv("LOCALAPPDATA")
	if localAppData == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		localAppData = filepath.Join(home, "AppData", "Local")
	}
	return filepath.Join(localAppData, "ocli"), nil
}

func isAlreadyInstalledBinary(exePath string) bool {
	installDir, err := windowsInstallDir()
	if err != nil {
		return false
	}
	installedExe := filepath.Join(installDir, "ocli.exe")
	if _, err := os.Stat(installedExe); err != nil {
		return false
	}
	return strings.EqualFold(filepath.Base(exePath), "ocli.exe")
}

// isStandaloneLaunch returns true when the binary was started by double-clicking in
// Windows Explorer or opened directly from a browser/GUI download bar (creating a
// standalone console window with only 1 attached process).
func isStandaloneLaunch() bool {
	if mousetrap.StartedByExplorer() {
		return true
	}
	kernel32 := syscall.NewLazyDLL("kernel32.dll")
	proc := kernel32.NewProc("GetConsoleProcessList")
	var pids [2]uint32
	count, _, _ := proc.Call(uintptr(unsafe.Pointer(&pids[0])), 2)
	return count == 1
}

func installSelfToOS() (string, error) {
	srcExe, err := os.Executable()
	if err != nil {
		return "", err
	}
	if resolved, err := filepath.EvalSymlinks(srcExe); err == nil {
		srcExe = resolved
	}

	installDir, err := windowsInstallDir()
	if err != nil {
		return "", err
	}

	dstExe := filepath.Join(installDir, "ocli.exe")
	if err := copyExecutableFile(srcExe, dstExe); err != nil {
		return "", err
	}

	// Best-effort copy into %LOCALAPPDATA%\Microsoft\WindowsApps\ocli.exe if present,
	// because WindowsApps is already in the default user PATH for open terminals.
	if localAppData := os.Getenv("LOCALAPPDATA"); localAppData != "" {
		winAppsDir := filepath.Join(localAppData, "Microsoft", "WindowsApps")
		if info, statErr := os.Stat(winAppsDir); statErr == nil && info.IsDir() {
			_ = copyExecutableFile(srcExe, filepath.Join(winAppsDir, "ocli.exe"))
		}
	}

	if err := addDirToWindowsUserPath(installDir); err != nil {
		return dstExe, err
	}

	return dstExe, nil
}

func addDirToWindowsUserPath(dir string) error {
	k, err := registry.OpenKey(registry.CURRENT_USER, `Environment`, registry.QUERY_VALUE|registry.SET_VALUE)
	if err != nil {
		return err
	}
	defer k.Close()

	currentPath, valType, err := k.GetStringValue("Path")
	if err != nil && err != registry.ErrNotExist {
		return err
	}

	newPath, updated := appendToPathList(currentPath, dir, true)
	if !updated {
		return nil
	}

	if valType == registry.SZ {
		err = k.SetStringValue("Path", newPath)
	} else {
		err = k.SetExpandStringValue("Path", newPath)
	}
	if err != nil {
		return err
	}

	broadcastEnvironmentChange()
	return nil
}

func broadcastEnvironmentChange() {
	user32 := syscall.NewLazyDLL("user32.dll")
	sendMessageTimeout := user32.NewProc("SendMessageTimeoutW")
	envPtr, err := syscall.UTF16PtrFromString("Environment")
	if err != nil {
		return
	}
	const (
		hwndBroadcast   = 0xffff
		wmSettingChange = 0x001a
		smtoAbortIfHung = 0x0002
	)
	var result uintptr
	_, _, _ = sendMessageTimeout.Call(
		hwndBroadcast,
		wmSettingChange,
		0,
		uintptr(unsafe.Pointer(envPtr)),
		smtoAbortIfHung,
		1000,
		uintptr(unsafe.Pointer(&result)),
	)
}
