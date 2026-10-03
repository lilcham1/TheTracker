package core

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

const (
	draft = "DOTA_GAMERULES_STATE_HERO_SELECTION"
)

// newTestTracker writes to a throwaway folder, so no test can reach the real
// history file.
func newTestTracker(t *testing.T) (*Tracker, *Store) {
	t.Helper()
	store := NewStore(t.TempDir())
	return NewTracker(store), store
}

func payload(matchID, state string, clock float64, team, winTeam string) jsonMap {
	player := jsonMap{
		"activity": "playing", "kills": 5.0, "assists": 9.0, "last_hits": 120.0, "denies": 8.0,
		"gpm": 512.0, "xpm": 640.0, "gold": 900.0,
	}
	if team != "" {
		player["team_name"] = team
	}
	return jsonMap{
		"map":    jsonMap{"matchid": matchID, "game_state": state, "clock_time": clock, "win_team": winTeam},
		"player": player,
		"hero":   jsonMap{"name": "npc_dota_hero_kez", "alive": true, "level": 12.0},
	}
}

func logged(tr *Tracker, needle string) bool {
	for _, l := range tr.Status(true).Log {
		if strings.Contains(l, needle) {
			return true
		}
	}
	return false
}

func TestAWinIsReadFromWinTeam(t *testing.T) {
	tr, store := newTestTracker(t)
	tr.HandleUpdate(payload("9001", stateInProgress, 600, "radiant", "none"))
	if tr.Status(false).Current.Won != nil {
		t.Fatal("no result while the game is running")
	}
	tr.HandleUpdate(payload("9001", statePostGame, 1900, "radiant", "radiant"))

	s := tr.Status(false).Current.Summary
	if s == nil || s.Won == nil || !*s.Won || s.Incomplete {
		t.Fatalf("expected a complete win, got %+v", s)
	}
	if *s.Assists != 9 || *s.GPM != 512 || *s.XPM != 640 || *s.LastHits != 120 || *s.Level != 12 {
		t.Fatalf("final stats not carried into the summary: %+v", s)
	}
	h := store.History()
	if len(h) != 1 || h[0].MatchID != "9001" {
		t.Fatalf("the match should be in history once, got %d", len(h))
	}
}

func TestALossIsReadFromWinTeam(t *testing.T) {
	tr, _ := newTestTracker(t)
	tr.HandleUpdate(payload("9002", stateInProgress, 600, "dire", "none"))
	tr.HandleUpdate(payload("9002", statePostGame, 1900, "dire", "radiant"))
	if s := tr.Status(false).Current.Summary; s.Won == nil || *s.Won {
		t.Fatal("dire player, radiant win: should be a loss")
	}
}

func TestWithoutATeamThereIsNoResultRatherThanAGuess(t *testing.T) {
	tr, _ := newTestTracker(t)
	tr.HandleUpdate(payload("9003", stateInProgress, 600, "", "none"))
	tr.HandleUpdate(payload("9003", statePostGame, 1900, "", "radiant"))
	if tr.Status(false).Current.Summary.Won != nil {
		t.Fatal("a result was guessed without knowing the player's team")
	}
}

func TestAnUnendedMatchIsKeptWhenTheNextOneStarts(t *testing.T) {
	// Twenty-five minutes in, then Dota never reports the end — the player
	// left — and the next match begins.
	tr, store := newTestTracker(t)
	tr.HandleUpdate(payload("1", stateInProgress, 1500, "radiant", "none"))
	tr.HandleUpdate(payload("2", draft, -60, "dire", "none"))

	h := store.History()
	if len(h) != 1 || h[0].MatchID != "1" || !h[0].Incomplete {
		t.Fatalf("the previous match must be saved as incomplete, got %+v", h)
	}
	if tr.Status(false).Current.MatchID != "2" {
		t.Fatal("the new match should be current")
	}
}

func TestAMatchAbandonedInTheDraftIsNotKept(t *testing.T) {
	tr, store := newTestTracker(t)
	tr.HandleUpdate(payload("1", draft, -60, "radiant", "none"))
	tr.HandleUpdate(payload("2", draft, -60, "radiant", "none"))
	if len(store.History()) != 0 {
		t.Fatal("a game that never started is not a match")
	}
}

func TestARemakeInTheFirstMinutesIsNotKept(t *testing.T) {
	tr, store := newTestTracker(t)
	tr.HandleUpdate(payload("1", stateInProgress, 120, "radiant", "none"))
	tr.HandleUpdate(payload("2", draft, -60, "radiant", "none"))
	if len(store.History()) != 0 {
		t.Fatal("a two-minute remake should not be saved")
	}
}

func TestSimulatedMatchesAreNeverSaved(t *testing.T) {
	tr, store := newTestTracker(t)
	saved := 0
	tr.OnSaved = func(MatchSummary) { saved++ }

	body := payload("3", stateInProgress, 300, "radiant", "none")
	body[SimulatedMarker] = true
	tr.HandleUpdate(body)
	if !tr.Status(false).Current.Simulated {
		t.Fatal("the marker was not read off the payload")
	}
	end := payload("3", statePostGame, 900, "radiant", "radiant")
	end[SimulatedMarker] = true
	tr.HandleUpdate(end)

	if s := tr.Status(false).Current; !s.Ended || s.Summary == nil {
		t.Fatal("a simulated match should still end and show its summary")
	}
	if len(store.History()) != 0 || saved != 0 {
		t.Fatal("a simulated match reached history or the cloud")
	}
}

func TestDotaTalkingIsNoticedOutsideAMatch(t *testing.T) {
	// The main menu: Dota is running and posting, but there is no match.
	tr, _ := newTestTracker(t)
	tr.HandleUpdate(jsonMap{"provider": jsonMap{"appid": 570.0}, "player": jsonMap{"activity": "menu"}})
	s := tr.Status(false)
	if s.GsiAgeSecs == nil {
		t.Fatal("a menu payload is what proves the setup works")
	}
	if s.Current != nil || s.Live {
		t.Fatal("a menu payload is not a match")
	}
}

func TestDotaTalkingIsNoticedWithTrackingSwitchedOff(t *testing.T) {
	tr, _ := newTestTracker(t)
	tr.SetEnabled(false)
	tr.HandleUpdate(payload("1", stateInProgress, 600, "radiant", "none"))
	s := tr.Status(false)
	if s.GsiAgeSecs == nil || s.Current != nil {
		t.Fatal("with tracking off the feed is still noticed, but nothing is recorded")
	}
}

func TestALateSavedMatchIsDatedByItsLastPayload(t *testing.T) {
	m := newMatchState("1", nil, "")
	m.LastSeenAt = ptr("2026-09-19T21:00:00.000Z")
	now := time.Date(2026, 9, 20, 9, 0, 0, 0, time.UTC)
	if got := buildSummary(m, true, now).Date; got != "2026-09-19T21:00:00.000Z" {
		t.Fatalf("incomplete match dated %s", got)
	}
	if got := buildSummary(m, false, now).Date; got != "2026-09-20T09:00:00.000Z" {
		t.Fatalf("complete match dated %s", got)
	}
}

func TestHistoryWrittenByEarlierVersionsStillLoads(t *testing.T) {
	// The original format, before results and final stats existed.
	old := `[{"matchid":"1","heroName":null,"date":"2026-09-05T00:00:00Z","duration":"25:00",
		"kills":6,"totalDeaths":0,"totalGoldLost":0,"deaths":[],"keyItems":[],
		"checkpoints":{"5":{"lastHits":30,"denies":2},"10":null},
		"roshanDeaths":0,"gameType":"unspecified","comparison":null,"gamesComparedAgainst":null}]`
	store := NewStore(t.TempDir())
	if err := store.writeFile("history.json", []byte(old)); err != nil {
		t.Fatal(err)
	}
	h := store.History()
	if len(h) != 1 || h[0].Won != nil || h[0].Incomplete {
		t.Fatalf("old history did not load cleanly: %+v", h)
	}
	if cp := h[0].Checkpoints[5]; cp == nil || cp.LastHits != 30 || h[0].Checkpoints[10] != nil {
		t.Fatalf("checkpoints misread: %+v", h[0].Checkpoints)
	}
	// And it writes back in the same shape.
	raw, _ := json.Marshal(h[0])
	for _, key := range []string{`"matchid"`, `"heroName"`, `"totalDeaths"`, `"checkpoints":{"10":null,"5":{"lastHits":30,"denies":2}}`, `"gameType"`} {
		if !strings.Contains(string(raw), key) {
			t.Fatalf("history JSON lost %s: %s", key, raw)
		}
	}
}

func TestTheSameMatchIsNotSavedTwice(t *testing.T) {
	// Restarting the app on the post-game screen — the updater does exactly
	// that — makes a new tracker see POST_GAME for a match already saved.
	tr, store := newTestTracker(t)
	tr.HandleUpdate(payload("7", stateInProgress, 600, "radiant", "none"))
	tr.HandleUpdate(payload("7", statePostGame, 1900, "radiant", "radiant"))

	second := NewTracker(store)
	saved := 0
	second.OnSaved = func(MatchSummary) { saved++ }
	second.HandleUpdate(payload("7", statePostGame, 1900, "radiant", "radiant"))

	h := store.History()
	if len(h) != 1 || saved != 0 {
		t.Fatalf("expected one record and no second sync, got %d records, %d syncs", len(h), saved)
	}
	if *h[0].LastHits != 120 {
		t.Fatal("the first, complete record must be the one kept")
	}
}

func TestSavedMatchesAreHandedToSyncOnce(t *testing.T) {
	tr, _ := newTestTracker(t)
	var got []string
	tr.OnSaved = func(m MatchSummary) { got = append(got, m.MatchID) }
	tr.HandleUpdate(payload("5", stateInProgress, 600, "radiant", "none"))
	tr.HandleUpdate(payload("5", statePostGame, 1900, "radiant", "dire"))
	tr.HandleUpdate(payload("5", statePostGame, 1905, "radiant", "dire"))
	if len(got) != 1 || got[0] != "5" {
		t.Fatalf("expected exactly one sync for match 5, got %v", got)
	}
}

func TestDeathsAndGoldLostAreRecorded(t *testing.T) {
	tr, _ := newTestTracker(t)
	alive := payload("8", stateInProgress, 400, "radiant", "none")
	alive["player"].(jsonMap)["gold"] = 2400.0
	tr.HandleUpdate(alive)

	dead := payload("8", stateInProgress, 410, "radiant", "none")
	dead["player"].(jsonMap)["gold"] = 1900.0
	dead["hero"].(jsonMap)["alive"] = false
	tr.HandleUpdate(dead)
	tr.HandleUpdate(dead) // still dead: must not count twice

	m := tr.Status(false).Current
	if len(m.Deaths) != 1 || *m.Deaths[0].GoldLost != 500 || m.Deaths[0].Clock != "6:50" {
		t.Fatalf("death misrecorded: %+v", m.Deaths)
	}
}

func TestKeyItemsAreLoggedOnceAndMovingSlotsIsNotAPurchase(t *testing.T) {
	tr, _ := newTestTracker(t)
	with := func(clock float64, items jsonMap) jsonMap {
		p := payload("9", stateInProgress, clock, "radiant", "none")
		p["items"] = items
		return p
	}
	tr.HandleUpdate(with(300, jsonMap{"slot0": jsonMap{"name": "item_blink"}, "slot1": jsonMap{"name": "empty"}, "stash0": jsonMap{"name": "item_black_king_bar"}}))
	// Moved to another slot, plus an item that is not on the key list.
	tr.HandleUpdate(with(310, jsonMap{"slot3": jsonMap{"name": "item_blink"}, "slot1": jsonMap{"name": "item_branches"}}))
	// Daedalus arrives under Valve's internal name.
	tr.HandleUpdate(with(900, jsonMap{"slot3": jsonMap{"name": "item_blink"}, "slot0": jsonMap{"name": "item_greater_crit"}}))

	log := tr.Status(false).Current.KeyItemLog
	if len(log) != 2 || log[0].Item != "blink" || log[0].Clock != "5:00" || log[1].Item != "greater_crit" {
		t.Fatalf("key item log wrong: %+v", log)
	}
}

func TestCheckpointsAreTakenAtTheirMinuteOnly(t *testing.T) {
	tr, _ := newTestTracker(t)
	// The draft reports a clock too; it must not stamp anything.
	tr.HandleUpdate(payload("10", draft, 400, "radiant", "none"))
	if tr.Status(false).Current.Checkpoints[5] != nil {
		t.Fatal("a checkpoint was taken off the draft clock")
	}
	tr.HandleUpdate(payload("10", stateInProgress, 601, "radiant", "none"))
	cps := tr.Status(false).Current.Checkpoints
	if cps[10] == nil || cps[10].LastHits != 120 {
		t.Fatalf("10-minute checkpoint missing: %+v", cps)
	}
	if cps[5] != nil {
		t.Fatal("the 5-minute mark was long past; stamping it now would record the wrong last hits")
	}
}

func TestStartingTheAppMidGameDoesNotBackfillEveryCheckpoint(t *testing.T) {
	tr, _ := newTestTracker(t)
	tr.HandleUpdate(payload("11", stateInProgress, 1830, "radiant", "none"))
	for minute, cp := range tr.Status(false).Current.Checkpoints {
		if cp != nil {
			t.Fatalf("checkpoint %d was stamped 30 minutes in with the current last hits", minute)
		}
	}
}

func TestComparisonsUseThePeerGroup(t *testing.T) {
	mk := func(id, gameType string, deaths int, lh10 int64) MatchSummary {
		return MatchSummary{MatchID: id, GameType: gameType, TotalDeaths: deaths, Checkpoints: map[int]*Checkpoint{10: {LastHits: lh10}}}
	}
	h := []MatchSummary{mk("a", "turbo", 8, 40), mk("b", "turbo", 6, 50), mk("c", "turbo", 2, 70), mk("d", "ranked", 1, 90)}
	RecomputeComparisons(h)

	c := h[2].Comparison
	if *h[2].GamesComparedAgainst != 2 {
		t.Fatalf("turbo game should be compared against the 2 other turbo games, got %d", *h[2].GamesComparedAgainst)
	}
	if c.Deaths.Verdict != "better" || !c.Deaths.IsBest || *c.Deaths.Avg != 7 {
		t.Fatalf("deaths comparison wrong: %+v", c.Deaths)
	}
	if cp := c.Checkpoints[10]; cp.Verdict != "better" || !cp.IsBest || *cp.Avg != 45 {
		t.Fatalf("last-hit comparison wrong: %+v", cp)
	}
	if h[3].Comparison.Deaths.Verdict != "no_data" {
		t.Fatal("a match with no peers has nothing to compare against")
	}
}

func TestEditingSessions(t *testing.T) {
	tr, store := newTestTracker(t)
	for _, id := range []string{"1", "2"} {
		tr.HandleUpdate(payload(id, stateInProgress, 600, "radiant", "none"))
		tr.HandleUpdate(payload(id, statePostGame, 1900, "radiant", "radiant"))
	}

	h, updated, err := store.SetHistoryGameType("1", "turbo")
	if err != nil || updated.GameType != "turbo" || h[0].GameType != "turbo" {
		t.Fatalf("retag failed: %v", err)
	}
	if *h[0].GamesComparedAgainst != 0 || *h[1].GamesComparedAgainst != 0 {
		t.Fatal("comparisons were not recomputed after the peer groups changed")
	}
	if _, _, err := store.SetHistoryGameType("1", "nonsense"); err == nil {
		t.Fatal("an unknown game type was accepted")
	}
	if _, _, err := store.SetHistoryGameType("404", "turbo"); err == nil {
		t.Fatal("retagging a missing match should fail")
	}

	h, err = store.SetHistoryNotes("2", "fed mid")
	if err != nil || h[1].Notes != "fed mid" {
		t.Fatalf("notes not saved: %v", err)
	}
	h, err = store.DeleteHistoryMatch("1")
	if err != nil || len(h) != 1 || h[0].MatchID != "2" || len(store.History()) != 1 {
		t.Fatalf("delete failed: %v %+v", err, h)
	}
	if _, err := store.DeleteHistoryMatch("1"); err == nil {
		t.Fatal("deleting a missing match should say so")
	}
}

func TestLiveMeansStillReporting(t *testing.T) {
	tr, _ := newTestTracker(t)
	clock := time.Date(2026, 10, 3, 20, 0, 0, 0, time.UTC)
	tr.now = func() time.Time { return clock }

	tr.HandleUpdate(payload("1", stateInProgress, 600, "radiant", "none"))
	if !tr.IsLive() {
		t.Fatal("an in-progress match that just reported is live")
	}
	// The player quits to the menu: menu payloads are ignored, so the match
	// state never changes. Only the silence shows it is over.
	clock = clock.Add(2 * time.Minute)
	tr.HandleUpdate(jsonMap{"player": jsonMap{"activity": "menu"}})
	if tr.IsLive() {
		t.Fatal("the overlay would stay up over the desktop after leaving a game")
	}
}

func TestClockFormatting(t *testing.T) {
	for secs, want := range map[float64]string{0: "0:00", 59.9: "0:59", 600: "10:00", -75: "-1:15", 3725: "62:05"} {
		if got := FmtClock(secs); got != want {
			t.Errorf("FmtClock(%v) = %s, want %s", secs, got, want)
		}
	}
	if s, ok := ParseClock("12:34"); !ok || s != 754 {
		t.Error("ParseClock(12:34)")
	}
	if _, ok := ParseClock("??:??"); ok {
		t.Error("an unknown clock parsed as a time")
	}
}

func TestKeyItemsUseValveInternalNames(t *testing.T) {
	for _, shop := range []string{"daedalus", "linkens_sphere", "aghanims_scepter", "eye_of_skadi"} {
		if IsKeyItem(shop) {
			t.Errorf("%s is a shop name, not a name GSI ever sends", shop)
		}
	}
	for _, internal := range []string{"greater_crit", "sphere", "ultimate_scepter", "skadi", "mage_slayer"} {
		if !IsKeyItem(internal) {
			t.Errorf("%s should be tracked", internal)
		}
	}
	if len(keyItemSet) != len(KeyItems) {
		t.Error("the key item list has a duplicate")
	}
}
