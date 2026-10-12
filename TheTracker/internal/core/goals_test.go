package core

import (
	"math"
	"testing"
)

func TestBenchAtAndQuantile(t *testing.T) {
	pts := []benchPoint{{0.5, 500}, {0.1, 300}, {0.9, 700}}
	if v := benchAt(pts, 0.7); v != 600 {
		t.Fatalf("between published points: %v", v)
	}
	if benchAt(pts, 0.05) != 300 || benchAt(pts, 0.99) != 700 {
		t.Fatal("outside: the nearest end")
	}
	if q := quantile([]float64{10, 20, 30, 40, 50}, 0.75); q != 40 {
		t.Fatalf("quantile: %v", q)
	}
}

func TestDotaGoalsForRoleAndHero(t *testing.T) {
	bench := func(lo, hi float64) []any {
		return []any{map[string]any{"percentile": 0.1, "value": lo}, map[string]any{"percentile": 0.9, "value": hi}}
	}
	f := newFakeAPI(t, map[string]any{
		"/heroes": odHeroes, // Anti-Mage 1
		"/benchmarks": map[string]any{"result": map[string]any{
			"gold_per_min":      bench(300, 700), // 0.7 → 600
			"last_hits_per_min": bench(3, 11),    // 0.7 → 9
			"deaths_per_min":    bench(0.1, 0.5), // 0.3 → 0.2 (fewer is better)
			"assists_per_min":   bench(0.1, 0.5), // 0.7 → 0.4
		}},
		"/heroes/1/itemPopularity": map[string]any{
			"early_game_items": map[string]any{"36": 90},
			"mid_game_items":   map[string]any{"116": 120, "63": 200, "208": 80, "48": 50},
		},
		"/constants/items": map[string]any{
			"black_king_bar": map[string]any{"id": 116, "dname": "Black King Bar", "cost": 4050},
			"power_treads":   map[string]any{"id": 63, "dname": "Power Treads", "cost": 1400},
			"manta":          map[string]any{"id": 208, "dname": "Manta Style", "cost": 4600},
			"glimmer_cape":   map[string]any{"id": 48, "dname": "Glimmer Cape", "cost": 1950},
			"magic_wand":     map[string]any{"id": 36, "dname": "Magic Wand", "cost": 450},
		},
	})
	a := newTestApp(t)
	a.Dota.api = &service{Name: "OpenDota", Base: f.srv.URL, Attempts: 1}

	core, err := a.DotaGoals(1, "core", "push")
	if err != nil {
		t.Fatal(err)
	}
	byKey := map[string]Goal{}
	for _, g := range core.Goals {
		byKey[g.Key] = g
	}
	if g := byKey["lh10"]; g.Target != 72 || g.At != 600 {
		t.Fatalf("last hits at 10: 9 a minute × 10 × 0.8: %+v", g)
	}
	if g := byKey["nw10"]; g.Target != 5100 {
		t.Fatalf("net worth at 10: 600 + 600×10×0.75: %+v", g)
	}
	if g := byKey["deaths"]; math.Abs(g.Target-0.2) > 1e-9 || !g.AtMost {
		t.Fatalf("deaths: a rate at the better end: %+v", g)
	}
	// The usual big items, boots left out: BKB, then Manta.
	bkb, manta := byKey["item_black_king_bar"], byKey["item_manta"]
	if bkb.Target != roundTo((1300+4050-600)/(10*earlyGoldShare)) || manta.Target != roundTo((1300+4050+4600-600)/(10*earlyGoldShare)) || byKey["item_power_treads"].Key != "" {
		t.Fatalf("item timings: %+v / %+v", bkb, manta)
	}

	sup, _ := a.DotaGoals(1, "support", "push")
	keys := map[string]bool{}
	for _, g := range sup.Goals {
		keys[g.Key] = true
	}
	if keys["lh10"] || !keys["assists20"] || !keys["item_glimmer_cape"] || sup.Role != "Support" {
		t.Fatalf("support goals: %+v", sup)
	}

	// Checking a finished match against them.
	checked := checkDotaGoals(core, dotaGoalFacts{Clock: 2400, Ended: true, Deaths: 10,
		LastHits: map[int]float64{10: 80}, NetWorth: map[int]float64{10: 4000},
		ItemTimes: map[string]float64{"black_king_bar": 600}})
	state := map[string]string{}
	for _, g := range checked.Goals {
		state[g.Key] = g.State
	}
	if state["lh10"] != "met" || state["nw10"] != "missed" || state["item_black_king_bar"] != "met" || state["item_manta"] != "missed" || state["deaths"] != "missed" || state["nw20"] != "unknown" {
		t.Fatalf("states: %v", state)
	}
	if pts := goalPoints(checked); len(pts) != 3 {
		t.Fatalf("missed goals with a value become points (net worth, deaths; Manta never bought has none): %+v", pts)
	}

	// Mid-match: progress so far on goals still ahead.
	live := checkDotaGoals(core, dotaGoalFacts{Clock: 300, Deaths: 0, NowLastHits: ptr(31.0), LastHits: map[int]float64{}, NetWorth: map[int]float64{}, Assists: map[int]float64{}})
	for _, g := range live.Goals {
		if g.Key == "lh10" && (g.State != "pending" || g.Value == nil || *g.Value != 31) {
			t.Fatalf("progress toward last hits at 10: %+v", g)
		}
	}
}

func TestDeadlockTierTimes(t *testing.T) {
	costs := map[int64]int64{1: 800, 2: 3200, 3: 6400}
	t3, t4 := tierTimes([]deadlockBuy{{At: 100, ID: 1}, {At: 900, ID: 2}, {At: 700, ID: 2}, {At: 1500, ID: 3}}, costs)
	if t3 != 700 || t4 != 1500 {
		t.Fatalf("first 3,200 and 6,400 items: %d %d", t3, t4)
	}
}

func TestSomeoneElsesGameIsWatching(t *testing.T) {
	a := newTestApp(t)
	me := uint64(42)
	a.Store.SaveLink("dota", Link{AccountID: &me})
	send := func(steamID string) {
		a.Tracker.HandleUpdate(jsonMap{
			"map":    jsonMap{"matchid": "5", "clock_time": 300.0, "game_state": stateInProgress},
			"player": jsonMap{"activity": "playing", "steamid": steamID},
			"hero":   jsonMap{"name": "npc_dota_hero_axe", "alive": true},
		})
	}
	send("76561197960265828") // account 100: not the linked one
	if s := a.Tracker.Status(false); !s.Watching || s.Current != nil {
		t.Fatalf("another player's game: watching, not recorded: %+v", s)
	}
	send("76561197960265770") // account 42
	if s := a.Tracker.Status(false); s.Watching || s.Current == nil {
		t.Fatalf("the player's own game: %+v", s)
	}
}
