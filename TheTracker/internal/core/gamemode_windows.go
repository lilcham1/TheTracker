package core

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"unicode/utf16"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	shell32             = windows.NewLazySystemDLL("shell32.dll")
	procShellExecuteExW = shell32.NewProc("ShellExecuteExW")
)

type windowsTasks struct{}

// hidden runs a console program without a window, so nothing flashes over
// the game.
func hidden(name string, args ...string) *exec.Cmd {
	cmd := exec.Command(name, args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: windows.CREATE_NO_WINDOW}
	return cmd
}

func (windowsTasks) UserSID() (string, error) {
	u, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		return "", err
	}
	return u.User.Sid.String(), nil
}

func (windowsTasks) Installed() bool {
	return hidden("schtasks.exe", "/query", "/tn", gameModeOnTask).Run() == nil &&
		hidden("schtasks.exe", "/query", "/tn", gameModeOffTask).Run() == nil
}

func (windowsTasks) Run(task string) error {
	out, err := hidden("schtasks.exe", "/run", "/tn", task).CombinedOutput()
	if err != nil {
		return fmt.Errorf("Windows wouldn't start Game mode's task: %s", strings.TrimSpace(string(out)))
	}
	return nil
}

// writeUTF16 writes a task definition the way schtasks reads it.
func writeUTF16(path, text string) error {
	u := utf16.Encode([]rune(text))
	b := make([]byte, 2, 2+len(u)*2)
	b[0], b[1] = 0xFF, 0xFE
	for _, v := range u {
		b = append(b, byte(v), byte(v>>8))
	}
	return os.WriteFile(path, b, 0o600)
}

func (windowsTasks) Install(onXML, offXML string) error {
	dir, err := os.MkdirTemp("", "thetracker-gamemode-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir)
	on, off := filepath.Join(dir, "on.xml"), filepath.Join(dir, "off.xml")
	if err := writeUTF16(on, onXML); err != nil {
		return err
	}
	if err := writeUTF16(off, offXML); err != nil {
		return err
	}
	return runElevated(`/c schtasks /create /tn "` + gameModeOnTask + `" /xml "` + on + `" /f && schtasks /create /tn "` + gameModeOffTask + `" /xml "` + off + `" /f`)
}

func (windowsTasks) Remove() error {
	return runElevated(`/c schtasks /delete /tn "` + gameModeOnTask + `" /f & schtasks /delete /tn "` + gameModeOffTask + `" /f`)
}

// runElevated runs cmd.exe with the given arguments after Windows' approval
// prompt, hidden, and waits for it.
func runElevated(args string) error {
	type shellExecuteInfo struct {
		size        uint32
		mask        uint32
		hwnd        uintptr
		verb        *uint16
		file        *uint16
		parameters  *uint16
		directory   *uint16
		show        int32
		instApp     uintptr
		idList      uintptr
		class       *uint16
		keyClass    uintptr
		hotKey      uint32
		iconMonitor uintptr
		process     windows.Handle
	}
	const seeMaskNoCloseProcess = 0x40
	const swHide = 0
	verb, _ := windows.UTF16PtrFromString("runas")
	file, _ := windows.UTF16PtrFromString(filepath.Join(os.Getenv("SystemRoot"), "System32", "cmd.exe"))
	params, _ := windows.UTF16PtrFromString(args)
	info := shellExecuteInfo{mask: seeMaskNoCloseProcess, verb: verb, file: file, parameters: params, show: swHide}
	info.size = uint32(unsafe.Sizeof(info))
	if ok, _, err := procShellExecuteExW.Call(uintptr(unsafe.Pointer(&info))); ok == 0 {
		if errors.Is(err, windows.ERROR_CANCELLED) {
			return errors.New("Windows' approval was declined, so nothing was changed.")
		}
		return fmt.Errorf("Couldn't ask Windows for approval: %v", err)
	}
	if info.process == 0 {
		return errors.New("Windows didn't run the setup.")
	}
	defer windows.CloseHandle(info.process)
	if _, err := windows.WaitForSingleObject(info.process, windows.INFINITE); err != nil {
		return err
	}
	var code uint32
	if err := windows.GetExitCodeProcess(info.process, &code); err != nil {
		return err
	}
	if code != 0 {
		return fmt.Errorf("Windows couldn't register Game mode (error %d).", code)
	}
	return nil
}
