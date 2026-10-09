package core

import "testing"

func TestGuessRole(t *testing.T) {
	cm := "npc_dota_hero_crystal_maiden"
	mk := func(edit func(m *MatchState)) *MatchState {
		m := newMatchState("1", &cm, "")
		if edit != nil {
			edit(m)
		}
		return m
	}
	cp := func(lh int64) *Checkpoint { return &Checkpoint{LastHits: lh} }
	support := roleHint{"support", "hero"}
	cases := []struct {
		name string
		m    *MatchState
		hint roleHint
		want LiveRole
	}{
		{"nothing known", mk(nil), roleHint{}, LiveRole{"core", "default"}},
		{"the hero", mk(nil), support, LiveRole{"support", "hero"}},
		{"wards beat the hero", mk(func(m *MatchState) { m.SupportItems = true }), roleHint{"core", "your_games"}, LiveRole{"support", "wards"}},
		{"few last hits at 5", mk(func(m *MatchState) { m.Checkpoints[5] = cp(6) }), roleHint{"core", "hero"}, LiveRole{"support", "last_hits"}},
		{"many last hits at 5 beat wards", mk(func(m *MatchState) { m.Checkpoints[5] = cp(25); m.SupportItems = true }), support, LiveRole{"core", "last_hits"}},
		{"in between at 5 proves nothing", mk(func(m *MatchState) { m.Checkpoints[5] = cp(12) }), support, LiveRole{"support", "hero"}},
		{"10:00 outranks 5:00", mk(func(m *MatchState) { m.Checkpoints[5] = cp(6); m.Checkpoints[10] = cp(45) }), support, LiveRole{"core", "last_hits"}},
		{"the player's choice wins", mk(func(m *MatchState) { m.Checkpoints[10] = cp(80); m.RoleChoice = "support" }), roleHint{}, LiveRole{"support", "chosen"}},
	}
	for _, c := range cases {
		if got := guessRole(c.m, c.hint); got != c.want {
			t.Errorf("%s: got %+v, want %+v", c.name, got, c.want)
		}
	}
}

func TestHeroRoleHint(t *testing.T) {
	cm, jug := "npc_dota_hero_crystal_maiden", "npc_dota_hero_juggernaut"
	meta := &DotaMeta{Heroes: []MetaHero{{Slug: "crystal_maiden", Position: "hard_support"}, {Slug: "juggernaut", Position: "carry"}}}
	if h := heroRoleHint(cm, nil, meta); h != (roleHint{"support", "hero"}) {
		t.Fatalf("the hero's usual position: %+v", h)
	}
	if h := heroRoleHint(jug, nil, nil); h != (roleHint{}) {
		t.Fatalf("no meta, no games: %+v", h)
	}
	game := func(hero string, lh int64) MatchSummary {
		return MatchSummary{HeroName: &hero, Checkpoints: map[int]*Checkpoint{10: {LastHits: lh}}}
	}
	// Three games of Crystal Maiden farmed as a core outweigh her usual role.
	hist := []MatchSummary{game(cm, 50), game(cm, 45), game(cm, 12), game(jug, 3)}
	if h := heroRoleHint(cm, hist, meta); h != (roleHint{"core", "your_games"}) {
		t.Fatalf("the player's own games: %+v", h)
	}
	// Two games aren't enough to go on.
	if h := heroRoleHint(cm, hist[:2], meta); h != (roleHint{"support", "hero"}) {
		t.Fatalf("too few games: %+v", h)
	}
}

func TestSupportItemsAndRoleChoice(t *testing.T) {
	a := newTestApp(t)
	a.Store.WriteCache("od_meta", DotaMeta{Heroes: []MetaHero{{Slug: "juggernaut", Position: "carry"}}})
	send := func(clock float64, item string, lastHits float64) {
		a.Tracker.HandleUpdate(jsonMap{
			"map":    jsonMap{"matchid": "42", "clock_time": clock, "game_state": stateInProgress},
			"player": jsonMap{"activity": "playing", "last_hits": lastHits},
			"hero":   jsonMap{"name": "npc_dota_hero_juggernaut", "alive": true},
			"items":  jsonMap{"slot0": jsonMap{"name": "item_" + item}},
		})
	}
	role := func() LiveRole { return *a.Live(false).Role }

	send(30, "tango", 0)
	if r := role(); r != (LiveRole{"core", "hero"}) {
		t.Fatalf("Juggernaut reads as a core: %+v", r)
	}
	// A sentry bought at 6:00 is a core's purchase too (with a core's farm
	// at the 5:00 mark, taken by this update).
	send(360, "ward_sentry", 12)
	if r := role(); r.Role != "core" {
		t.Fatalf("late wards must not count: %+v", r)
	}

	a.Tracker.current = nil
	send(-60, "ward_observer", 0)
	send(10, "tango", 0) // the ward was placed
	if r := role(); r != (LiveRole{"support", "wards"}) {
		t.Fatalf("wards bought before the horn: %+v", r)
	}
	a.Tracker.SetRole("core")
	if r := role(); r != (LiveRole{"core", "chosen"}) {
		t.Fatalf("the player's choice: %+v", r)
	}
	a.Tracker.SetRole("auto")
	if r := role(); r.Why != "wards" {
		t.Fatalf("back to auto: %+v", r)
	}
}

func TestRoleModeForOldSettings(t *testing.T) {
	read := func(until string) string {
		p, err := parsePrefs([]byte(`{"overlay":{"dota":{"until":` + until + `}}}`))
		if err != nil {
			t.Fatal(err)
		}
		return p.Overlay.Dota.RoleMode
	}
	if m := read(`{"stack":10,"bounty":12,"lotus":15,"power":0,"wisdom":0}`); m != "auto" {
		t.Fatalf("the core preset follows the role: %s", m)
	}
	if m := read(`{"stack":20,"bounty":20,"lotus":20,"power":0,"wisdom":0}`); m != "auto" {
		t.Fatalf("the support preset follows the role: %s", m)
	}
	if m := read(`{"stack":30,"bounty":12,"lotus":15,"power":0,"wisdom":0}`); m != "fixed" {
		t.Fatalf("hand-tuned windows stay: %s", m)
	}
	if m := read(`{"stack":0,"bounty":0,"lotus":0,"power":0,"wisdom":0}`); m != "fixed" {
		t.Fatalf("everything, whole game stays: %s", m)
	}
	if defaultOverlay().sanitized().Dota.RoleMode != "auto" {
		t.Fatal("a new install follows the role")
	}
	o := defaultOverlay()
	o.Dota.RoleMode = "fixed"
	if o.sanitized().Dota.RoleMode != "fixed" {
		t.Fatal("a saved choice is kept")
	}
}
