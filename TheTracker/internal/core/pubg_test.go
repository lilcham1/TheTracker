package core

import (
	"strings"
	"testing"
)

const pgMe = "account.0123456789abcdef0123456789abcdef"

// pgMatch is a three-team match: my squad of two placed 2nd.
func pgMatch(id string, createdAt string) map[string]any {
	part := func(pid, name, player string, kills int, dmg float64, place int) map[string]any {
		return map[string]any{"type": "participant", "id": pid, "attributes": map[string]any{"stats": map[string]any{
			"name": name, "playerId": player, "kills": float64(kills), "damageDealt": dmg, "winPlace": float64(place), "DBNOs": 2.0,
			"assists": 1.0, "headshotKills": 1.0, "longestKill": 210.5, "timeSurvived": 1500.0, "walkDistance": 2400.0, "rideDistance": 900.0,
			"revives": 1.0, "heals": 3.0, "boosts": 4.0, "killPlace": 9.0, "deathType": "byplayer"}}}
	}
	roster := func(rank int, ids ...string) map[string]any {
		refs := []any{}
		for _, i := range ids {
			refs = append(refs, map[string]any{"type": "participant", "id": i})
		}
		return map[string]any{"type": "roster", "attributes": map[string]any{"stats": map[string]any{"rank": float64(rank)}},
			"relationships": map[string]any{"participants": map[string]any{"data": refs}}}
	}
	return map[string]any{
		"data": map[string]any{"type": "match", "id": id, "attributes": map[string]any{"createdAt": createdAt, "duration": 1800.0,
			"gameMode": "squad-fpp", "mapName": "Baltic_Main", "matchType": "official", "isCustomMatch": false}},
		"included": []any{
			part("p1", "Me", pgMe, 4, 512.4, 2), part("p2", "Mate", "account.ffffffffffffffffffffffffffffffff", 1, 140, 2),
			part("p3", "Winner", "account.eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee", 6, 800, 1), part("p4", "Third", "account.dddddddddddddddddddddddddddddddd", 0, 50, 3),
			roster(2, "p1", "p2"), roster(1, "p3"), roster(3, "p4"),
			map[string]any{"type": "asset", "attributes": map[string]any{"name": "telemetry", "URL": "https://telemetry-cdn.pubg.com/x.json"}},
		},
	}
}

func pgTestApp(t *testing.T) (*App, *fakeConvex) {
	t.Helper()
	cloud := newFakeConvex(t)
	a := newTestApp(t)
	a.Cloud.base = cloud.srv.URL
	return a, cloud
}

func TestPubgReadsAMatchForMe(t *testing.T) {
	m, ok := pubgMatchFrom(pgMatch("aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee", "2026-10-12T20:00:00Z"), pgMe)
	if !ok {
		t.Fatal("I'm in that match")
	}
	if m.Map != "Erangel" || m.ModeName != "Squad FPP" || m.Place != 2 || m.Teams != 3 || m.Kills != 4 || m.Knocks != 2 || m.Headshots != 1 {
		t.Fatalf("match: %+v", m)
	}
	if len(m.Team) != 2 || m.Team[0].Name != "Me" || !m.Team[0].Me || m.Team[1].Name != "Mate" {
		t.Fatalf("team: %+v", m.Team)
	}
	if m.StartTime != 1791835200 || m.Ranked || m.Custom {
		t.Fatalf("time/ranked: %+v", m)
	}
	if _, ok := pubgMatchFrom(pgMatch("x", "2026-10-12T20:00:00Z"), "account.00000000000000000000000000000000"); ok {
		t.Fatal("someone else's match")
	}
	for mode, want := range map[string]string{"squad": "Squad TPP", "duo-fpp": "Duo FPP", "tdm": "Team Deathmatch"} {
		if got := pubgModeName(mode); got != want {
			t.Errorf("%s: %q", mode, got)
		}
	}
}

func TestPubgOverviewThroughTheCloud(t *testing.T) {
	a, cloud := pgTestApp(t)
	// Before the deployment has the function or the key: a clear message.
	if _, err := a.Pubg.Search("Me", "steam"); err != errPubgNotSetUp {
		t.Fatalf("not set up: %v", err)
	}
	m1, m2 := "11111111-1111-1111-1111-111111111111", "22222222-2222-2222-2222-222222222222"
	cloud.mu.Lock()
	cloud.pubg = map[string]any{
		"/shards/steam/players?filter[playerNames]=Me": map[string]any{"data": []any{map[string]any{"type": "player", "id": pgMe, "attributes": map[string]any{"name": "Me"}}}},
		"/shards/steam/players/" + pgMe: map[string]any{"data": map[string]any{"id": pgMe, "relationships": map[string]any{"matches": map[string]any{"data": []any{
			map[string]any{"type": "match", "id": m1}, map[string]any{"type": "match", "id": m2}}}}}},
		"/shards/steam/seasons": map[string]any{"data": []any{
			map[string]any{"id": "division.bro.official.pc-2018-37", "attributes": map[string]any{"isCurrentSeason": false}},
			map[string]any{"id": "division.bro.official.pc-2018-38", "attributes": map[string]any{"isCurrentSeason": true}}}},
		"/shards/steam/players/" + pgMe + "/seasons/division.bro.official.pc-2018-38": map[string]any{"data": map[string]any{"attributes": map[string]any{"gameModeStats": map[string]any{
			"squad-fpp": map[string]any{"roundsPlayed": 40.0, "wins": 3.0, "top10s": 18.0, "kills": 52.0, "damageDealt": 9800.0, "headshotKills": 11.0, "longestKill": 330.0},
			"solo":      map[string]any{"roundsPlayed": 0.0}}}}},
		"/shards/steam/players/" + pgMe + "/seasons/division.bro.official.pc-2018-38/ranked": map[string]any{"data": map[string]any{"attributes": map[string]any{"rankedGameModeStats": map[string]any{
			"squad-fpp": map[string]any{"roundsPlayed": 12.0, "wins": 1.0, "currentRankPoint": 2410.0, "currentTier": map[string]any{"tier": "Gold", "subTier": "2"},
				"bestTier": map[string]any{"tier": "Platinum", "subTier": "5"}, "bestRankPoint": 2510.0, "avgRank": 9.4, "kda": 1.6}}}}},
		"/shards/steam/matches/" + m1: pgMatch(m1, "2026-10-12T20:00:00Z"),
		"/shards/steam/matches/" + m2: pgMatch(m2, "2026-10-12T21:00:00Z"),
	}
	cloud.mu.Unlock()

	found, err := a.Pubg.Search("Me", "steam")
	if err != nil || len(found) != 1 || found[0].AccountID != pgMe {
		t.Fatalf("search: %+v %v", found, err)
	}
	if _, err := a.Pubg.Search("Nobody", "steam"); err == nil || !strings.Contains(err.Error(), "case-sensitive") {
		t.Fatalf("unknown name: %v", err)
	}
	if err := a.Pubg.SetLink(found[0]); err != nil {
		t.Fatal(err)
	}
	ov, err := a.Pubg.Overview(false)
	if err != nil {
		t.Fatal(err)
	}
	if ov.Season != "division.bro.official.pc-2018-38" || len(ov.Modes) != 1 || ov.Modes[0].Rounds != 40 || ov.Modes[0].ModeName != "Squad FPP" {
		t.Fatalf("season: %+v", ov)
	}
	if len(ov.Ranked) != 1 || ov.Ranked[0].Tier != "Gold" || ov.Ranked[0].Points != 2410 || ov.Ranked[0].BestTier != "Platinum" {
		t.Fatalf("ranked: %+v", ov.Ranked)
	}
	if len(ov.Matches) != 2 || ov.Matches[0].ID != m2 || ov.Missing != 0 {
		t.Fatalf("matches newest first: %+v", ov.Matches)
	}
	// Matches are kept for good: the cloud isn't asked for them again.
	cloud.mu.Lock()
	delete(cloud.pubg, "/shards/steam/matches/"+m1)
	cloud.mu.Unlock()
	if ov, err = a.Pubg.Overview(true); err != nil || len(ov.Matches) != 2 {
		t.Fatalf("cached matches: %+v %v", ov.Matches, err)
	}
}

func TestPubgCountsAsAGameForTheConnectionCheck(t *testing.T) {
	a := newTestApp(t)
	old := runningPrograms
	runningPrograms = func() map[uint32]string { return map[uint32]string{1: "TslGame"} }
	t.Cleanup(func() { runningPrograms = old })
	a.Store.UpdatePrefs(func(p *Prefs) { p.Games = Games{PUBG: true, Chosen: true} })
	if g := a.activeGame(); g != "PUBG" {
		t.Fatalf("PUBG open: %q", g)
	}
	a.Store.UpdatePrefs(func(p *Prefs) { p.Games = Games{Dota: true, Chosen: true} })
	if g := a.activeGame(); g != "" {
		t.Fatalf("PUBG switched off: %q", g)
	}
}
