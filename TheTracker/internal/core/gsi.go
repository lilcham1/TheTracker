package core

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// Dota's Game State Integration: the config file that tells Dota where to
// post, and the local listener it posts to.
//
// GSI needs two things: a .cfg in Dota's gamestate_integration folder, and
// -gamestateintegration in the game's launch options. The app writes the
// first itself on every launch. The second lives in a file Steam rewrites
// from memory on exit, so it is detected and reported, and "Play Dota" in the
// app starts the game with the flag attached.

const (
	gsiCfgName = "gamestate_integration_thetracker.cfg"
	// DefaultGsiPort is where the listener has always been. If something
	// else holds it, the next few ports are tried and the config follows.
	DefaultGsiPort = 3000
	gsiPortTries   = 6
	dotaAppID      = "570"
)

// gsiConfigText is generated rather than shipped as a file, so it can never
// disagree with the port the listener actually bound. The token comes back
// in every payload and proves it was Dota that sent it.
func gsiConfigText(port int, token string) string {
	return fmt.Sprintf(`"Dota 2 Integration Configuration"
{
    "uri"           "http://localhost:%d/"
    "timeout"       "5.0"
    "buffer"        "0.1"
    "throttle"      "0.1"
    "heartbeat"     "30.0"
    "auth"
    {
        "token"         "%s"
    }
    "data"
    {
        "provider"      "1"
        "map"           "1"
        "player"        "1"
        "hero"          "1"
        "abilities"     "0"
        "items"         "1"
    }
}
`, port, token)
}

type GsiStatus struct {
	// Every Dota install found, across every Steam library.
	CfgDirs []string `json:"cfgDirs"`
	// The config is present and matches the port we listen on.
	Installed bool `json:"installed"`
	// A config of ours exists but is out of date.
	Stale bool `json:"stale"`
	// Whether any Steam user here has -gamestateintegration set. Nil when
	// the launch options could not be read at all.
	LaunchOption *bool `json:"launchOption"`
	Port         int   `json:"port"`
	// When the config was first written, as Unix seconds: the point from
	// which every Dota match should have reached the app.
	InstalledAt *int64 `json:"installedAt"`
	// Set when the listener could not start, or had to move port.
	ListenerError  string `json:"listenerError,omitempty"`
	ListenerNotice string `json:"listenerNotice,omitempty"`
}

// Gsi owns the listener and the config file.
type Gsi struct {
	store   *Store
	tracker *Tracker

	mu     sync.Mutex
	port   int
	err    string
	notice string
	server *http.Server

	// Overridable for tests: where Steam libraries are looked for.
	steamRoot func() string
}

func NewGsi(store *Store, tracker *Tracker) *Gsi {
	return &Gsi{store: store, tracker: tracker, port: DefaultGsiPort, steamRoot: SteamPath}
}

// token returns the shared secret written into the config, creating it once.
func (g *Gsi) token() string {
	if raw, err := os.ReadFile(g.store.path("gsi_token.txt")); err == nil {
		if t := strings.TrimSpace(string(raw)); t != "" {
			return t
		}
	}
	t := randomToken(16)
	_ = g.store.writeFile("gsi_token.txt", []byte(t))
	return t
}

// ---------- Listener ----------

// Start binds the listener on loopback and serves in the background. Dota
// posts to localhost, so nothing legitimate ever arrives from another
// machine.
func (g *Gsi) Start() {
	var ln net.Listener
	var err error
	port := DefaultGsiPort
	for i := 0; i < gsiPortTries; i++ {
		ln, err = net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", DefaultGsiPort+i))
		if err == nil {
			port = DefaultGsiPort + i
			break
		}
	}

	g.mu.Lock()
	if err != nil {
		g.err = fmt.Sprintf("Live tracking could not start: ports %d–%d are all in use by other programs. Close whatever is using them and restart TheTracker.",
			DefaultGsiPort, DefaultGsiPort+gsiPortTries-1)
		g.mu.Unlock()
		return
	}
	g.port = port
	if port != DefaultGsiPort {
		g.notice = fmt.Sprintf("Port %d was taken by another program, so TheTracker is listening on %d instead. If Dota is already running, restart it so it picks up the change.",
			DefaultGsiPort, port)
	}
	g.server = &http.Server{Handler: http.HandlerFunc(g.serve), ReadHeaderTimeout: 5 * time.Second}
	srv := g.server
	g.mu.Unlock()

	go func() { _ = srv.Serve(ln) }()
}

func (g *Gsi) Stop() {
	g.mu.Lock()
	srv := g.server
	g.mu.Unlock()
	if srv != nil {
		_ = srv.Close()
	}
}

func (g *Gsi) Port() int {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.port
}

var errRejected = errors.New("rejected")

// Accept validates and applies one posted payload. Split from the HTTP
// handler so the simulator and the tests go through the same checks.
func (g *Gsi) Accept(r *http.Request, body []byte) error {
	// A web page can make the browser POST to localhost. Dota is not a
	// browser and sends none of these headers, so their presence means the
	// request came from a page, and a page has no business posting matches.
	if r.Header.Get("Origin") != "" || r.Header.Get("Sec-Fetch-Site") != "" {
		return errRejected
	}
	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		return err
	}
	// A payload that carries a token must carry ours. One without any is
	// still accepted: a Dota that was already running when the config gained
	// its token keeps posting the old shape until it restarts, and dropping
	// those would silently stop tracking mid-session.
	if auth := sub(payload, "auth"); auth != nil {
		if tok, _ := getStr(auth, "token"); tok != g.token() {
			return errRejected
		}
	}
	g.tracker.HandleUpdate(payload)
	return nil
}

func (g *Gsi) serve(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost || r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	if err := g.Accept(r, body); errors.Is(err, errRejected) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	// Dota retries anything that is not a 2xx, so even an unparseable body
	// gets an ok.
	_, _ = io.WriteString(w, "ok")
}

// ---------- Config file ----------

// steamLibraries lists every Steam library. Games are often on a second
// drive, which only libraryfolders.vdf knows about.
func (g *Gsi) steamLibraries() []string {
	root := g.steamRoot()
	if root == "" {
		return nil
	}
	libs := []string{root}
	raw, err := os.ReadFile(filepath.Join(root, "steamapps", "libraryfolders.vdf"))
	if err != nil {
		return libs
	}
	for _, line := range strings.Split(string(raw), "\n") {
		fields := quoted(strings.TrimSpace(line))
		if len(fields) < 2 || fields[0] != "path" {
			continue
		}
		p := filepath.Clean(strings.ReplaceAll(fields[1], `\\`, `\`))
		if st, err := os.Stat(p); err != nil || !st.IsDir() {
			continue
		}
		dup := false
		for _, l := range libs {
			if strings.EqualFold(filepath.Clean(l), p) {
				dup = true
			}
		}
		if !dup {
			libs = append(libs, p)
		}
	}
	return libs
}

// DotaCfgDirs is every Dota cfg folder on this machine.
func (g *Gsi) DotaCfgDirs() []string {
	dirs := []string{}
	for _, lib := range g.steamLibraries() {
		cfg := filepath.Join(lib, "steamapps", "common", "dota 2 beta", "game", "dota", "cfg")
		if st, err := os.Stat(cfg); err == nil && st.IsDir() {
			dirs = append(dirs, cfg)
		}
	}
	return dirs
}

// launchOptionSet reads -gamestateintegration out of Steam's per-user config.
// Read-only on purpose: Steam rewrites this file from memory when it exits.
func (g *Gsi) launchOptionSet() *bool {
	root := g.steamRoot()
	if root == "" {
		return nil
	}
	entries, err := os.ReadDir(filepath.Join(root, "userdata"))
	if err != nil {
		return nil
	}
	sawAny := false
	for _, e := range entries {
		raw, err := os.ReadFile(filepath.Join(root, "userdata", e.Name(), "config", "localconfig.vdf"))
		if err != nil {
			continue
		}
		sawAny = true
		text := string(raw)
		// Dota is appid 570. The file is large, so look at the window after
		// each occurrence of the id rather than parsing the whole VDF. Every
		// occurrence is checked: the id also appears in unrelated sections
		// before the one that holds the launch options.
		for from := 0; ; {
			at := strings.Index(text[from:], `"570"`)
			if at < 0 {
				break
			}
			at += from
			window := text[at:min(len(text), at+4000)]
			if o := strings.Index(window, `"LaunchOptions"`); o >= 0 {
				if f := quoted(strings.SplitN(window[o:], "\n", 2)[0]); len(f) >= 2 && strings.Contains(f[1], "-gamestateintegration") {
					return ptr(true)
				}
			}
			from = at + 5
		}
	}
	if !sawAny {
		return nil
	}
	return ptr(false)
}

func (g *Gsi) Status() GsiStatus {
	g.mu.Lock()
	port, lerr, notice := g.port, g.err, g.notice
	g.mu.Unlock()

	dirs := g.DotaCfgDirs()
	wanted := strings.TrimSpace(gsiConfigText(port, g.token()))
	s := GsiStatus{CfgDirs: dirs, Port: port, LaunchOption: g.launchOptionSet(), ListenerError: lerr, ListenerNotice: notice}
	for _, dir := range dirs {
		raw, err := os.ReadFile(filepath.Join(dir, "gamestate_integration", gsiCfgName))
		if err != nil {
			continue
		}
		if strings.TrimSpace(strings.ReplaceAll(string(raw), "\r\n", "\n")) != wanted {
			s.Stale = true
			continue
		}
		s.Installed = true
	}
	s.InstalledAt = g.installedAt()
	return s
}

// installedAt is when live tracking was first set up on this PC. Kept in the
// data folder rather than read off the config file's timestamp, because the
// config is rewritten whenever its contents change and that would reset the
// "matches since setup" count.
func (g *Gsi) installedAt() *int64 {
	var v struct {
		At int64 `json:"at"`
	}
	if g.store.readJSON("gsi_installed.json", &v) && v.At > 0 {
		return &v.At
	}
	return nil
}

func (g *Gsi) rememberInstalled(at time.Time) {
	if g.installedAt() == nil {
		_ = g.store.writeJSON("gsi_installed.json", map[string]int64{"at": at.Unix()})
	}
}

// Install writes the config into every Dota install found and returns the
// paths written.
func (g *Gsi) Install() ([]string, error) {
	dirs := g.DotaCfgDirs()
	if len(dirs) == 0 {
		return nil, errors.New("Couldn't find Dota 2. Is it installed through Steam?")
	}
	text := gsiConfigText(g.Port(), g.token())
	written, failures := []string{}, []string{}
	for _, dir := range dirs {
		folder := filepath.Join(dir, "gamestate_integration")
		path := filepath.Join(folder, gsiCfgName)
		// An existing file's timestamp is the best record of when tracking
		// was first set up, so it is noted before being overwritten.
		if st, err := os.Stat(path); err == nil {
			g.rememberInstalled(st.ModTime())
		}
		if err := os.MkdirAll(folder, 0o755); err != nil {
			failures = append(failures, err.Error())
			continue
		}
		if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
			failures = append(failures, err.Error())
			continue
		}
		written = append(written, path)
	}
	if len(written) == 0 {
		return nil, fmt.Errorf("Couldn't write the config. %s", strings.Join(failures, "; "))
	}
	g.rememberInstalled(time.Now())
	return written, nil
}

// Remove deletes the config again, so enabling this is not a one-way door.
func (g *Gsi) Remove() []string {
	removed := []string{}
	for _, dir := range g.DotaCfgDirs() {
		path := filepath.Join(dir, "gamestate_integration", gsiCfgName)
		if os.Remove(path) == nil {
			removed = append(removed, path)
		}
	}
	return removed
}

// EnsureInstalled runs at startup and writes only when something is missing
// or stale, so a normal launch touches no files.
func (g *Gsi) EnsureInstalled() {
	s := g.Status()
	if s.ListenerError != "" || len(s.CfgDirs) == 0 || (s.Installed && !s.Stale) {
		if s.Installed {
			g.adoptInstallTime()
		}
		return
	}
	_, _ = g.Install()
}

// adoptInstallTime records the config's timestamp as the setup time for an
// install that predates the app keeping its own record.
func (g *Gsi) adoptInstallTime() {
	if g.installedAt() != nil {
		return
	}
	for _, dir := range g.DotaCfgDirs() {
		if st, err := os.Stat(filepath.Join(dir, "gamestate_integration", gsiCfgName)); err == nil {
			g.rememberInstalled(st.ModTime())
			return
		}
	}
}

// The original Electron tracker had players install its config under this
// name, pointing at the same port, so every update arrived twice.
const legacyCfgName = "gamestate_integration_lasthits.cfg"

// isLegacyConfig recognises the old file by where it sends data, not by name
// alone: the same name aimed elsewhere belongs to something else.
func isLegacyConfig(text string) bool {
	for _, line := range strings.Split(text, "\n") {
		f := quoted(strings.TrimSpace(line))
		if len(f) >= 2 && f[0] == "uri" {
			uri := strings.TrimRight(f[1], "/")
			return uri == "http://localhost:3000" || uri == "http://127.0.0.1:3000"
		}
	}
	return false
}

func (g *Gsi) RemoveLegacyConfigs() []string {
	removed := []string{}
	for _, dir := range g.DotaCfgDirs() {
		path := filepath.Join(dir, "gamestate_integration", legacyCfgName)
		raw, err := os.ReadFile(path)
		if err == nil && isLegacyConfig(string(raw)) && os.Remove(path) == nil {
			removed = append(removed, path)
		}
	}
	return removed
}

// LaunchDota starts Dota through Steam with the GSI flag attached for that
// session, without touching Steam's own config.
func (g *Gsi) LaunchDota() error {
	root := g.steamRoot()
	if root == "" {
		return errors.New("Couldn't find Steam on this PC.")
	}
	exe := filepath.Join(root, "steam.exe")
	if _, err := os.Stat(exe); err != nil {
		return fmt.Errorf("Steam isn't where it said it was (%s).", exe)
	}
	if err := exec.Command(exe, "-applaunch", dotaAppID, "-gamestateintegration").Start(); err != nil {
		return fmt.Errorf("Couldn't start Dota through Steam: %v", err)
	}
	return nil
}

// ListenerError is the reason the listener is not running, or "". Cheap, for
// callers that poll.
func (g *Gsi) ListenerError() string {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.err
}
