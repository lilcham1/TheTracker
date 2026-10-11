package main

// Finding Dota's window, so the overlay sits on the game wherever it is and
// only shows while the game is in front.

import (
	"strings"
	"sync"
	"syscall"
	"time"
	"unsafe"

	"github.com/wailsapp/wails/v3/pkg/application"
	"golang.org/x/sys/windows"
)

var (
	user32                       = syscall.NewLazyDLL("user32.dll")
	procEnumWindows              = user32.NewProc("EnumWindows")
	procGetWindowThreadProcessId = user32.NewProc("GetWindowThreadProcessId")
	procIsWindow                 = user32.NewProc("IsWindow")
	procIsWindowVisible          = user32.NewProc("IsWindowVisible")
	procIsIconic                 = user32.NewProc("IsIconic")
	procGetClientRect            = user32.NewProc("GetClientRect")
	procClientToScreen           = user32.NewProc("ClientToScreen")
	procGetForegroundWindow      = user32.NewProc("GetForegroundWindow")
)

const gameExe = "dota2.exe"

// gameWindow is what is known about Dota's window right now.
type gameWindow struct {
	pid uint32
	// The game's drawing area in screen pixels (a windowed game's title bar
	// left out).
	area      application.Rect
	minimized bool
}

var dota struct {
	mu       sync.Mutex
	hwnd     uintptr
	pid      uint32
	lookedAt time.Time
}

// The EnumWindows callback is made once: Windows callbacks made from Go are
// a limited resource.
var (
	enumMu     sync.Mutex
	enumPID    uint32
	enumBest   uintptr
	enumArea   int64
	enumWindow = syscall.NewCallback(func(hwnd, _ uintptr) uintptr {
		var pid uint32
		procGetWindowThreadProcessId.Call(hwnd, uintptr(unsafe.Pointer(&pid)))
		if pid != enumPID {
			return 1
		}
		if v, _, _ := procIsWindowVisible.Call(hwnd); v == 0 {
			return 1
		}
		var r windows.Rect
		procGetClientRect.Call(hwnd, uintptr(unsafe.Pointer(&r)))
		// The biggest visible window of the process is the game.
		if a := int64(r.Right-r.Left) * int64(r.Bottom-r.Top); a > enumArea || enumBest == 0 {
			enumBest, enumArea = hwnd, a
		}
		return 1
	})
)

func processID(exe string) uint32 {
	snap, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return 0
	}
	defer windows.CloseHandle(snap)
	var e windows.ProcessEntry32
	e.Size = uint32(unsafe.Sizeof(e))
	for err = windows.Process32First(snap, &e); err == nil; err = windows.Process32Next(snap, &e) {
		if strings.EqualFold(windows.UTF16ToString(e.ExeFile[:]), exe) {
			return e.ProcessID
		}
	}
	return 0
}

// What is known about Dota right now.
type gameState int

const (
	gameNone    gameState = iota // not running
	gameLoading                  // running, its window not found (yet)
	gameFound                    // running, window known
)

// findGame returns Dota's window and whether Dota runs at all. A lost window
// is looked for again at most every two seconds; in between, a Dota that was
// running still counts as running, so the overlay doesn't pop up over other
// apps while the game builds its window.
func findGame() (gameWindow, gameState) {
	dota.mu.Lock()
	defer dota.mu.Unlock()
	if dota.hwnd != 0 {
		if ok, _, _ := procIsWindow.Call(dota.hwnd); ok == 0 {
			dota.hwnd = 0
		}
	}
	if dota.hwnd == 0 {
		if time.Since(dota.lookedAt) < 2*time.Second {
			if dota.pid != 0 {
				return gameWindow{pid: dota.pid}, gameLoading
			}
			return gameWindow{}, gameNone
		}
		dota.lookedAt = time.Now()
		pid := processID(gameExe)
		dota.pid = pid
		if pid == 0 {
			return gameWindow{}, gameNone
		}
		enumMu.Lock()
		enumPID, enumBest, enumArea = pid, 0, 0
		procEnumWindows.Call(enumWindow, 0)
		dota.hwnd = enumBest
		enumMu.Unlock()
		if dota.hwnd == 0 {
			return gameWindow{pid: pid}, gameLoading
		}
	}
	g := gameWindow{pid: dota.pid}
	if v, _, _ := procIsIconic.Call(dota.hwnd); v != 0 {
		g.minimized = true
		return g, gameFound
	}
	var r windows.Rect
	procGetClientRect.Call(dota.hwnd, uintptr(unsafe.Pointer(&r)))
	origin := struct{ X, Y int32 }{}
	procClientToScreen.Call(dota.hwnd, uintptr(unsafe.Pointer(&origin)))
	g.area = application.Rect{X: int(origin.X), Y: int(origin.Y), Width: int(r.Right - r.Left), Height: int(r.Bottom - r.Top)}
	if g.area.Width < 200 || g.area.Height < 200 {
		g.minimized = true
	}
	return g, gameFound
}

// inFront reports whether the game is the window in front.
func inFront(gamePID uint32) bool {
	hwnd, _, _ := procGetForegroundWindow.Call()
	if hwnd == 0 {
		return false
	}
	var pid uint32
	procGetWindowThreadProcessId.Call(hwnd, uintptr(unsafe.Pointer(&pid)))
	return pid == gamePID
}
