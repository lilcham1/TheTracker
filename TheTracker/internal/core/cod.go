package core

// Call of Duty. Activision publishes no stats API, so the player logs each
// match in a few taps (mode, result, kills, deaths…) and everything else —
// overview, goals, review, coach — works from that log. What the app sees
// for itself is when the game runs: each stretch with cod.exe open is a
// session, and when one ends the app asks for the matches played in it.
//
// Black Ops 7 and Modern Warfare 4 are win/loss games; Warzone is placed.

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
)

type CodMatch struct {
	ID string `json:"id"`
	// When it was logged (unix seconds).
	Date int64 `json:"date"`
	// bo7 | mw4 | warzone
	Game string `json:"game"`
	Mode string `json:"mode"`
	Map  string `json:"map,omitempty"`
	// win | loss | draw; empty for Warzone, which has Place.
	Result  string `json:"result,omitempty"`
	Place   int    `json:"place,omitempty"`
	Kills   int    `json:"kills"`
	Deaths  int    `json:"deaths"`
	Assists int    `json:"assists"`
	// Multiplayer's score, Warzone's damage; 0 when not logged.
	Score  int    `json:"score,omitempty"`
	Damage int    `json:"damage,omitempty"`
	Note   string `json:"note,omitempty"`
	// The play session it belongs to, if any.
	Session string `json:"session,omitempty"`
}

// CodSession is one stretch with the game open.
type CodSession struct {
	ID    string `json:"id"`
	Start int64  `json:"start"`
	// While open: the last time the game was seen running.
	End  int64 `json:"end"`
	Open bool  `json:"open,omitempty"`
}

var codGames = map[string]string{"bo7": "Black Ops 7", "mw4": "Modern Warfare 4", "warzone": "Warzone"}

func codPlaced(game string) bool { return game == "warzone" }

const (
	codMatchesFile  = "cod_matches.json"
	codSessionsFile = "cod_sessions.json"
	// Shorter than this with the game open is a launcher check, not play.
	codMinSession = 3 * time.Minute
)

type Cod struct {
	store *Store
	mu    sync.Mutex
	now   func() time.Time
	// Called when a session ends, to ask for its matches.
	OnSessionEnd func(CodSession)
	lastSaved    time.Time
}

func NewCod(store *Store) *Cod { return &Cod{store: store, now: time.Now} }

func (c *Cod) Matches() []CodMatch {
	var list []CodMatch
	c.store.readJSON(codMatchesFile, &list)
	sort.SliceStable(list, func(i, j int) bool { return list[i].Date > list[j].Date })
	return list
}

func (c *Cod) Sessions() []CodSession {
	var list []CodSession
	c.store.readJSON(codSessionsFile, &list)
	return list
}

func (c *Cod) match(id string) (CodMatch, bool) {
	for _, m := range c.Matches() {
		if m.ID == id {
			return m, true
		}
	}
	return CodMatch{}, false
}

// SaveMatch adds a match, or replaces the one with the same id.
func (c *Cod) SaveMatch(m CodMatch) (CodMatch, error) {
	if _, ok := codGames[m.Game]; !ok {
		return m, errors.New("Pick the game: Black Ops 7, Modern Warfare 4 or Warzone.")
	}
	m.Mode, m.Map, m.Note = strings.TrimSpace(m.Mode), strings.TrimSpace(m.Map), strings.TrimSpace(m.Note)
	if m.Mode == "" {
		return m, errors.New("Pick the mode you played.")
	}
	if len(m.Mode) > 40 || len(m.Map) > 40 || len(m.Note) > 500 {
		return m, errors.New("That's longer than a mode, map or note can be.")
	}
	if codPlaced(m.Game) {
		m.Result = ""
		if m.Place < 1 || m.Place > 150 {
			return m, errors.New("Enter where you placed, from 1 up.")
		}
	} else {
		m.Place = 0
		if m.Result != "win" && m.Result != "loss" && m.Result != "draw" {
			return m, errors.New("Pick win, loss or draw.")
		}
	}
	if m.Kills < 0 || m.Deaths < 0 || m.Assists < 0 || m.Score < 0 || m.Damage < 0 ||
		m.Kills > 300 || m.Deaths > 300 || m.Assists > 300 || m.Score > 100000 || m.Damage > 100000 {
		return m, errors.New("Kills, deaths and the rest are whole numbers from 0.")
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	list := c.Matches()
	now := c.now()
	if m.ID == "" {
		m.ID = randomToken(8)
		m.Date = now.Unix()
		if m.Session == "" {
			m.Session = c.sessionAt(now)
		}
		list = append([]CodMatch{m}, list...)
	} else {
		found := false
		for i := range list {
			if list[i].ID == m.ID {
				m.Date, found = list[i].Date, true
				if m.Session == "" {
					m.Session = list[i].Session
				}
				list[i] = m
			}
		}
		if !found {
			return m, errors.New("That match isn't in your log any more.")
		}
	}
	return m, c.store.writeJSON(codMatchesFile, list)
}

func (c *Cod) DeleteMatch(id string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	list := c.Matches()
	out := list[:0]
	for _, m := range list {
		if m.ID != id {
			out = append(out, m)
		}
	}
	return c.store.writeJSON(codMatchesFile, out)
}

// sessionAt is the session a match logged at t belongs to: the one open,
// else one that ended in the last three hours.
func (c *Cod) sessionAt(t time.Time) string {
	for _, s := range c.Sessions() {
		if s.Open || (t.Unix()-s.End >= 0 && t.Unix()-s.End < 3*3600) {
			return s.ID
		}
	}
	return ""
}

// SessionTick records whether the game is running; the watcher calls it
// every few seconds.
func (c *Cod) SessionTick(running bool) {
	c.mu.Lock()
	now := c.now()
	list := c.Sessions()
	open := len(list) > 0 && list[0].Open
	var ended *CodSession
	switch {
	case running && !open:
		list = append([]CodSession{{ID: randomToken(6), Start: now.Unix(), End: now.Unix(), Open: true}}, list...)
		c.saveSessions(list)
	case running && open:
		list[0].End = now.Unix()
		// Written once a minute: enough to close it well after a crash.
		if now.Sub(c.lastSaved) >= time.Minute {
			c.saveSessions(list)
		}
	case !running && open:
		s := list[0]
		s.Open = false
		if time.Duration(s.End-s.Start)*time.Second < codMinSession {
			list = list[1:]
		} else {
			list[0] = s
			ended = &s
		}
		c.saveSessions(list)
	}
	c.mu.Unlock()
	if ended != nil && c.OnSessionEnd != nil {
		c.OnSessionEnd(*ended)
	}
}

func (c *Cod) saveSessions(list []CodSession) {
	if len(list) > 300 {
		list = list[:300]
	}
	c.lastSaved = c.now()
	_ = c.store.writeJSON(codSessionsFile, list)
}

// CloseStaleSession ends a session left open by a crash or restart while the
// game isn't running, at the last time it was seen.
func (c *Cod) CloseStaleSession(running bool) {
	if !running {
		c.SessionTick(false)
	}
}

// ---------- Overview ----------

type CodStats struct {
	Game    string  `json:"game"`
	Label   string  `json:"label"`
	Matches int     `json:"matches"`
	Wins    int     `json:"wins"`
	Losses  int     `json:"losses"`
	Draws   int     `json:"draws"`
	Kills   int     `json:"kills"`
	Deaths  int     `json:"deaths"`
	Assists int     `json:"assists"`
	KD      float64 `json:"kd"`
	WinRate float64 `json:"winRate"`
	// Warzone.
	AvgPlace  float64 `json:"avgPlace,omitempty"`
	Top10     int     `json:"top10,omitempty"`
	Wins1     int     `json:"wins1,omitempty"`
	AvgDamage float64 `json:"avgDamage,omitempty"`
	// K/D per match, oldest first, over the last 20.
	Form []float64 `json:"form"`
	// The mode with the most matches.
	TopMode string `json:"topMode,omitempty"`
}

type CodOverview struct {
	Matches  []CodMatch   `json:"matches"`
	Sessions []CodSession `json:"sessions"`
	Stats    []CodStats   `json:"stats"`
	// The latest session with nothing logged yet, if it ended recently.
	ToLog   *CodSession `json:"toLog,omitempty"`
	Playing bool        `json:"playing"`
	// Play time and matches over the last 7 days.
	WeekMinutes int `json:"weekMinutes"`
	WeekMatches int `json:"weekMatches"`
}

func codKD(k, d int) float64 {
	if d == 0 {
		return float64(k)
	}
	return float64(k) / float64(d)
}

func (c *Cod) Overview() CodOverview {
	matches := c.Matches()
	sessions := c.Sessions()
	now := c.now()
	ov := CodOverview{Matches: matches, Sessions: sessions, Stats: []CodStats{}}
	if ov.Matches == nil {
		ov.Matches = []CodMatch{}
	}
	if len(ov.Sessions) > 20 {
		ov.Sessions = ov.Sessions[:20]
	}
	if ov.Sessions == nil {
		ov.Sessions = []CodSession{}
	}
	logged := map[string]bool{}
	for _, m := range matches {
		logged[m.Session] = true
		if now.Unix()-m.Date < 7*86400 {
			ov.WeekMatches++
		}
	}
	for i, s := range sessions {
		if s.Open {
			ov.Playing = true
		}
		if now.Unix()-s.End < 7*86400 {
			ov.WeekMinutes += int((s.End - s.Start) / 60)
		}
		if i == 0 && !s.Open && !logged[s.ID] && now.Unix()-s.End < 12*3600 {
			s := s
			ov.ToLog = &s
		}
	}
	for _, game := range []string{"bo7", "mw4", "warzone"} {
		var ms []CodMatch
		for _, m := range matches {
			if m.Game == game {
				ms = append(ms, m)
			}
		}
		if len(ms) > 0 {
			ov.Stats = append(ov.Stats, codStatsOf(game, ms))
		}
	}
	return ov
}

// codStatsOf sums one game's matches, newest first.
func codStatsOf(game string, ms []CodMatch) CodStats {
	s := CodStats{Game: game, Label: codGames[game], Matches: len(ms), Form: []float64{}}
	modes := map[string]int{}
	places, dmg, dmgN := 0, 0, 0
	for i, m := range ms {
		s.Kills, s.Deaths, s.Assists = s.Kills+m.Kills, s.Deaths+m.Deaths, s.Assists+m.Assists
		modes[m.Mode]++
		switch m.Result {
		case "win":
			s.Wins++
		case "loss":
			s.Losses++
		case "draw":
			s.Draws++
		}
		if codPlaced(game) {
			places += m.Place
			if m.Place <= 10 {
				s.Top10++
			}
			if m.Place == 1 {
				s.Wins1++
			}
			if m.Damage > 0 {
				dmg, dmgN = dmg+m.Damage, dmgN+1
			}
		}
		if i < 20 {
			s.Form = append([]float64{round2(codKD(m.Kills, m.Deaths))}, s.Form...)
		}
	}
	s.KD = round2(codKD(s.Kills, s.Deaths))
	if n := s.Wins + s.Losses + s.Draws; n > 0 {
		s.WinRate = round2(float64(s.Wins) / float64(n))
	}
	if codPlaced(game) {
		s.AvgPlace = round2(float64(places) / float64(len(ms)))
		if dmgN > 0 {
			s.AvgDamage = float64(dmg / dmgN)
		}
	}
	best := 0
	for mode, n := range modes {
		if n > best || n == best && mode < s.TopMode {
			s.TopMode, best = mode, n
		}
	}
	return s
}

func round2(f float64) float64 { return float64(int(f*100+0.5)) / 100 }

// ---------- Goals ----------

// CodGoals sets goals from the player's own logged matches of one game
// (the latest match's game when none is given).
func (a *App) CodGoals(game string) GoalSet {
	matches := a.Cod.Matches()
	if game == "" && len(matches) > 0 {
		game = matches[0].Game
	}
	pct, label := goalLevel(a.Store.LoadPrefs().Coach.CodLevel)
	set := GoalSet{Game: "cod", Hero: codGames[game], Level: label, Goals: []Goal{}}
	var kills, deaths, score, dmg, place []float64
	cut := a.Cod.now().AddDate(0, 0, -60).Unix()
	for _, m := range matches {
		if m.Game != game || m.Date < cut {
			continue
		}
		kills, deaths = append(kills, float64(m.Kills)), append(deaths, float64(m.Deaths))
		if m.Score > 0 {
			score = append(score, float64(m.Score))
		}
		if m.Damage > 0 {
			dmg = append(dmg, float64(m.Damage))
		}
		if m.Place > 0 {
			place = append(place, float64(m.Place))
		}
	}
	if len(kills) < 5 {
		set.Note = "Goals start after five logged matches of one game."
		return set
	}
	for _, l := range [][]float64{kills, deaths, score, dmg, place} {
		sort.Float64s(l)
	}
	round := func(f float64) float64 { return float64(int(f + 0.5)) }
	set.Goals = append(set.Goals,
		Goal{Key: "kills", Label: "Kills", Format: "count", Target: round(quantile(kills, pct)), Why: "Kills your best matches reach.", State: "pending"},
		Goal{Key: "deaths", Label: "Deaths", Format: "count", Target: round(quantile(deaths, 1-pct)), AtMost: true, Why: "As few deaths as your best matches.", State: "pending"})
	if codPlaced(game) {
		if len(dmg) >= 5 {
			set.Goals = append(set.Goals, Goal{Key: "damage", Label: "Damage", Format: "count", Target: roundStep(quantile(dmg, pct), 50), Why: "Damage your best matches reach.", State: "pending"})
		}
		set.Goals = append(set.Goals, Goal{Key: "place", Label: "Place", Format: "count", Target: round(quantile(place, 1-pct)), AtMost: true, Why: "Where your best matches finish.", State: "pending"})
	} else if len(score) >= 5 {
		set.Goals = append(set.Goals, Goal{Key: "score", Label: "Score", Format: "count", Target: roundStep(quantile(score, pct), 100), Why: "Score your best matches reach.", State: "pending"})
	}
	set.Note = fmt.Sprintf("From your last %d logged %s matches.", len(kills), codGames[game])
	return set
}

func checkCodGoals(set GoalSet, m CodMatch) GoalSet {
	out := set
	out.Goals = make([]Goal, len(set.Goals))
	for i, g := range set.Goals {
		var v float64
		switch g.Key {
		case "kills":
			v = float64(m.Kills)
		case "deaths":
			v = float64(m.Deaths)
		case "score":
			v = float64(m.Score)
		case "damage":
			v = float64(m.Damage)
		case "place":
			v = float64(m.Place)
		}
		g.Value = &v
		met := v >= g.Target
		if g.AtMost {
			met = v <= g.Target
		}
		// A figure not logged for this match is neither met nor missed.
		g.State = "missed"
		if (g.Key == "score" || g.Key == "damage") && v == 0 {
			g.State, g.Value = "pending", nil
		} else if met {
			g.State = "met"
		}
		out.Goals[i] = g
	}
	return out
}

// CodStreams lists live Call of Duty streams on Twitch.
func (a *App) CodStreams() map[string]any {
	streams, reason := a.liveStreams("cod", false)
	if streams == nil {
		streams = []LiveStream{}
	}
	return map[string]any{"streams": streams, "reason": reason}
}
