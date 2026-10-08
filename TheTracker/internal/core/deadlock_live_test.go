package core

import (
	"strings"
	"testing"
)

func TestLiveNamesMatchTheSamePersonOnly(t *testing.T) {
	stream := func(login, name string) LiveStream { return LiveStream{Login: login, Name: name} }
	yes := []struct {
		steam string
		s     LiveStream
	}{
		{"Mew2King", stream("mew2king", "Mew2King")},
		{"TTV_HazeMain", stream("hazemain", "HazeMain")},
		{"ttvHazeMain", stream("hazemain", "HazeMain")},
		{"HazeMain twitch", stream("hazemain", "HazeMain")},
		{"[EU] SoulTaker ttv", stream("soultaker", "SoulTaker")},
		{"twitch.tv/lockjaw99", stream("lockjaw99", "Lockjaw99")},
		{"pandaego live", stream("pandaego", "Pandaego")},
		{"MaleniaDL on twitch", stream("maleniadl", "maleniadl")},
		{"LukieVibinYT", stream("lukievibin", "lukievibin")},
		{"connorDMG + TTV", stream("connordmg", "connorDMG")},
		{"(TTV) Gibdin", stream("gibdin", "Gibdin")},
	}
	for _, c := range yes {
		if !sameStreamer(c.steam, "", c.s) {
			t.Errorf("%q should match %q", c.steam, c.s.Login)
		}
	}
	no := []struct {
		steam string
		s     LiveStream
	}{
		{"KenshinH", stream("kenshittv", "KenshiTTV")}, // a different name that starts the same
		{"metro_mann", stream("metro", "Metro")},       // someone else with a longer name
		{"Bob", stream("bobross", "BobRoss")},          // a short name inside a longer one
		{"Haze", stream("hazemain", "HazeMain")},       // the Steam name is the shorter one
		{"???", stream("abc", "abc")},                  // nothing left to compare
		{"PlayerOne", stream("playertwo", "PlayerTwo")},
		{"average catgirl enjoyer", stream("average", "average")}, // a common word in a long name
		{"thedeathydlfan", stream("deathy", "Deathy")},            // a fan, not the streamer
		{"olive", stream("o", "o")},                               // "live" is only a tag as a word of its own
	}
	for _, c := range no {
		if sameStreamer(c.steam, "", c.s) {
			t.Errorf("%q must not match %q", c.steam, c.s.Login)
		}
	}
}

func TestLiveMatchesTheSteamProfileAddressToo(t *testing.T) {
	s := LiveStream{Login: "deludeddelirium", Name: "DeludedDelirium"}
	if !sameStreamer("DΣLIЯIUM", "DeludedDelirium", s) {
		t.Fatal("the custom profile address names the channel")
	}
	if steamVanity("https://steamcommunity.com/id/DeludedDelirium/") != "DeludedDelirium" || steamVanity("https://steamcommunity.com/profiles/76561198061446721/") != "" {
		t.Fatal("profile address read wrongly")
	}

}

func TestDeadlockLiveBoard(t *testing.T) {
	f := newFakeAPI(t, map[string]any{
		"/v1/assets/heroes": []any{
			map[string]any{"id": 1, "name": "Haze", "images": map[string]any{"icon_image_small": "haze.png"}},
			map[string]any{"id": 2, "name": "Seven"},
			map[string]any{"id": 3, "name": "Paradox"},
		},
		"/v1/matches/active": []any{
			map[string]any{"match_id": 10, "start_time": 1000, "match_mode_parsed": "Ranked", "game_mode_parsed": "KECitadelGameModeNormal", "players": []any{
				map[string]any{"account_id": 1, "hero_id": 1}, map[string]any{"account_id": 2, "hero_id": 2}, map[string]any{"account_id": 3, "hero_id": 1},
			}},
			map[string]any{"match_id": 11, "start_time": 2000, "match_mode_parsed": "Unranked", "game_mode_parsed": "KECitadelGameModeStreetBrawl", "players": []any{
				map[string]any{"account_id": 4, "hero_id": 3}, map[string]any{"account_id": 5, "hero_id": 1},
			}},
		},
		"/v1/players/steam": []any{
			map[string]any{"account_id": 1, "personaname": "TTV_BigHaze", "realname": "not shown"},
			map[string]any{"account_id": 2, "personaname": "sevenfan"},
			map[string]any{"account_id": 3, "personaname": "quietplayer"},
			map[string]any{"account_id": 5, "personaname": "smallhaze"},
		},
	})
	a := newTestApp(t)
	a.Deadlock.api = &service{Name: "The Deadlock API", Base: f.srv.URL, Attempts: 1}

	// Before Twitch is set up the players are still there.
	b, err := a.DeadlockLive(false)
	if err != nil {
		t.Fatal(err)
	}
	if b.StreamsAvailable || b.Matches != 1 || len(b.Heroes) != 2 || b.Heroes[0].Name != "Haze" || len(b.Heroes[0].Players) != 2 {
		t.Fatalf("without Twitch, and without the Street Brawl match: %+v", b)
	}
	if b.Heroes[0].Players[0].Mode != "Ranked" {
		t.Fatalf("modes wrong: %+v", b.Heroes[0].Players)
	}

	conv := newFakeConvex(t)
	a.Cloud.base = conv.srv.URL
	conv.twitch = map[string]any{"ok": false, "reason": "not_set_up"}
	if b, _ = a.DeadlockLive(true); b.StreamsAvailable || b.Reason != "not_set_up" {
		t.Fatalf("not set up should say so: %+v", b)
	}

	conv.twitch = map[string]any{"ok": true, "streams": []any{
		map[string]any{"login": "bighaze", "name": "BigHaze", "title": "rank 1 haze", "viewers": 900},
		map[string]any{"login": "sevenfan", "name": "SevenFan", "viewers": 40},
		map[string]any{"login": "someoneelse", "name": "SomeoneElse", "viewers": 2000},
	}}
	b, err = a.DeadlockLive(true)
	if err != nil {
		t.Fatal(err)
	}
	if !b.StreamsAvailable {
		t.Fatalf("streams should be available: %+v", b)
	}
	haze := b.Heroes[0]
	if haze.Name != "Haze" || haze.Streams != 1 || haze.Ranked != 1 || haze.Players[0].Stream == nil || haze.Players[0].Stream.Login != "bighaze" || haze.Players[0].Name != "TTV_BigHaze" {
		t.Fatalf("the Haze streamer should lead: %+v", haze)
	}
	if b.Heroes[1].Name != "Seven" || b.Heroes[1].Players[0].Stream == nil {
		t.Fatalf("heroes with streamers come first: %+v", b.Heroes)
	}
	if len(b.Other) != 1 || b.Other[0].Login != "someoneelse" {
		t.Fatalf("an unmatched stream is listed apart: %+v", b.Other)
	}
}

func TestLinkedStreamersAreFoundWhateverTheirName(t *testing.T) {
	if l, ok := twitchFrom("https://www.twitch.tv/TiffIsPurrfect?sr=a"); !ok || l != "tiffispurrfect" {
		t.Fatalf("twitch link read wrongly: %q", l)
	}
	if _, ok := twitchFrom("not a channel!"); ok {
		t.Fatal("rubbish accepted as a channel")
	}

	f := newFakeAPI(t, map[string]any{
		"/v1/assets/heroes": []any{map[string]any{"id": 13, "name": "Haze"}, map[string]any{"id": 2, "name": "Seven"}},
		"/v1/matches/active": []any{
			map[string]any{"match_id": 10, "start_time": 1000, "match_mode_parsed": "Ranked", "game_mode_parsed": "KECitadelGameModeNormal", "players": []any{
				map[string]any{"account_id": 7, "hero_id": 13}, map[string]any{"account_id": 8, "hero_id": 2},
			}},
		},
		"/v1/players/steam": []any{
			map[string]any{"account_id": 7, "personaname": "Tiff ♡"},
			map[string]any{"account_id": 8, "personaname": "someone"},
		},
	})
	a := newTestApp(t)
	a.Deadlock.api = &service{Name: "The Deadlock API", Base: f.srv.URL, Attempts: 1}
	conv := newFakeConvex(t)
	a.Cloud.base = conv.srv.URL
	conv.twitch = map[string]any{"ok": true, "streams": []any{
		map[string]any{"login": "tiffispurrfect", "name": "TiffIsPurrfect", "viewers": 79},
		map[string]any{"login": "offmatch", "name": "OffMatch", "viewers": 5},
	}}

	// Without a link the names don't agree, so she isn't found.
	b, err := a.DeadlockLive(true)
	if err != nil {
		t.Fatal(err)
	}
	if b.Heroes[0].Streams != 0 || b.Heroes[1].Streams != 0 {
		t.Fatalf("a different name must not match: %+v", b.Heroes)
	}

	if _, err := a.Store.LinkStreamer("twitch.tv/TiffIsPurrfect", 7, "Tiff ♡", nil); err != nil {
		t.Fatal(err)
	}
	a.Store.LinkStreamer("offmatch", 99, "Elsewhere", nil)
	b, _ = a.DeadlockLive(true)
	var haze LiveHero
	for _, h := range b.Heroes {
		if h.Name == "Haze" {
			haze = h
		}
	}
	if haze.Streams != 1 || haze.Players[0].Stream == nil || haze.Players[0].Stream.Login != "tiffispurrfect" || !haze.Players[0].Linked {
		t.Fatalf("the linked account should carry her stream on Haze: %+v", haze)
	}
	if len(b.Linked) != 2 || b.Linked[0].Twitch != "offmatch" || b.Linked[0].Stream == nil || b.Linked[0].HeroName != "" {
		t.Fatalf("a linked streamer who is live but not in a listed match: %+v", b.Linked)
	}
	if b.Linked[1].Twitch != "tiffispurrfect" || b.Linked[1].HeroName != "Haze" || b.Linked[1].Mode != "Ranked" {
		t.Fatalf("a linked streamer in a listed match: %+v", b.Linked)
	}
	if left := a.Store.UnlinkStreamer("https://twitch.tv/offmatch"); len(left) != 1 || left[0].Twitch != "tiffispurrfect" {
		t.Fatalf("unlink failed: %+v", left)
	}
}

func TestStreamersCarryTheirRankAndIcon(t *testing.T) {
	liveRanks.mu.Lock()
	liveRanks.seen = map[uint64]rankSeen{}
	liveRanks.mu.Unlock()
	f := newFakeAPI(t, map[string]any{
		"/v1/assets/heroes": []any{map[string]any{"id": 13, "name": "Haze"}},
		"/v1/assets/ranks": []any{map[string]any{"tier": 8, "name": "Oracle", "images": map[string]any{
			"large": "rank08_lg.png", "small_subrank3": "rank08_sm_3.png", "small_subrank3_webp": "rank08_sm_3.webp",
		}}},
		"/v1/matches/active": []any{
			map[string]any{"match_id": 10, "start_time": 1000, "match_mode_parsed": "Ranked", "game_mode_parsed": "KECitadelGameModeNormal", "players": []any{
				map[string]any{"account_id": 7, "hero_id": 13}, map[string]any{"account_id": 8, "hero_id": 13},
			}},
		},
		"/v1/players/steam": []any{
			map[string]any{"account_id": 7, "personaname": "TTV_Streamer"},
			map[string]any{"account_id": 8, "personaname": "quiet"},
		},
		"/v1/players/rank": []any{
			map[string]any{"account_id": 7, "badge": 83},
			map[string]any{"account_id": 8, "badge": 115},
		},
	})
	a := newTestApp(t)
	a.Deadlock.api = &service{Name: "The Deadlock API", Base: f.srv.URL, Attempts: 1}
	conv := newFakeConvex(t)
	a.Cloud.base = conv.srv.URL
	conv.twitch = map[string]any{"ok": true, "streams": []any{map[string]any{"login": "streamer", "name": "Streamer", "viewers": 10}}}

	b, err := a.DeadlockLive(true)
	if err != nil {
		t.Fatal(err)
	}
	var streamer, quiet *LivePlayer
	for i := range b.Heroes[0].Players {
		p := &b.Heroes[0].Players[i]
		if p.AccountID == 7 {
			streamer = p
		} else {
			quiet = p
		}
	}
	if streamer == nil || streamer.Rank == nil || streamer.Rank.Label != "Oracle 3" || streamer.Rank.Icon == nil || *streamer.Rank.Icon != "rank08_sm_3.png" {
		t.Fatalf("the streamer's rank and icon: %+v", streamer)
	}
	if quiet.Rank != nil {
		t.Fatal("ranks are only looked up for streamers")
	}
	q := strings.Join(f.queries, " ")
	if !strings.Contains(q, "/v1/players/rank?account_ids=7") || strings.Contains(q, "/v1/players/rank?account_ids=7,8") {
		t.Fatalf("only the streamer's rank should be asked for: %s", q)
	}
}

func TestLeaderboardNamesFindStreamers(t *testing.T) {
	liveRanks.mu.Lock()
	liveRanks.seen = map[uint64]rankSeen{}
	liveRanks.mu.Unlock()
	board := func(entries ...map[string]any) map[string]any {
		list := []any{}
		for _, e := range entries {
			list = append(list, e)
		}
		return map[string]any{"entries": list}
	}
	f := newFakeAPI(t, map[string]any{
		"/v1/assets/heroes": []any{map[string]any{"id": 13, "name": "Haze"}, map[string]any{"id": 2, "name": "Seven"}},
		"/v1/matches/active": []any{
			map[string]any{"match_id": 10, "start_time": 1000, "match_mode_parsed": "Ranked", "game_mode_parsed": "KECitadelGameModeNormal", "players": []any{
				map[string]any{"account_id": 7, "hero_id": 13}, // "BigStreamer" on the board, a different Steam name now
				map[string]any{"account_id": 8, "hero_id": 2},  // named exactly like the "Twin" channel
				map[string]any{"account_id": 20, "hero_id": 2}, // one of two accounts behind "Twin" on the board
				map[string]any{"account_id": 30, "hero_id": 13},
				map[string]any{"account_id": 31, "hero_id": 13}, // "Doubled": two candidates both live
			}},
		},
		"/v1/players/steam": []any{
			map[string]any{"account_id": 7, "personaname": "renamed lol"},
			map[string]any{"account_id": 8, "personaname": "Twin"},
			map[string]any{"account_id": 20, "personaname": "x"},
			map[string]any{"account_id": 30, "personaname": "y"},
			map[string]any{"account_id": 31, "personaname": "z"},
		},
		"/v1/leaderboard/Europe": board(
			map[string]any{"rank": 3, "account_name": "BigStreamer", "possible_account_ids": []any{7}},
			map[string]any{"rank": 9, "account_name": "Twin", "possible_account_ids": []any{20, 21}},
			map[string]any{"rank": 12, "account_name": "Doubled", "possible_account_ids": []any{30, 31}},
			map[string]any{"rank": 40, "account_name": "Elsewhere", "possible_account_ids": []any{99}},
		),
		"/v1/players/rank": []any{map[string]any{"account_id": 99, "badge": 114}},
	})
	a := newTestApp(t)
	a.Deadlock.api = &service{Name: "The Deadlock API", Base: f.srv.URL, Attempts: 1}
	conv := newFakeConvex(t)
	a.Cloud.base = conv.srv.URL
	conv.twitch = map[string]any{"ok": true, "streams": []any{
		map[string]any{"login": "bigstreamer", "name": "BigStreamer", "viewers": 900},
		map[string]any{"login": "twin", "name": "Twin", "viewers": 50},
		map[string]any{"login": "doubled", "name": "Doubled", "viewers": 30},
		map[string]any{"login": "elsewhere", "name": "Elsewhere", "viewers": 70},
	}}

	b, err := a.DeadlockLive(true)
	if err != nil {
		t.Fatal(err)
	}
	by := map[uint64]LivePlayer{}
	for _, h := range b.Heroes {
		for _, p := range h.Players {
			by[p.AccountID] = p
		}
	}
	if p := by[7]; p.Stream == nil || p.Stream.Login != "bigstreamer" || p.Via != "leaderboard" {
		t.Fatalf("the leaderboard should tie BigStreamer to account 7: %+v", p)
	}
	// An exact Steam name beats the leaderboard.
	if p := by[8]; p.Stream == nil || p.Via != "name" || by[20].Stream != nil {
		t.Fatalf("the Steam name should win over the leaderboard: %+v / %+v", p, by[20])
	}
	// Two live candidates: nobody gets the stream.
	if by[30].Stream != nil || by[31].Stream != nil {
		t.Fatal("an ambiguous leaderboard name must not place a stream")
	}
	if len(b.Ranked) != 1 || b.Ranked[0].Stream.Login != "elsewhere" || b.Ranked[0].Position != 40 || b.Ranked[0].Region != "Europe" || b.Ranked[0].Rank == nil || b.Ranked[0].Rank.Label != "Eternus 4" {
		t.Fatalf("a leaderboard streamer whose match isn't listed: %+v", b.Ranked)
	}
	if len(b.Other) != 1 || b.Other[0].Login != "doubled" {
		t.Fatalf("the ambiguous stream stays unplaced: %+v", b.Other)
	}
}
