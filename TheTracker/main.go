// TheTracker — a desktop match tracker for Dota 2 and Deadlock.
//
// This file is the desktop shell: the main window, the in-game overlay, the
// tray icon and start-with-Windows. Everything the app actually does lives in
// internal/core, reached from the UI through internal/api.
package main

import (
	"context"
	"embed"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
	"unsafe"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"
	"github.com/wailsapp/wails/v3/pkg/services/notifications"

	"thetracker/internal/api"
	"thetracker/internal/core"
)

//go:embed all:frontend
var frontendFS embed.FS

//go:embed build/tray.png
var trayIcon []byte

//go:embed build/appicon.png
var appIcon []byte

const (
	// Passed by the start-with-Windows entry, so a launch at login goes to
	// the tray instead of putting a window in front of whatever the player
	// was about to do.
	minimizedArg = "--minimized"
	// The base overlay size, before the scale setting.
	overlayW, overlayH = 340, 300
	overlayMargin      = 24
)

func main() {
	assets, err := fs.Sub(frontendFS, "frontend")
	if err != nil {
		log.Fatal(err)
	}

	args := os.Args[1:]
	for i, a := range args {
		// Development only: serve the UI and its API on a local port, with
		// no window, so the whole app can be driven from a browser or a
		// script. Never used by the installed app.
		if a == "--serve" && i+1 < len(args) {
			serveHeadless(args[i+1], assets)
			return
		}
	}
	minimized := false
	for _, a := range args {
		minimized = minimized || a == minimizedArg
	}

	sh := &shell{notifier: notifications.New()}
	backend := core.NewApp(core.DefaultDataDir(), sh)

	app := application.New(application.Options{
		Name:        "TheTracker",
		Description: "Match tracker for Dota 2 and Deadlock",
		Icon:        appIcon,
		Assets:      application.AssetOptions{Handler: selfTestHook(api.New(backend, assets)), DisableLogging: true},
		Services:    []application.Service{application.NewService(&notifyService{sh: sh})},
		Windows: application.WindowsOptions{
			// Closing the window is not quitting: the tray keeps the app
			// alive so matches go on being recorded.
			DisableQuitOnLastWindowClosed: true,
			WebviewUserDataPath:           webviewDataDir(),
		},
		// One running copy. A second would fight the first for the live
		// feed's port and both would write history.json; the launch is
		// handed to the running one instead.
		SingleInstance: &application.SingleInstanceOptions{
			UniqueID:               "dev.lilcham1.thetracker",
			OnSecondInstanceLaunch: func(application.SecondInstanceData) { sh.ShowMainWindow() },
		},
		OnShutdown: func() {
			sh.saveWindowState()
			backend.Stop()
		},
	})
	sh.app, sh.backend = app, backend

	// The live listener, config check and background loops. Started after
	// the single-instance check above, so a second launch exits before it
	// ever reaches for the port.
	// THETRACKER_NO_DOTA_SETUP leaves Dota's config alone, for trial runs
	// against a throwaway data folder.
	backend.Start(os.Getenv("THETRACKER_NO_DOTA_SETUP") == "")

	sh.buildMainWindow(minimized)
	sh.buildOverlay()
	sh.buildTray()

	// A launch without a working tray is always shown: hidden with no tray
	// would be unreachable.
	if minimized && !sh.TrayAvailable() {
		sh.ShowMainWindow()
	}

	go sh.selfTest()
	go sh.followGame()

	if err := app.Run(); err != nil {
		log.Fatal(err)
	}
}

// webviewDataDir keeps the browser profile where it has always been, beside
// rather than inside the install folder, so updating never clears it.
func webviewDataDir() string {
	base, err := os.UserCacheDir() // %LOCALAPPDATA%
	if err != nil {
		return ""
	}
	return filepath.Join(base, "dev.lilcham1.thetracker")
}

// ---------- The shell ----------

type shell struct {
	app     *application.App
	backend *core.App
	main    *application.WebviewWindow
	overlay *application.WebviewWindow
	tray    atomic.Bool
	// Set once the app is quitting, so the close handler stops hiding.
	quitting atomic.Bool
	// Desktop notifications, and whether Windows let them be set up.
	notifier *notifications.NotificationService
	notifyOK atomic.Bool
	// Whether the overlay is meant to be up. It is drawn only while Dota
	// (or TheTracker) is in front, so this and IsVisible can differ.
	overlayWanted atomic.Bool
	// Dota's area the overlay was last placed in.
	placedMu sync.Mutex
	placedIn application.Rect
	// The self-test places the overlay itself.
	selfTesting atomic.Bool
}

// notifyService starts the notification service without letting it stop the
// app: on a PC where toasts cannot be registered, TheTracker simply runs
// without them.
type notifyService struct{ sh *shell }

func (n *notifyService) ServiceName() string { return "thetracker/notifications" }

func (n *notifyService) ServiceStartup(ctx context.Context, options application.ServiceOptions) error {
	if err := n.sh.notifier.ServiceStartup(ctx, options); err != nil {
		log.Printf("notifications unavailable: %v", err)
		return nil
	}
	// Clicking a notification opens the app on the page it is about.
	n.sh.notifier.OnNotificationResponse(func(r notifications.NotificationResult) {
		if r.Error != nil {
			return
		}
		view, _ := r.Response.UserInfo["view"].(string)
		n.sh.ShowMainWindow()
		if viewName.MatchString(view) && n.sh.main != nil {
			n.sh.main.ExecJS("go(\"" + view + "\")")
		}
	})
	n.sh.notifyOK.Store(true)
	return nil
}

func (n *notifyService) ServiceShutdown() error {
	if n.sh.notifyOK.Load() {
		return n.sh.notifier.ServiceShutdown()
	}
	return nil
}

var viewName = regexp.MustCompile("^[a-z][a-z-]{0,30}$")

func (s *shell) Notify(n core.Notification) {
	if !s.notifyOK.Load() {
		return
	}
	err := s.notifier.SendNotification(notifications.NotificationOptions{
		ID: fmt.Sprintf("tt-%d", time.Now().UnixNano()), Title: n.Title, Body: n.Body,
		Data: map[string]interface{}{"view": n.View},
	})
	if err != nil {
		log.Printf("notification not shown: %v", err)
	}
}

func (s *shell) buildMainWindow(hidden bool) {
	chrome := application.NewRGBPtr(12, 14, 19) // --bg in style.css
	opts := application.WebviewWindowOptions{
		Name: "main", Title: "TheTracker", URL: "/",
		Width: 1240, Height: 800, MinWidth: 960, MinHeight: 620,
		Hidden: hidden,
		// The page's own background, so there is no white flash while the
		// webview starts.
		BackgroundColour: application.NewRGB(12, 14, 19),
		// The title bar and window border take the page's own colour, so the
		// minimise, maximise and close buttons sit in the app rather than in
		// a system-coloured strip above it, and Windows' accent-coloured
		// border does not frame it. The title text is drawn in the same
		// colour: the name is already in the sidebar.
		Windows: application.WindowsWindow{
			Theme:       application.Dark,
			DisableIcon: true,
			CustomTheme: application.ThemeSettings{
				DarkModeActive:   &application.WindowTheme{BorderColour: chrome, TitleBarColour: chrome, TitleTextColour: chrome},
				DarkModeInactive: &application.WindowTheme{BorderColour: chrome, TitleBarColour: chrome, TitleTextColour: chrome},
			},
		},
	}
	// Reopen where it was left, if that place still exists. A monitor that
	// has since been unplugged would otherwise leave the window off-screen.
	if ws := s.backend.Store.LoadPrefs().Window; ws.Set && s.onAScreen(ws) {
		opts.InitialPosition = application.WindowXY
		opts.X, opts.Y, opts.Width, opts.Height = ws.X, ws.Y, ws.Width, ws.Height
	}
	s.main = s.app.Window.NewWithOptions(opts)

	// Closing either hides to the tray or quits, explicitly.
	s.main.RegisterHook(events.Common.WindowClosing, func(e *application.WindowEvent) {
		s.saveWindowState()
		if s.quitting.Load() {
			return
		}
		if s.TrayAvailable() && s.backend.Store.LoadPrefs().General.CloseToTray {
			s.main.Hide()
			e.Cancel()
			return
		}
		s.Quit()
	})
}

func (s *shell) onAScreen(ws core.WindowState) bool {
	if ws.Width < 400 || ws.Height < 300 {
		return false
	}
	for _, sc := range s.app.Screen.GetAll() {
		b := sc.Bounds
		// Enough of the title bar on this screen to grab.
		if ws.X+120 < b.X+b.Width && ws.X+ws.Width-120 > b.X && ws.Y >= b.Y-8 && ws.Y+40 < b.Y+b.Height {
			return true
		}
	}
	return false
}

func (s *shell) saveWindowState() {
	if s.main == nil || !s.main.IsVisible() || s.main.IsMinimised() || s.main.IsMaximised() || s.main.IsFullscreen() {
		return
	}
	b := s.main.Bounds()
	if b.Width < 400 || b.Height < 300 {
		return
	}
	s.backend.Store.UpdatePrefs(func(p *core.Prefs) {
		p.Window = core.WindowState{X: b.X, Y: b.Y, Width: b.Width, Height: b.Height, Set: true}
	})
}

// buildOverlay creates the overlay once, hidden, and from then on only
// toggles it. Creating it on demand raced and could leave two stacked
// always-on-top windows over the game.
func (s *shell) buildOverlay() {
	s.overlay = s.app.Window.NewWithOptions(application.WebviewWindowOptions{
		Name: "overlay", Title: "TheTracker Overlay", URL: "/overlay.html",
		Width: overlayW, Height: overlayH,
		InitialPosition: application.WindowXY, X: overlayMargin, Y: overlayMargin,
		Hidden: true, Frameless: true, AlwaysOnTop: true, DisableResize: true,
		BackgroundType: application.BackgroundTypeTransparent,
		// Click-through is applied in ApplyOverlay, before every show, not
		// here. Asking for it at creation makes the framework build a plain
		// layered window, which cannot be see-through: the overlay came up as
		// a solid white box. Created without it, the window is composited
		// with real transparency, and click-through is then added on top.
		Windows: application.WindowsWindow{
			HiddenOnTaskbar:                   true,
			DisableFramelessWindowDecorations: true,
		},
	})
}

func (s *shell) buildTray() {
	// Never fatal: a missing tray costs the app its background mode, not its
	// ability to start. Without one, closing the window quits.
	defer func() {
		if r := recover(); r != nil {
			s.tray.Store(false)
		}
	}()
	tray := s.app.SystemTray.New()
	tray.SetIcon(trayIcon)
	tray.SetTooltip("TheTracker — tracking in the background")
	menu := s.app.NewMenu()
	menu.Add("Open TheTracker").OnClick(func(*application.Context) { s.ShowMainWindow() })
	menu.AddSeparator()
	menu.Add("Quit TheTracker").OnClick(func(*application.Context) { s.Quit() })
	tray.SetMenu(menu)
	// Left click opens the app; the menu is on right click, the way every
	// other tray app on Windows behaves.
	tray.OnClick(func() { s.ShowMainWindow() })
	s.tray.Store(true)
}

func (s *shell) ShowMainWindow() {
	if s.main == nil {
		return
	}
	if s.main.IsMinimised() {
		s.main.UnMinimise()
	}
	s.main.Show()
	s.main.Focus()
}

func (s *shell) Quit() {
	s.quitting.Store(true)
	s.app.Quit()
}

func (s *shell) TrayAvailable() bool { return s.tray.Load() }

// ---------- Overlay ----------

func (s *shell) ShowOverlay() error {
	if s.overlay == nil {
		return errors.New("The overlay window is unavailable.")
	}
	s.overlayWanted.Store(true)
	// Positioned before showing, so it never flashes in the wrong corner or
	// on the wrong monitor on the way to the right one.
	s.ApplyOverlay(s.backend.Store.LoadPrefs().Overlay)
	if g, ok := findGame(); ok && !s.selfTesting.Load() && (g.minimized || !inFront(g.pid)) {
		return nil // followGame shows it once Dota is in front
	}
	s.overlay.Show()
	s.overlay.SetAlwaysOnTop(true)
	return nil
}

func (s *shell) HideOverlay() {
	s.overlayWanted.Store(false)
	if s.overlay != nil {
		s.overlay.Hide()
	}
}

// OverlayVisible says whether the overlay is on, even while it is tucked
// away because another window is in front of the game.
func (s *shell) OverlayVisible() bool { return s.overlay != nil && s.overlayWanted.Load() }

// followGame keeps the overlay on Dota's window: placed inside it wherever
// it is, and drawn only while the game (or TheTracker itself) is in front,
// so it never floats over a browser or another app. Without Dota running
// the overlay shows on the monitor from Settings, as before.
func (s *shell) followGame() {
	for range time.Tick(400 * time.Millisecond) {
		if s.overlay == nil || !s.overlayWanted.Load() || s.selfTesting.Load() {
			continue
		}
		g, ok := findGame()
		show := !ok || (!g.minimized && inFront(g.pid))
		if ok && show {
			s.placedMu.Lock()
			moved := g.area != s.placedIn
			s.placedMu.Unlock()
			if moved {
				s.ApplyOverlay(s.backend.Store.LoadPrefs().Overlay)
			}
		}
		drawn := s.overlay.IsVisible()
		if show && !drawn {
			s.overlay.Show()
			s.overlay.SetAlwaysOnTop(true)
		} else if !show && drawn {
			s.overlay.Hide()
		}
	}
}

// scaleAt is the scale factor of the monitor a physical rectangle is on.
func (s *shell) scaleAt(r application.Rect) float64 {
	cx, cy := r.X+r.Width/2, r.Y+r.Height/2
	for _, sc := range s.app.Screen.GetAll() {
		b := sc.PhysicalBounds
		if cx >= b.X && cx < b.X+b.Width && cy >= b.Y && cy < b.Y+b.Height && sc.ScaleFactor > 0 {
			return float64(sc.ScaleFactor)
		}
	}
	return 1
}

// screenFor picks the overlay's display: the one chosen in settings, else
// the one the main window is on, else the primary.
func (s *shell) screenFor(name string) *application.Screen {
	screens := s.app.Screen.GetAll()
	if name != "" {
		for _, sc := range screens {
			if sc.Name == name {
				return sc
			}
		}
	}
	if s.main != nil {
		if sc, err := s.main.GetScreen(); err == nil && sc != nil {
			return sc
		}
	}
	return s.app.Screen.GetPrimary()
}

func (s *shell) ApplyOverlay(o core.OverlaySettings) {
	if s.overlay == nil {
		return
	}
	s.overlay.SetIgnoreMouseEvents(o.ClickThrough)

	// On Dota's window when the game is open, wherever it is.
	if g, ok := findGame(); ok && !g.minimized && !s.selfTesting.Load() {
		s.overlay.SetPhysicalBounds(placeOverlay(g.area, s.scaleAt(g.area), o))
		s.placedMu.Lock()
		s.placedIn = g.area
		s.placedMu.Unlock()
		return
	}
	sc := s.screenFor(o.Monitor)
	if sc == nil {
		return
	}
	// Screen bounds are in the coordinates of the whole desktop, so the
	// screen's own origin is part of every position; without it the window
	// lands on the primary display whichever one was chosen.
	s.overlay.SetPhysicalBounds(placeOverlay(sc.PhysicalBounds, float64(sc.ScaleFactor), o))
}

func (s *shell) Monitors() []core.Monitor {
	out := []core.Monitor{}
	for _, sc := range s.app.Screen.GetAll() {
		out = append(out, core.Monitor{Name: sc.Name, Width: sc.PhysicalBounds.Width, Height: sc.PhysicalBounds.Height, Primary: sc.IsPrimary})
	}
	return out
}

// ---------- Start with Windows ----------

func (s *shell) AutostartEnabled() bool {
	on, err := s.app.Autostart.IsEnabled()
	return err == nil && on
}

func (s *shell) SetAutostart(on bool) error {
	if !on {
		return s.app.Autostart.Disable()
	}
	return s.app.Autostart.EnableWithOptions(application.AutostartOptions{
		Identifier: "TheTracker",
		Arguments:  []string{minimizedArg},
	})
}

// ---------- Updates ----------

// InstallUpdate hands over to the installer and quits. /P runs it without
// its wizard and /R makes it reopen the app when it is done.
func (s *shell) InstallUpdate(installer string, quiet bool) error {
	args := []string{"/P", "/R"}
	if quiet {
		args = append(args, "/M")
	}
	cmd := exec.Command(installer, args...)
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("Couldn't start the installer: %v", err)
	}
	_ = cmd.Process.Release()
	s.Quit()
	return nil
}

// ---------- Headless development server ----------

func serveHeadless(addr string, assets fs.FS) {
	host, _, err := net.SplitHostPort(addr)
	if err != nil || (host != "127.0.0.1" && host != "localhost") {
		log.Fatal("--serve only binds loopback, e.g. --serve 127.0.0.1:8765")
	}
	// Serve the UI straight from disk when asked, so an edit shows on reload.
	if dir := os.Getenv("THETRACKER_FRONTEND_DIR"); dir != "" {
		assets = os.DirFS(dir)
	}
	backend := core.NewApp(core.DefaultDataDir(), &core.NoShell{})
	backend.Start(false)
	handler := api.New(backend, assets)
	guarded := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// The API can sign in and restore backups; a page on some other
		// site must not be able to reach it through the browser.
		if site := r.Header.Get("Sec-Fetch-Site"); site != "" && site != "same-origin" && site != "none" {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		if origin := r.Header.Get("Origin"); origin != "" && origin != "http://"+r.Host {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		handler.ServeHTTP(w, r)
	})
	log.Printf("TheTracker %s serving on http://%s (data: %s)", core.Version, addr, backend.Store.Dir)
	log.Fatal(http.ListenAndServe(addr, guarded))
}

// ---------- Self-test ----------
//
// With THETRACKER_SELFTEST=<file>, the app checks itself from the inside a
// few seconds after starting — that the real window loaded the real UI, that
// every page draws, that the overlay window positions itself — writes what
// it found to the file, and quits. It is how a build is verified without
// anyone looking at the screen.

func selfTestHook(next http.Handler) http.Handler {
	out := os.Getenv("THETRACKER_SELFTEST")
	if out == "" {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/selftest_report" {
			body, _ := io.ReadAll(r.Body)
			f, err := os.OpenFile(out, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
			if err == nil {
				_, _ = f.Write(append(body, 0x0a))
				_ = f.Close()
			}
			return
		}
		next.ServeHTTP(w, r)
	})
}

const selfTestPage = `(async () => {
  const wait = (ms) => new Promise((r) => setTimeout(r, ms));
  const report = (o) => fetch("/api/selftest_report", { method: "POST", body: JSON.stringify(o) });
  try {
    const pages = {};
    for (const id of Object.keys(VIEWS)) {
      go(id);
      await wait(1500);
      const text = document.getElementById("page").innerText;
      pages[id] = /failed to draw/.test(text) ? "FAILED" : text.length;
    }
    await report({ window: "main", version: S.boot && S.boot.version, origin: location.origin, pages, live: !!S.live, visible: document.visibilityState });
  } catch (e) {
    await report({ window: "main", error: String(e && e.stack || e) });
  }
})()`

const selfTestOverlay = `fetch("/api/selftest_report", { method: "POST", body: JSON.stringify({ window: "overlay", chips: !!document.getElementById("chips"), lead: settings.leadSeconds, transparent: getComputedStyle(document.body).backgroundColor }) })`

func (s *shell) selfTest() {
	out := os.Getenv("THETRACKER_SELFTEST")
	if out == "" {
		return
	}
	note := func(format string, args ...any) {
		f, err := os.OpenFile(out, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
		if err == nil {
			fmt.Fprintf(f, format+"\n", args...)
			_ = f.Close()
		}
	}
	s.selfTesting.Store(true)
	time.Sleep(6 * time.Second)
	note("shell: tray=%v mainVisible=%v overlayVisible=%v autostart=%v monitors=%d notifications=%v", s.TrayAvailable(), s.main.IsVisible(), s.OverlayVisible(), s.AutostartEnabled(), len(s.Monitors()), s.notifyOK.Load())
	for _, m := range s.Monitors() {
		note("monitor: %q %dx%d primary=%v", m.Name, m.Width, m.Height, m.Primary)
	}
	// The overlay is transparent and empty outside a match, so showing it
	// here puts nothing on screen.
	for _, corner := range []string{"top-left", "bottom-right"} {
		o := s.backend.Store.LoadPrefs().Overlay
		o.Corner = corner
		s.ApplyOverlay(o)
		before := screenPixels(s.overlay.PhysicalBounds())
		err := s.ShowOverlay()
		s.ApplyOverlay(o)
		time.Sleep(1200 * time.Millisecond)
		b := s.overlay.Bounds()
		// Only statistics are kept: how much of the area changed when the
		// empty overlay appeared, and how much of it is pure white. A
		// see-through overlay changes nothing.
		changed, white := comparePixels(before, screenPixels(s.overlay.PhysicalBounds()))
		note("overlay %s: err=%v visible=%v clickThrough=%v bounds=%+v changedByShowing=%.1f%% white=%.1f%%", corner, err, s.OverlayVisible(), s.overlay.IsIgnoreMouseEvents(), b, changed, white)
		s.HideOverlay()
		time.Sleep(300 * time.Millisecond)
	}
	// The same measurement with something deliberately drawn, to prove it
	// would notice: a 100 by 100 square is about a tenth of the overlay.
	if s.ShowOverlay() == nil {
		time.Sleep(600 * time.Millisecond)
		before := screenPixels(s.overlay.PhysicalBounds())
		s.overlay.ExecJS("document.body.insertAdjacentHTML('beforeend', '<div id=\"probe\" style=\"position:fixed;left:0;top:0;width:100px;height:100px;background:#ff00ff\"></div>')")
		time.Sleep(900 * time.Millisecond)
		changed, _ := comparePixels(before, screenPixels(s.overlay.PhysicalBounds()))
		note("overlay with a test square drawn: changed=%.1f%% (expected about 9.8%%)", changed)
		s.overlay.ExecJS("document.getElementById('probe').remove()")
		time.Sleep(300 * time.Millisecond)
		s.HideOverlay()
	}
	note("overlay hidden again: visible=%v", s.OverlayVisible())
	s.overlay.ExecJS(selfTestOverlay)
	s.main.ExecJS(selfTestPage)
	time.Sleep(time.Duration(8+2*18) * time.Second)
	note("done")
	s.Quit()
}

// screenPixels reads the colour of every pixel in a rectangle of the screen.
func screenPixels(r application.Rect) []uint32 {
	if r.Width <= 0 || r.Height <= 0 {
		return nil
	}
	user32, gdi32 := syscall.NewLazyDLL("user32.dll"), syscall.NewLazyDLL("gdi32.dll")
	screen, _, _ := user32.NewProc("GetDC").Call(0)
	defer user32.NewProc("ReleaseDC").Call(0, screen)
	mem, _, _ := gdi32.NewProc("CreateCompatibleDC").Call(screen)
	defer gdi32.NewProc("DeleteDC").Call(mem)
	bmp, _, _ := gdi32.NewProc("CreateCompatibleBitmap").Call(screen, uintptr(r.Width), uintptr(r.Height))
	defer gdi32.NewProc("DeleteObject").Call(bmp)
	gdi32.NewProc("SelectObject").Call(mem, bmp)
	const srcCopy, captureBlt = 0x00CC0020, 0x40000000
	gdi32.NewProc("BitBlt").Call(mem, 0, 0, uintptr(r.Width), uintptr(r.Height), screen, uintptr(r.X), uintptr(r.Y), srcCopy|captureBlt)

	type bitmapInfoHeader struct {
		Size          uint32
		Width, Height int32
		Planes, Bits  uint16
		Compression   uint32
		SizeImage     uint32
		XPels, YPels  int32
		ClrUsed       uint32
		ClrImportant  uint32
	}
	hdr := bitmapInfoHeader{Width: int32(r.Width), Height: -int32(r.Height), Planes: 1, Bits: 32}
	hdr.Size = uint32(unsafe.Sizeof(hdr))
	px := make([]uint32, r.Width*r.Height)
	gdi32.NewProc("GetDIBits").Call(mem, bmp, 0, uintptr(r.Height), uintptr(unsafe.Pointer(&px[0])), uintptr(unsafe.Pointer(&hdr)), 0)
	return px
}

// comparePixels returns the share of pixels that differ, and the share of
// the second capture that is pure white, both as percentages.
func comparePixels(a, b []uint32) (changed, white float64) {
	if len(a) == 0 || len(a) != len(b) {
		return -1, -1
	}
	var c, w int
	for i := range a {
		if a[i]&0xFFFFFF != b[i]&0xFFFFFF {
			c++
		}
		if b[i]&0xFFFFFF == 0xFFFFFF {
			w++
		}
	}
	return float64(c) * 100 / float64(len(a)), float64(w) * 100 / float64(len(a))
}
