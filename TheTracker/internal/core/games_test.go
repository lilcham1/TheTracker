package core

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// ---------- Sign in with Steam ----------

// loginHarness is an app wired to a fake Steam, a fake cloud and a fake
// OpenDota, with a "browser" that behaves like a player logging in.
type loginHarness struct {
	app    *App
	cloud  *fakeConvex
	checks int // how many times Steam was asked to confirm a statement
	// What the browser brings back from Steam.
	sig   string
	steam string // 64-bit id
	// Overrides for misbehaving browsers.
	tamperState bool
	cancel      bool
}

func newLoginHarness(t *testing.T) *loginHarness {
	h := &loginHarness{sig: "good", steam: "76561198810668586"}
	steam := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.ParseForm()
		h.checks++
		if r.Form.Get("openid.mode") == "check_authentication" && r.Form.Get("openid.sig") == "good" {
			fmt.Fprint(w, "ns:http://specs.openid.net/auth/2.0\nis_valid:true\n")
			return
		}
		fmt.Fprint(w, "ns:http://specs.openid.net/auth/2.0\nis_valid:false\n")
	}))
	t.Cleanup(steam.Close)
	old := steamOpenIDEndpoint
	steamOpenIDEndpoint = steam.URL
	t.Cleanup(func() { steamOpenIDEndpoint = old })

	od := newFakeAPI(t, map[string]any{
		"/players/850402858": map[string]any{"rank_tier": 72, "profile": map[string]any{"personaname": "lilcham", "avatarfull": "https://avatars.steamstatic.com/a.jpg"}},
	})
	h.cloud = newFakeConvex(t)
	h.app = NewApp(t.TempDir(), &NoShell{})
	h.app.SetAPIBases(od.srv.URL, "http://127.0.0.1:1", h.cloud.srv.URL)

	// The "browser": follow the link to Steam, log in, and come back to the
	// app's listener with Steam's statement.
	h.app.OpenURL = func(link string) error {
		u, _ := url.Parse(link)
		returnTo := u.Query().Get("openid.return_to")
		if !strings.HasPrefix(returnTo, "http://127.0.0.1:") || u.Query().Get("openid.realm") == "" {
			t.Errorf("the sign-in link must send Steam back to this PC only: %s", link)
		}
		back, _ := url.Parse(returnTo)
		q := back.Query()
		if h.tamperState {
			q.Set("state", "someone-elses")
		}
		if h.cancel {
			q.Set("openid.mode", "cancel")
		} else {
			q.Set("openid.mode", "id_res")
			q.Set("openid.ns", "http://specs.openid.net/auth/2.0")
			q.Set("openid.claimed_id", "https://steamcommunity.com/openid/id/"+h.steam)
			q.Set("openid.identity", "https://steamcommunity.com/openid/id/"+h.steam)
			q.Set("openid.return_to", returnTo)
			q.Set("openid.sig", h.sig)
		}
		back.RawQuery = q.Encode()
		go func() {
			if resp, err := http.Get(back.String()); err == nil {
				io.Copy(io.Discard, resp.Body)
				resp.Body.Close()
			}
		}()
		return nil
	}
	return h
}

func (h *loginHarness) run(t *testing.T) SteamLoginStatus {
	t.Helper()
	if err := h.app.StartSteamLogin(); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "the sign-in to settle", func() bool {
		s := h.app.SteamLoginStatus()
		return !s.Pending || h.tamperState
	})
	if h.tamperState {
		time.Sleep(150 * time.Millisecond)
	}
	return h.app.SteamLoginStatus()
}

func TestSteamSignInSetsUpEverythingAtOnce(t *testing.T) {
	h := newLoginHarness(t)
	s := h.run(t)
	if s.Error != "" || !s.Auth.SignedIn || s.Auth.Steam == nil {
		t.Fatalf("sign-in failed: %+v", s)
	}
	id := s.Auth.Steam
	if id.AccountID != 850402858 || id.SteamID != "76561198810668586" || id.Name != "lilcham" || id.Avatar == nil {
		t.Fatalf("identity wrong: %+v", id)
	}
	// One sign-in links both games and names the leaderboard profile.
	for _, game := range []string{"dota", "deadlock"} {
		l := h.app.Store.LoadLink(game)
		if l.AccountID == nil || *l.AccountID != 850402858 || *l.Personaname != "lilcham" {
			t.Fatalf("%s was not linked by signing in: %+v", game, l)
		}
	}
	if p := h.app.Store.LoadProfile(); p.Username != "lilcham" || p.Rank == nil || *p.Rank != "Divine 2" {
		t.Fatalf("profile should come from the Steam account, not a form: %+v", p)
	}
	// The server confirmed it with Steam, so the app must not have: a
	// statement can only be confirmed once.
	if h.checks != 0 {
		t.Fatalf("the app asked Steam to confirm a statement the server was going to confirm (%d times)", h.checks)
	}
	// And the session survives a restart.
	again := NewCloud(h.app.Store)
	again.base = h.cloud.srv.URL
	again.Restore()
	waitFor(t, "the session to restore", func() bool { a := again.Auth(); return a.SignedIn && a.Steam != nil && a.Steam.Name == "lilcham" })

	if out := h.app.SignOut(); out.SignedIn || out.Steam != nil {
		t.Fatalf("still signed in: %+v", out)
	}
	if h.app.Store.LoadLink("dota").AccountID != nil || h.app.Store.LoadLink("deadlock").AccountID != nil {
		t.Fatal("signing out should disconnect both games")
	}
}

func TestSteamSignInStillWorksWhenTheLeaderboardServiceCannot(t *testing.T) {
	// A cloud deployment that has not been updated to know Steam sign-in, or
	// is simply unreachable, must not block signing in.
	for _, mode := range []string{"unconfigured", "redacted", "down"} {
		h := newLoginHarness(t)
		if mode == "down" {
			h.cloud.srv.Close()
		} else {
			h.cloud.steamMode = mode
		}
		s := h.run(t)
		if s.Error != "" || s.Auth.Steam == nil || s.Auth.Steam.AccountID != 850402858 {
			t.Fatalf("%s: sign-in should succeed without the cloud: %+v", mode, s)
		}
		if s.Auth.SignedIn || s.Auth.LastError == nil {
			t.Fatalf("%s: the leaderboard is not connected, and that needs saying: %+v", mode, s.Auth)
		}
		if h.checks != 1 {
			t.Fatalf("%s: with no server to do it, the app itself must confirm with Steam (asked %d times)", mode, h.checks)
		}
		if h.app.Store.LoadLink("dota").AccountID == nil {
			t.Fatalf("%s: games were not linked", mode)
		}
	}
}

func TestAForgedSteamAnswerIsRefused(t *testing.T) {
	// The server looks at the statement and refuses it.
	h := newLoginHarness(t)
	h.cloud.steamMode = "reject"
	s := h.run(t)
	if s.Error != "Steam did not confirm that sign-in." {
		t.Fatalf("expected the server's own sentence, got %q", s.Error)
	}
	if s.Auth.Steam != nil || h.app.Store.LoadLink("dota").AccountID != nil {
		t.Fatal("a refused sign-in still linked an account")
	}

	// No server, and Steam says the signature is not its own.
	h = newLoginHarness(t)
	h.cloud.steamMode = "unconfigured"
	h.sig = "forged"
	if s := h.run(t); s.Error == "" || s.Auth.Steam != nil {
		t.Fatalf("a forged statement was accepted locally: %+v", s)
	}

	// An answer that does not carry this sign-in's state never reaches the
	// verification step at all.
	h = newLoginHarness(t)
	h.tamperState = true
	if s := h.run(t); s.Auth.Steam != nil || len(h.cloud.paths()) != 0 {
		t.Fatalf("an answer with the wrong state was processed: %+v", s)
	}
	h.app.CancelSteamLogin()

	h = newLoginHarness(t)
	h.cancel = true
	if s := h.run(t); !strings.Contains(s.Error, "cancelled") || s.Auth.Steam != nil {
		t.Fatalf("cancelling at Steam should read as cancelled: %+v", s)
	}
}

// ---------- Games on and off ----------

func TestGamesCanBeSwitchedOffButNotAll(t *testing.T) {
	a := newTestApp(t)
	if g := a.Store.LoadPrefs().Games; !g.Dota || !g.Deadlock || g.CS2 || g.Overwatch || g.Chosen {
		t.Fatalf("defaults: the two original games on, the question not yet answered: %+v", g)
	}
	if _, err := a.SetGames(Games{}); err == nil {
		t.Fatal("an app with every game off has nothing to show")
	}
	p, err := a.SetGames(Games{Deadlock: true, Overwatch: true})
	if err != nil || p.Games.Dota || !p.Games.Deadlock || !p.Games.Overwatch || !p.Games.Chosen {
		t.Fatalf("games not saved: %v %+v", err, p.Games)
	}

	// An older prefs file knows nothing of games: the original two stay on.
	a.Store.writeFile("prefs.json", []byte(`{"favorites":{},"overlay":{}}`))
	if g := a.Store.LoadPrefs().Games; !g.Dota || !g.Deadlock {
		t.Fatalf("an existing install lost its games: %+v", g)
	}
}

func TestTheListenerOnlyRunsForGamesThatNeedIt(t *testing.T) {
	a := newTestApp(t)
	a.applyGames(Games{Deadlock: true, Overwatch: true})
	if a.Gsi.server != nil {
		t.Fatal("with only Deadlock and Overwatch on, nothing posts to the listener and it should not run")
	}
	a.applyGames(Games{CS2: true})
	defer a.Gsi.Stop()
	if a.Gsi.ListenerError() == "" && a.Gsi.server == nil {
		t.Fatal("CS2 needs the listener")
	}
}

// ---------- CS2 ----------

func cs2Payload(mapName, phase, team string, ct, tScore, kills, deaths int, extra ...func(jsonMap)) jsonMap {
	p := jsonMap{
		"provider": jsonMap{"appid": 730.0, "steamid": "76561198810668586"},
		"map":      jsonMap{"name": mapName, "mode": "competitive", "phase": phase, "round": float64(ct + tScore), "team_ct": jsonMap{"score": float64(ct)}, "team_t": jsonMap{"score": float64(tScore)}},
		"player": jsonMap{"steamid": "76561198810668586", "team": team, "activity": "playing",
			"state":       jsonMap{"health": 100.0, "armor": 100.0, "money": 4000.0, "round_killhs": 0.0},
			"match_stats": jsonMap{"kills": float64(kills), "deaths": float64(deaths), "assists": 3.0, "mvps": 2.0, "score": 40.0}},
	}
	for _, f := range extra {
		f(p)
	}
	return p
}

func TestCs2MatchIsRecordedWithItsResult(t *testing.T) {
	store := NewStore(t.TempDir())
	c := NewCs2(store)

	c.HandleUpdate(cs2Payload("de_mirage", "warmup", "CT", 0, 0, 0, 0))
	if s := c.Status(); s.Current == nil || !s.Live || s.Current.Map != "de_mirage" {
		t.Fatalf("a match should be live from warmup: %+v", s)
	}
	// First half on CT, 8-4 up. Two headshots in one round, one in the next.
	hs := func(n float64) func(jsonMap) {
		return func(p jsonMap) { p["player"].(jsonMap)["state"].(jsonMap)["round_killhs"] = n }
	}
	c.HandleUpdate(cs2Payload("de_mirage", "live", "CT", 3, 1, 4, 1, hs(1)))
	c.HandleUpdate(cs2Payload("de_mirage", "live", "CT", 3, 1, 5, 1, hs(2)))
	c.HandleUpdate(cs2Payload("de_mirage", "live", "CT", 4, 1, 5, 1, hs(0)))
	c.HandleUpdate(cs2Payload("de_mirage", "live", "CT", 5, 1, 6, 1, hs(1)))
	c.HandleUpdate(cs2Payload("de_mirage", "live", "CT", 8, 4, 12, 5))
	// Sides swap: the player is now T, and the scores swap with the sides.
	c.HandleUpdate(cs2Payload("de_mirage", "live", "T", 4, 8, 12, 5))
	if m := c.Status().Current; m.MyScore != 8 || m.TheirScore != 4 {
		t.Fatalf("after the side swap the player's score is still 8-4, got %d-%d", m.MyScore, m.TheirScore)
	}
	// Dead and spectating a teammate: their numbers are not the player's.
	c.HandleUpdate(cs2Payload("de_mirage", "live", "T", 4, 9, 30, 0, func(p jsonMap) { p["player"].(jsonMap)["steamid"] = "76561190000000001" }))
	if m := c.Status().Current; m.Kills != 12 {
		t.Fatalf("a spectated teammate's kills were counted as the player's: %d", m.Kills)
	}
	c.HandleUpdate(cs2Payload("de_mirage", "gameover", "T", 7, 13, 21, 14))

	h := c.History()
	if len(h) != 1 {
		t.Fatalf("expected one saved match, got %d", len(h))
	}
	m := h[0]
	if m.Result != "win" || m.MyScore != 13 || m.TheirScore != 7 || m.Kills != 21 || m.Deaths != 14 || m.HeadshotKills != 3 || m.Incomplete {
		t.Fatalf("match misrecorded: %+v", m)
	}
	// The post-game scoreboard keeps arriving; it is not a second match.
	c.HandleUpdate(cs2Payload("de_mirage", "gameover", "T", 7, 13, 21, 14))
	if len(c.History()) != 1 {
		t.Fatal("the scoreboard of a finished match was saved as another match")
	}

	if _, err := c.Delete(m.ID); err != nil || len(c.History()) != 0 {
		t.Fatal("delete failed")
	}
	if _, err := c.Delete("nope"); err == nil {
		t.Fatal("deleting a missing match should say so")
	}
}

func TestCs2LeavingEarlyAndShortGames(t *testing.T) {
	c := NewCs2(NewStore(t.TempDir()))
	// Two rounds of a practice server, then back to the menu: not a match.
	c.HandleUpdate(cs2Payload("de_dust2", "live", "CT", 1, 1, 2, 1))
	c.HandleUpdate(jsonMap{"provider": jsonMap{"appid": 730.0}, "player": jsonMap{"activity": "menu"}})
	if len(c.History()) != 0 {
		t.Fatal("a two-round game should not be kept")
	}
	// Leaving a real match at 6-5 is kept, without a result.
	c.HandleUpdate(cs2Payload("de_nuke", "live", "T", 5, 6, 9, 8))
	c.HandleUpdate(cs2Payload("de_ancient", "warmup", "CT", 0, 0, 0, 0)) // a new map: the old match is over
	h := c.History()
	if len(h) != 1 || h[0].Map != "de_nuke" || !h[0].Incomplete || h[0].Result != "" || h[0].MyScore != 6 {
		t.Fatalf("an abandoned match should be kept as incomplete with no result: %+v", h)
	}
	// A draw is a draw.
	c.HandleUpdate(cs2Payload("de_ancient", "gameover", "CT", 12, 12, 20, 20))
	if h := c.History(); len(h) != 2 || h[1].Result != "draw" {
		t.Fatalf("12-12 should be a draw: %+v", h)
	}
}

func TestCs2PayloadsAreRoutedByGame(t *testing.T) {
	a := newTestApp(t)
	body := `{"provider":{"appid":730,"steamid":"1"},"map":{"name":"de_inferno","phase":"live"},"player":{"steamid":"1","team":"CT"}}`
	if err := post(a.Gsi, body, nil); err != nil {
		t.Fatal(err)
	}
	if s := a.Cs2.Status(); s.Current == nil || s.Current.Map != "de_inferno" {
		t.Fatal("a CS2 payload did not reach the CS2 tracker")
	}
	if a.Tracker.Status(false).GsiAgeSecs != nil {
		t.Fatal("a CS2 payload was counted as Dota talking")
	}
}

func TestCs2ConfigInstall(t *testing.T) {
	c := NewCs2(NewStore(t.TempDir()))
	root, _ := fakeSteam(t, "")
	c.steamRoot = func() string { return root }
	if err := c.Install(3000, "tok"); err == nil {
		t.Fatal("with CS2 not installed there is nowhere to write the config")
	}
	cfg := filepath.Join(filepath.Dir(root), "SteamLibrary", "steamapps", "common", "Counter-Strike Global Offensive", "game", "csgo", "cfg")
	os.MkdirAll(cfg, 0o755)
	if s := c.Setup(3000, "tok"); len(s.CfgDirs) != 1 || s.Installed {
		t.Fatalf("before install: %+v", s)
	}
	if err := c.Install(3000, "tok"); err != nil {
		t.Fatal(err)
	}
	if !c.Setup(3000, "tok").Installed {
		t.Fatal("not reported installed")
	}
	if c.Setup(3001, "tok").Installed {
		t.Fatal("a config pointing at another port is not installed")
	}
	raw, _ := os.ReadFile(filepath.Join(cfg, cs2CfgName))
	for _, want := range []string{`"http://127.0.0.1:3000/"`, `"player_match_stats"    "1"`, `"token"         "tok"`} {
		if !strings.Contains(string(raw), want) {
			t.Errorf("config is missing %s", want)
		}
	}
	c.Remove()
	if c.Setup(3000, "tok").Installed {
		t.Fatal("remove left the config behind")
	}
}

// ---------- Overwatch ----------

func TestOverwatchOverview(t *testing.T) {
	store := NewStore(t.TempDir())
	stat := func(played, won int, secs int) map[string]any {
		return map[string]any{"games_played": played, "games_won": won, "games_lost": played - won, "time_played": secs, "winrate": float64(won) * 100 / float64(played), "kda": 2.5,
			"average": map[string]any{"eliminations": 18.5, "deaths": 7.1, "damage": 8600.0, "healing": 1200.0}}
	}
	f := newFakeAPI(t, map[string]any{
		"/players": map[string]any{"results": []any{
			map[string]any{"player_id": "abc%7Cdef", "name": "Super", "avatar": "https://d15f34w2p8l1cc.cloudfront.net/a.png", "title": "Shinigami", "is_public": true},
			map[string]any{"name": "no id"},
		}},
		"/heroes": []any{
			map[string]any{"key": "ana", "name": "Ana", "role": "support", "portrait": "https://d15f34w2p8l1cc.cloudfront.net/ana.png"},
			map[string]any{"key": "reinhardt", "name": "Reinhardt", "role": "tank"},
		},
		"/players/abc|def/summary": map[string]any{"username": "Super", "title": "Shinigami", "endorsement": map[string]any{"level": 3},
			"competitive": map[string]any{"pc": map[string]any{"season": 21, "tank": map[string]any{"division": "diamond", "tier": 2, "rank_icon": "https://static.playoverwatch.com/d.png"}, "damage": nil}}},
		"/players/abc|def/stats/summary": map[string]any{
			"general": stat(100, 55, 36000),
			"roles":   map[string]any{"tank": stat(60, 36, 20000), "support": stat(40, 19, 16000)},
			"heroes":  map[string]any{"ana": stat(40, 19, 16000), "reinhardt": stat(60, 36, 20000), "newhero": stat(1, 1, 60)},
		},
	})
	o := NewOverwatch(store)
	o.api = &service{Name: "The Overwatch API", Base: f.srv.URL, Attempts: 1}

	if _, err := o.Overview("all", false); err == nil {
		t.Fatal("no profile connected should be an error")
	}
	found, err := o.Search("Super")
	if err != nil || len(found) != 1 || found[0].PlayerID != "abc|def" || !found[0].Public {
		t.Fatalf("search: %v %+v", err, found)
	}
	o.SetLink(OwLink{PlayerID: found[0].PlayerID, Name: found[0].Name})

	ov, err := o.Overview("competitive", false)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(strings.Join(f.queries, " "), "gamemode=competitive") {
		t.Fatal("the mode was not passed to the API")
	}
	if ov.Name != "Super" || ov.Endorsement != 3 || ov.General.GamesPlayed != 100 || ov.General.WinRate != 55 {
		t.Fatalf("overview wrong: %+v", ov)
	}
	if len(ov.Ranks) != 1 || ov.Ranks[0].Role != "Tank" || ov.Ranks[0].Division != "diamond" || ov.Ranks[0].Tier != 2 {
		t.Fatalf("ranks wrong (an unranked role must be left out): %+v", ov.Ranks)
	}
	if len(ov.Roles) != 2 || ov.Roles[0].Name != "Tank" {
		t.Fatalf("roles wrong: %+v", ov.Roles)
	}
	// Most-played first; a hero the hero list does not know keeps its key.
	if len(ov.Heroes) != 3 || ov.Heroes[0].Name != "Reinhardt" || ov.Heroes[1].Name != "Ana" || ov.Heroes[1].Role != "support" || ov.Heroes[2].Name != "newhero" {
		t.Fatalf("heroes wrong: %+v", ov.Heroes)
	}
}

// ---------- Leaderboards ----------

func TestGlobalLeaderboards(t *testing.T) {
	d, f, _ := newTestDota(t, map[string]any{
		"/ILeaderboard/GetDivisionLeaderboard/v0001": map[string]any{"time_posted": 1791018781, "leaderboard": []any{
			map[string]any{"rank": 1, "name": "Nightfall", "country": "ru"},
			map[string]any{"rank": 2, "name": "9Class", "team_tag": "PV"},
		}},
	})
	d.valve = &service{Name: "Dota's leaderboard", Base: f.srv.URL, Attempts: 1}
	b, err := d.Leaderboard("nowhere", false)
	if err != nil || b.Region != "europe" || len(b.Players) != 2 || b.Players[1].Team != "PV" || len(b.Regions) != 4 {
		t.Fatalf("an unknown region falls back to the first; got %v %+v", err, b)
	}
	if !strings.Contains(f.queries[len(f.queries)-1], "division=europe") {
		t.Fatal("region not passed")
	}

	store := NewStore(t.TempDir())
	df := newFakeAPI(t, map[string]any{
		"/v1/assets/heroes": []any{map[string]any{"id": 19, "name": "Shiv", "images": map[string]any{"icon_image_small": "https://assets-bucket.deadlock-api.com/s.png"}}},
		"/v1/leaderboard/Asia": map[string]any{"entries": []any{
			map[string]any{"rank": 1, "account_name": "Josh", "top_hero_ids": []int{19, 999}, "possible_account_ids": []int{1, 2, 3, 4, 5, 6, 42}},
			map[string]any{"rank": 2, "account_name": "me", "top_hero_ids": []int{}, "possible_account_ids": []int{42}},
		}},
	})
	dl := NewDeadlock(store)
	dl.api = &service{Name: "The Deadlock API", Base: df.srv.URL, Attempts: 1}
	linked(store, "deadlock", 42)
	lb, err := dl.Leaderboard("Asia", false)
	if err != nil || len(lb.Players) != 2 || lb.Region != "Asia" {
		t.Fatalf("deadlock leaderboard: %v %+v", err, lb)
	}
	if lb.Players[0].Heroes[0] != "Shiv" || len(lb.Players[0].Heroes) != 1 {
		t.Fatalf("hero ids should resolve to names, unknown ones dropped: %+v", lb.Players[0])
	}
	// A name shared by many accounts proves nothing; a name that could only
	// be the linked account is marked.
	if lb.Players[0].IsMe || !lb.Players[1].IsMe {
		t.Fatalf("own row marking wrong: %+v", lb.Players)
	}
}
