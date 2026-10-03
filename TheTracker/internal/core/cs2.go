package core

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Counter-Strike 2, from Valve's Game State Integration — the same official
// local feed Dota uses. The game posts the local player's own state to this
// PC while a match runs: map, score, rounds, kills, deaths.
//
// That is all there is. Valve publishes no match-history API for CS2 that
// works without each player's private match-sharing code, and no public
// leaderboard feed, so everything here is what the app recorded live. A match
// played while the app was closed is not recoverable.

const (
	cs2AppID   = 730
	cs2CfgName = "gamestate_integration_thetracker.cfg"
)

func cs2ConfigText(port int, token string) string {
	return fmt.Sprintf(`"TheTracker"
{
    "uri"           "http://127.0.0.1:%d/"
    "timeout"       "5.0"
    "buffer"        "0.1"
    "throttle"      "0.5"
    "heartbeat"     "30.0"
    "auth"
    {
        "token"         "%s"
    }
    "data"
    {
        "provider"              "1"
        "map"                   "1"
        "round"                 "1"
        "player_id"             "1"
        "player_state"          "1"
        "player_match_stats"    "1"
    }
}
`, port, token)
}

// Cs2Match is one match, live or finished.
type Cs2Match struct {
	ID         string `json:"id"`
	Date       string `json:"date"`
	Map        string `json:"map"`
	Mode       string `json:"mode"`
	Phase      string `json:"phase"` // warmup | live | intermission | gameover
	Team       string `json:"team"`  // CT | T, the side being played now
	Round      int    `json:"round"`
	MyScore    int    `json:"myScore"`
	TheirScore int    `json:"theirScore"`
	Kills      int    `json:"kills"`
	Deaths     int    `json:"deaths"`
	Assists    int    `json:"assists"`
	MVPs       int    `json:"mvps"`
	Score      int    `json:"score"`
	// Headshot kills, summed round by round.
	HeadshotKills int `json:"headshotKills"`
	// Right now, for the Live page.
	Health int `json:"health"`
	Armor  int `json:"armor"`
	Money  int `json:"money"`
	// win | loss | draw, set when the game says the match is over. Empty for
	// a match that was left before the end.
	Result     string `json:"result"`
	Ended      bool   `json:"ended"`
	Incomplete bool   `json:"incomplete"`
	Simulated  bool   `json:"simulated,omitempty"`

	lastRoundHS int
}

type Cs2Status struct {
	Current    *Cs2Match `json:"current"`
	GsiAgeSecs *int64    `json:"gsiAgeSecs"`
	Live       bool      `json:"live"`
}

type Cs2 struct {
	mu      sync.Mutex
	store   *Store
	current *Cs2Match

	lastPayloadAt      time.Time
	lastMatchPayloadAt time.Time
	now                func() time.Time

	steamRoot func() string
}

func NewCs2(store *Store) *Cs2 {
	return &Cs2{store: store, now: time.Now, steamRoot: SteamPath}
}

// ---------- History ----------

func (c *Cs2) History() []Cs2Match {
	out := []Cs2Match{}
	c.store.readJSON("cs2_history.json", &out)
	return out
}

func (c *Cs2) save(m Cs2Match) {
	c.store.historyMu.Lock()
	defer c.store.historyMu.Unlock()
	h := c.History()
	for _, old := range h {
		if old.ID == m.ID {
			return
		}
	}
	_ = c.store.writeJSON("cs2_history.json", append(h, m))
}

func (c *Cs2) Delete(id string) ([]Cs2Match, error) {
	c.store.historyMu.Lock()
	defer c.store.historyMu.Unlock()
	h := c.History()
	out := []Cs2Match{}
	for _, m := range h {
		if m.ID != id {
			out = append(out, m)
		}
	}
	if len(out) == len(h) {
		return h, errors.New("That match isn't in your CS2 history.")
	}
	return out, c.store.writeJSON("cs2_history.json", out)
}

// ---------- Live ----------

func (c *Cs2) Status() Cs2Status {
	c.mu.Lock()
	defer c.mu.Unlock()
	s := Cs2Status{}
	if c.current != nil {
		m := *c.current
		s.Current = &m
		s.Live = !m.Ended && m.Phase != "" && !c.lastMatchPayloadAt.IsZero() && c.now().Sub(c.lastMatchPayloadAt) < matchFeedTimeout
	}
	if !c.lastPayloadAt.IsZero() {
		s.GsiAgeSecs = ptr(int64(c.now().Sub(c.lastPayloadAt).Seconds()))
	}
	return s
}

// A match shorter than this that never reached "gameover" is a warmup, a
// practice server or a join-and-leave, and is not kept.
const cs2MinRounds = 5

func (c *Cs2) finalizeLocked(incomplete bool) {
	m := c.current
	if m == nil || m.Ended {
		return
	}
	m.Ended, m.Incomplete = true, incomplete
	if !incomplete {
		switch {
		case m.MyScore > m.TheirScore:
			m.Result = "win"
		case m.MyScore < m.TheirScore:
			m.Result = "loss"
		default:
			m.Result = "draw"
		}
	}
	if m.Simulated || (incomplete && m.MyScore+m.TheirScore < cs2MinRounds) {
		return
	}
	c.save(*m)
}

// HandleUpdate applies one CS2 GSI payload.
func (c *Cs2) HandleUpdate(body jsonMap) {
	c.mu.Lock()
	defer c.mu.Unlock()
	now := c.now()
	c.lastPayloadAt = now

	gmap, player, provider := sub(body, "map"), sub(body, "player"), sub(body, "provider")
	if gmap == nil {
		// Back in the menus: whatever was running is over.
		if c.current != nil && !c.current.Ended {
			c.finalizeLocked(true)
		}
		return
	}
	c.lastMatchPayloadAt = now

	name, _ := getStr(gmap, "name")
	phase, _ := getStr(gmap, "phase")
	simulated, _ := getBool(body, SimulatedMarker)

	// A different map, or a fresh warmup after a finished game, is a new
	// match. CS2's feed carries no match id.
	cur := c.current
	if cur != nil && !cur.Ended && cur.Map != name {
		c.finalizeLocked(true)
		cur = nil
	}
	if cur == nil || cur.Ended {
		if phase == "gameover" {
			return // the scoreboard of a match already recorded
		}
		cur = &Cs2Match{ID: "cs" + strconv.FormatInt(now.UnixMilli(), 10) + randomToken(2), Date: now.UTC().Format("2006-01-02T15:04:05.000Z"), Map: name, Simulated: simulated}
		c.current = cur
	}

	cur.Phase = phase
	cur.Mode, _ = getStr(gmap, "mode")
	if r, ok := getInt(gmap, "round"); ok {
		cur.Round = int(r)
	}

	// While dead the feed describes whoever is being spectated. Only the
	// local player's own numbers count.
	mine := true
	if pid, ok := getStr(player, "steamid"); ok {
		if own, ok := getStr(provider, "steamid"); ok && own != "" {
			mine = pid == own
		}
	}
	if mine && player != nil {
		if team, ok := getStr(player, "team"); ok {
			cur.Team = team
		}
		if ms := sub(player, "match_stats"); ms != nil {
			cur.Kills, cur.Deaths, cur.Assists = int(jI64(ms, "kills")), int(jI64(ms, "deaths")), int(jI64(ms, "assists"))
			cur.MVPs, cur.Score = int(jI64(ms, "mvps")), int(jI64(ms, "score"))
		}
		if st := sub(player, "state"); st != nil {
			cur.Health, cur.Armor, cur.Money = int(jI64(st, "health")), int(jI64(st, "armor")), int(jI64(st, "money"))
			// round_killhs counts up within a round and drops to zero at
			// the next; add each increase.
			hs := int(jI64(st, "round_killhs"))
			if hs > cur.lastRoundHS {
				cur.HeadshotKills += hs - cur.lastRoundHS
			}
			cur.lastRoundHS = hs
		}
	}

	// Scores follow the side, and sides swap at half time, so "my score" is
	// always the score of the side being played now.
	ct, t := int(jI64(sub(gmap, "team_ct"), "score")), int(jI64(sub(gmap, "team_t"), "score"))
	switch cur.Team {
	case "CT":
		cur.MyScore, cur.TheirScore = ct, t
	case "T":
		cur.MyScore, cur.TheirScore = t, ct
	}

	if phase == "gameover" {
		c.finalizeLocked(false)
	}
}

// ---------- Setup ----------

type Cs2Setup struct {
	CfgDirs   []string `json:"cfgDirs"`
	Installed bool     `json:"installed"`
	Port      int      `json:"port"`
}

func (c *Cs2) cfgDirs() []string {
	g := &Gsi{steamRoot: c.steamRoot}
	dirs := []string{}
	for _, lib := range g.steamLibraries() {
		cfg := filepath.Join(lib, "steamapps", "common", "Counter-Strike Global Offensive", "game", "csgo", "cfg")
		if st, err := os.Stat(cfg); err == nil && st.IsDir() {
			dirs = append(dirs, cfg)
		}
	}
	return dirs
}

func (c *Cs2) Setup(port int, token string) Cs2Setup {
	s := Cs2Setup{CfgDirs: c.cfgDirs(), Port: port}
	want := strings.TrimSpace(cs2ConfigText(port, token))
	for _, dir := range s.CfgDirs {
		raw, err := os.ReadFile(filepath.Join(dir, cs2CfgName))
		if err == nil && strings.TrimSpace(strings.ReplaceAll(string(raw), "\r\n", "\n")) == want {
			s.Installed = true
		}
	}
	return s
}

// Install writes the config into every CS2 install found. CS2 reads it at
// launch and needs no launch option.
func (c *Cs2) Install(port int, token string) error {
	dirs := c.cfgDirs()
	if len(dirs) == 0 {
		return errors.New("Couldn't find Counter-Strike 2. Is it installed through Steam?")
	}
	wrote := false
	var last error
	for _, dir := range dirs {
		if err := os.WriteFile(filepath.Join(dir, cs2CfgName), []byte(cs2ConfigText(port, token)), 0o644); err != nil {
			last = err
			continue
		}
		wrote = true
	}
	if !wrote {
		return fmt.Errorf("Couldn't write the config: %v", last)
	}
	return nil
}

func (c *Cs2) Remove() {
	for _, dir := range c.cfgDirs() {
		_ = os.Remove(filepath.Join(dir, cs2CfgName))
	}
}
