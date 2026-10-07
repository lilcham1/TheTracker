package core

import (
	"encoding/base64"
	"os"
	"strings"
	"testing"
)

func TestSteamAccountFromWhatPeoplePaste(t *testing.T) {
	for in, want := range map[string]uint64{
		"850402858":         850402858,
		"76561198810668586": 850402858,
		"https://steamcommunity.com/profiles/76561198810668586/": 850402858,
	} {
		if got, ok := steamAccountFrom(in); !ok || got != want {
			t.Errorf("%q: got %d %v, want %d", in, got, ok, want)
		}
	}
	for _, in := range []string{"Miracle", "https://steamcommunity.com/id/someone", "0", "99999999999999"} {
		if _, ok := steamAccountFrom(in); ok {
			t.Errorf("%q should be searched by name, not read as an id", in)
		}
	}
}

func TestCompareDotaUsesBothAccounts(t *testing.T) {
	won := func(id int, hero int, win bool, gpm int) map[string]any {
		return map[string]any{"match_id": id, "hero_id": hero, "player_slot": 0, "radiant_win": win, "kills": 10, "deaths": 2, "assists": 5, "gold_per_min": gpm, "xp_per_min": 600, "last_hits": 200, "hero_damage": 20000}
	}
	d, f, store := newTestDota(t, map[string]any{
		"/players/1/matches": []any{won(1, 1, true, 600), won(2, 1, false, 500)},
		"/players/2/matches": []any{won(3, 2, true, 400), won(4, 5, true, 450), won(5, 2, false, 350)},
		"/players/1":         map[string]any{"rank_tier": 54, "profile": map[string]any{"personaname": "me"}},
		"/players/2":         map[string]any{"rank_tier": 71, "profile": map[string]any{"personaname": "Friend"}},
	})
	a := newTestApp(t)
	a.Store, a.Dota = store, d
	linked(store, "dota", 1)

	c, err := a.Compare(Friend{Game: "dota", ID: "2", Name: "Friend"}, "", false)
	if err != nil {
		t.Fatal(err)
	}
	if c.Me.Games != 2 || c.Me.WinRate != 50 || c.Them.Games != 3 || c.Them.Wins != 2 {
		t.Fatalf("records wrong: me %+v them %+v", c.Me, c.Them)
	}
	if c.Me.Rank != "Legend 4" || c.Them.Rank != "Divine 1" || c.Them.Name != "Friend" {
		t.Fatalf("names and ranks wrong: %+v / %+v", c.Me, c.Them)
	}
	if c.Me.Stats[0] != 550 || c.Them.Stats[0] != 400 || len(c.StatLabels) != len(c.Me.Stats) {
		t.Fatalf("averages wrong: %v %v", c.Me.Stats, c.Them.Stats)
	}
	if len(c.Them.Heroes) != 2 || c.Them.Heroes[0].Name != "Axe" || c.Them.Heroes[0].Games != 2 {
		t.Fatalf("heroes wrong, most played first: %+v", c.Them.Heroes)
	}
	if !strings.Contains(strings.Join(f.queries, " "), "/players/2/matches") {
		t.Fatal("the friend's matches were not asked for")
	}
	if fr := store.Friends(); len(fr) != 1 || fr[0].ID != "2" {
		t.Fatalf("the friend should be remembered: %+v", fr)
	}
	// The friend list keeps the newest first and no duplicates.
	store.RememberFriend(Friend{Game: "deadlock", ID: "9", Name: "Other"})
	store.RememberFriend(Friend{Game: "dota", ID: "2", Name: "Friend"})
	if fr := store.Friends(); len(fr) != 2 || fr[0].ID != "2" {
		t.Fatalf("recent friends wrong: %+v", fr)
	}
	if fr := store.ForgetFriend("dota", "2"); len(fr) != 1 || fr[0].ID != "9" {
		t.Fatalf("forgetting failed: %+v", fr)
	}
}

func TestSaveImageOnlyWritesPNGs(t *testing.T) {
	dir := t.TempDir()
	png := append([]byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'}, make([]byte, 32)...)
	path, err := SaveImage(dir, "Dota 2 / form", "data:image/png;base64,"+base64.StdEncoding.EncodeToString(png))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(path, dir) || strings.Contains(path[len(dir):], "/form") || !strings.HasSuffix(path, ".png") {
		t.Fatalf("unexpected path %s", path)
	}
	if raw, _ := os.ReadFile(path); len(raw) != len(png) {
		t.Fatal("the image was not written whole")
	}
	if _, err := SaveImage(dir, "x", "data:image/png;base64,"+base64.StdEncoding.EncodeToString([]byte("<html>"))); err == nil {
		t.Fatal("something that is not a PNG must be refused")
	}
	if _, err := SaveImage(dir, "x", "data:text/html;base64,AAAA"); err == nil {
		t.Fatal("a non-image data URL must be refused")
	}
}
