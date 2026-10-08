package core

import (
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
		{"HazeMain twitch", stream("hazemain", "HazeMain")},
		{"[EU] SoulTaker ttv", stream("soultaker", "SoulTaker")},
		{"twitch.tv/lockjaw99", stream("lockjaw99", "Lockjaw99")},
		{"Ʀüh - xNovaa", stream("xnovaa", "xNovaa")},
		{"metro_mann", stream("metro", "Metro")},
		{"sidescrap<3jula", stream("sidescrap", "sidescrap")},
	}
	for _, c := range yes {
		if !sameStreamer(c.steam, "", c.s, false) {
			t.Errorf("%q should match %q", c.steam, c.s.Login)
		}
	}
	no := []struct {
		steam string
		s     LiveStream
	}{
		{"Bob", stream("bobross", "BobRoss")},    // a short name inside a longer one
		{"Gamer123", stream("gam", "gam")},       // the channel name is too short to trust partly
		{"Haze", stream("hazemain", "HazeMain")}, // the Steam name is the shorter one
		{"???", stream("abc", "abc")},            // nothing left to compare
		{"PlayerOne", stream("playertwo", "PlayerTwo")},
		{"average catgirl enjoyer", stream("average", "average")}, // a common word in a long name
		{"thedeathydlfan", stream("deathy", "Deathy")},            // a fan, not the streamer
	}
	for _, c := range no {
		if sameStreamer(c.steam, "", c.s, false) {
			t.Errorf("%q must not match %q", c.steam, c.s.Login)
		}
	}
}

func TestLiveMatchesTheSteamProfileAddressToo(t *testing.T) {
	s := LiveStream{Login: "deludeddelirium", Name: "DeludedDelirium"}
	if !sameStreamer("DΣLIЯIUM", "DeludedDelirium", s, true) {
		t.Fatal("the custom profile address names the channel")
	}
	if steamVanity("https://steamcommunity.com/id/DeludedDelirium/") != "DeludedDelirium" || steamVanity("https://steamcommunity.com/profiles/76561198061446721/") != "" {
		t.Fatal("profile address read wrongly")
	}
	// A partial match is not an exact one.
	if sameStreamer("metro_mann", "", LiveStream{Login: "metro"}, true) || !sameStreamer("metro_mann", "", LiveStream{Login: "metro"}, false) {
		t.Fatal("exact and partial passes mixed up")
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
