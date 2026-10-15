package core

import (
	"strings"
	"testing"
	"time"
)

// codClock lets a test move time.
func codClock(a *App, start time.Time) *time.Time {
	now := start
	a.Cod.now = func() time.Time { return now }
	return &now
}

func TestCodLogSaveEditDelete(t *testing.T) {
	a := newTestApp(t)
	codClock(a, time.Date(2026, 10, 15, 20, 0, 0, 0, time.UTC))
	for _, bad := range []CodMatch{
		{Game: "bo6", Mode: "Hardpoint", Result: "win"},
		{Game: "bo7", Result: "win"},
		{Game: "bo7", Mode: "Hardpoint", Result: "won"},
		{Game: "warzone", Mode: "Quads", Place: 0},
		{Game: "bo7", Mode: "Hardpoint", Result: "win", Kills: -1},
	} {
		if _, err := a.Cod.SaveMatch(bad); err == nil {
			t.Fatalf("accepted %+v", bad)
		}
	}
	m, err := a.Cod.SaveMatch(CodMatch{Game: "bo7", Mode: " Hardpoint ", Result: "win", Kills: 20, Deaths: 10, Place: 4})
	if err != nil || m.ID == "" || m.Mode != "Hardpoint" || m.Place != 0 || m.Date == 0 {
		t.Fatalf("saved %+v %v", m, err)
	}
	wz, _ := a.Cod.SaveMatch(CodMatch{Game: "warzone", Mode: "Battle Royale Quads", Result: "win", Place: 3, Kills: 5, Deaths: 1})
	if wz.Result != "" || wz.Place != 3 {
		t.Fatalf("warzone keeps a place, not a result: %+v", wz)
	}
	m.Kills = 25
	if e, err := a.Cod.SaveMatch(m); err != nil || e.Date != m.Date {
		t.Fatalf("edit %+v %v", e, err)
	}
	if got := a.Cod.Matches(); len(got) != 2 || got[1].Kills != 25 && got[0].Kills != 25 {
		t.Fatalf("list %+v", got)
	}
	if _, err := a.Cod.SaveMatch(CodMatch{ID: "gone", Game: "bo7", Mode: "TDM", Result: "loss"}); err == nil {
		t.Fatal("edited a match that isn't there")
	}
	if err := a.Cod.DeleteMatch(m.ID); err != nil || len(a.Cod.Matches()) != 1 {
		t.Fatalf("delete: %v %d", err, len(a.Cod.Matches()))
	}
}

func TestCodSessions(t *testing.T) {
	a := newTestApp(t)
	now := codClock(a, time.Date(2026, 10, 15, 20, 0, 0, 0, time.UTC))
	var ended []CodSession
	a.Cod.OnSessionEnd = func(s CodSession) { ended = append(ended, s) }
	tick := func(running bool, after time.Duration) {
		*now = now.Add(after)
		a.Cod.SessionTick(running)
	}
	// Two minutes open: a launcher check, dropped.
	tick(true, 0)
	tick(true, 2*time.Minute)
	tick(false, 5*time.Second)
	if len(a.Cod.Sessions()) != 0 || len(ended) != 0 {
		t.Fatalf("short session kept: %+v", a.Cod.Sessions())
	}
	// An hour and a half of play.
	tick(true, time.Minute)
	if !a.Cod.Overview().Playing {
		t.Fatal("not playing")
	}
	m, _ := a.Cod.SaveMatch(CodMatch{Game: "bo7", Mode: "Domination", Result: "loss", Kills: 10, Deaths: 12})
	for i := 0; i < 18; i++ {
		tick(true, 5*time.Minute)
	}
	tick(false, 5*time.Second)
	s := a.Cod.Sessions()
	if len(s) != 1 || s[0].Open || s[0].End-s[0].Start != 90*60 || len(ended) != 1 || m.Session != s[0].ID {
		t.Fatalf("session %+v ended %+v match %+v", s, ended, m)
	}
	if n := codNotification(ended[0]); !strings.Contains(n.Body, "1 h 30 min") || n.View != "cod-matches" {
		t.Fatalf("notification %+v", n)
	}
	// Logged during the session: nothing left to log.
	if ov := a.Cod.Overview(); ov.ToLog != nil || ov.WeekMinutes != 90 || ov.Playing {
		t.Fatalf("overview %+v", ov)
	}
	// A second session with nothing logged asks for its matches, and a match
	// logged after it ends joins it.
	tick(true, time.Hour)
	tick(true, 30*time.Minute)
	tick(false, time.Second)
	ov := a.Cod.Overview()
	if ov.ToLog == nil || ov.ToLog.ID != a.Cod.Sessions()[0].ID {
		t.Fatalf("to log %+v", ov.ToLog)
	}
	*now = now.Add(20 * time.Minute)
	late, _ := a.Cod.SaveMatch(CodMatch{Game: "bo7", Mode: "TDM", Result: "win", Kills: 30, Deaths: 10})
	if late.Session != ov.ToLog.ID || a.Cod.Overview().ToLog != nil {
		t.Fatalf("late match %+v", late)
	}
}

// codSeed logs n matches of a game, one session, a minute apart.
func codSeed(t *testing.T, a *App, now *time.Time, ms ...CodMatch) []CodMatch {
	t.Helper()
	var out []CodMatch
	for _, m := range ms {
		*now = now.Add(time.Minute)
		saved, err := a.Cod.SaveMatch(m)
		if err != nil {
			t.Fatal(err)
		}
		out = append(out, saved)
	}
	return out
}

func TestCodOverviewGoalsAndReview(t *testing.T) {
	a := newTestApp(t)
	now := codClock(a, time.Date(2026, 10, 15, 18, 0, 0, 0, time.UTC))
	a.Cod.SessionTick(true) // all played in one session
	var base []CodMatch
	for i := 0; i < 8; i++ {
		res := "win"
		if i%2 == 1 {
			res = "loss"
		}
		mode := "Hardpoint"
		if i >= 4 {
			mode = "Search and Destroy"
			res = "loss"
		}
		base = append(base, CodMatch{Game: "bo7", Mode: mode, Result: res, Kills: 20 + i, Deaths: 15, Score: 3000 + 100*i})
	}
	codSeed(t, a, now, base...)
	ov := a.Cod.Overview()
	if len(ov.Stats) != 1 || ov.Stats[0].Matches != 8 || ov.Stats[0].Wins != 2 || ov.Stats[0].KD < 1.5 || len(ov.Stats[0].Form) != 8 {
		t.Fatalf("stats %+v", ov.Stats)
	}
	goals := a.CodGoals("")
	if len(goals.Goals) != 3 || goals.Goals[1].Key != "deaths" || !goals.Goals[1].AtMost {
		t.Fatalf("goals %+v", goals)
	}
	// A bad S&D loss, the third loss in a row.
	bad := codSeed(t, a, now, CodMatch{Game: "bo7", Mode: "Search and Destroy", Result: "loss", Kills: 4, Deaths: 14})[0]
	r, err := a.CodReview("")
	if err != nil || r.MatchKey != bad.ID || r.Won == nil || *r.Won {
		t.Fatalf("review %+v %v", r, err)
	}
	kinds := map[string]bool{}
	for _, p := range r.Wrong {
		kinds[p.Kind] = true
	}
	for _, k := range []string{"low_kd", "tilt", "weak_mode"} {
		if !kinds[k] {
			t.Errorf("missing %s in %+v", k, r.Wrong)
		}
	}
	if !strings.HasPrefix(r.Verdict, "Lost. Biggest issue") || r.Goals == nil {
		t.Fatalf("verdict %q goals %+v", r.Verdict, r.Goals)
	}
	w, _ := a.CodWorkOn()
	if w.Game != "cod" || w.Matches != 9 {
		t.Fatalf("work on %+v", w)
	}
	// Warzone: placement and damage.
	var wz []CodMatch
	for i := 0; i < 6; i++ {
		wz = append(wz, CodMatch{Game: "warzone", Mode: "Battle Royale Quads", Place: 5 + i, Kills: 6, Deaths: 1, Damage: 2000 + 100*i})
	}
	codSeed(t, a, now, wz...)
	out := codSeed(t, a, now, CodMatch{Game: "warzone", Mode: "Battle Royale Quads", Place: 60, Kills: 0, Deaths: 1, Damage: 300})[0]
	r, _ = a.CodReview(out.ID)
	kinds = map[string]bool{}
	for _, p := range r.Wrong {
		kinds[p.Kind] = true
	}
	if !kinds["early_out"] || !kinds["low_damage"] || !strings.HasPrefix(r.Verdict, "#60") {
		t.Fatalf("warzone review %+v", r)
	}
	if g := a.CodGoals("warzone"); len(g.Goals) != 4 || g.Goals[3].Key != "place" {
		t.Fatalf("warzone goals %+v", g)
	}
}

func TestCodGameModeOffersUpdate(t *testing.T) {
	if m := missingPriorityGames([]string{"dota2.exe", "project8.exe", "cs2.exe", "Overwatch.exe", "TslGame.exe"}); len(m) != 1 || m[0] != "Call of Duty" {
		t.Fatalf("missing %v", m)
	}
}
