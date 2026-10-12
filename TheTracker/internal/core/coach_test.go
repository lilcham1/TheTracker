package core

import (
	"strings"
	"testing"
)

func TestCoachAsksOnceAndKeepsTheAnswer(t *testing.T) {
	me := uint64(42)
	f := newFakeAPI(t, map[string]any{
		"/heroes": odHeroes,
		"/matches/100": map[string]any{
			"match_id": 100, "duration": 2400, "start_time": 1700000000, "radiant_win": false, "game_mode": 22,
			"players": []any{map[string]any{"account_id": 42, "hero_id": 1, "player_slot": 0, "kills": 4, "deaths": 9, "assists": 3, "last_hits": 220, "gold_per_min": 410,
				"benchmarks": map[string]any{"deaths_per_min": map[string]any{"raw": 0.225, "pct": 0.05}}}},
		},
	})
	a := newTestApp(t)
	a.Dota.api = &service{Name: "OpenDota", Base: f.srv.URL, Attempts: 1}
	a.Store.SaveLink("dota", Link{AccountID: &me})
	cloud := newFakeConvex(t)
	a.Cloud.base = cloud.srv.URL

	// Nothing on the service yet: the app says so, and never errors.
	r, err := a.Coach("dota", 100, false)
	if err != nil || r.Status != "not_set_up" {
		t.Fatalf("not set up: %+v %v", r, err)
	}
	cloud.mu.Lock()
	cloud.twitch = map[string]any{"ok": true} // any value: the stub answers the coach when it isn't nil
	cloud.mu.Unlock()
	r, err = a.Coach("dota", 100, false)
	if err != nil || r.Status != "ready" || r.Note == nil || r.Note.Focus[0].Title != "BKB before the fight" || r.Note.Model != "claude-opus-5-5" {
		t.Fatalf("ready: %+v %v", r, err)
	}
	// Kept locally: the service isn't asked again.
	cloud.mu.Lock()
	cloud.twitch = nil
	cloud.mu.Unlock()
	if r, _ = a.Coach("dota", 100, false); r.Status != "ready" {
		t.Fatalf("cached: %+v", r)
	}
	// Off in Settings.
	a.Store.SaveCoach(CoachPrefs{AIOff: true})
	if r, _ = a.Coach("dota", 100, false); r.Status != "off" {
		t.Fatalf("off: %+v", r)
	}
	// The facts carry the review and nothing personal.
	rev, _ := a.DotaReview(100)
	facts := coachFacts(rev)
	if facts["hero"] != "Anti-Mage" || facts["result"] != "lost" || facts["deaths"] != 9 {
		t.Fatalf("facts: %+v", facts)
	}
	for k := range facts {
		if strings.Contains(strings.ToLower(k), "account") || strings.Contains(strings.ToLower(k), "steam") {
			t.Fatalf("facts carry %s", k)
		}
	}
}
