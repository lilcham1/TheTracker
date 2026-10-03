package core

import (
	"strings"
	"testing"
)

// ---------- CS2: rounds, damage, weapons ----------

// cs2Round plays one round through the tracker the way the game reports it:
// buy time, the action, then the score going up.
type cs2Feed struct {
	c      *Cs2
	ct, t  int
	kills  int
	deaths int
}

func (f *cs2Feed) send(team, roundPhase, weapon string, rk, hs, dmg, health int) {
	f.c.HandleUpdate(jsonMap{
		"provider": jsonMap{"appid": 730.0, "steamid": "me"},
		"map":      jsonMap{"name": "de_mirage", "mode": "competitive", "phase": "live", "team_ct": jsonMap{"score": float64(f.ct)}, "team_t": jsonMap{"score": float64(f.t)}},
		"round":    jsonMap{"phase": roundPhase},
		"player": jsonMap{"steamid": "me", "team": team,
			"state":       jsonMap{"health": float64(health), "round_kills": float64(rk), "round_killhs": float64(hs), "round_totaldmg": float64(dmg)},
			"match_stats": jsonMap{"kills": float64(f.kills), "deaths": float64(f.deaths)},
			"weapons":     jsonMap{"weapon_0": jsonMap{"name": "weapon_knife", "state": "holstered"}, "weapon_1": jsonMap{"name": "weapon_" + weapon, "state": "active"}}},
	})
}

// round plays a round on `team`; ctWins says which side takes it.
func (f *cs2Feed) round(team, weapon string, kills, hs, dmg int, died, ctWins bool) {
	f.send(team, "freezetime", weapon, 0, 0, 0, 100)
	// Kills arrive one at a time, as they do in play.
	for k := 1; k <= kills; k++ {
		f.kills++
		f.send(team, "live", weapon, k, min(k, hs), dmg*k/kills, 100)
	}
	if kills == 0 {
		f.send(team, "live", weapon, 0, 0, dmg, 100)
	}
	if died {
		f.send(team, "live", weapon, kills, hs, dmg, 0)
	}
	if ctWins {
		f.ct++
	} else {
		f.t++
	}
	// The round is over: the score has moved and the counters still show
	// this round's numbers until the next buy time.
	f.send(team, "over", weapon, kills, hs, dmg, map[bool]int{true: 0, false: 100}[died])
	if died {
		f.deaths++
	}
}

func TestCs2RoundsDamageAndWeapons(t *testing.T) {
	c := NewCs2(NewStore(t.TempDir()))
	f := &cs2Feed{c: c}

	f.round("CT", "m4a1", 2, 1, 180, false, true)  // won, 2 kills
	f.round("CT", "awp", 0, 0, 35, true, false)    // lost, died
	f.round("CT", "m4a1", 3, 2, 300, false, true)  // won, 3 kills
	f.round("CT", "deagle", 1, 1, 100, true, true) // won, traded
	m := c.Status().Current

	if len(m.Rounds) != 4 {
		t.Fatalf("expected 4 rounds recorded, got %d: %+v", len(m.Rounds), m.Rounds)
	}
	want := []Cs2Round{
		{N: 1, Won: true, Side: "CT", Kills: 2, HS: 1, Damage: 180},
		{N: 2, Won: false, Side: "CT", Kills: 0, HS: 0, Damage: 35, Died: true},
		{N: 3, Won: true, Side: "CT", Kills: 3, HS: 2, Damage: 300},
		{N: 4, Won: true, Side: "CT", Kills: 1, HS: 1, Damage: 100, Died: true},
	}
	for i, w := range want {
		if m.Rounds[i] != w {
			t.Errorf("round %d: got %+v, want %+v", i+1, m.Rounds[i], w)
		}
	}
	// The counters linger after a round ends; they must not be counted into
	// the next one.
	if m.HeadshotKills != 4 || m.Damage != 615 {
		t.Fatalf("totals double-counted or lost: %d headshots, %d damage", m.HeadshotKills, m.Damage)
	}
	if m.WeaponKills["m4a1"] != 5 || m.WeaponKills["deagle"] != 1 || m.WeaponKills["awp"] != 0 || m.WeaponKills["knife"] != 0 {
		t.Fatalf("kills by weapon wrong: %v", m.WeaponKills)
	}
	if m.Weapon != "deagle" || m.MyScore != 3 || m.TheirScore != 1 {
		t.Fatalf("live state wrong: %+v", m)
	}

	// Half time: sides swap and the scores trade places. That is not a round.
	f.ct, f.t = f.t, f.ct
	f.round("T", "ak47", 1, 0, 90, false, false) // T side wins: the player's team
	m = c.Status().Current
	if len(m.Rounds) != 5 || !m.Rounds[4].Won || m.Rounds[4].Side != "T" || m.MyScore != 4 {
		t.Fatalf("the side swap was mishandled: %+v (score %d-%d)", m.Rounds, m.MyScore, m.TheirScore)
	}
}

func TestCs2DeathNoticedLateBelongsToTheRoundItHappenedIn(t *testing.T) {
	// Once dead the feed follows a teammate, so the player's own death count
	// only arrives with the next round's buy time.
	c := NewCs2(NewStore(t.TempDir()))
	f := &cs2Feed{c: c}
	f.round("CT", "m4a1", 1, 0, 100, false, true)
	// Round 2: the death is never seen as health 0.
	f.send("CT", "freezetime", "m4a1", 0, 0, 0, 100)
	f.send("CT", "live", "m4a1", 0, 0, 20, 100)
	f.t++
	f.c.HandleUpdate(jsonMap{ // spectating someone else as the round ends
		"provider": jsonMap{"appid": 730.0, "steamid": "me"},
		"map":      jsonMap{"name": "de_mirage", "phase": "live", "team_ct": jsonMap{"score": float64(f.ct)}, "team_t": jsonMap{"score": float64(f.t)}},
		"round":    jsonMap{"phase": "over"},
		"player":   jsonMap{"steamid": "teammate", "team": "CT", "state": jsonMap{"health": 100.0, "round_kills": 4.0}},
	})
	f.deaths++
	f.send("CT", "freezetime", "m4a1", 0, 0, 0, 100)

	m := c.Status().Current
	if len(m.Rounds) != 2 || !m.Rounds[1].Died || m.Rounds[0].Died {
		t.Fatalf("the death belongs to round 2: %+v", m.Rounds)
	}
	if m.Rounds[1].Kills != 0 || m.WeaponKills["m4a1"] != 1 {
		t.Fatalf("a spectated teammate's kills were counted: %+v %v", m.Rounds[1], m.WeaponKills)
	}
}

func TestCs2TestMatchIsNeverSaved(t *testing.T) {
	c := NewCs2(NewStore(t.TempDir()))
	if err := c.StartSimulation(); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "the test match to produce a round", func() bool {
		s := c.Status()
		return s.Simulating && s.Current != nil && len(s.Current.Rounds) >= 1
	})
	c.StopSimulation()
	s := c.Status()
	if s.Simulating || s.Current == nil || !s.Current.Ended || !s.Current.Simulated {
		t.Fatalf("stopping should end the test match: %+v", s)
	}
	if len(c.History()) != 0 {
		t.Fatal("a test match was saved")
	}

	// And it will not start over a real one.
	c2 := NewCs2(NewStore(t.TempDir()))
	c2.HandleUpdate(cs2Payload("de_nuke", "live", "CT", 3, 2, 5, 2))
	if c2.StartSimulation() == nil {
		t.Fatal("the test match would have replaced a real match in progress")
	}
}

// ---------- CS2 lifetime ----------

func TestCs2LifetimeParsing(t *testing.T) {
	stat := func(name string, v float64) jsonMap { return jsonMap{"name": name, "value": v} }
	l := ParseCs2Lifetime([]jsonMap{
		stat("total_kills", 10000), stat("total_deaths", 8000), stat("total_kills_headshot", 4500),
		stat("total_shots_fired", 200000), stat("total_shots_hit", 40000), stat("total_damage_done", 1500000),
		stat("total_rounds_played", 20000), stat("total_wins", 10400), stat("total_matches_played", 800), stat("total_matches_won", 420),
		stat("total_time_played", 3600000), stat("total_mvps", 900),
		stat("total_kills_ak47", 4000), stat("total_shots_ak47", 60000), stat("total_hits_ak47", 13200),
		stat("total_kills_awp", 1500), stat("total_shots_awp", 5000), stat("total_hits_awp", 2500),
		stat("total_kills_knife", 40), stat("total_kills_enemy_blinded", 300), stat("total_kills_glock", 0),
		stat("total_rounds_map_de_dust2", 5000), stat("total_wins_map_de_dust2", 2600),
		stat("total_rounds_map_de_inferno", 9000), stat("total_wins_map_de_inferno", 4410),
	})
	if !l.Available || l.KD != 1.25 || l.HeadshotRate != 45 || l.Accuracy != 20 || l.ADR != 75 || l.MatchWinRate != 52.5 || l.HoursPlayed != 1000 {
		t.Fatalf("headline figures wrong: %+v", l)
	}
	if len(l.Weapons) != 3 || l.Weapons[0].Key != "ak47" || *l.Weapons[0].Accuracy != 22 || l.Weapons[1].Key != "awp" || *l.Weapons[1].Accuracy != 50 {
		t.Fatalf("weapons wrong (most kills first; \"enemy blinded\" is not a weapon; unused weapons dropped): %+v", l.Weapons)
	}
	if l.Weapons[2].Key != "knife" || l.Weapons[2].Accuracy != nil {
		t.Fatalf("a knife has kills but no accuracy: %+v", l.Weapons[2])
	}
	if len(l.Maps) != 2 || l.Maps[0].Key != "de_inferno" || l.Maps[0].Rate != 49 || l.Maps[1].Rate != 52 {
		t.Fatalf("maps wrong: %+v", l.Maps)
	}
	// Nothing at all is still a valid, empty answer, not a division by zero.
	if e := ParseCs2Lifetime(nil); e.KD != 0 || e.Accuracy != 0 || e.ADR != 0 {
		t.Fatalf("empty stats: %+v", e)
	}
}

func TestCs2LifetimeSaysWhyItIsMissing(t *testing.T) {
	a := newTestApp(t)
	if got := a.Cs2Lifetime(false); got.Available || got.Reason != "signed_out" {
		t.Fatalf("with no Steam account there is nobody to look up: %+v", got)
	}
	linked(a.Store, "dota", 850402858)
	if id := a.steamID64(); id != "76561198810668586" {
		t.Fatalf("the 64-bit id is derived from the linked account: %s", id)
	}
	// newTestApp points the cloud at a dead port.
	if got := a.Cs2Lifetime(false); got.Available || got.Reason != "unreachable" {
		t.Fatalf("an unreachable service should read as such: %+v", got)
	}

	// A deployment that answers but has no key, and one that has not been
	// updated at all, both mean "not switched on yet".
	f := newFakeConvex(t)
	a.Cloud.base = f.srv.URL
	f.lifetime = map[string]any{"ok": false, "reason": "not_set_up"}
	if got := a.Cs2Lifetime(false); got.Reason != "not_set_up" {
		t.Fatalf("no key on the server: %+v", got)
	}
	f.lifetime = map[string]any{"ok": false, "reason": "private"}
	if got := a.Cs2Lifetime(false); got.Reason != "private" {
		t.Fatalf("a private profile should say so: %+v", got)
	}
	f.lifetime = map[string]any{"ok": true, "stats": []any{map[string]any{"name": "total_kills", "value": 50}, map[string]any{"name": "total_deaths", "value": 25}}}
	got := a.Cs2Lifetime(false)
	if !got.Available || got.KD != 2 {
		t.Fatalf("stats should come through: %+v", got)
	}
	if f.argsFor("steam:cs2Stats")["steamId"] != "76561198810668586" {
		t.Fatal("the lookup must be for the player's own account")
	}
}

// ---------- Overwatch: meta and hero detail ----------

func TestOverwatchMetaAndHeroCareer(t *testing.T) {
	store := NewStore(t.TempDir())
	f := newFakeAPI(t, map[string]any{
		"/heroes": []any{
			map[string]any{"key": "ana", "name": "Ana", "role": "support"},
			map[string]any{"key": "genji", "name": "Genji", "role": "damage"},
		},
		"/maps": []any{map[string]any{"key": "kings-row", "name": "King's Row"}, map[string]any{"key": "busan", "name": "Busan"}},
		"/heroes/stats": []any{
			map[string]any{"hero": "ana", "pickrate": 32, "winrate": 49.7, "banrate": 10.9},
			map[string]any{"hero": "genji", "pickrate": 9.1, "winrate": 52.3, "banrate": 1.2},
			map[string]any{"hero": "unreleased", "pickrate": 1, "winrate": 99, "banrate": 0},
		},
		"/players/Name-1/stats/career": map[string]any{"genji": map[string]any{
			"best":          map[string]any{"eliminations_most_in_game": 56, "multikill_best": 4},
			"average":       map[string]any{"eliminations_avg_per_10_min": 18.9, "objective_time_avg_per_10_min": 51},
			"hero_specific": map[string]any{"dragonblade_kills": 300},
			"combat":        map[string]any{"weapon_accuracy": 31, "time_spent_on_fire": 4000},
			"unknown_group": map[string]any{"x": 1},
		}},
	})
	o := NewOverwatch(store)
	o.api = &service{Name: "The Overwatch API", Base: f.srv.URL, Attempts: 1}

	m, err := o.Meta("competitive", "nowhere", "diamond", "kings-row", false)
	if err != nil {
		t.Fatal(err)
	}
	q := strings.Join(f.queries, " ")
	for _, want := range []string{"region=europe", "competitive_division=diamond", "map=kings-row", "gamemode=competitive"} {
		if !strings.Contains(q, want) {
			t.Errorf("filter %s was not sent: %s", want, q)
		}
	}
	if len(m.Heroes) != 2 || m.Heroes[0].Name != "Genji" || m.Heroes[0].WinRate != 52.3 || m.Heroes[1].BanRate != 10.9 {
		t.Fatalf("meta rows wrong (best win rate first, unknown heroes dropped): %+v", m.Heroes)
	}
	if m.Region != "europe" || len(m.Maps) != 3 || m.Maps[0].ID != "all" || m.Maps[1].Label != "Busan" {
		t.Fatalf("an unknown region falls back; maps are offered alphabetically after \"all\": %+v", m)
	}
	// Quick play has no ranks.
	if qp, _ := o.Meta("quickplay", "asia", "diamond", "all", false); qp.Division != "all" || qp.Region != "asia" {
		t.Fatalf("quick play must not filter by rank: %+v", qp)
	}

	if _, err := o.HeroCareer("genji", "competitive"); err == nil {
		t.Fatal("no profile connected should be an error")
	}
	o.SetLink(OwLink{PlayerID: "Name-1", Name: "Name"})
	h, err := o.HeroCareer("genji", "all")
	if err != nil {
		t.Fatal(err)
	}
	if h.Name != "Genji" || h.Role != "damage" || h.Mode != "quickplay" {
		t.Fatalf("hero header wrong: %+v", h)
	}
	if len(h.Groups) != 4 || h.Groups[0].Key != "best" || h.Groups[1].Key != "average" || h.Groups[2].Key != "hero_specific" {
		t.Fatalf("groups should come in reading order, unknown ones dropped: %+v", h.Groups)
	}
	best := h.Groups[0].Stats
	if best[0].Label != "Eliminations" || best[0].Value != 56 || best[1].Label != "Multikill best" {
		t.Fatalf("labels should be words, not keys: %+v", best)
	}
	kinds := map[string]string{}
	for _, g := range h.Groups {
		for _, s := range g.Stats {
			kinds[s.Key] = s.Kind
		}
	}
	if kinds["weapon_accuracy"] != "percent" || kinds["time_spent_on_fire"] != "time" || kinds["objective_time_avg_per_10_min"] != "time" || kinds["dragonblade_kills"] != "number" {
		t.Fatalf("stat kinds wrong: %v", kinds)
	}
}
