package core

import (
	"fmt"
	"math"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

// SimulatedMarker is a top-level key the built-in simulator adds to every
// payload. Real Dota never sends it; a match carrying it is shown live but
// never saved or synced.
const SimulatedMarker = "thetracker_simulated"

// A match that ends without Dota reporting the post-game state is kept only
// if it got at least this far. Anything shorter is almost always a remake or
// an abandon in the first minutes.
const minIncompleteClock = 5 * 60.0

// How long after its minute a checkpoint can still be taken. Dota posts at
// least every 30 seconds, so a running app always lands inside this.
const checkpointWindow = 75.0

const (
	stateInProgress = "DOTA_GAMERULES_STATE_GAME_IN_PROGRESS"
	statePostGame   = "DOTA_GAMERULES_STATE_POST_GAME"
)

// Tracker turns raw GSI payloads into match state and, when a match ends,
// into a history entry.
type Tracker struct {
	mu sync.Mutex

	store   *Store
	current *MatchState
	enabled bool
	log     []string

	// When Dota last posted anything at all, menus included — the answer to
	// "is Dota talking to us", which is a different question from "are we
	// recording".
	lastPayloadAt time.Time
	// When Dota last posted while the player was in a match.
	lastMatchPayloadAt time.Time
	// A game the player is watching (spectating or a replay), and when Dota
	// last posted about it. Never recorded.
	watchingID string
	watchingAt time.Time

	// Called (outside the lock) with each match newly written to history.
	OnSaved func(MatchSummary)

	now func() time.Time
}

func NewTracker(store *Store) *Tracker {
	return &Tracker{store: store, enabled: true, now: time.Now}
}

// LiveStatus is what the UI and the overlay poll.
type LiveStatus struct {
	Current         *MatchState `json:"current"`
	TrackingEnabled bool        `json:"trackingEnabled"`
	// Seconds since Dota last posted anything. Nil if it has not since the
	// app started.
	GsiAgeSecs *int64 `json:"gsiAgeSecs"`
	// True while a match is running and Dota is still reporting on it.
	Live bool `json:"live"`
	// True while the player is watching a game (spectating or a replay)
	// rather than playing one.
	Watching bool     `json:"watching"`
	Log      []string `json:"log,omitempty"`
}

// matchFeedTimeout: Dota heartbeats every 30 seconds, so silence well past
// that means it has stopped talking.
const matchFeedTimeout = 45 * time.Second

func (t *Tracker) Status(withLog bool) LiveStatus {
	t.mu.Lock()
	defer t.mu.Unlock()
	s := LiveStatus{Current: t.current.clone(), TrackingEnabled: t.enabled, Live: t.liveLocked()}
	s.Watching = t.watchingID != "" && t.now().Sub(t.watchingAt) < matchFeedTimeout && !s.Live
	if !t.lastPayloadAt.IsZero() {
		s.GsiAgeSecs = ptr(int64(t.now().Sub(t.lastPayloadAt).Seconds()))
	}
	if withLog {
		s.Log = append([]string{}, t.log...)
	}
	return s
}

func (t *Tracker) liveLocked() bool {
	m := t.current
	if m == nil || m.Ended || !m.InProgress {
		return false
	}
	// Leaving a game early sends the player to the menu, whose payloads are
	// ignored — so InProgress alone would stay true until the next match.
	return !t.lastMatchPayloadAt.IsZero() && t.now().Sub(t.lastMatchPayloadAt) < matchFeedTimeout
}

// IsLive reports whether a match is running right now; it drives the overlay.
func (t *Tracker) IsLive() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.liveLocked()
}

func (t *Tracker) SetEnabled(on bool) {
	t.mu.Lock()
	t.enabled = on
	t.mu.Unlock()
}

func (t *Tracker) SetGameType(gameType string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.current != nil && !t.current.Ended && IsGameType(gameType) {
		t.current.GameType = gameType
	}
}

// SetRole fixes the player's role for this match (core | support), or lets
// the app work it out again ("auto").
func (t *Tracker) SetRole(role string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.current == nil || t.current.Ended {
		return
	}
	switch role {
	case "core", "support":
		t.current.RoleChoice = role
	case "auto":
		t.current.RoleChoice = ""
	}
}

// The match in progress is kept here across an update's restart.
const restartFile = "live_match.json"

// A kept match older than this is not picked up again: the restart it was
// kept for didn't happen, or took too long to trust.
const restartWithin = 10 * time.Minute

type restartState struct {
	SavedAt time.Time   `json:"savedAt"`
	Match   *MatchState `json:"match"`
}

// SaveForRestart keeps the match in progress on disk, for the next start to
// carry on with. Test matches are not kept.
func (t *Tracker) SaveForRestart() {
	t.mu.Lock()
	defer t.mu.Unlock()
	m := t.current
	if m == nil || m.Ended || m.Simulated {
		return
	}
	_ = t.store.writeJSON(restartFile, restartState{SavedAt: t.now(), Match: m.clone()})
	t.logf("Kept match %s for the restart", m.MatchID)
}

// RestoreAfterRestart picks up a match kept by SaveForRestart. The next
// payload for the same match carries on from it; a different match id
// finishes it like any match that stopped reporting.
func (t *Tracker) RestoreAfterRestart() {
	var st restartState
	ok := t.store.readJSON(restartFile, &st)
	_ = os.Remove(t.store.path(restartFile))
	if !ok || st.Match == nil || t.now().Sub(st.SavedAt) > restartWithin || st.Match.Ended {
		return
	}
	m := st.Match
	if m.OwnedItemCounts == nil {
		m.OwnedItemCounts = map[string]int{}
	}
	if m.Checkpoints == nil {
		m.Checkpoints = map[int]*Checkpoint{}
	}
	for _, minute := range CheckpointMinutes {
		if _, ok := m.Checkpoints[minute]; !ok {
			m.Checkpoints[minute] = nil
		}
	}
	t.mu.Lock()
	t.current = m
	t.logf("Carrying on with match %s after the update", m.MatchID)
	t.mu.Unlock()
}

func (t *Tracker) MarkRoshanDeath() {
	t.mu.Lock()
	defer t.mu.Unlock()
	m := t.current
	if m == nil || m.Ended {
		return
	}
	m.Roshan.Deaths++
	m.Roshan.LastDeathClock = ptr(m.LastClockTime)
	m.Roshan.WasAlive = false
	t.logf("Roshan death #%d at %s", m.Roshan.Deaths, FmtClock(m.LastClockTime))
}

func (t *Tracker) logf(format string, args ...any) {
	line := fmt.Sprintf("[%s] %s", t.now().Format("15:04:05"), fmt.Sprintf(format, args...))
	t.log = append(t.log, line)
	if len(t.log) > 300 {
		t.log = t.log[len(t.log)-300:]
	}
}

// ---------- JSON helpers ----------
//
// GSI payloads are decoded into generic maps: the feed's shape varies with
// game state and Valve adds fields without notice, so missing keys are normal
// and must never be an error.

type jsonMap = map[string]any

func sub(m jsonMap, key string) jsonMap {
	if v, ok := m[key].(map[string]any); ok {
		return v
	}
	return nil
}

func getStr(m jsonMap, key string) (string, bool) {
	v, ok := m[key].(string)
	return v, ok
}

func getNum(m jsonMap, key string) (float64, bool) {
	v, ok := m[key].(float64)
	return v, ok
}

func getInt(m jsonMap, key string) (int64, bool) {
	v, ok := m[key].(float64)
	return int64(v), ok
}

func getBool(m jsonMap, key string) (bool, bool) {
	v, ok := m[key].(bool)
	return v, ok
}

// idString reads an id that may arrive as a string or a number.
func idString(v any) (string, bool) {
	switch x := v.(type) {
	case string:
		return x, x != ""
	case float64:
		return strconv.FormatFloat(x, 'f', -1, 64), true
	}
	return "", false
}

// watchingPayload tells a spectated game or a replay from the player's own
// match. Playing, Dota describes the player directly (player.activity,
// hero.name); watching, it lists every player grouped by team (team2,
// team3) and says nothing about "you".
func watchingPayload(player, hero jsonMap) bool {
	for _, m := range []jsonMap{player, hero} {
		if sub(m, "team2") != nil || sub(m, "team3") != nil {
			return true
		}
	}
	if player == nil && hero == nil {
		return false
	}
	_, hasActivity := getStr(player, "activity")
	_, hasHero := getStr(hero, "name")
	return !hasActivity && !hasHero
}

// ---------- The update path ----------

// HandleUpdate applies one GSI payload.
func (t *Tracker) HandleUpdate(body jsonMap) {
	var saved *MatchSummary
	t.mu.Lock()
	saved = t.handleLocked(body)
	hook := t.OnSaved
	t.mu.Unlock()
	if saved != nil && hook != nil {
		hook(*saved)
	}
}

func (t *Tracker) handleLocked(body jsonMap) (saved *MatchSummary) {
	now := t.now()
	t.lastPayloadAt = now
	if !t.enabled {
		return nil
	}

	gmap, player, hero, items := sub(body, "map"), sub(body, "player"), sub(body, "hero"), sub(body, "items")

	// Watching a game, not playing it: noted, never recorded.
	if watchingPayload(player, hero) {
		if id, ok := idString(gmap["matchid"]); ok && id != "0" {
			if id != t.watchingID {
				t.logf("Watching match %s (spectating or a replay): not recorded", id)
			}
			t.watchingID, t.watchingAt = id, now
		}
		return nil
	}
	t.watchingID = ""

	if activity, ok := getStr(player, "activity"); ok && activity != "playing" {
		return nil
	}
	matchID, ok := idString(gmap["matchid"])
	if !ok || matchID == "0" {
		return nil
	}
	t.lastMatchPayloadAt = now

	var heroName *string
	if n, ok := getStr(hero, "name"); ok {
		heroName = &n
	}
	simulated, _ := getBool(body, SimulatedMarker)

	if t.current == nil || t.current.MatchID != matchID {
		// A new match id proves the previous match is over, whether or not
		// Dota ever said so. It is kept if it was actually played.
		if p := t.current; p != nil && !p.Ended && p.ReachedGame && p.LastClockTime >= minIncompleteClock {
			saved = t.finalizeLocked(true)
		}
		t.current = newMatchState(matchID, heroName, now.Format(time.RFC3339))
		t.current.Simulated = simulated
		note := ""
		if simulated {
			note = " (simulated, will not be saved)"
		}
		t.logf("New match detected: %s%s", matchID, note)
	}

	m := t.current
	if m.Ended {
		return saved
	}

	clock, ok := getNum(gmap, "clock_time")
	if !ok {
		clock = m.LastClockTime
	}
	m.LastClockTime = clock
	m.LastSeenAt = ptr(now.UTC().Format("2006-01-02T15:04:05.000Z"))

	gameState, _ := getStr(gmap, "game_state")
	m.InProgress = gameState == stateInProgress
	if m.InProgress {
		m.ReachedGame = true
	}

	// The result. GSI reports the player's side as player.team_name and,
	// once the ancient falls, the winner as map.win_team ("none" until then).
	// Read in this order because the winning payload can carry both.
	if team, ok := getStr(player, "team_name"); ok {
		team = strings.ToLower(team)
		if team == "radiant" || team == "dire" {
			m.Team = &team
		}
	}
	if winner, ok := getStr(gmap, "win_team"); ok {
		winner = strings.ToLower(winner)
		if (winner == "radiant" || winner == "dire") && m.Team != nil {
			m.Won = ptr(*m.Team == winner)
		}
	}
	if v, ok := getInt(player, "assists"); ok {
		m.Assists = &v
	}
	if v, ok := getInt(player, "gpm"); ok {
		m.GPM = &v
	}
	if v, ok := getInt(player, "xpm"); ok {
		m.XPM = &v
	}
	if v, ok := getInt(hero, "level"); ok {
		m.Level = &v
	}
	if v, ok := getBool(gmap, "daytime"); ok {
		m.Daytime = &v
	}
	if heroName != nil {
		m.HeroName = heroName
	}
	if v, ok := getInt(player, "kills"); ok {
		m.Kills = v
	}

	gold, hasGold := getInt(player, "gold")
	if alive, ok := getBool(hero, "alive"); ok {
		if m.WasAlive && !alive {
			var lost *int64
			if hasGold && m.PrevGold != nil {
				lost = ptr(max(*m.PrevGold-gold, 0))
			}
			d := Death{Clock: FmtClock(clock), GoldLost: lost}
			m.Deaths = append(m.Deaths, d)
			if lost != nil {
				t.logf("Death at %s, lost %dg", d.Clock, *lost)
			} else {
				t.logf("Death at %s", d.Clock)
			}
		}
		m.WasAlive = alive
	}
	if hasGold {
		m.PrevGold = &gold
		m.Gold = &gold
	}

	if v, ok := getInt(player, "last_hits"); ok {
		m.LastHits = v
	}
	if v, ok := getInt(player, "denies"); ok {
		m.Denies = v
	}
	// Checkpoints are only taken off the real game clock. The draft and
	// strategy time report a clock as well, and a pause there must not be
	// able to stamp a checkpoint.
	//
	// A checkpoint is also only taken close to its minute. Starting the app
	// half an hour into a game used to stamp all five at once with the
	// current last hits, which then skewed every later comparison; a mark
	// that was missed now stays empty instead.
	if m.InProgress {
		for _, minute := range CheckpointMinutes {
			at := float64(minute) * 60
			if clock >= at && clock < at+checkpointWindow && m.Checkpoints[minute] == nil {
				m.Checkpoints[minute] = &Checkpoint{LastHits: m.LastHits, Denies: m.Denies}
				t.logf("%d min: %d last hits, %d denies", minute, m.LastHits, m.Denies)
			}
		}
	}

	// Item ownership: total count across inventory, backpack, neutral and
	// teleport slots, so moving an item between slots is not a purchase.
	if items != nil {
		counts := map[string]int{}
		for slot, raw := range items {
			if !(strings.HasPrefix(slot, "slot") || strings.HasPrefix(slot, "teleport") || strings.HasPrefix(slot, "neutral")) {
				continue
			}
			data, _ := raw.(map[string]any)
			name, ok := getStr(data, "name")
			if !ok || name == "empty" {
				continue
			}
			counts[strings.TrimPrefix(name, "item_")]++
		}
		if !m.SupportItems && clock < supportItemsBefore {
			for name := range counts {
				if supportItems[name] {
					m.SupportItems = true
					t.logf("Support items at %s: %s", FmtClock(clock), name)
					break
				}
			}
		}
		for _, name := range sortedKeys(counts) {
			if !IsKeyItem(name) {
				continue
			}
			for i := m.OwnedItemCounts[name]; i < counts[name]; i++ {
				m.KeyItemLog = append(m.KeyItemLog, KeyItemEntry{Clock: FmtClock(clock), Item: name})
				t.logf("Bought %s at %s", name, FmtClock(clock))
			}
		}
		m.OwnedItemCounts = counts
	}

	if gameState == statePostGame {
		if s := t.finalizeLocked(false); s != nil {
			saved = s
		}
	}
	return saved
}

// finalizeLocked ends the current match and writes it to history. It returns
// the summary only if this call is what saved it.
func (t *Tracker) finalizeLocked(incomplete bool) *MatchSummary {
	m := t.current
	if m == nil || m.Ended {
		return nil
	}
	summary := buildSummary(m, incomplete, t.now())

	var saved *MatchSummary
	final := summary
	if !m.Simulated && t.store != nil {
		t.store.UpdateHistory(func(h []MatchSummary) ([]MatchSummary, bool) {
			// The same match can reach this twice: restart the app on the
			// post-game screen and the new instance sees POST_GAME for a
			// match the old one already saved. The first record is the
			// complete one.
			for _, existing := range h {
				if existing.MatchID == m.MatchID {
					final = existing
					return h, false
				}
			}
			h = append(h, summary)
			RecomputeComparisons(h)
			final = h[len(h)-1]
			saved = &final
			return h, true
		})
	}

	m.Ended = true
	m.InProgress = false
	m.Summary = &final

	result := "result unknown"
	if m.Won != nil {
		result = map[bool]string{true: "won", false: "lost"}[*m.Won]
	}
	how := ""
	switch {
	case m.Simulated:
		how = ", simulated, not saved"
	case incomplete:
		how = ", saved as incomplete: Dota never reported the end"
	}
	t.logf("Match %s ended (%s)%s. %d deaths, %dg lost", m.MatchID, result, how, len(m.Deaths), m.totalGoldLost())
	return saved
}

func buildSummary(m *MatchState, incomplete bool, now time.Time) MatchSummary {
	date := now.UTC().Format("2006-01-02T15:04:05.000Z")
	// A late-saved match is dated by the last time Dota reported on it, not
	// by when the next match happened to start.
	if incomplete && m.LastSeenAt != nil {
		date = *m.LastSeenAt
	}
	c := m.clone()
	return MatchSummary{
		MatchID:       m.MatchID,
		HeroName:      m.HeroName,
		Date:          date,
		Duration:      FmtClock(m.LastClockTime),
		Kills:         m.Kills,
		TotalDeaths:   len(m.Deaths),
		TotalGoldLost: m.totalGoldLost(),
		Deaths:        c.Deaths,
		KeyItems:      c.KeyItemLog,
		Checkpoints:   c.Checkpoints,
		RoshanDeaths:  m.Roshan.Deaths,
		GameType:      m.GameType,
		Won:           m.Won,
		LastHits:      ptr(m.LastHits),
		Denies:        ptr(m.Denies),
		Assists:       m.Assists,
		GPM:           m.GPM,
		XPM:           m.XPM,
		Level:         m.Level,
		Incomplete:    incomplete,
	}
}

// ---------- Comparisons ----------

// RecomputeComparisons rebuilds every match's comparison against its current
// peers: the other matches of the same game type.
func RecomputeComparisons(history []MatchSummary) {
	for i := range history {
		var peers []*MatchSummary
		for j := range history {
			if i != j && history[j].GameType == history[i].GameType && history[j].MatchID != history[i].MatchID {
				peers = append(peers, &history[j])
			}
		}
		cmp := computeComparison(&history[i], peers)
		history[i].Comparison = &cmp
		history[i].GamesComparedAgainst = ptr(len(peers))
	}
}

func computeComparison(s *MatchSummary, peers []*MatchSummary) Comparison {
	collect := func(get func(*MatchSummary) *float64) []float64 {
		var vals []float64
		for _, p := range peers {
			if v := get(p); v != nil {
				vals = append(vals, *v)
			}
		}
		return vals
	}
	avg := func(vals []float64) *float64 {
		if len(vals) == 0 {
			return nil
		}
		sum := 0.0
		for _, v := range vals {
			sum += v
		}
		return ptr(sum / float64(len(vals)))
	}
	metric := func(value *float64, get func(*MatchSummary) *float64, higherBetter bool) CompareMetric {
		vals := collect(get)
		c := CompareValues(value, avg(vals), higherBetter)
		if value != nil && len(vals) > 0 {
			best := vals[0]
			for _, v := range vals {
				if higherBetter {
					best = math.Max(best, v)
				} else {
					best = math.Min(best, v)
				}
			}
			if higherBetter {
				c.IsBest = *value > best
			} else {
				c.IsBest = *value < best
			}
		}
		return c
	}

	deaths := func(p *MatchSummary) *float64 { return ptr(float64(p.TotalDeaths)) }
	gold := func(p *MatchSummary) *float64 { return ptr(float64(p.TotalGoldLost)) }

	out := Comparison{
		Deaths:      metric(deaths(s), deaths, false),
		GoldLost:    metric(gold(s), gold, false),
		Checkpoints: map[int]CompareMetric{},
	}
	for _, minute := range CheckpointMinutes {
		get := func(p *MatchSummary) *float64 {
			if cp := p.Checkpoints[minute]; cp != nil {
				return ptr(float64(cp.LastHits))
			}
			return nil
		}
		out.Checkpoints[minute] = metric(get(s), get, true)
	}
	return out
}

// CompareValues classifies a value against an average. Within 8% (or half a
// point, whichever is larger) counts as similar.
func CompareValues(value, avg *float64, higherBetter bool) CompareMetric {
	if value == nil || avg == nil {
		return CompareMetric{Value: value, Verdict: "no_data"}
	}
	diff := *value - *avg
	threshold := math.Max(math.Abs(*avg)*0.08, 0.5)
	verdict := "similar"
	if math.Abs(diff) > threshold {
		if (diff > 0) == higherBetter {
			verdict = "better"
		} else {
			verdict = "worse"
		}
	}
	return CompareMetric{Value: value, Avg: ptr(math.Round(*avg*10) / 10), Verdict: verdict}
}

// FmtClock formats seconds as m:ss, keeping the sign for the pre-horn clock.
func FmtClock(seconds float64) string {
	sign := ""
	if seconds < 0 {
		sign = "-"
	}
	abs := int64(math.Floor(math.Abs(seconds)))
	return fmt.Sprintf("%s%d:%02d", sign, abs/60, abs%60)
}

// ParseClock is the inverse of FmtClock for non-negative clocks.
func ParseClock(s string) (int, bool) {
	parts := strings.Split(s, ":")
	if len(parts) != 2 {
		return 0, false
	}
	m, err1 := strconv.Atoi(parts[0])
	sec, err2 := strconv.Atoi(parts[1])
	if err1 != nil || err2 != nil || m < 0 || sec < 0 {
		return 0, false
	}
	return m*60 + sec, true
}

// ---------- History edits ----------

func normalizeSummary(s *MatchSummary) {
	if s.Deaths == nil {
		s.Deaths = []Death{}
	}
	if s.KeyItems == nil {
		s.KeyItems = []KeyItemEntry{}
	}
	if s.Checkpoints == nil {
		s.Checkpoints = map[int]*Checkpoint{}
	}
}

// History returns saved matches, oldest first, with empty collections
// normalized so the UI never meets a null where a list should be.
func (s *Store) History() []MatchSummary {
	h := s.LoadHistory()
	for i := range h {
		normalizeSummary(&h[i])
	}
	return h
}

var errNoMatch = fmt.Errorf("That match isn't in your sessions.")

func (s *Store) editMatch(matchID string, edit func(*MatchSummary) error) ([]MatchSummary, *MatchSummary, error) {
	var out *MatchSummary
	var editErr error
	h, err := s.UpdateHistory(func(h []MatchSummary) ([]MatchSummary, bool) {
		for i := range h {
			if h[i].MatchID == matchID {
				if editErr = edit(&h[i]); editErr != nil {
					return h, false
				}
				RecomputeComparisons(h)
				m := h[i]
				out = &m
				return h, true
			}
		}
		editErr = errNoMatch
		return h, false
	})
	if editErr != nil {
		return nil, nil, editErr
	}
	for i := range h {
		normalizeSummary(&h[i])
	}
	return h, out, err
}

// SetHistoryGameType re-tags a finished match and recomputes comparisons, so
// peer-group stats stay consistent.
func (s *Store) SetHistoryGameType(matchID, gameType string) ([]MatchSummary, *MatchSummary, error) {
	if !IsGameType(gameType) {
		return nil, nil, fmt.Errorf("That isn't a game type TheTracker knows.")
	}
	return s.editMatch(matchID, func(m *MatchSummary) error { m.GameType = gameType; return nil })
}

func (s *Store) SetHistoryNotes(matchID, notes string) ([]MatchSummary, error) {
	if len(notes) > 2000 {
		notes = notes[:2000]
	}
	h, _, err := s.editMatch(matchID, func(m *MatchSummary) error { m.Notes = notes; return nil })
	return h, err
}

func (s *Store) DeleteHistoryMatch(matchID string) ([]MatchSummary, error) {
	found := false
	h, err := s.UpdateHistory(func(h []MatchSummary) ([]MatchSummary, bool) {
		out := h[:0:0]
		for _, m := range h {
			if m.MatchID == matchID {
				found = true
				continue
			}
			out = append(out, m)
		}
		if found {
			RecomputeComparisons(out)
		}
		return out, found
	})
	if !found {
		return nil, errNoMatch
	}
	for i := range h {
		normalizeSummary(&h[i])
	}
	return h, err
}
