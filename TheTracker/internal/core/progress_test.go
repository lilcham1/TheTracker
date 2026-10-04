package core

import (
	"testing"
	"time"
)

// ---------- Overwatch progress ----------

func TestOverwatchProgressIsTheDifferenceBetweenLooks(t *testing.T) {
	store := NewStore(t.TempDir())
	stat := func(played, won int, secs int) map[string]any {
		return map[string]any{"games_played": played, "games_won": won, "games_lost": played - won, "time_played": secs, "winrate": 50.0, "kda": 2.0}
	}
	rank := func(division string, tier int) map[string]any {
		return map[string]any{"username": "Me", "competitive": map[string]any{"pc": map[string]any{"tank": map[string]any{"division": division, "tier": tier}}}}
	}
	routes := map[string]any{
		"/heroes": []any{
			map[string]any{"key": "ana", "name": "Ana", "role": "support"},
			map[string]any{"key": "genji", "name": "Genji", "role": "damage"},
		},
		"/players/me/summary": rank("gold", 3),
	}
	set := func(general map[string]any, heroes map[string]any) {
		routes["/players/me/stats/summary"] = map[string]any{"general": general, "heroes": heroes}
	}
	f := newFakeAPI(t, routes)
	o := NewOverwatch(store)
	o.api = &service{Name: "The Overwatch API", Base: f.srv.URL, Attempts: 1}
	clock := time.Date(2026, 10, 1, 18, 0, 0, 0, time.UTC)
	o.now = func() time.Time { return clock }
	look := func() {
		t.Helper()
		if _, err := o.Overview("all", true); err != nil {
			t.Fatal(err)
		}
	}

	if p := o.Progress("all"); p.Since != nil || len(p.Sessions) != 0 {
		t.Fatalf("with no profile there is nothing to show: %+v", p)
	}
	o.SetLink(OwLink{PlayerID: "me", Name: "Me"})

	// First look: tracking begins, nothing to compare with yet.
	set(stat(100, 50, 36000), map[string]any{"ana": stat(60, 30, 20000), "genji": stat(40, 20, 16000)})
	look()
	p := o.Progress("all")
	if p.Since == nil || *p.Since != clock.Unix() || len(p.Sessions) != 0 || len(p.Ranks) != 1 {
		t.Fatalf("the first look is a baseline: %+v", p)
	}

	// The same totals an hour later leave no trace.
	clock = clock.Add(time.Hour)
	look()
	if n := len(o.snapshots("me")); n != 1 {
		t.Fatalf("an unchanged profile must not add a snapshot, have %d", n)
	}

	// Three games on Ana, two won; then one more on Genji 40 minutes later:
	// one sitting.
	clock = clock.Add(time.Hour)
	set(stat(103, 52, 37800), map[string]any{"ana": stat(63, 32, 21800), "genji": stat(40, 20, 16000)})
	look()
	clock = clock.Add(40 * time.Minute)
	set(stat(104, 52, 38400), map[string]any{"ana": stat(63, 32, 21800), "genji": stat(41, 20, 16600)})
	look()
	// The next day: two games, and a rank up.
	clock = clock.Add(22 * time.Hour)
	routes["/players/me/summary"] = rank("gold", 2)
	set(stat(106, 54, 39600), map[string]any{"ana": stat(63, 32, 21800), "genji": stat(43, 22, 17800)})
	look()

	p = o.Progress("all")
	if len(p.Sessions) != 2 {
		t.Fatalf("expected two sittings, got %+v", p.Sessions)
	}
	today, before := p.Sessions[0], p.Sessions[1]
	if today.Games != 2 || today.Won != 2 || today.Lost != 0 || today.Time != 1200 || len(today.Heroes) != 1 || today.Heroes[0].Name != "Genji" {
		t.Fatalf("newest sitting first, with only the heroes played: %+v", today)
	}
	if before.Games != 4 || before.Won != 2 || before.Lost != 2 || before.Time != 2400 {
		t.Fatalf("changes close together are one sitting: %+v", before)
	}
	if len(before.Heroes) != 2 || before.Heroes[0].Key != "ana" || before.Heroes[0].Games != 3 || before.Heroes[0].Won != 2 || before.Heroes[1].Games != 1 {
		t.Fatalf("per-hero changes, most played first: %+v", before.Heroes)
	}
	if p.Total.Games != 6 || p.Total.Won != 4 || len(p.Total.Heroes) != 2 {
		t.Fatalf("total since tracking began: %+v", p.Total)
	}
	if len(p.Ranks) != 2 || p.Ranks[0].Ranks[0].Tier != 2 || p.Ranks[1].Ranks[0].Tier != 3 {
		t.Fatalf("rank changes, newest first: %+v", p.Ranks)
	}

	// A new season resets the totals: that is not minus a hundred games.
	clock = clock.Add(48 * time.Hour)
	set(stat(2, 1, 1200), map[string]any{"ana": stat(2, 1, 1200)})
	look()
	clock = clock.Add(24 * time.Hour)
	set(stat(5, 3, 3000), map[string]any{"ana": stat(5, 3, 3000)})
	look()
	p = o.Progress("all")
	if len(p.Sessions) != 3 || p.Sessions[0].Games != 3 || p.Sessions[0].Won != 2 || p.Total.Games != 9 {
		t.Fatalf("a reset starts the count again: %+v", p)
	}
	// Other modes have their own log.
	if q := o.Progress("competitive"); q.Since != nil || len(q.Sessions) != 0 {
		t.Fatalf("competitive was never looked at: %+v", q)
	}
}

// ---------- Notifications ----------

func TestMatchSavedNotifications(t *testing.T) {
	a := newTestApp(t)
	shell := a.Shell.(*NoShell)
	won := true
	gpm := int64(612)
	m := MatchSummary{MatchID: "1", HeroName: ptr("npc_dota_hero_shadow_fiend"), Kills: 12, TotalDeaths: 3, Duration: "34:10", Won: &won, GPM: &gpm}

	// Off by default.
	a.notify(dotaNotification(m))
	if len(shell.Notifications()) != 0 {
		t.Fatal("notifications must be opt-in")
	}
	if bg := a.SetNotify(true); !bg.Notify {
		t.Fatal("the setting did not stick")
	}
	a.notify(dotaNotification(m))
	got := shell.Notifications()
	if len(got) != 1 || got[0].Title != "Dota 2: Won as Shadow Fiend" || got[0].Body != "12 kills, 3 deaths, 612 GPM, 34:10" || got[0].View != "sessions" {
		t.Fatalf("dota notification wrong: %+v", got)
	}

	// A CS2 match that ends is announced through the tracker's own hook.
	a.Cs2.HandleUpdate(cs2Payload("de_mirage", "live", "CT", 12, 7, 20, 11))
	a.Cs2.HandleUpdate(cs2Payload("de_mirage", "gameover", "CT", 13, 7, 21, 11))
	waitFor(t, "the CS2 notification", func() bool { return len(shell.Notifications()) == 2 })
	if n := shell.Notifications()[1]; n.Title != "CS2: Won 13 : 7 on Mirage" || n.View != "cs-matches" {
		t.Fatalf("cs2 notification wrong: %+v", n)
	}

	// The test button works whatever the setting.
	a.SetNotify(false)
	a.TestNotification()
	if len(shell.Notifications()) != 3 {
		t.Fatal("the test notification should always show")
	}
}
