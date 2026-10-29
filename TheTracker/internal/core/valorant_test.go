package core

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const valMe = "11111111-1111-1111-1111-111111111111"

func valID(team string, i int) string {
	if team == "Blue" && i == 0 {
		return valMe
	}
	c := map[string]string{"Blue": "b", "Red": "c"}[team]
	return fmt.Sprintf("%s%07d-1111-1111-1111-111111111111", c, i)
}

// valMatch builds a 10-round match: Blue (me on Jett) wins 6–4. Round 1 I
// take three kills and plant; in the odd rounds I'm the first to die, alone
// in B Main and untraded; round 3 I clutch a 1v5. extraScore raises my
// score, firstDeaths false leaves out my odd-round deaths.
func valMatch(id, started string, extraScore int, firstDeaths bool) jsonMap {
	agents := map[string][]string{"Blue": {"Jett", "Sova", "Omen", "Killjoy", "Raze"}, "Red": {"Reyna", "Skye", "Viper", "Cypher", "Phoenix"}}
	var players []any
	for _, team := range []string{"Blue", "Red"} {
		for i := 0; i < 5; i++ {
			score, kills, deaths := 2000, 12, 12
			if team == "Blue" && i == 0 {
				score, kills, deaths = 2500+extraScore, 16, 5
				if !firstDeaths {
					deaths = 0
				}
			}
			players = append(players, map[string]any{"puuid": valID(team, i), "name": fmt.Sprintf("%s%d", team, i), "tag": "EUW", "team_id": team, "party_id": "p" + team,
				"agent": map[string]any{"name": agents[team][i]}, "tier": map[string]any{"name": "Gold 2"},
				"ability_casts": map[string]any{"ability1": 10, "ability2": 8, "grenade": 6, "ultimate": 2},
				"stats": map[string]any{"score": score, "kills": kills, "deaths": deaths, "assists": 4, "headshots": 20, "bodyshots": 60, "legshots": 5,
					"damage": map[string]any{"dealt": 1500, "received": 1300}}})
		}
	}
	ref := func(p string) map[string]any { return map[string]any{"puuid": p} }
	loc := func(x, y float64) map[string]any { return map[string]any{"x": x, "y": y} }
	var kills, rounds []any
	k := func(round, at int, killer, victim string, x, y float64, mates ...any) {
		kills = append(kills, map[string]any{"round": round, "time_in_round_in_ms": at, "killer": ref(killer), "victim": ref(victim),
			"weapon": map[string]any{"name": "Vandal"}, "location": loc(x, y), "player_locations": mates, "assistants": []any{}})
	}
	for r := 0; r < 10; r++ {
		winner := "Blue"
		if r%2 == 1 && r != 3 {
			winner = "Red"
		}
		round := map[string]any{"id": r, "winning_team": winner}
		switch {
		case r == 0:
			round["plant"] = map[string]any{"player": ref(valMe)}
			k(r, 5000, valMe, valID("Red", 0), 0, 0)
			k(r, 6000, valMe, valID("Red", 1), 0, 0)
			k(r, 7000, valMe, valID("Red", 2), 0, 0)
		case r == 3:
			// Four teammates go down, then I take all five.
			for i := 1; i < 5; i++ {
				k(r, 1000*i, valID("Red", 0), valID("Blue", i), 0, 0)
			}
			for i := 0; i < 5; i++ {
				k(r, 6000+1000*i, valMe, valID("Red", i), 0, 0)
			}
		case r%2 == 1 && firstDeaths:
			mate := map[string]any{"player": ref(valID("Blue", 1)), "location": loc(6100, 2000)}
			k(r, 3000, valID("Red", 0), valMe, 3100, 2000, mate)
		}
		rounds = append(rounds, round)
	}
	return jsonMap{
		"metadata": map[string]any{"match_id": id, "started_at": started, "game_length_in_ms": 1800000, "is_completed": true,
			"map": map[string]any{"name": "Ascent"}, "queue": map[string]any{"id": "competitive", "name": "Competitive"}, "season": map[string]any{"short": "e10a1"}},
		"players": players, "rounds": rounds, "kills": kills,
		"teams": []any{
			map[string]any{"team_id": "Blue", "won": true, "rounds": map[string]any{"won": 6, "lost": 4}},
			map[string]any{"team_id": "Red", "won": false, "rounds": map[string]any{"won": 4, "lost": 6}},
		},
	}
}

var valTestContent = valContent{
	Agents: map[string]valAgent{"jett": {Name: "Jett", Icon: "https://media.valorant-api.com/agents/jett.png", Role: "Duelist"}},
	Maps: map[string]valMap{"ascent": {Name: "Ascent", Callouts: []valCallout{
		{Name: "B Main", X: 3000, Y: 2000}, {Name: "A Site", X: -4000, Y: 1000}, {Name: "Mid Top", X: 0, Y: 0},
	}}},
	Tiers: map[int]valTier{14: {Name: "Gold 2", Icon: "gold2.png"}},
}

// asJSON passes a fixture through JSON, as the API data arrives.
func asJSON(m jsonMap) jsonMap {
	raw, _ := json.Marshal(m)
	var out jsonMap
	json.Unmarshal(raw, &out)
	return out
}

func TestValorantReadsAMatchForMe(t *testing.T) {
	m, ok := valMatchFrom(asJSON(valMatch("aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee", "2026-10-28T20:00:00Z", 0, true)), valMe, valTestContent)
	if !ok {
		t.Fatal("I'm in that match")
	}
	if m.Map != "Ascent" || !m.Ranked || m.Won == nil || !*m.Won || m.TeamRounds != 6 || m.EnemyRounds != 4 || m.Agent != "Jett" {
		t.Fatalf("header: %+v", m)
	}
	if m.ACS != 250 || m.ADR != 150 || m.HSPct != 23.5 {
		t.Fatalf("per round: acs %v adr %v hs %v", m.ACS, m.ADR, m.HSPct)
	}
	if m.FirstBloods != 1 || m.FirstDeaths != 4 || m.Clutches != 1 || m.MultiKills != 2 || m.Plants != 1 || m.Utility != 26 {
		t.Fatalf("rounds: fb %d fd %d clutch %d multi %d plants %d util %d", m.FirstBloods, m.FirstDeaths, m.Clutches, m.MultiKills, m.Plants, m.Utility)
	}
	if m.KAST != 60 {
		t.Fatalf("KAST %v", m.KAST)
	}
	if len(m.DeathLog) != 4 {
		t.Fatalf("deaths %+v", m.DeathLog)
	}
	d := m.DeathLog[0]
	if d.Round != 2 || d.Place != "B Main" || !d.First || d.Traded || d.Mate != 30 || d.Agent != "Reyna" || d.Weapon != "Vandal" || !d.Lost {
		t.Fatalf("death: %+v", d)
	}
	if m.TeamPlace != 1 || len(m.Team) != 5 || len(m.Enemies) != 5 || !m.Team[0].Me {
		t.Fatalf("scoreboard: %+v", m.Team)
	}
	if _, ok := valMatchFrom(asJSON(valMatch("x", "2026-10-28T20:00:00Z", 0, true)), "99999999-1111-1111-1111-111111111111", valTestContent); ok {
		t.Fatal("someone else's match")
	}
}

// valTestApp is an app linked to a Valorant account with six matches on
// the fake cloud and valorant-api.com content on a fake server.
func valTestApp(t *testing.T) (*App, *fakeConvex) {
	a, cloud := pgTestApp(t)
	content := newFakeAPI(t, map[string]any{
		"/v1/agents":           map[string]any{"data": []any{map[string]any{"displayName": "Jett", "displayIcon": "jett.png", "role": map[string]any{"displayName": "Duelist"}}}},
		"/v1/maps":             map[string]any{"data": []any{map[string]any{"displayName": "Ascent", "listViewIcon": "ascent.png", "callouts": []any{map[string]any{"regionName": "Main", "superRegionName": "B", "location": map[string]any{"x": 3000, "y": 2000}}}}}},
		"/v1/competitivetiers": map[string]any{"data": []any{map[string]any{"tiers": []any{map[string]any{"tier": 14, "tierName": "GOLD 2", "smallIcon": "gold2.png", "color": "eccf56ff"}}}}},
	})
	a.Valorant.content = &service{Name: "valorant-api.com", Base: content.srv.URL, Attempts: 1}
	var list []any
	for i := 0; i < 6; i++ {
		id := fmt.Sprintf("%08d-bbbb-cccc-dddd-eeeeeeeeeeee", i)
		// The newest has my four first deaths; the others none.
		list = append(list, valMatch(id, fmt.Sprintf("2026-10-2%dT20:00:00Z", i), 100*i, i == 5))
	}
	cloud.mu.Lock()
	cloud.valorant = map[string]any{
		"/valorant/v2/account/Me/EUW":                               map[string]any{"puuid": valMe, "name": "Me", "tag": "EUW", "region": "eu", "account_level": 120},
		"/valorant/v4/by-puuid/matches/eu/pc/" + valMe + "?size=10": list,
		"/valorant/v3/by-puuid/mmr/eu/pc/" + valMe: map[string]any{"current": map[string]any{"tier": map[string]any{"id": 14, "name": "Gold 2"}, "rr": 57, "last_change": 18, "elo": 1157},
			"peak":     map[string]any{"tier": map[string]any{"id": 14, "name": "Gold 2"}, "season": map[string]any{"short": "e9a3"}},
			"seasonal": []any{map[string]any{"season": map[string]any{"short": "e10a1"}, "games": 40, "wins": 22, "end_tier": map[string]any{"name": "Gold 2"}}}},
	}
	cloud.mu.Unlock()

	if _, err := a.Valorant.Find("Me"); err == nil {
		t.Fatal("a Riot ID needs its tag")
	}
	link, err := a.Valorant.Find("Me#EUW")
	if err != nil || link.Puuid != valMe || link.Region != "eu" || link.Platform != "pc" {
		t.Fatalf("find: %+v %v", link, err)
	}
	a.Valorant.SetLink(link)
	return a, cloud
}

func TestValorantOverviewAndReview(t *testing.T) {
	a, cloud := valTestApp(t)
	ov, err := a.Valorant.Overview(false)
	if err != nil {
		t.Fatal(err)
	}
	if len(ov.Matches) != 6 || ov.Matches[0].ID != "00000005-bbbb-cccc-dddd-eeeeeeeeeeee" || ov.Rank == nil || ov.Rank.RR != 57 || ov.Rank.Icon != "gold2.png" || ov.Agents["jett"].Role != "Duelist" {
		t.Fatalf("overview: %d matches, rank %+v", len(ov.Matches), ov.Rank)
	}
	// Kept for good: still listed when the API is gone.
	cloud.mu.Lock()
	cloud.valorant = nil
	cloud.mu.Unlock()
	if kept := a.Valorant.kept(valMe); len(kept) != 6 {
		t.Fatalf("kept %d", len(kept))
	}

	r, err := a.ValorantReview("")
	if err != nil {
		t.Fatal(err)
	}
	kinds := map[string]bool{}
	for _, p := range r.Wrong {
		kinds[p.Kind] = true
	}
	if !kinds["first_deaths"] || !kinds["alone"] {
		t.Fatalf("wrong: %+v", r.Wrong)
	}
	good := map[string]bool{}
	for _, p := range r.Good {
		good[p.Kind] = true
	}
	if !good["clutch"] || !strings.HasPrefix(r.Verdict, "Won 6–4") || len(r.DeathLog) != 4 || r.DeathLog[0].Region != "B Main" || r.Goals == nil {
		t.Fatalf("review: %+v", r)
	}
	// The coach never sees other players' names.
	if raw, _ := json.Marshal(coachFacts(r)); strings.Contains(string(raw), "Red0") {
		t.Fatalf("a player's name reached the coach: %s", raw)
	}
	if w, _ := a.ValorantWorkOn(); w.Game != "valorant" || w.Matches != 6 {
		t.Fatalf("work on %+v", w)
	}
}

// With TT_VAL_DUMP set to a folder, writes the overview and a review as the
// pages receive them, to look at the pages with.
func TestValorantDump(t *testing.T) {
	dir := os.Getenv("TT_VAL_DUMP")
	if dir == "" {
		t.Skip()
	}
	a, _ := valTestApp(t)
	ov, _ := a.Valorant.Overview(false)
	r, _ := a.ValorantReview("")
	for name, v := range map[string]any{"overview.json": ov, "review.json": r} {
		raw, _ := json.Marshal(v)
		os.WriteFile(filepath.Join(dir, name), raw, 0o644)
	}
}
