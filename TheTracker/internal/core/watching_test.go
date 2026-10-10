package core

import "testing"

func TestWatchingAGameIsNotRecorded(t *testing.T) {
	a := newTestApp(t)
	tr := a.Tracker
	spectate := func(clock float64, state string) {
		tr.HandleUpdate(jsonMap{
			"map": jsonMap{"matchid": "555", "clock_time": clock, "game_state": state, "win_team": "radiant"},
			// Spectating: every player, by team, and nothing about "you".
			"player": jsonMap{
				"team2": jsonMap{"player0": jsonMap{"steamid": "1", "name": "A", "kills": 3.0}},
				"team3": jsonMap{"player5": jsonMap{"steamid": "2", "name": "B", "kills": 1.0}},
			},
			"hero": jsonMap{"team2": jsonMap{"player0": jsonMap{"name": "npc_dota_hero_axe"}}},
		})
	}
	spectate(600, stateInProgress)
	s := tr.Status(false)
	if !s.Watching || s.Live || s.Current != nil {
		t.Fatalf("spectating: watching, not live, no match: %+v", s)
	}
	spectate(2400, statePostGame)
	if len(a.Store.LoadHistory()) != 0 {
		t.Fatal("a watched game must not be saved")
	}

	// A replay: a match id and a clock, but no player activity and no hero.
	tr.HandleUpdate(jsonMap{
		"map":    jsonMap{"matchid": "556", "clock_time": 900.0, "game_state": stateInProgress},
		"player": jsonMap{},
		"hero":   jsonMap{},
	})
	if s := tr.Status(false); !s.Watching || s.Current != nil {
		t.Fatalf("a replay reads as watching: %+v", s)
	}

	// Then the player's own match: tracked as always.
	tr.HandleUpdate(jsonMap{
		"map":    jsonMap{"matchid": "557", "clock_time": 60.0, "game_state": stateInProgress},
		"player": jsonMap{"activity": "playing", "last_hits": 2.0},
		"hero":   jsonMap{"name": "npc_dota_hero_lina", "alive": true},
	})
	if s := tr.Status(false); s.Watching || !s.Live || s.Current == nil || s.Current.MatchID != "557" {
		t.Fatalf("playing again: %+v", s)
	}
}

func TestWatchedGamesDroppedFromHistory(t *testing.T) {
	a := newTestApp(t)
	hero := "npc_dota_hero_lina"
	zero := int64(0)
	a.Store.SaveHistory([]MatchSummary{
		{MatchID: "watched", Duration: "47:18", LastHits: &zero},
		{MatchID: "played", HeroName: &hero, LastHits: &zero},
		{MatchID: "no hero but kills", Kills: 4},
	})
	a.dropWatchedGames()
	h := a.Store.LoadHistory()
	if len(h) != 2 || h[0].MatchID != "played" || h[1].MatchID != "no hero but kills" {
		t.Fatalf("only the watched game goes: %+v", h)
	}
}
