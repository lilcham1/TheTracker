package core

import (
	"strings"
	"testing"
	"time"
)

func TestDotaStreamersPlacesStreamsOnHeroes(t *testing.T) {
	game := func(match, lobby, mode int, players ...map[string]any) map[string]any {
		list := []any{}
		for _, p := range players {
			list = append(list, p)
		}
		return map[string]any{"match_id": match, "lobby_type": lobby, "game_mode": mode, "activate_time": 1000, "players": list}
	}
	pl := func(id, hero int, name string) map[string]any {
		p := map[string]any{"account_id": id, "hero_id": hero}
		if name != "" {
			p["name"] = name
		}
		return p
	}
	f := newFakeAPI(t, map[string]any{
		"/heroes": odHeroes, // Anti-Mage 1, Axe 2, Crystal Maiden 5
		"/live": []any{
			game(1, 7, 22, pl(11, 1, "ProGuy"), pl(12, 2, ""), pl(13, 5, ""), pl(14, 2, "")),
			game(2, 0, 22, pl(21, 5, "")),
			game(3, 7, 23, pl(31, 1, "TurboGuy")), // Turbo: left out
			game(4, 1, 2, pl(41, 2, "LeaguePro")), // league lobby: left out
		},
		"/proPlayers": []any{
			map[string]any{"account_id": 12, "name": "Prostream", "personaname": "x"},
			map[string]any{"account_id": 13, "name": "Twice", "personaname": "x"},
			map[string]any{"account_id": 14, "name": "Twice", "personaname": "x"}, // two live accounts: ambiguous
		},
		"/search": []any{
			map[string]any{"account_id": 21, "personaname": "SearchMe", "last_match_time": time.Now().Add(-48 * time.Hour).Format(time.RFC3339)},
			map[string]any{"account_id": 22, "personaname": "SearchMe", "last_match_time": "2019-03-01T10:00:00.000Z"}, // abandoned
			map[string]any{"account_id": 99, "personaname": "SearchMeNot", "last_match_time": time.Now().Format(time.RFC3339)},
		},
		"/players/11":       map[string]any{"rank_tier": 80, "leaderboard_rank": 42},
		"/players/12":       map[string]any{"rank_tier": 54},
		"/players/21":       map[string]any{"rank_tier": 72},
		"/v1/players/steam": []any{},
		"/ILeaderboard/GetDivisionLeaderboard/v0001": map[string]any{"leaderboard": []any{map[string]any{"rank": 7, "name": "Topdog"}}},
	})
	dotaSearches.mu.Lock()
	dotaSearches.every = 0
	dotaSearches.mu.Unlock()
	a := newTestApp(t)
	a.Dota.api = &service{Name: "OpenDota", Base: f.srv.URL, Attempts: 1}
	a.Dota.valve = &service{Name: "Dota's leaderboard", Base: f.srv.URL, Attempts: 1}
	a.Deadlock.api = &service{Name: "The Deadlock API", Base: f.srv.URL, Attempts: 1}
	conv := newFakeConvex(t)
	a.Cloud.base = conv.srv.URL
	conv.twitch = map[string]any{"ok": true, "streams": []any{
		map[string]any{"login": "proguy", "name": "ProGuy", "viewers": 500},       // the pro name in the game
		map[string]any{"login": "prostream", "name": "Prostream", "viewers": 300}, // the pro list
		map[string]any{"login": "twice", "name": "Twice", "viewers": 200},         // ambiguous
		map[string]any{"login": "searchme", "name": "SearchMe", "viewers": 100},   // found by search
		map[string]any{"login": "topdog", "name": "Topdog", "viewers": 50},        // leaderboard, not in a game
		map[string]any{"login": "turboguy", "name": "TurboGuy", "viewers": 40},    // in a Turbo game only
	}}

	b, err := a.DotaStreamers(true)
	if err != nil {
		t.Fatal(err)
	}
	// Name searches run in the background; the next build uses them.
	waitFor(t, "the name searches", func() bool {
		dotaSearches.mu.Lock()
		defer dotaSearches.mu.Unlock()
		return !dotaSearches.running
	})
	if b, err = a.DotaStreamers(true); err != nil {
		t.Fatal(err)
	}
	if b.Matches != 2 || !b.StreamsAvailable {
		t.Fatalf("only the ranked and unranked standard games count: %+v", b)
	}
	by := map[uint64]LivePlayer{}
	hero := map[uint64]string{}
	for _, h := range b.Heroes {
		for _, p := range h.Players {
			by[p.AccountID] = p
			hero[p.AccountID] = h.Name
		}
	}
	if _, ok := by[31]; ok {
		t.Fatal("a Turbo game must be left out")
	}
	if p := by[11]; p.Stream == nil || p.Via != "name" || hero[11] != "Anti-Mage" || p.Rank == nil || p.Rank.Label != "Immortal #42" {
		t.Fatalf("the pro name in the game: %+v", p)
	}
	if p := by[12]; p.Stream == nil || p.Via != "pro" || p.Rank == nil || p.Rank.Label != "Legend 4" || p.Rank.Star == nil {
		t.Fatalf("the pro list: %+v", p)
	}
	if by[13].Stream != nil || by[14].Stream != nil {
		t.Fatal("a pro name on two live accounts must not be guessed")
	}
	if p := by[21]; p.Stream == nil || p.Via != "search" || p.Mode != "Unranked" || hero[21] != "Crystal Maiden" {
		t.Fatalf("the name search: %+v", p)
	}
	if len(b.Ranked) != 1 || b.Ranked[0].Stream.Login != "topdog" || b.Ranked[0].Position != 7 {
		t.Fatalf("a leaderboard streamer whose game isn't listed: %+v", b.Ranked)
	}
	if b.Heroes[0].Image == nil || !strings.Contains(*b.Heroes[0].Image, "/heroes/") {
		t.Fatalf("hero portraits: %+v", b.Heroes[0])
	}

	// Linking by hand wins over everything, and links are per game.
	if _, err := a.Store.LinkStreamer("dota", "twice", 13, "x", nil); err != nil {
		t.Fatal(err)
	}
	if len(a.Store.StreamLinksFor("deadlock")) != 0 {
		t.Fatal("a Dota link must not show up for Deadlock")
	}
	b, _ = a.DotaStreamers(true)
	for _, h := range b.Heroes {
		for _, p := range h.Players {
			if p.AccountID == 13 && (p.Stream == nil || p.Via != "link") {
				t.Fatalf("the link should place Twice: %+v", p)
			}
		}
	}
	if len(b.Linked) != 1 || b.Linked[0].HeroName != "Crystal Maiden" {
		t.Fatalf("linked status: %+v", b.Linked)
	}

	// Searches are remembered: a second build asks OpenDota again for none.
	before := strings.Count(strings.Join(f.queries, " "), "/search")
	a.DotaStreamers(true)
	if after := strings.Count(strings.Join(f.queries, " "), "/search"); after != before {
		t.Fatalf("a name already looked up was searched again (%d → %d)", before, after)
	}
}

func TestDotaMedals(t *testing.T) {
	if m := dotaMedal(0, 0); m != nil {
		t.Fatal("uncalibrated has no medal")
	}
	m := dotaMedal(35, 0)
	if m.Label != "Crusader 5" || m.Star == nil || !strings.HasSuffix(*m.Icon, "rank_icon_3.png") || !strings.HasSuffix(*m.Star, "rank_star_5.png") {
		t.Fatalf("crusader 5: %+v", m)
	}
	if m := dotaMedal(80, 0); m.Label != "Immortal" || m.Star != nil {
		t.Fatalf("immortal without a place: %+v", m)
	}
}

func TestDotaStreamersSteamAndLastGame(t *testing.T) {
	recentStart := time.Now().Add(-2 * time.Hour).Unix()
	dotaSearches.mu.Lock()
	dotaSearches.every = 0
	dotaSearches.mu.Unlock()
	dotaLastGames.mu.Lock()
	dotaLastGames.every = 0
	dotaLastGames.mu.Unlock()
	steamMemo.Lock()
	steamMemo.seen = map[uint64]steamSeen{}
	steamMemo.Unlock()

	f := newFakeAPI(t, map[string]any{
		"/heroes": odHeroes,
		"/live":   []any{}, // nothing in the top list
		"/proPlayers": []any{
			map[string]any{"account_id": 50, "name": "FarGuy"},
			map[string]any{"account_id": 51, "name": "TurboFan"},
			map[string]any{"account_id": 52, "name": "Between"},
		},
		"/search":                   []any{},
		"/players/52/recentMatches": []any{map[string]any{"hero_id": 5, "player_slot": 130, "radiant_win": false, "start_time": recentStart, "duration": 1800}},
		"/players/50":               map[string]any{"rank_tier": 65},
		"/v1/players/steam":         []any{},
		"/ILeaderboard/GetDivisionLeaderboard/v0001": map[string]any{"leaderboard": []any{}},
	})
	a := newTestApp(t)
	a.Dota.api = &service{Name: "OpenDota", Base: f.srv.URL, Attempts: 1}
	a.Dota.valve = &service{Name: "Dota's leaderboard", Base: f.srv.URL, Attempts: 1}
	a.Deadlock.api = &service{Name: "The Deadlock API", Base: f.srv.URL, Attempts: 1}
	conv := newFakeConvex(t)
	a.Cloud.base = conv.srv.URL
	conv.twitch = map[string]any{"ok": true, "streams": []any{
		map[string]any{"login": "farguy", "name": "FarGuy", "viewers": 90},
		map[string]any{"login": "turbofan", "name": "TurboFan", "viewers": 40},
		map[string]any{"login": "between", "name": "Between", "viewers": 30},
	}}

	// Without the Steam key the page says so, and nothing is placed live.
	b, err := a.DotaStreamers(true)
	if err != nil {
		t.Fatal(err)
	}
	if b.SteamAvailable || b.SteamReason != "not_set_up" {
		t.Fatalf("no Steam function should read as not set up: %+v", b)
	}

	conv.steamLive = map[string]any{"ok": true, "players": []any{
		map[string]any{"accountId": "50", "heroId": 2, "matchId": "777", "gameMode": 22, "lobbyType": 7, "gameTime": 600},
		map[string]any{"accountId": "51", "heroId": 1, "matchId": "778", "gameMode": 23, "lobbyType": 0, "gameTime": 300}, // Turbo
	}}
	steamMemo.Lock()
	steamMemo.seen = map[uint64]steamSeen{}
	steamMemo.Unlock()
	waitFor(t, "the last games", func() bool {
		dotaLastGames.mu.Lock()
		defer dotaLastGames.mu.Unlock()
		return !dotaLastGames.running
	})
	b, err = a.DotaStreamers(true)
	if err != nil {
		t.Fatal(err)
	}
	if !b.SteamAvailable {
		t.Fatalf("Steam should be on: %+v", b)
	}
	if len(b.Heroes) != 1 || b.Heroes[0].Name != "Axe" || b.Heroes[0].Players[0].Via != "steam" || b.Heroes[0].Players[0].Mode != "Ranked" || b.Heroes[0].Players[0].Rank == nil {
		t.Fatalf("FarGuy should be on Axe through Steam, ranked, with a medal: %+v", b.Heroes)
	}
	for _, h := range b.Heroes {
		for _, p := range h.Players {
			if p.AccountID == 51 {
				t.Fatal("a Turbo game must not place a streamer")
			}
		}
	}
	found := false
	for _, r := range b.Recent {
		if r.Stream.Login == "between" {
			found = true
			if r.HeroName != "Crystal Maiden" || !r.Won || r.EndedAt != recentStart+1800 {
				t.Fatalf("Between's last game: %+v", r)
			}
		}
	}
	if !found {
		t.Fatalf("Between should be listed with their last game: %+v", b.Recent)
	}
}
