package core

import (
	"archive/zip"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Shell is everything the backend needs from the desktop: windows, the tray,
// start-with-Windows. main.go implements it with real windows; tests and the
// headless server use NoShell. Keeping it behind an interface is what lets
// every other function in the app be exercised without a screen.
type Shell interface {
	ShowOverlay() error
	HideOverlay()
	OverlayVisible() bool
	// ApplyOverlay moves and sizes the overlay to match the settings.
	ApplyOverlay(OverlaySettings)
	Monitors() []Monitor
	TrayAvailable() bool
	AutostartEnabled() bool
	SetAutostart(bool) error
	// InstallUpdate runs the downloaded installer and quits the app.
	InstallUpdate(installerPath string) error
	ShowMainWindow()
	Quit()
}

type Monitor struct {
	Name    string `json:"name"`
	Width   int    `json:"width"`
	Height  int    `json:"height"`
	Primary bool   `json:"primary"`
}

// NoShell is a Shell with no desktop behind it.
type NoShell struct {
	mu        sync.Mutex
	overlay   bool
	autostart bool
}

func (n *NoShell) ShowOverlay() error           { n.mu.Lock(); n.overlay = true; n.mu.Unlock(); return nil }
func (n *NoShell) HideOverlay()                 { n.mu.Lock(); n.overlay = false; n.mu.Unlock() }
func (n *NoShell) OverlayVisible() bool         { n.mu.Lock(); defer n.mu.Unlock(); return n.overlay }
func (n *NoShell) ApplyOverlay(OverlaySettings) {}
func (n *NoShell) Monitors() []Monitor {
	return []Monitor{{Name: "Display 1", Width: 1920, Height: 1080, Primary: true}}
}
func (n *NoShell) TrayAvailable() bool    { return true }
func (n *NoShell) AutostartEnabled() bool { n.mu.Lock(); defer n.mu.Unlock(); return n.autostart }
func (n *NoShell) SetAutostart(on bool) error {
	n.mu.Lock()
	n.autostart = on
	n.mu.Unlock()
	return nil
}
func (n *NoShell) InstallUpdate(string) error {
	return errors.New("Updates can only be installed from the desktop app.")
}
func (n *NoShell) ShowMainWindow() {}
func (n *NoShell) Quit()           {}

// App ties the backend together. One per process.
type App struct {
	Store    *Store
	Tracker  *Tracker
	Gsi      *Gsi
	Dota     *Dota
	Deadlock *Deadlock
	Cloud    *Cloud
	Updater  *Updater
	Shell    Shell

	simMu   sync.Mutex
	simStop chan struct{}

	pendingUpdate string
}

func NewApp(dataDir string, shell Shell) *App {
	store := NewStore(dataDir)
	a := &App{Store: store, Shell: shell}
	a.Tracker = NewTracker(store)
	a.Gsi = NewGsi(store, a.Tracker)
	a.Dota = NewDota(store)
	a.Deadlock = NewDeadlock(store)
	a.Cloud = NewCloud(store)
	a.Updater = NewUpdater()
	a.Tracker.OnSaved = a.Cloud.PushMatch
	return a
}

// Start brings up everything that runs in the background: the live listener,
// the GSI config, the overlay watcher and the game-type backfill.
//
// manageDota is false for the headless development server, which must never
// rewrite the config in a real Dota install to point at a test listener.
func (a *App) Start(manageDota bool) {
	a.Store.MigrateLegacyDir()
	a.Gsi.Start()
	if manageDota {
		a.Gsi.EnsureInstalled()
		a.Gsi.RemoveLegacyConfigs()
	}
	a.Cloud.Restore()
	go a.overlayWatcher()
	go a.backfillLoop()
}

func (a *App) Stop() {
	a.StopSimulation()
	a.Gsi.Stop()
}

// overlayWatcher opens the overlay when a match starts and closes it when
// the match ends. It acts only on transitions, so a player who closes the
// overlay by hand mid-match does not get it forced back open.
func (a *App) overlayWatcher() {
	wasLive := false
	for {
		time.Sleep(2 * time.Second)
		live := a.Tracker.IsLive()
		if live == wasLive {
			continue
		}
		wasLive = live
		if !a.Store.LoadPrefs().Overlay.Auto {
			continue
		}
		if live {
			_ = a.Shell.ShowOverlay()
		} else {
			a.Shell.HideOverlay()
		}
	}
}

// backfillLoop periodically asks OpenDota to name the game types GSI cannot.
// OpenDota takes a few minutes to ingest a finished game, hence a loop
// rather than one lookup. With nothing untagged it never touches the network.
func (a *App) backfillLoop() {
	time.Sleep(45 * time.Second) // keep startup free of network work
	for {
		if resolved, _ := a.Dota.BackfillGameTypes(); len(resolved) > 0 {
			// The cloud row carries the game type too.
			for _, m := range resolved {
				a.Cloud.PushMatch(m)
			}
		}
		time.Sleep(5 * time.Minute)
	}
}

// ---------- Live ----------

type LiveView struct {
	LiveStatus
	// The listener could not start at all.
	ServerError string `json:"serverError,omitempty"`
	Simulating  bool   `json:"simulating"`
	// Progress against the player's goals in the running match.
	Goals []GoalResult `json:"goals"`
}

func (a *App) Live(withLog bool) LiveView {
	v := LiveView{LiveStatus: a.Tracker.Status(withLog), Simulating: a.Simulating(), Goals: []GoalResult{}}
	v.ServerError = a.Gsi.ListenerError()
	if m := v.Current; m != nil {
		g := a.Store.LoadPrefs().Goals
		snap := buildSummary(m, false, time.Now())
		v.Goals = EvaluateGoals(g, &snap)
	}
	return v
}

// ---------- Simulator ----------

func (a *App) Simulating() bool {
	a.simMu.Lock()
	defer a.simMu.Unlock()
	return a.simStop != nil
}

// StartSimulation plays a short fake match through the tracker, so the Live
// page and the overlay can be seen working without launching Dota. The match
// is marked simulated and is never saved or synced.
//
// The clock starts at 3:40 and runs at real speed, one game second per
// second, so every countdown lasts exactly as long as it would in a match:
// the camp pull from 3:45 to 3:52, the bounty and water runes into 4:00, and
// the next pull from 4:45. Run faster, a seven-second warning was gone in
// under two.
func (a *App) StartSimulation(seconds int) error {
	if a.Tracker.IsLive() && !a.Simulating() {
		return errors.New("A real match is running. The simulator would interrupt it.")
	}
	a.StopSimulation()
	seconds = clampI(seconds, 10, 600)

	stop := make(chan struct{})
	a.simMu.Lock()
	a.simStop = stop
	a.simMu.Unlock()

	matchID := "sim" + strconv.FormatInt(time.Now().Unix(), 10)
	send := func(clock float64, state string, alive bool, gold int, winner string) {
		a.Tracker.HandleUpdate(jsonMap{
			SimulatedMarker: true,
			"map":           jsonMap{"matchid": matchID, "clock_time": clock, "game_state": state, "win_team": winner},
			"player": jsonMap{
				"activity": "playing", "team_name": "radiant", "kills": float64(2 + int(clock-220)/20),
				"assists": float64(3 + int(clock-220)/15), "last_hits": 38 + (clock-220)/4, "denies": float64(4 + int(clock)/120),
				"gold": float64(gold), "gpm": 520.0, "xpm": 610.0,
			},
			"hero": jsonMap{"name": "npc_dota_hero_juggernaut", "alive": alive, "level": float64(min(1+int(clock)/75, 30))},
			"items": jsonMap{
				"slot0": jsonMap{"name": "item_phase_boots"},
				"slot1": jsonMap{"name": map[bool]string{true: "item_manta", false: "empty"}[clock > 500]},
			},
		})
	}

	go func() {
		clock := 220.0
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		for i := 0; i < seconds; i++ {
			// One death partway through, so the gold-lost figures move.
			dead := i >= 12 && i < 16
			gold := 2400
			if i >= 12 {
				gold = 1700
			}
			send(clock, stateInProgress, !dead, gold, "none")
			select {
			case <-stop:
				i = seconds
			case <-ticker.C:
			}
			clock++
		}
		send(clock, statePostGame, true, 1700, "radiant")
		a.simMu.Lock()
		if a.simStop == stop {
			a.simStop = nil
		}
		a.simMu.Unlock()
	}()
	return nil
}

func (a *App) StopSimulation() {
	a.simMu.Lock()
	stop := a.simStop
	a.simMu.Unlock()
	if stop != nil {
		select {
		case <-stop:
		default:
			close(stop)
		}
		// Wait for the goroutine to post the end of the match, so the next
		// call sees a settled tracker.
		for i := 0; i < 40 && a.Simulating(); i++ {
			time.Sleep(50 * time.Millisecond)
		}
	}
}

// ---------- Background settings ----------

type BackgroundSettings struct {
	StartWithWindows bool `json:"startWithWindows"`
	CloseToTray      bool `json:"closeToTray"`
	AutostartAsked   bool `json:"autostartAsked"`
	// False if the tray icon could not be created, in which case closing the
	// window quits regardless of the setting.
	TrayAvailable bool `json:"trayAvailable"`
}

func (a *App) Background() BackgroundSettings {
	g := a.Store.LoadPrefs().General
	return BackgroundSettings{
		// The registry is the source of truth: the player can remove the
		// entry from Task Manager, and the app must not go on claiming it.
		StartWithWindows: a.Shell.AutostartEnabled(),
		CloseToTray:      g.CloseToTray,
		AutostartAsked:   g.AutostartAsked,
		TrayAvailable:    a.Shell.TrayAvailable(),
	}
}

func (a *App) SetStartWithWindows(on bool) (BackgroundSettings, error) {
	if err := a.Shell.SetAutostart(on); err != nil {
		return a.Background(), fmt.Errorf("Couldn't change the startup setting: %v", err)
	}
	// Answering either way counts as having been asked.
	a.Store.UpdatePrefs(func(p *Prefs) { p.General.AutostartAsked = true })
	return a.Background(), nil
}

func (a *App) SetCloseToTray(on bool) BackgroundSettings {
	a.Store.UpdatePrefs(func(p *Prefs) { p.General.CloseToTray = on })
	return a.Background()
}

func (a *App) DismissAutostartPrompt() BackgroundSettings {
	a.Store.UpdatePrefs(func(p *Prefs) { p.General.AutostartAsked = true })
	return a.Background()
}

// ---------- Diagnostics ----------

type Check struct {
	Name   string `json:"name"`
	OK     bool   `json:"ok"`
	Detail string `json:"detail"`
	// Milliseconds the check took, for network checks.
	LatencyMs *int64 `json:"latencyMs"`
}

func timed(name, okDetail string, probe func() error) Check {
	start := time.Now()
	err := probe()
	ms := time.Since(start).Milliseconds()
	if err != nil {
		return Check{Name: name, Detail: err.Error(), LatencyMs: &ms}
	}
	return Check{Name: name, OK: true, Detail: okDetail, LatencyMs: &ms}
}

// Diagnostics checks each thing the app depends on and says, in plain words,
// which are working.
func (a *App) Diagnostics() []Check {
	checks := []Check{}

	accounts := DetectSteamAccounts()
	steam := Check{Name: "Steam", OK: len(accounts) > 0, Detail: "No Steam account found on this PC"}
	if steam.OK {
		steam.Detail = fmt.Sprintf("%d account(s) found on this PC", len(accounts))
	}
	checks = append(checks, steam)

	gs := a.Gsi.Status()
	live := a.Tracker.Status(false)
	gsi := Check{Name: "Dota live feed"}
	switch {
	case gs.ListenerError != "":
		gsi.Detail = gs.ListenerError
	case len(gs.CfgDirs) == 0:
		gsi.Detail = "Dota 2 wasn't found in any Steam library"
	case !gs.Installed:
		gsi.Detail = "The config file is missing from Dota's folder"
	case live.GsiAgeSecs != nil && *live.GsiAgeSecs < 45:
		gsi.OK, gsi.Detail = true, fmt.Sprintf("Receiving data from Dota on port %d", gs.Port)
	case live.GsiAgeSecs != nil:
		gsi.OK, gsi.Detail = true, fmt.Sprintf("Set up. Last heard from Dota %s", agoText(*live.GsiAgeSecs))
	case gs.LaunchOption != nil && !*gs.LaunchOption:
		gsi.Detail = "Set up, but Dota's launch option is missing, so it isn't sending anything"
	default:
		gsi.OK, gsi.Detail = true, "Set up. Nothing received yet, which is expected while Dota is closed"
	}
	checks = append(checks, gsi)

	probe := filepath.Join("cache", ".write-test")
	data := Check{Name: "Data folder", Detail: a.Store.Dir}
	if err := a.Store.writeFile(probe, []byte("ok")); err != nil {
		data.Detail = "Can't write to " + a.Store.Dir
	} else {
		_ = os.Remove(a.Store.path(probe))
		data.OK = true
	}
	checks = append(checks, data)

	// The network checks run together; each can take seconds.
	net := make([]Check, 3)
	var wg sync.WaitGroup
	wg.Add(3)
	go func() {
		defer wg.Done()
		net[0] = timed("OpenDota", "Reachable", func() error {
			var v any
			return (&service{Name: "OpenDota", Base: a.Dota.api.Base, Attempts: 1}).get("/constants/game_mode", &v)
		})
	}()
	go func() {
		defer wg.Done()
		net[1] = timed("Deadlock API", "Reachable", func() error {
			var v any
			return (&service{Name: "The Deadlock API", Base: a.Deadlock.api.Base, Attempts: 1}).get("/v1/info", &v)
		})
	}()
	go func() {
		defer wg.Done()
		net[2] = timed("Cloud sync", "Reachable", a.Cloud.Ping)
	}()
	wg.Wait()
	return append(checks, net...)
}

func agoText(secs int64) string {
	switch {
	case secs < 60:
		return "just now"
	case secs < 3600:
		return fmt.Sprintf("%d min ago", secs/60)
	case secs < 86400:
		return fmt.Sprintf("%d h ago", secs/3600)
	}
	return fmt.Sprintf("%d days ago", secs/86400)
}

// ---------- Export and backup ----------

// ExportDir is where exports and backups are written: a folder the player
// can find, not the hidden app-data one.
func ExportDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		home = os.TempDir()
	}
	return filepath.Join(home, "Documents", "TheTracker")
}

func derefI(v *int64) string {
	if v == nil {
		return ""
	}
	return strconv.FormatInt(*v, 10)
}

// ExportSessions writes the session history as CSV (one row per match, for a
// spreadsheet) or JSON (everything) and returns the file's path.
func (a *App) ExportSessions(format, dir string) (string, error) {
	h := a.Store.History()
	if len(h) == 0 {
		return "", errors.New("There are no sessions to export yet.")
	}
	if dir == "" {
		dir = ExportDir()
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("Couldn't create %s: %v", dir, err)
	}
	stamp := time.Now().Format("2006-01-02_150405")

	if format == "json" {
		path := filepath.Join(dir, "sessions_"+stamp+".json")
		raw, _ := json.MarshalIndent(h, "", "  ")
		return path, os.WriteFile(path, raw, 0o644)
	}

	path := filepath.Join(dir, "sessions_"+stamp+".csv")
	f, err := os.Create(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	// A byte-order mark makes Excel read the file as UTF-8.
	_, _ = f.Write([]byte{0xEF, 0xBB, 0xBF})
	w := csv.NewWriter(f)
	_ = w.Write([]string{"date", "match_id", "hero", "game_type", "result", "duration", "kills", "deaths", "assists",
		"last_hits", "denies", "gpm", "xpm", "gold_lost", "lh_5", "lh_10", "lh_15", "lh_20", "lh_25", "incomplete", "notes"})
	for _, m := range h {
		result := ""
		if m.Won != nil {
			result = map[bool]string{true: "win", false: "loss"}[*m.Won]
		}
		hero := ""
		if m.HeroName != nil {
			hero = heroSlug(*m.HeroName)
		}
		row := []string{m.Date, m.MatchID, hero, m.GameType, result, m.Duration, strconv.FormatInt(m.Kills, 10),
			strconv.Itoa(m.TotalDeaths), derefI(m.Assists), derefI(m.LastHits), derefI(m.Denies), derefI(m.GPM), derefI(m.XPM),
			strconv.FormatInt(m.TotalGoldLost, 10)}
		for _, minute := range CheckpointMinutes {
			if cp := m.Checkpoints[minute]; cp != nil {
				row = append(row, strconv.FormatInt(cp.LastHits, 10))
			} else {
				row = append(row, "")
			}
		}
		row = append(row, strconv.FormatBool(m.Incomplete), m.Notes)
		_ = w.Write(row)
	}
	w.Flush()
	return path, w.Error()
}

// backupFiles is what a backup holds: the player's data and settings. Not
// the cache, which is refetched, and not auth.json, which holds a sign-in
// token that has no business sitting in a zip in Documents.
var backupFiles = []string{"history.json", "prefs.json", "profile.json", "dota_account.json", "deadlock.json"}

// Backup zips the player's data into dir and returns the zip's path.
func (a *App) Backup(dir string) (string, error) {
	if dir == "" {
		dir = ExportDir()
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("Couldn't create %s: %v", dir, err)
	}
	path := filepath.Join(dir, "TheTracker-backup_"+time.Now().Format("2006-01-02_150405")+".zip")
	f, err := os.Create(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	zw := zip.NewWriter(f)
	n := 0
	for _, name := range backupFiles {
		raw, err := os.ReadFile(a.Store.path(name))
		if err != nil {
			continue
		}
		w, err := zw.Create(name)
		if err != nil {
			return "", err
		}
		if _, err := w.Write(raw); err != nil {
			return "", err
		}
		n++
	}
	if err := zw.Close(); err != nil {
		return "", err
	}
	if n == 0 {
		f.Close()
		_ = os.Remove(path)
		return "", errors.New("There is nothing to back up yet.")
	}
	return path, nil
}

// LatestBackup is the newest backup zip in the export folder, or "".
func LatestBackup(dir string) string {
	if dir == "" {
		dir = ExportDir()
	}
	matches, _ := filepath.Glob(filepath.Join(dir, "TheTracker-backup_*.zip"))
	latest := ""
	for _, m := range matches {
		if m > latest { // the timestamp in the name sorts lexically
			latest = m
		}
	}
	return latest
}

// Restore replaces the player's data with the contents of a backup zip. The
// current files are themselves backed up first, so a restore can be undone.
func (a *App) Restore(zipPath string) (int, error) {
	zr, err := zip.OpenReader(zipPath)
	if err != nil {
		return 0, errors.New("That file isn't a TheTracker backup.")
	}
	defer zr.Close()

	allowed := map[string]bool{}
	for _, n := range backupFiles {
		allowed[n] = true
	}
	contents := map[string][]byte{}
	for _, f := range zr.File {
		// Only the known file names, taken by exact match: nothing in a zip
		// gets to choose where it is written.
		if !allowed[f.Name] || f.UncompressedSize64 > 64<<20 {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			continue
		}
		raw, err := io.ReadAll(io.LimitReader(rc, 64<<20))
		rc.Close()
		if err != nil || !json.Valid(raw) {
			return 0, fmt.Errorf("%s in that backup is damaged; nothing was restored.", f.Name)
		}
		contents[f.Name] = raw
	}
	if len(contents) == 0 {
		return 0, errors.New("That file isn't a TheTracker backup.")
	}

	if _, err := a.Backup(filepath.Join(a.Store.Dir, "before-restore")); err != nil && !strings.Contains(err.Error(), "nothing to back up") {
		return 0, fmt.Errorf("Couldn't save the current data first, so nothing was restored: %v", err)
	}
	a.Store.historyMu.Lock()
	defer a.Store.historyMu.Unlock()
	for name, raw := range contents {
		if err := a.Store.writeFile(name, raw); err != nil {
			return 0, err
		}
	}
	return len(contents), nil
}

// Reveal opens Explorer with a file selected.
func Reveal(path string) error {
	if _, err := os.Stat(path); err != nil {
		return errors.New("That file no longer exists.")
	}
	return exec.Command("explorer", "/select,"+path).Start()
}

// ---------- Updates ----------

func (a *App) InstallUpdate() error {
	if a.Tracker.IsLive() && !a.Simulating() {
		return errors.New("A match is running. Update once it has finished, so it gets recorded.")
	}
	path, err := a.Updater.Download()
	if err != nil {
		return err
	}
	return a.Shell.InstallUpdate(path)
}

// SetAPIBases points the public-API clients somewhere else. For tests, which
// must never depend on, or load, the real services.
func (a *App) SetAPIBases(dota, deadlock, cloud string) {
	a.Dota.api.Base, a.Deadlock.api.Base, a.Cloud.base = dota, deadlock, cloud
	a.Dota.api.Attempts, a.Deadlock.api.Attempts = 1, 1
	a.Updater.FeedURL = cloud + "/latest.json"
}
