package core

import (
	"strings"
	"testing"
)

// A Dire player (slot 128) who died three times: once alone in the enemy
// jungle during a 1-for-3 fight, once in a 2-for-2 trade, once picked off
// outside any fight; bought back after the first and died again.
func fightsMatch() (jsonMap, []jsonMap) {
	players := []jsonMap{}
	for slot := 0; slot < 10; slot++ {
		ps := slot
		if slot >= 5 {
			ps = 128 + slot - 5
		}
		p := jsonMap{"player_slot": float64(ps), "hero_id": float64(slot + 1), "lane_role": float64(1 + slot%5),
			"networth_t": []any{0.0, 500.0, 1000.0, 2000.0, 3000.0, 4000.0, 5000.0, 6000.0, 7000.0, 8000.0, 9000.0, 10000.0, 11000.0, 12000.0, 13000.0, 14000.0, 15000.0, 16000.0, 17000.0, 18000.0, 19000.0, 20000.0}}
		players = append(players, p)
	}
	me := players[5] // Dire, lane_role 1; the Radiant lane_role 1 is players[0]
	me["networth_t"] = []any{0.0, 400.0, 800.0, 1500.0, 2200.0, 3000.0, 3800.0, 4600.0, 5400.0, 6200.0, 7000.0, 7800.0, 8600.0, 9400.0, 10200.0, 11000.0, 11800.0, 12600.0, 13400.0, 14200.0, 15000.0, 15800.0}
	me["deaths_log"] = []any{jsonMap{"time": 700.0, "key": "npc_dota_hero_antimage"}, jsonMap{"time": 760.0, "key": "npc_dota_hero_axe"}, jsonMap{"time": 1300.0, "key": "npc_dota_hero_antimage"}}
	me["buyback_log"] = []any{jsonMap{"time": 720.0}}
	me["life_state"] = jsonMap{"0": 1900.0, "1": 20.0, "2": 280.0}
	me["teamfight_participation"] = 0.45
	me["obs_log"] = []any{}
	fightPlayers := func(set func(i int, p jsonMap)) []any {
		out := []any{}
		for i := 0; i < 10; i++ {
			p := jsonMap{"deaths_pos": jsonMap{}, "damage": 0.0, "deaths": 0.0, "killed": jsonMap{}}
			set(i, p)
			out = append(out, p)
		}
		return out
	}
	// Fight 1 at 690–710: me alone (Dire) against three Radiant heroes, in
	// the Radiant jungle (low x+y: the Dire's "their" side).
	f1 := jsonMap{"start": 690.0, "end": 710.0, "players": fightPlayers(func(i int, p jsonMap) {
		if i == 5 {
			p["deaths"], p["damage"], p["deaths_pos"] = 1.0, 300.0, jsonMap{"140": jsonMap{"90": 1.0}}
		}
		if i < 3 {
			p["damage"] = 400.0
		}
	})}
	// Fight 2 at 750–780: 2 for 2, me among the dead, in the river.
	f2 := jsonMap{"start": 750.0, "end": 780.0, "players": fightPlayers(func(i int, p jsonMap) {
		p["damage"] = 300.0
		if i == 5 || i == 6 || i == 0 || i == 1 {
			p["deaths"] = 1.0
		}
		if i == 5 {
			p["deaths_pos"] = jsonMap{"128": jsonMap{"128": 1.0}}
		}
	})}
	m := jsonMap{"teamfights": []any{f1, f2}, "radiant_gold_adv": []any{0.0, 500.0, 1000.0, 2000.0, 3000.0, 4000.0, 4500.0, 5000.0, 5500.0, 6000.0, 6500.0, 7000.0, 7000.0, 7000.0, 7000.0, 7000.0, 7000.0, 7000.0, 7000.0, 7000.0, 7000.0, 7000.0}}
	return m, players
}

func TestDotaFactsFromParsedMatch(t *testing.T) {
	m, players := fightsMatch()
	heroes := map[int]DotaHero{1: {ID: 1, Name: "Anti-Mage", Slug: "antimage"}, 2: {ID: 2, Name: "Axe", Slug: "axe"}, 6: {ID: 6, Name: "Drow Ranger", Slug: "drow_ranger"}}
	f := dotaFactsFrom(m, players, 5, heroes)
	if f.HeroNPC != "npc_dota_hero_drow_ranger" || f.RivalHero != "Anti-Mage" || f.LaneRole != 1 {
		t.Fatalf("identity: %+v", f)
	}
	if len(f.Deaths) != 3 || f.DeadSeconds != 300 || f.Buybacks != 1 {
		t.Fatalf("deaths %d dead %d buybacks %d", len(f.Deaths), f.DeadSeconds, f.Buybacks)
	}
	d1, d2, d3 := f.Deaths[0], f.Deaths[1], f.Deaths[2]
	if d1.Killer != "Anti-Mage" || d1.Fight == nil || d1.Fight.AlliesIn != 1 || d1.Fight.EnemiesIn != 3 || d1.Fight.AlliesDead != 1 || d1.Fight.EnemiesDead != 0 {
		t.Fatalf("death 1: %+v %+v", d1, d1.Fight)
	}
	if !d1.Theirs || d1.Region != "their jungle" || !d1.BuyBack || d1.GoldAdv != -7000 {
		t.Fatalf("death 1 place: %+v", d1)
	}
	if fightText(d1) != "alone against 3" {
		t.Fatalf("fight text: %q", fightText(d1))
	}
	if d2.Fight == nil || d2.Fight.AlliesDead != 2 || d2.Fight.EnemiesDead != 2 || d2.Region != "the river" || fightText(d2) != "traded 2 for 2" {
		t.Fatalf("death 2: %+v %q", d2, fightText(d2))
	}
	if d3.Fight != nil || fightText(d3) != "picked off by Anti-Mage outside a fight" {
		t.Fatalf("death 3: %+v %q", d3, fightText(d3))
	}
	// Gold curves from the Dire side are the Radiant advantage negated.
	if f.GoldAdv[1] != -500 || f.NetWorth[20] != 15000 || f.RivalNetWorth[20] != 19000 {
		t.Fatalf("curves: %v %v", f.GoldAdv[:3], f.NetWorth[20])
	}

	wrong, good, facts := dotaFactPoints(f, 2200, false)
	kinds := map[string]ReviewPoint{}
	for _, p := range wrong {
		kinds[p.Kind] = p
	}
	for _, k := range []string{"wasted_buyback", "dead_time", "participation", "outfarmed"} {
		if _, ok := kinds[k]; !ok {
			t.Errorf("missing point %s in %v", k, kinds)
		}
	}
	if _, ok := kinds["alone_deaths"]; ok {
		t.Error("one lone death isn't a pattern yet")
	}
	if len(good) != 0 {
		t.Errorf("good: %+v", good)
	}
	joined := strings.Join(facts, " | ")
	for _, want := range []string{"Dead for 5:00", "45% of the team's fights", "15,000 vs 19,000 for Anti-Mage", "at the end: -7,000"} {
		if !strings.Contains(joined, want) {
			t.Errorf("facts miss %q: %s", want, joined)
		}
	}
}

func TestDotaDeathLogMergesLiveDeaths(t *testing.T) {
	m, players := fightsMatch()
	heroes := map[int]DotaHero{1: {ID: 1, Name: "Anti-Mage", Slug: "antimage"}, 6: {ID: 6, Name: "Drow Ranger", Slug: "drow_ranger"}}
	f := dotaFactsFrom(m, players, 5, heroes)
	gold := int64(400)
	local := &MatchSummary{Deaths: []Death{{Clock: "11:38", At: 698, GoldLost: &gold}, {Clock: "21:45", At: 1305}}}
	log := dotaDeathLog(f.Deaths, local)
	if len(log) != 3 || log[0].Clock != "11:40" || log[0].Fight != "alone against 3" || log[0].Region != "their jungle" {
		t.Fatalf("log: %+v", log)
	}
	if log[0].GoldLost == nil || *log[0].GoldLost != 400 {
		t.Fatalf("live gold lost not merged: %+v", log[0])
	}
	if log[0].Note != "bought back, team 7,000 gold down" {
		t.Fatalf("note: %q", log[0].Note)
	}
}

func TestMapSpots(t *testing.T) {
	cases := []struct {
		x, y    float64
		radiant bool
		want    string
	}{
		{80, 80, true, "your base"}, {80, 80, false, "their base"},
		{176, 176, true, "their base"}, {176, 176, false, "your base"},
		{128, 128, true, "the river"},
		{140, 90, true, "your jungle"}, {140, 90, false, "their jungle"},
		{160, 140, true, "their jungle"},
		{80, 120, true, "the top lane on your side"}, {120, 175, true, "the top lane on their side"},
		{120, 80, true, "the bottom lane on your side"}, {175, 120, true, "the bottom lane on their side"},
		{112, 112, true, "the mid lane on your side"}, {146, 146, false, "the mid lane on your side"},
	}
	for _, c := range cases {
		if got := regionText(spotOf(c.x, c.y, c.radiant)); got != c.want {
			t.Errorf("(%v,%v) radiant=%v: %q, want %q", c.x, c.y, c.radiant, got, c.want)
		}
	}
	// GSI world units map onto the same grid.
	if worldToGrid(0) != 128 || worldToGrid(-8192) != 64 || worldToGrid(8192) != 192 {
		t.Fatal("world to grid")
	}
}

func TestDeathRegionFromTheSnapshot(t *testing.T) {
	// A Radiant hero on the Dire side.
	snap := snapshotOf(900, jsonMap{"xpos": 2500.0, "ypos": 1000.0, "level": 10.0}, jsonMap{"team_name": "radiant", "gold": 300.0})
	if !snap.HasPos || !snap.HasSide || deathRegion(snap) != "their jungle" {
		t.Fatalf("snapshot %+v region %q", snap, deathRegion(snap))
	}
	// Dire sees the mirror: the same world spot is their own side.
	if r := deathRegion(&aliveSnapshot{HasPos: true, HasSide: true, Radiant: false, X: 2500, Y: 1000}); r != "your jungle" {
		t.Fatalf("dire region %q", r)
	}
}
