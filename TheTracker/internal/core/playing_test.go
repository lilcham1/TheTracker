package core

import "testing"

func TestActiveGame(t *testing.T) {
	a := newTestApp(t)
	running := map[uint32]string{}
	old := runningPrograms
	runningPrograms = func() map[uint32]string { return running }
	t.Cleanup(func() { runningPrograms = old })
	a.Store.UpdatePrefs(func(p *Prefs) { p.Games = Games{Dota: true, Deadlock: true, CS2: false, Overwatch: true, Chosen: true} })

	if g := a.activeGame(); g != "" {
		t.Fatalf("nothing running: %q", g)
	}
	running[10] = "project8"
	if g := a.activeGame(); g != "Deadlock" {
		t.Fatalf("Deadlock open: %q", g)
	}
	running = map[uint32]string{11: "cs2"}
	if g := a.activeGame(); g != "" {
		t.Fatalf("CS2 is switched off in the app: %q", g)
	}
	running = map[uint32]string{12: "Overwatch"}
	if g := a.activeGame(); g != "Overwatch" {
		t.Fatalf("Overwatch open: %q", g)
	}
	// Dota counts only during a match, not with the game merely open.
	running = map[uint32]string{13: "dota2"}
	if g := a.activeGame(); g != "" {
		t.Fatalf("Dota open without a match: %q", g)
	}
	a.Tracker.HandleUpdate(jsonMap{
		"map":    jsonMap{"matchid": "5", "clock_time": 100.0, "game_state": stateInProgress},
		"player": jsonMap{"activity": "playing"},
		"hero":   jsonMap{"name": "npc_dota_hero_axe"},
	})
	if g := a.activeGame(); g != "Dota 2" {
		t.Fatalf("Dota match: %q", g)
	}
}
