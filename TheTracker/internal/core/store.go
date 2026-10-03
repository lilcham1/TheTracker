// Package core is TheTracker's backend: the live GSI tracker, local
// persistence, and the clients for the public APIs the app reads. Nothing in
// here knows about windows or the tray — the desktop shell in main.go and the
// HTTP layer in internal/api sit on top of it — so all of it can be tested
// without a screen.
package core

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// Version is the running app's version. Overridden at build time with
// -ldflags "-X thetracker/internal/core.Version=…".
var Version = "1.0.0"

// Store owns every file the app writes. All paths hang off Dir, so a test or
// a smoke run points it at a throwaway folder and can never touch real data.
type Store struct {
	Dir string

	// One writer at a time for history.json. The GSI goroutine, the backfill
	// loop and the UI all read-modify-write it; unguarded, two of them could
	// interleave and one update would be lost.
	historyMu sync.Mutex
}

// DefaultDataDir is where data has always lived, so an install upgraded from
// the Tauri build keeps its history, settings and linked accounts.
// THETRACKER_LOG_DIR overrides it.
func DefaultDataDir() string {
	for _, v := range []string{"THETRACKER_LOG_DIR", "DOTA_TRACKER_LOG_DIR"} {
		if d := os.Getenv(v); d != "" {
			return d
		}
	}
	base, err := os.UserConfigDir() // %APPDATA% on Windows
	if err != nil {
		base = os.TempDir()
	}
	return filepath.Join(base, "TheTracker", "logs")
}

func NewStore(dir string) *Store {
	_ = os.MkdirAll(dir, 0o755)
	return &Store{Dir: dir}
}

func (s *Store) path(name string) string { return filepath.Join(s.Dir, name) }

// readJSON fills v from a file, leaving v untouched if the file is missing or
// unreadable — callers pass a value already holding the defaults.
func (s *Store) readJSON(name string, v any) bool {
	raw, err := os.ReadFile(s.path(name))
	if err != nil {
		return false
	}
	return json.Unmarshal(raw, v) == nil
}

// writeJSON writes via a temporary file and a rename. A crash or a full disk
// mid-write used to be able to leave history.json truncated, which then read
// as an empty history; a rename either happens or it does not.
func (s *Store) writeJSON(name string, v any) error {
	raw, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	return s.writeFile(name, raw)
}

func (s *Store) writeFile(name string, raw []byte) error {
	full := s.path(name)
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		return err
	}
	tmp := full + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, full)
}

// ---------- History ----------

func (s *Store) LoadHistory() []MatchSummary {
	out := []MatchSummary{}
	s.readJSON("history.json", &out)
	return out
}

func (s *Store) SaveHistory(h []MatchSummary) error {
	if h == nil {
		h = []MatchSummary{}
	}
	return s.writeJSON("history.json", h)
}

// UpdateHistory runs fn on the current history under the lock and saves the
// result if fn reports a change.
func (s *Store) UpdateHistory(fn func(h []MatchSummary) ([]MatchSummary, bool)) ([]MatchSummary, error) {
	s.historyMu.Lock()
	defer s.historyMu.Unlock()
	h, changed := fn(s.LoadHistory())
	if !changed {
		return h, nil
	}
	return h, s.SaveHistory(h)
}

// ---------- Profile ----------

type Profile struct {
	Username string  `json:"username"`
	Rank     *string `json:"rank"`
	Role     *string `json:"role"`
}

func (s *Store) LoadProfile() Profile {
	var p Profile
	s.readJSON("profile.json", &p)
	return p
}

func (s *Store) SaveProfile(p Profile) error { return s.writeJSON("profile.json", p) }

// ---------- Linked accounts ----------

// Link is the Steam account a game's match history is read for.
type Link struct {
	AccountID   *uint64 `json:"accountId"`
	Personaname *string `json:"personaname"`
	Avatar      *string `json:"avatar"`
}

func linkFile(game string) string {
	if game == "deadlock" {
		return "deadlock.json"
	}
	return "dota_account.json"
}

func (s *Store) LoadLink(game string) Link {
	var l Link
	s.readJSON(linkFile(game), &l)
	return l
}

func (s *Store) SaveLink(game string, l Link) error { return s.writeJSON(linkFile(game), l) }

// ---------- Device id ----------

// DeviceID is a stable anonymous id for this install, used only to let an
// account claim matches this install synced before it signed in.
func (s *Store) DeviceID() string {
	if raw, err := os.ReadFile(s.path("device_id.txt")); err == nil {
		if id := strings.TrimSpace(string(raw)); id != "" {
			return id
		}
	}
	id := newUUID()
	_ = s.writeFile("device_id.txt", []byte(id))
	return id
}

func newUUID() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

func randomToken(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// ---------- Disk cache ----------
//
// API payloads are kept on disk with a wall-clock stamp so a relaunch starts
// warm. ReadCache honours the TTL; ReadCacheStale ignores it, which is what
// lets a page show the last good data, labelled as such, when the API is
// down — instead of an error where the numbers used to be.

type cacheEnvelope struct {
	FetchedAt int64           `json:"fetchedAt"`
	Payload   json.RawMessage `json:"payload"`
}

func (s *Store) readCache(name string) (cacheEnvelope, bool) {
	var env cacheEnvelope
	if !s.readJSON(filepath.Join("cache", name+".json"), &env) || len(env.Payload) == 0 {
		return env, false
	}
	return env, true
}

func (s *Store) ReadCache(name string, ttl time.Duration, v any) bool {
	env, ok := s.readCache(name)
	if !ok {
		return false
	}
	age := time.Now().Unix() - env.FetchedAt
	if age < 0 || age >= int64(ttl.Seconds()) {
		return false
	}
	return json.Unmarshal(env.Payload, v) == nil
}

// ReadCacheStale returns whatever is cached regardless of age, and when it
// was fetched.
func (s *Store) ReadCacheStale(name string, v any) (time.Time, bool) {
	env, ok := s.readCache(name)
	if !ok || json.Unmarshal(env.Payload, v) != nil {
		return time.Time{}, false
	}
	return time.Unix(env.FetchedAt, 0), true
}

func (s *Store) WriteCache(name string, v any) {
	raw, err := json.Marshal(v)
	if err != nil {
		return
	}
	env, _ := json.Marshal(cacheEnvelope{FetchedAt: time.Now().Unix(), Payload: raw})
	_ = s.writeFile(filepath.Join("cache", name+".json"), env)
}

// MigrateLegacyDir copies data from the app's original folder name on first
// run. Copied, never moved: if anything goes wrong the original is untouched.
func (s *Store) MigrateLegacyDir() {
	if _, err := os.Stat(s.path("history.json")); err == nil {
		return
	}
	base, err := os.UserConfigDir()
	if err != nil {
		return
	}
	old := filepath.Join(base, "DotaTracker", "logs")
	entries, err := os.ReadDir(old)
	if err != nil || old == s.Dir {
		return
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		target := s.path(e.Name())
		if _, err := os.Stat(target); err == nil {
			continue
		}
		if raw, err := os.ReadFile(filepath.Join(old, e.Name())); err == nil {
			_ = os.WriteFile(target, raw, 0o644)
		}
	}
}
