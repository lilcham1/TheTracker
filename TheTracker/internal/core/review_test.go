package core

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestDotaReview(t *testing.T) {
	me := uint64(42)
	f := newFakeAPI(t, map[string]any{
		"/heroes": odHeroes, // Anti-Mage 1, Axe 2, Crystal Maiden 5
		"/matches/100": map[string]any{
			"match_id": 100, "duration": 2400, "start_time": 1700000000, "radiant_win": false, "game_mode": 22, "version": 22,
			"players": []any{map[string]any{
				"account_id": 42, "hero_id": 1, "player_slot": 0, "kills": 4, "deaths": 9, "assists": 3, "last_hits": 220, "gold_per_min": 410,
				"lh_t": []any{0, 4, 9, 14, 19, 24, 29, 33, 37, 41, 44}, "lane_efficiency_pct": 41.0,
				"benchmarks": map[string]any{
					"gold_per_min":        map[string]any{"raw": 410.0, "pct": 0.12},
					"last_hits_per_min":   map[string]any{"raw": 5.5, "pct": 0.2},
					"deaths_per_min":      map[string]any{"raw": 0.225, "pct": 0.05},
					"hero_damage_per_min": map[string]any{"raw": 900.0, "pct": 0.9},
				},
			}},
		},
	})
	a := newTestApp(t)
	a.Dota.api = &service{Name: "OpenDota", Base: f.srv.URL, Attempts: 1}
	a.Store.SaveLink("dota", Link{AccountID: &me})
	// TheTracker recorded the match live, with the reasons for two deaths.
	hero := "npc_dota_hero_antimage"
	a.Store.SaveHistory([]MatchSummary{{MatchID: "100", HeroName: &hero, TotalGoldLost: 2600, Deaths: []Death{
		{Clock: "12:00", Reasons: []string{"Your Black King Bar was ready: use it before the fight turns", "No TP scroll: buy one"}},
		{Clock: "13:30", Reasons: []string{"Your Manta Style was ready: use it", "2nd death in 5 minutes: play safer"}},
	}}})

	r, err := a.DotaReview(100)
	if err != nil {
		t.Fatal(err)
	}
	if r.Hero != "Anti-Mage" || r.Won == nil || *r.Won || r.Deaths != 9 {
		t.Fatalf("header: %+v", r)
	}
	kinds := map[string]bool{}
	for _, p := range r.Wrong {
		kinds[p.Kind] = true
	}
	if len(r.Wrong) != 4 || !kinds["deaths_per_min"] || !kinds["gold_per_min"] {
		t.Fatalf("the biggest issues, four at most: %+v", r.Wrong)
	}
	if len(r.Good) != 1 || r.Good[0].Kind != "hero_damage_per_min" || !strings.Contains(r.Good[0].Detail, "better than 90% of Anti-Mage players") {
		t.Fatalf("what went well: %+v", r.Good)
	}
	if len(r.DeathLog) != 2 || len(r.DeathLog[0].Reasons) != 2 {
		t.Fatalf("deaths with reasons: %+v", r.DeathLog)
	}
	if !strings.HasPrefix(r.Verdict, "Lost. Biggest issue:") {
		t.Fatalf("verdict: %q", r.Verdict)
	}
}

func TestDeadlockReviewFromMetadata(t *testing.T) {
	stats := func(at, creeps, possible, dealt, taken, lost int) map[string]any {
		return map[string]any{"time_stamp_s": at, "creep_kills": creeps, "possible_creeps": possible, "player_damage": dealt, "player_damage_taken": taken, "gold_death_loss": lost}
	}
	meta := jsonMap{"match_info": map[string]any{
		"match_id": 7, "duration_s": 1800, "start_time": 1700000000, "winning_team": 1,
		"players": []any{
			map[string]any{"account_id": 9, "player_slot": 1, "team": 0, "hero_id": 13, "kills": 2, "deaths": 6, "assists": 3, "net_worth": 20000, "last_hits": 150,
				"death_details": []any{
					map[string]any{"game_time_s": 200, "killer_player_slot": 7, "time_to_kill_s": 1.2},
					map[string]any{"game_time_s": 450, "killer_player_slot": 7, "time_to_kill_s": 6.0},
					map[string]any{"game_time_s": 900, "killer_player_slot": 7, "time_to_kill_s": 1.5},
					map[string]any{"game_time_s": 1300, "killer_player_slot": 8, "time_to_kill_s": 3.0},
				},
				"stats": []any{stats(300, 10, 30, 1000, 2000, 0), stats(600, 20, 50, 2000, 5000, 500), stats(1800, 90, 200, 9000, 21000, 4200)},
			},
			map[string]any{"account_id": 10, "player_slot": 7, "team": 1, "hero_id": 2},
			map[string]any{"account_id": 11, "player_slot": 8, "team": 1, "hero_id": 3},
		},
	}}
	// As it arrives: decoded from JSON, numbers as floats.
	b, _ := json.Marshal(meta)
	meta = jsonMap{}
	json.Unmarshal(b, &meta)
	raw, err := deadlockReviewFrom(meta, 9)
	if err != nil {
		t.Fatal(err)
	}
	if raw.Won || raw.HeroID != 13 || raw.LaneCreeps != 20 || raw.LanePossible != 50 || raw.SoulsLostToDeaths != 4200 || len(raw.DeathLog) != 4 || raw.DeathLog[0].KillerHero != 2 {
		t.Fatalf("read from the metadata: %+v", raw)
	}
	if _, err := deadlockReviewFrom(meta, 999); err == nil {
		t.Fatal("an account not in the match is an error")
	}
}

func TestTallyWorkOn(t *testing.T) {
	p := func(kind string, v float64) ReviewPoint {
		return ReviewPoint{Kind: kind, Title: kind, Habit: "do " + kind, Value: &v}
	}
	// Newest first, as the history comes.
	reviews := []MatchReview{
		{Wrong: []ReviewPoint{p("deaths", 9), p("lane", 40)}, Good: []ReviewPoint{p("damage", 900)}},
		{Wrong: []ReviewPoint{p("deaths", 8)}, Good: []ReviewPoint{p("damage", 800)}},
		{Wrong: []ReviewPoint{p("deaths", 7), p("lane", 45)}},
		{Wrong: []ReviewPoint{p("no_tp", 1)}, Good: []ReviewPoint{p("damage", 700)}},
	}
	w := tallyWorkOn("dota", reviews)
	if w.Matches != 4 || len(w.Items) != 2 || w.Items[0].Kind != "deaths" || w.Items[0].Count != 3 || w.Items[1].Kind != "lane" {
		t.Fatalf("recurring issues, a one-off left out: %+v", w.Items)
	}
	if s := w.Items[0].Series; len(s) != 3 || s[0] != 7 || s[2] != 9 {
		t.Fatalf("series oldest first: %v", s)
	}
	if len(w.Strengths) != 1 || w.Strengths[0].Kind != "damage" {
		t.Fatalf("strengths in most matches: %+v", w.Strengths)
	}
}
