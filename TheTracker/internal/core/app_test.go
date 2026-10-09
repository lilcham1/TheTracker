package core

import (
	"archive/zip"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// ---------- Preferences ----------

func TestOverlayValuesAreClamped(t *testing.T) {
	safe := OverlaySettings{Opacity: 0, Scale: 9, LeadSeconds: 99, Corner: "middle"}.sanitized()
	// An opacity of 0 would leave an invisible window and no way back.
	if safe.Opacity != 0.25 || safe.Scale != 1.5 || safe.LeadSeconds != 30 || safe.Corner != "top-left" {
		t.Fatalf("not clamped: %+v", safe)
	}
}

func TestPrefsDefaultsAndOlderFiles(t *testing.T) {
	store := NewStore(t.TempDir())
	d := store.LoadPrefs()
	if !d.Overlay.ClickThrough {
		t.Fatal("click-through must default on, or the overlay eats game input")
	}
	if d.Overlay.LeadSeconds != 5 || !d.Overlay.Auto || !d.Overlay.Dota.Runes || !d.Overlay.Dota.Lotus || !d.Overlay.Dota.Stacks {
		t.Fatalf("overlay defaults wrong: %+v", d.Overlay)
	}
	if !d.General.CloseToTray || d.General.AutostartAsked {
		t.Fatalf("general defaults wrong: %+v", d.General)
	}

	// A file from before most of these settings existed, carrying panel keys
	// that have since been removed. It must load, keep what it set, and
	// default the rest — not reset everything.
	old := `{"favorites":{"dota":"juggernaut"},"builds":[{"id":"b1","game":"dota","hero":"axe","name":"Blink"}],
		"overlay":{"opacity":0.5,"corner":"bottom-right","dota":{"stats":true,"roshan":false,"runes":false,"daynight":true}}}`
	store.writeFile("prefs.json", []byte(old))
	p := store.LoadPrefs()
	if *p.Favorites.Dota != "juggernaut" || p.Overlay.Opacity != 0.5 || p.Overlay.Corner != "bottom-right" {
		t.Fatalf("saved values lost: %+v", p)
	}
	if p.Overlay.Dota.Runes || !p.Overlay.Dota.Stacks || !p.Overlay.Dota.Lotus {
		t.Fatalf("runes was switched off and should stay off; the panels the file never mentioned default on: %+v", p.Overlay.Dota)
	}
	if !p.General.CloseToTray || p.Overlay.LeadSeconds != 5 || !p.Overlay.ClickThrough {
		t.Fatal("settings the old file predates must take their defaults, not read as false or zero")
	}
	if len(p.Builds) != 1 || p.Builds[0].Items == nil {
		t.Fatal("a build without items should load with an empty list, not null")
	}
}

func TestBuildsAndFavourites(t *testing.T) {
	store := NewStore(t.TempDir())
	p := store.UpsertBuild(Build{Game: "dota", Hero: "axe", Name: "Blink first", Items: []string{"blink"}})
	if len(p.Builds) != 1 || p.Builds[0].ID == "" || p.Builds[0].UpdatedAt == "" {
		t.Fatalf("a new build gets an id and a timestamp: %+v", p.Builds)
	}
	id := p.Builds[0].ID
	p = store.UpsertBuild(Build{ID: id, Game: "dota", Hero: "axe", Name: "Renamed"})
	if len(p.Builds) != 1 || p.Builds[0].Name != "Renamed" {
		t.Fatal("saving with an existing id replaces, it does not duplicate")
	}
	if p = store.DeleteBuild(id); len(p.Builds) != 0 {
		t.Fatal("delete failed")
	}

	p = store.SetFavorite("deadlock", ptr("Haze"))
	p = store.SetFavorite("dota", ptr("axe"))
	if *p.Favorites.Deadlock != "Haze" || *p.Favorites.Dota != "axe" {
		t.Fatalf("favourites are per game: %+v", p.Favorites)
	}
	if p = store.SetFavorite("dota", nil); p.Favorites.Dota != nil || *p.Favorites.Deadlock != "Haze" {
		t.Fatal("clearing one game's favourite must not touch the other's")
	}
	if g := store.SaveGoals(Goals{LastHits10: 9999, MaxDeaths: -4, MinGPM: 500}).Goals; g.LastHits10 != 200 || g.MaxDeaths != 0 || g.MinGPM != 500 {
		t.Fatalf("goals not clamped: %+v", g)
	}
}

func TestConcurrentSettingsChangesAreNotLost(t *testing.T) {
	store := NewStore(t.TempDir())
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			store.UpsertBuild(Build{Game: "dota", Hero: "axe", Name: "b"})
		}()
	}
	wg.Wait()
	if n := len(store.LoadPrefs().Builds); n != 20 {
		t.Fatalf("20 saves at once left %d builds: updates overwrote each other", n)
	}
}

// ---------- Updater ----------

func TestVersionComparison(t *testing.T) {
	less := [][2]string{{"0.17.0", "1.0.0"}, {"1.0.0", "1.0.1"}, {"1.9.0", "1.10.0"}, {"v1.0.0", "1.1"}, {"1.0.0-beta", "1.0.1"}}
	for _, c := range less {
		if !versionLess(c[0], c[1]) || versionLess(c[1], c[0]) {
			t.Errorf("%s should be older than %s", c[0], c[1])
		}
	}
	if versionLess("1.0.0", "1.0.0") || versionLess("1.0", "1.0.0") {
		t.Error("equal versions are not an update")
	}
}

// The signature format has to stay the one every installed copy checks.
// testdata/signed.txt was signed with the release key by the same signer that
// signs every installer, so this verifies against the real thing.
func TestSignatureVerification(t *testing.T) {
	data, err1 := os.ReadFile(filepath.Join("testdata", "signed.txt"))
	sig, err2 := os.ReadFile(filepath.Join("testdata", "signed.txt.sig"))
	if err1 != nil || err2 != nil {
		t.Fatal("signature fixture missing")
	}
	// The feed carries the .sig file's contents as-is.
	if err := VerifySignature(updatePubKey, string(sig), data); err != nil {
		t.Fatalf("a genuine signature failed verification: %v", err)
	}

	tampered := append([]byte{}, data...)
	tampered[len(tampered)/2] ^= 1
	if VerifySignature(updatePubKey, string(sig), tampered) == nil {
		t.Fatal("a file with one flipped bit passed verification")
	}
	if VerifySignature(updatePubKey, base64.StdEncoding.EncodeToString([]byte("untrusted comment: x\nAAAA\ntrusted comment: y\nAAAA")), data) == nil {
		t.Fatal("a junk signature passed verification")
	}
	if VerifySignature(updatePubKey, "", data) == nil {
		t.Fatal("an empty signature passed verification")
	}

	// And any installer named in the environment, for checking a release
	// build before it is published.
	if path := os.Getenv("THETRACKER_VERIFY"); path != "" {
		data, _ := os.ReadFile(path)
		sig, _ := os.ReadFile(path + ".sig")
		if err := VerifySignature(updatePubKey, string(sig), data); err != nil {
			t.Fatalf("%s failed verification: %v", path, err)
		}
		t.Logf("verified %s", path)
	}
}

func TestUpdateCheckAndRefusals(t *testing.T) {
	manifest := map[string]any{"version": "99.0.0", "notes": "New things", "pub_date": "2026-10-03T00:00:00Z",
		"platforms": map[string]any{"windows-x86_64": map[string]any{"signature": "AAAA", "url": "https://example.com/evil.exe"}}}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { json.NewEncoder(w).Encode(manifest) }))
	defer srv.Close()

	u := NewUpdater()
	u.FeedURL = srv.URL
	info, err := u.Check()
	if err != nil || !info.Available || *info.Version != "99.0.0" || *info.Notes != "New things" || info.Current != Version {
		t.Fatalf("check: %v %+v", err, info)
	}
	// A feed pointing anywhere but the project's releases is never fetched.
	if _, err := u.Download(); err == nil || !strings.Contains(err.Error(), "unexpected") {
		t.Fatalf("an installer from another host was not refused: %v", err)
	}

	manifest["version"] = Version
	if info, _ := u.Check(); info.Available {
		t.Fatal("the running version is not an update")
	}
	if _, err := u.Download(); err == nil {
		t.Fatal("there is nothing to download when up to date")
	}

	srv.Close()
	if _, err := u.Check(); err == nil {
		t.Fatal("an unreachable server is an error, not \"up to date\"")
	}
}

// ---------- Cloud ----------

// fakeConvex stands in for the deployment and records every call.
type fakeConvex struct {
	mu        sync.Mutex
	calls     []map[string]any
	tokens    []string
	expireJWT bool
	// How the Steam provider answers: "" accepts, "unconfigured" behaves like
	// a server that predates Steam sign-in, "reject" refuses the statement.
	steamMode string
	// What the Twitch streams function answers; nil means the deployment
	// predates it.
	twitch map[string]any
	// What the Steam live-hero function answers; nil: not deployed.
	steamLive map[string]any
	srv       *httptest.Server
}

func newFakeConvex(t *testing.T) *fakeConvex {
	f := &fakeConvex{}
	f.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		raw, _ := io.ReadAll(r.Body)
		json.Unmarshal(raw, &body)
		f.mu.Lock()
		f.calls = append(f.calls, body)
		f.tokens = append(f.tokens, r.Header.Get("Authorization"))
		expire, steamMode, twitch, steamLive := f.expireJWT, f.steamMode, f.twitch, f.steamLive
		f.mu.Unlock()

		reply := func(v any) { json.NewEncoder(w).Encode(map[string]any{"status": "success", "value": v}) }
		fail := func(msg string) { json.NewEncoder(w).Encode(map[string]any{"status": "error", "errorMessage": msg}) }
		args, _ := body["args"].(map[string]any)
		switch body["path"] {
		case "auth:signIn":
			if rt, ok := args["refreshToken"]; ok {
				if rt != "refresh-1" {
					fail("Invalid refresh token")
					return
				}
				reply(map[string]any{"tokens": map[string]any{"token": "jwt-2", "refreshToken": "refresh-2"}})
				return
			}
			params, _ := args["params"].(map[string]any)
			openid, _ := params["openid"].(map[string]any)
			switch {
			case args["provider"] != "steam" || openid["openid.claimed_id"] == nil:
				fail("Uncaught Error: Missing Steam sign-in details.")
				return
			case steamMode == "unconfigured":
				fail("Uncaught Error: Provider `steam` is not configured, available providers are `password`.")
				return
			case steamMode == "redacted":
				fail("[Request ID: 29305b355fafee2a] Server Error")
				return
			case steamMode == "reject":
				fail("Uncaught Error: Steam did not confirm that sign-in.\n    at authorize (../convex/auth.ts:59:10)")
				return
			}
			reply(map[string]any{"tokens": map[string]any{"token": "jwt-1", "refreshToken": "refresh-1"}})

		case "steamlive:dotaLive":
			if steamLive == nil {
				fail("Could not find public function for 'steamlive:dotaLive'")
				return
			}
			reply(steamLive)
		case "twitch:deadlockStreams", "twitch:streams":
			if twitch == nil {
				fail("Could not find public function for 'twitch:deadlockStreams'")
				return
			}
			reply(twitch)
		case "profiles:whoami":
			reply(map[string]any{"userId": "user-1"})
		case "matches:upsert":
			if r.Header.Get("Authorization") == "" {
				fail("Sign in to publish matches")
				return
			}
			if expire && r.Header.Get("Authorization") == "Bearer jwt-1" {
				fail("Unauthenticated: token expired")
				return
			}
			reply(map[string]any{"updated": false})
		case "leaderboard:globalTop":
			reply([]any{map[string]any{"username": "me", "value": 300}})
		default:
			reply(nil)
		}
	}))
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fakeConvex) paths() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := []string{}
	for _, c := range f.calls {
		out = append(out, c["path"].(string))
	}
	return out
}

func (f *fakeConvex) argsFor(path string) map[string]any {
	f.mu.Lock()
	defer f.mu.Unlock()
	for i := len(f.calls) - 1; i >= 0; i-- {
		if f.calls[i]["path"] == path {
			return f.calls[i]["args"].(map[string]any)
		}
	}
	return nil
}

func newTestCloud(t *testing.T) (*Cloud, *fakeConvex, *Store) {
	f := newFakeConvex(t)
	store := NewStore(t.TempDir())
	c := NewCloud(store)
	c.base = f.srv.URL
	return c, f, store
}

func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	for i := 0; i < 200; i++ {
		if cond() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

var testIdentity = SteamIdentity{SteamID: "76561198810668586", AccountID: 850402858, Name: "lilcham"}

var testOpenID = map[string]string{
	"openid.mode": "id_res", "openid.sig": "good",
	"openid.claimed_id": "https://steamcommunity.com/openid/id/76561198810668586",
}

func sampleMatch(id string) MatchSummary {
	return MatchSummary{
		MatchID: id, HeroName: ptr("npc_dota_hero_axe"), Date: "2026-10-03T10:00:00.000Z", Duration: "30:00", Kills: 7,
		TotalDeaths: 1, TotalGoldLost: 300, GameType: "turbo",
		Deaths: []Death{{Clock: "12:00", GoldLost: ptr(int64(300))}}, KeyItems: []KeyItemEntry{{Clock: "9:00", Item: "blink"}},
		Checkpoints: map[int]*Checkpoint{10: {LastHits: 60, Denies: 4}, 25: {LastHits: 180, Denies: 9}, 5: nil},
		Won:         ptr(true), GPM: ptr(int64(600)), Notes: "private note",
	}
}

func TestSigningInClaimsTheDeviceAndSyncsHistory(t *testing.T) {
	c, f, store := newTestCloud(t)
	store.SaveHistory([]MatchSummary{sampleMatch("1"), sampleMatch("2")})
	store.SaveProfile(Profile{Username: "lilcham"})

	err := c.SteamSignIn(testIdentity, testOpenID)
	auth := c.Auth()
	if err != nil || !auth.SignedIn || auth.Steam == nil || auth.Steam.AccountID != 850402858 || *auth.UserID != "user-1" {
		t.Fatalf("sign in: %v %+v", err, auth)
	}
	if sent := f.argsFor("auth:signIn"); sent["provider"] != "steam" || sent["params"].(map[string]any)["openid"].(map[string]any)["openid.sig"] != "good" {
		t.Fatalf("the server must be handed Steam's statement to verify for itself: %v", sent)
	}
	raw, _ := json.Marshal(auth)
	if strings.Contains(string(raw), "jwt") || strings.Contains(string(raw), "refresh") {
		t.Fatalf("a token reached the UI: %s", raw)
	}
	waitFor(t, "history to sync", func() bool { s := c.Status(); return s.Synced == 2 && s.Pending == 0 })

	paths := strings.Join(f.paths(), " ")
	for _, want := range []string{"matches:claimDevice", "matches:upsert", "profiles:upsert"} {
		if !strings.Contains(paths, want) {
			t.Errorf("%s was never called (calls: %s)", want, paths)
		}
	}

	// The server's validator rejects unknown arguments, so the row must be
	// exactly these fields — no local-only ones.
	args := f.argsFor("matches:upsert")
	want := []string{"deviceId", "username", "matchid", "heroName", "date", "duration", "kills", "totalDeaths",
		"totalGoldLost", "roshanDeaths", "gameType", "lastHits25", "checkpoints", "deaths", "keyItems"}
	if len(args) != len(want) {
		t.Fatalf("the cloud row has %d fields, the server accepts %d: %v", len(args), len(want), args)
	}
	for _, k := range want {
		if _, ok := args[k]; !ok {
			t.Errorf("cloud row is missing %s", k)
		}
	}
	if args["lastHits25"] != 180.0 || args["username"] != "lilcham" {
		t.Fatalf("row values wrong: %v", args)
	}
	cps := args["checkpoints"].(map[string]any)
	if cps["5"] != nil || cps["10"].(map[string]any)["lastHits"] != 60.0 {
		t.Fatalf("checkpoints wrong: %v", cps)
	}

	// The session survives a restart, via the refresh token on disk.
	again := NewCloud(store)
	again.base = f.srv.URL
	again.Restore()
	waitFor(t, "the session to restore", func() bool { return again.Auth().SignedIn })

	if c.SignOut().SignedIn {
		t.Fatal("still signed in after signing out")
	}
	if _, err := os.Stat(store.path("auth.json")); err == nil {
		t.Fatal("the stored session was not removed on sign-out")
	}
}

func TestSignedOutMatchesAreHeldNotLost(t *testing.T) {
	c, f, _ := newTestCloud(t)
	c.PushMatch(sampleMatch("1"))
	waitFor(t, "the job to settle", func() bool { s := c.Status(); return s.Pending == 0 && s.NeedsSignIn })
	if len(f.paths()) != 0 {
		t.Fatal("a match was sent to the server with nobody signed in")
	}
	if s := c.Status(); s.Synced != 0 || s.LastError != nil {
		t.Fatalf("being signed out is not a sync error: %+v", s)
	}
}

func TestAnExpiredTokenIsRefreshedAndTheMatchStillLands(t *testing.T) {
	c, f, _ := newTestCloud(t)
	if err := c.SteamSignIn(testIdentity, testOpenID); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "sign-in sync to settle", func() bool { return c.Status().Pending == 0 })

	f.mu.Lock()
	f.expireJWT = true
	f.mu.Unlock()
	c.PushMatch(sampleMatch("9"))
	waitFor(t, "the match to sync after a refresh", func() bool { s := c.Status(); return s.Synced == 1 && s.Pending == 0 })
	if s := c.Status(); s.LastError != nil || !s.Connected {
		t.Fatalf("the refresh should have been invisible: %+v", s)
	}
}

func TestLeaderboardIsReadableSignedOut(t *testing.T) {
	c, f, _ := newTestCloud(t)
	rows, err := c.GlobalLeaderboard("last_hits_25", "all", 0)
	if err != nil || len(rows.([]any)) != 1 {
		t.Fatalf("leaderboard: %v %v", rows, err)
	}
	if f.argsFor("leaderboard:globalTop")["limit"] != 10.0 {
		t.Fatal("limit should default to 10")
	}
	f.srv.Close()
	if _, err := c.GlobalLeaderboard("last_hits_25", "all", 5); err == nil || !strings.Contains(err.Error(), "connection") {
		t.Fatalf("an outage should read as one: %v", err)
	}
}

// ---------- Insights and goals ----------

func TestInsightsNeedEnoughGamesAndSayWhatTheySee(t *testing.T) {
	if got := BuildInsights([]MatchSummary{sampleMatch("1")}); len(got) != 0 {
		t.Fatal("one game is not a pattern")
	}
	var h []MatchSummary
	for i := 0; i < 8; i++ {
		m := sampleMatch(string(rune('a' + i)))
		// Last hits climbing from 40 to 75; two deaths a game, both between
		// 10 and 20 minutes; odd games lost with more deaths.
		m.Checkpoints = map[int]*Checkpoint{10: {LastHits: int64(40 + i*5)}}
		m.Deaths = []Death{{Clock: "12:30", GoldLost: ptr(int64(400))}, {Clock: "17:05", GoldLost: ptr(int64(600))}}
		m.TotalDeaths = 2
		m.Won = ptr(i%2 == 0)
		if i%2 == 1 {
			m.TotalDeaths = 6
		}
		m.KeyItems = []KeyItemEntry{{Clock: "8:00", Item: "boots"}, {Clock: "14:00", Item: "blink"}}
		h = append(h, m)
	}
	titles := map[string]Insight{}
	for _, in := range BuildInsights(h) {
		titles[in.Title] = in
	}
	if in, ok := titles["Farming by 10 minutes"]; !ok || in.Tone != "good" || !strings.Contains(in.Detail, "58") {
		t.Fatalf("rising last hits should read as an improvement around the 58 average: %+v", in)
	}
	if in, ok := titles["When you die"]; !ok || !strings.Contains(in.Detail, "10 to 20 minutes") || !strings.Contains(in.Detail, "100%") {
		t.Fatalf("every death was between 10 and 20 minutes: %+v", in)
	}
	if in, ok := titles["What a death costs"]; !ok || !strings.Contains(in.Detail, "500 gold") {
		t.Fatalf("average gold lost is 500: %+v", in)
	}
	if in, ok := titles["Deaths and results"]; !ok || !strings.Contains(in.Detail, "2.0") || !strings.Contains(in.Detail, "6.0") {
		t.Fatalf("deaths in wins vs losses: %+v", in)
	}
	if in, ok := titles["Blink Dagger timing"]; !ok || !strings.Contains(in.Detail, "14:00") {
		t.Fatalf("blink timing: %+v", in)
	}
	if _, ok := titles["Boots timing"]; ok {
		t.Fatal("basic boots are not worth an insight")
	}
}

func TestGoals(t *testing.T) {
	g := Goals{LastHits10: 50, MaxDeaths: 5, MinGPM: 550}
	m := sampleMatch("1") // 60 LH at 10, 1 death, 600 GPM
	for _, r := range EvaluateGoals(g, &m) {
		if !r.Met || r.Value == nil {
			t.Errorf("%s should be met: %+v", r.Key, r)
		}
	}
	short := MatchSummary{TotalDeaths: 9}
	res := EvaluateGoals(g, &short)
	if res[0].Value != nil || res[0].Met {
		t.Fatal("a match that never reached 10:00 has no last-hit value; it is neither met nor missed")
	}
	if res[1].Met {
		t.Fatal("nine deaths misses a five-death goal")
	}
	if len(EvaluateGoals(Goals{}, &m)) != 0 {
		t.Fatal("goals set to zero are off")
	}

	p := GoalsProgress(g, []MatchSummary{m, short}, 20)
	if p[0].Counted != 1 || p[0].Met != 1 || p[1].Counted != 2 || p[1].Met != 1 {
		t.Fatalf("progress wrong: %+v", p)
	}
	if p := GoalsProgress(g, nil, 20); len(p) != 3 || p[0].Counted != 0 {
		t.Fatalf("with no sessions the goals are still listed: %+v", p)
	}
}

// ---------- App: simulator, export, backup ----------

func newTestApp(t *testing.T) *App {
	a := NewApp(t.TempDir(), &NoShell{})
	// Never the real deployment or the real APIs from a unit test.
	a.Cloud.base = "http://127.0.0.1:1"
	a.Dota.api.Base, a.Deadlock.api.Base = "http://127.0.0.1:1", "http://127.0.0.1:1"
	return a
}

func TestTheSimulatorRunsAMatchThatIsNeverSaved(t *testing.T) {
	a := newTestApp(t)
	if err := a.StartSimulation(10); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "the simulated match to go live", func() bool { return a.Live(false).Live })
	v := a.Live(false)
	if !v.Simulating || !v.Current.Simulated || *v.Current.HeroName != "npc_dota_hero_juggernaut" {
		t.Fatalf("live view during a simulation: %+v", v)
	}

	a.StopSimulation()
	v = a.Live(false)
	if v.Simulating || !v.Current.Ended || v.Current.Summary == nil || *v.Current.Summary.Won != true {
		t.Fatalf("stopping should end the match cleanly with a result: %+v", v.Current)
	}
	if len(a.Store.History()) != 0 {
		t.Fatal("a simulated match was saved to history")
	}
}

func TestTheSimulatorWillNotInterruptARealMatch(t *testing.T) {
	a := newTestApp(t)
	a.Tracker.HandleUpdate(payload("real", stateInProgress, 600, "radiant", "none"))
	if err := a.StartSimulation(10); err == nil {
		t.Fatal("starting the simulator over a real match would end it as incomplete")
	}
	if a.Tracker.Status(false).Current.MatchID != "real" {
		t.Fatal("the real match was replaced")
	}
}

func TestLiveViewShowsGoalProgress(t *testing.T) {
	a := newTestApp(t)
	a.Store.SaveGoals(Goals{MaxDeaths: 3, LastHits10: 100})
	a.Tracker.HandleUpdate(payload("1", stateInProgress, 601, "radiant", "none"))
	goals := a.Live(false).Goals
	if len(goals) != 2 || *goals[0].Value != 120 || !goals[0].Met || !goals[1].Met {
		t.Fatalf("goal progress during a match: %+v", goals)
	}
}

func TestExportBackupAndRestore(t *testing.T) {
	a := newTestApp(t)
	out := t.TempDir()
	if _, err := a.ExportSessions("csv", out); err == nil {
		t.Fatal("exporting nothing should say so")
	}
	if _, err := a.Backup(out); err == nil {
		t.Fatal("backing up nothing should say so")
	}

	m := sampleMatch("1")
	m.Notes = "line one, with a comma and a \"quote\""
	a.Store.SaveHistory([]MatchSummary{m, sampleMatch("2")})
	a.Store.SetFavorite("dota", ptr("axe"))
	a.Store.writeJSON("auth.json", storedAuth{RefreshToken: ptr("secret-refresh-token")})

	csvPath, err := a.ExportSessions("csv", out)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(csvPath)
	lines := strings.Split(strings.TrimSpace(string(raw)), "\n")
	if len(lines) != 3 || !strings.Contains(lines[0], "lh_10") {
		t.Fatalf("CSV should be a header and one row per match:\n%s", raw)
	}
	if !strings.Contains(lines[1], ",axe,turbo,win,30:00,7,1,") || !strings.Contains(lines[1], `"line one, with a comma and a ""quote"""`) {
		t.Fatalf("CSV row wrong: %s", lines[1])
	}
	jsonPath, err := a.ExportSessions("json", out)
	var back []MatchSummary
	if raw, _ := os.ReadFile(jsonPath); err != nil || json.Unmarshal(raw, &back) != nil || len(back) != 2 {
		t.Fatal("JSON export does not round-trip")
	}

	zipPath, err := a.Backup(out)
	if err != nil {
		t.Fatal(err)
	}
	zr, _ := zip.OpenReader(zipPath)
	names := []string{}
	for _, f := range zr.File {
		names = append(names, f.Name)
		rc, _ := f.Open()
		body, _ := io.ReadAll(rc)
		rc.Close()
		if strings.Contains(string(body), "secret-refresh-token") {
			t.Fatal("the sign-in token was written into the backup")
		}
	}
	zr.Close()
	if joined := strings.Join(names, " "); !strings.Contains(joined, "history.json") || !strings.Contains(joined, "prefs.json") || strings.Contains(joined, "auth.json") {
		t.Fatalf("backup contents: %v", names)
	}
	if LatestBackup(out) != zipPath {
		t.Fatal("the newest backup was not found")
	}

	// Lose everything, then restore.
	a.Store.SaveHistory(nil)
	a.Store.SetFavorite("dota", nil)
	n, err := a.Restore(zipPath)
	if err != nil || n != 2 {
		t.Fatalf("restore: %d %v", n, err)
	}
	if h := a.Store.History(); len(h) != 2 || h[0].Notes != m.Notes {
		t.Fatal("history was not restored")
	}
	if fav := a.Store.LoadPrefs().Favorites.Dota; fav == nil || *fav != "axe" {
		t.Fatal("settings were not restored")
	}
	// What was there before the restore is kept, so it can be undone.
	if LatestBackup(filepath.Join(a.Store.Dir, "before-restore")) == "" {
		t.Fatal("the pre-restore data was not saved first")
	}

	if _, err := a.Restore(csvPath); err == nil {
		t.Fatal("a file that is not a backup was accepted")
	}
	// A zip whose entries try to choose their own destination.
	evil := filepath.Join(out, "evil.zip")
	f, _ := os.Create(evil)
	zw := zip.NewWriter(f)
	w, _ := zw.Create("../../outside.json")
	w.Write([]byte("{}"))
	zw.Close()
	f.Close()
	if _, err := a.Restore(evil); err == nil {
		t.Fatal("a zip with a path outside the data folder was accepted")
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(a.Store.Dir), "outside.json")); err == nil {
		t.Fatal("a zip entry escaped the data folder")
	}
}

func TestBackgroundSettings(t *testing.T) {
	a := newTestApp(t)
	b := a.Background()
	if b.StartWithWindows || !b.CloseToTray || b.AutostartAsked || !b.TrayAvailable {
		t.Fatalf("defaults: %+v", b)
	}
	b, err := a.SetStartWithWindows(true)
	if err != nil || !b.StartWithWindows || !b.AutostartAsked {
		t.Fatalf("enabling autostart also counts as having answered the prompt: %+v", b)
	}
	if b = a.SetCloseToTray(false); b.CloseToTray || !b.StartWithWindows {
		t.Fatalf("close-to-tray off: %+v", b)
	}
	a2 := newTestApp(t)
	if b := a2.DismissAutostartPrompt(); !b.AutostartAsked || b.StartWithWindows {
		t.Fatalf("\"not now\" records the answer without enabling anything: %+v", b)
	}
}

func TestDiagnosticsReportEachDependency(t *testing.T) {
	a := newTestApp(t)
	checks := a.Diagnostics()
	byName := map[string]Check{}
	for _, c := range checks {
		byName[c.Name] = c
		if c.Detail == "" {
			t.Errorf("%s has no explanation", c.Name)
		}
	}
	for _, name := range []string{"Steam", "Dota live feed", "Data folder", "OpenDota", "Deadlock API", "Cloud sync"} {
		if _, ok := byName[name]; !ok {
			t.Errorf("no check for %s", name)
		}
	}
	if !byName["Data folder"].OK {
		t.Error("the data folder is writable and should pass")
	}
	// The test points every API at a dead port: each must fail, with a
	// reason, rather than hang or pass.
	for _, name := range []string{"OpenDota", "Deadlock API", "Cloud sync"} {
		if byName[name].OK {
			t.Errorf("%s reported OK against an unreachable server", name)
		}
	}
}
