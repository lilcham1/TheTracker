package core

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// fakeAPI serves canned JSON by path, records what was asked for, and can be
// switched to failing so the stale-cache path is exercised.
type fakeAPI struct {
	routes  map[string]any
	down    atomic.Bool
	queries []string
	srv     *httptest.Server
}

func newFakeAPI(t *testing.T, routes map[string]any) *fakeAPI {
	f := &fakeAPI{routes: routes}
	f.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.queries = append(f.queries, r.URL.Path+"?"+r.URL.RawQuery)
		if f.down.Load() {
			http.Error(w, "boom", 503)
			return
		}
		body, ok := f.routes[r.URL.Path]
		if !ok {
			http.NotFound(w, r)
			return
		}
		json.NewEncoder(w).Encode(body)
	}))
	t.Cleanup(f.srv.Close)
	return f
}

func linked(store *Store, game string, id uint64) {
	store.SaveLink(game, Link{AccountID: &id, Personaname: ptr("me")})
}

var odHeroes = []any{
	map[string]any{"id": 1, "name": "npc_dota_hero_antimage", "localized_name": "Anti-Mage", "primary_attr": "agi", "roles": []string{"Carry", "Escape"}},
	map[string]any{"id": 2, "name": "npc_dota_hero_axe", "localized_name": "Axe", "primary_attr": "str", "roles": []string{"Initiator", "Durable"}},
	map[string]any{"id": 5, "name": "npc_dota_hero_crystal_maiden", "localized_name": "Crystal Maiden", "primary_attr": "int", "roles": []string{"Support", "Disabler"}},
}

func newTestDota(t *testing.T, routes map[string]any) (*Dota, *fakeAPI, *Store) {
	store := NewStore(t.TempDir())
	if _, ok := routes["/heroes"]; !ok {
		routes["/heroes"] = odHeroes
	}
	f := newFakeAPI(t, routes)
	d := NewDota(store)
	d.api = &service{Name: "OpenDota", Base: f.srv.URL, Attempts: 1}
	return d, f, store
}

func TestModeAndQueueClassification(t *testing.T) {
	// Ranked Turbo exists. Checking the lobby first would file it as plain
	// "ranked" and the overlay would use full-length timings in a game that
	// runs at double speed.
	cases := []struct {
		mode, lobby int
		want        string
	}{{23, 7, "turbo"}, {23, 0, "turbo"}, {22, 7, "ranked"}, {22, 0, "all_pick"}, {1, 0, "all_pick"}, {2, 1, "other"}, {18, 0, "other"}}
	for _, c := range cases {
		got := Classify(c.mode, c.lobby)
		if got != c.want {
			t.Errorf("Classify(%d,%d) = %s, want %s", c.mode, c.lobby, got, c.want)
		}
		if !IsGameType(got) {
			t.Errorf("Classify produced %s, which the UI has no filter for", got)
		}
	}
	// Modes 1 and 22 are both All Pick to a player.
	if GameModeKey(1) != GameModeKey(22) || GameModeName(1) != GameModeName(22) {
		t.Error("modes 1 and 22 should share a filter")
	}
	for _, ranked := range []int{5, 6, 7} {
		if !IsRankedLobby(ranked) {
			t.Errorf("lobby %d is a ranked queue", ranked)
		}
	}
	// Battle Cup and tournaments are competitive but not the ladder.
	for _, other := range []int{0, 1, 2, 4, 8, 9, 12, 14} {
		if IsRankedLobby(other) {
			t.Errorf("lobby %d is not the ranked ladder", other)
		}
	}
	for id := 1; id <= 25; id++ {
		if GameModeName(id) == "Unknown Mode" {
			t.Errorf("mode %d should have a name", id)
		}
	}
	if GameModeName(999) != "Unknown Mode" {
		t.Error("an unknown mode should say so")
	}
}

func TestRankLabels(t *testing.T) {
	for tier, want := range map[int]string{54: "Legend 4", 11: "Herald 1", 80: "Immortal", 85: "Immortal", 0: "", 99: ""} {
		if got := RankLabel(tier); got != want {
			t.Errorf("RankLabel(%d) = %q, want %q", tier, got, want)
		}
	}
}

func TestMatchHistoryAsksForTurboAndParsesResults(t *testing.T) {
	d, f, store := newTestDota(t, map[string]any{
		"/players/42/matches": []any{
			// Dire player, Dire win, ranked Turbo.
			map[string]any{"match_id": 100, "hero_id": 2, "player_slot": 130, "radiant_win": false, "game_mode": 23, "lobby_type": 7,
				"kills": 10, "deaths": 0, "assists": 5, "gold_per_min": 800, "xp_per_min": 900, "last_hits": 150, "duration": 1500, "start_time": 1790000000, "party_size": 2},
			// Radiant player, Dire win, abandoned.
			map[string]any{"match_id": 99, "hero_id": 77, "player_slot": 1, "radiant_win": false, "game_mode": 22, "lobby_type": 0,
				"kills": 1, "deaths": 4, "assists": 1, "leaver_status": 2},
			map[string]any{"hero_id": 1}, // no match id: skipped
		},
	})
	linked(store, "dota", 42)

	h, err := d.History(0, false)
	if err != nil {
		t.Fatal(err)
	}
	q := f.queries[0]
	if !strings.Contains(q, "significant=0") {
		t.Fatalf("without significant=0 OpenDota hides every Turbo game: %s", q)
	}
	for _, field := range []string{"project=game_mode", "project=lobby_type", "project=player_slot", "project=radiant_win", "project=duration", "project=gold_per_min"} {
		if !strings.Contains(q, field) {
			t.Errorf("%s was not requested, so that column would read as zero", field)
		}
	}

	if len(h.Matches) != 2 {
		t.Fatalf("expected 2 matches, got %d", len(h.Matches))
	}
	a, b := h.Matches[0], h.Matches[1]
	if !a.Won || a.Radiant || a.ModeKey != "turbo" || !a.Ranked || a.GameType != "turbo" || a.HeroName != "Axe" || a.HeroSlug != "axe" {
		t.Fatalf("first match misparsed: %+v", a)
	}
	if a.KDA != 15 {
		t.Fatalf("a deathless game counts as one death for KDA, got %v", a.KDA)
	}
	if *a.PartySize != 2 {
		t.Fatal("party size lost")
	}
	if b.Won || !b.Abandoned || b.HeroName != "Hero 77" || b.PartySize != nil {
		t.Fatalf("second match misparsed: %+v", b)
	}
	if h.Summary.Wins != 1 || h.Summary.Losses != 1 || h.Summary.WinRate != 50 || h.Stale {
		t.Fatalf("summary wrong: %+v", h.Summary)
	}
}

func TestAnOutageShowsTheLastGoodDataMarkedStale(t *testing.T) {
	d, f, store := newTestDota(t, map[string]any{
		"/players/42/matches": []any{map[string]any{"match_id": 100, "hero_id": 2}},
	})
	linked(store, "dota", 42)
	if _, err := d.History(0, false); err != nil {
		t.Fatal(err)
	}

	f.down.Store(true)
	h, err := d.History(0, true) // forced refresh while the API is down
	if err != nil {
		t.Fatalf("with a cached copy on disk an outage must not be an error: %v", err)
	}
	if !h.Stale || h.Error == "" || len(h.Matches) != 1 {
		t.Fatalf("expected the cached match, marked stale with a reason: %+v", h.Freshness)
	}

	// With nothing cached there is nothing to fall back on, and it says why.
	linked(store, "dota", 43)
	if _, err := d.History(0, false); err == nil || !strings.Contains(err.Error(), "OpenDota") {
		t.Fatalf("expected a readable error naming OpenDota, got %v", err)
	}
}

func TestHistoryNeedsALinkedAccount(t *testing.T) {
	d, _, _ := newTestDota(t, map[string]any{})
	if _, err := d.History(0, false); err == nil {
		t.Fatal("no account linked should be an error, not an empty list")
	}
}

func TestMatchDetailMarksThePlayerAndResolvesItems(t *testing.T) {
	d, _, store := newTestDota(t, map[string]any{
		"/matches/100": map[string]any{
			"duration": 1800, "radiant_win": true, "radiant_score": 30, "dire_score": 20, "game_mode": 23, "lobby_type": 0,
			"players": []any{
				map[string]any{"account_id": 42, "personaname": "me", "hero_id": 1, "player_slot": 0, "kills": 9, "item_0": 1, "item_1": 0, "item_2": 999},
				map[string]any{"hero_id": 2, "player_slot": 128},
			},
		},
		"/constants/items": map[string]any{
			"blink":        map[string]any{"id": 1, "dname": "Blink Dagger", "cost": 2250},
			"recipe_blink": map[string]any{"id": 2, "dname": "Recipe", "cost": 0},
		},
	})
	linked(store, "dota", 42)

	m, err := d.Detail(100)
	if err != nil {
		t.Fatal(err)
	}
	if len(m.Players) != 2 || m.ModeName != "Turbo" || !m.RadiantWin {
		t.Fatalf("detail misparsed: %+v", m)
	}
	me, other := m.Players[0], m.Players[1]
	if !me.IsMe || !me.Radiant || len(me.Items) != 1 || me.Items[0] != "blink" {
		t.Fatalf("own row wrong: %+v", me)
	}
	if other.IsMe || other.Name != "Anonymous" || other.Radiant || other.AccountID != nil {
		t.Fatalf("anonymous row wrong: %+v", other)
	}

	// Linking a different account changes who "me" is, even from cache.
	linked(store, "dota", 7)
	if m, _ := d.Detail(100); m.Players[0].IsMe {
		t.Fatal("the cached scoreboard kept marking the old account")
	}

	cat := d.ItemCatalog()
	if len(cat) != 1 || cat[0].Key != "blink" {
		t.Fatalf("the catalog should hold buyable items only: %+v", cat)
	}
}

func TestDraftAdviceRanksByResultsAgainstTheEnemyLineup(t *testing.T) {
	d, _, _ := newTestDota(t, map[string]any{
		// Anti-Mage's record against each hero: he beats CM, loses to Axe.
		"/heroes/1/matchups": []any{
			map[string]any{"hero_id": 2, "games_played": 100, "wins": 40},
			map[string]any{"hero_id": 5, "games_played": 100, "wins": 65},
			map[string]any{"hero_id": 99, "games_played": 500, "wins": 0}, // unknown hero
		},
	})
	picks, err := d.DraftAdvice([]int{1})
	if err != nil {
		t.Fatal(err)
	}
	if len(picks) != 2 || picks[0].HeroName != "Axe" || picks[0].WinRate != 60 || picks[1].HeroName != "Crystal Maiden" || picks[1].WinRate != 35 {
		t.Fatalf("Axe wins 60%% against Anti-Mage and should lead: %+v", picks)
	}
	if *picks[0].Versus[0] != 60 {
		t.Fatal("per-enemy breakdown wrong")
	}
	if none, _ := d.DraftAdvice(nil); len(none) != 0 {
		t.Fatal("no enemies picked means no advice")
	}
}

func TestBackfillOnlyFillsBlanksAndStopsAsking(t *testing.T) {
	now := time.Date(2026, 10, 3, 12, 0, 0, 0, time.UTC)
	mk := func(id, date, gameType string) MatchSummary {
		return MatchSummary{MatchID: id, Date: date, GameType: gameType}
	}
	got := backfillCandidates([]MatchSummary{
		mk("fresh", "2026-10-03T10:00:00.000Z", "unspecified"),
		mk("stale", "2026-09-26T10:00:00.000Z", "unspecified"), // OpenDota will never have it
		mk("tagged", "2026-10-03T10:00:00.000Z", "turbo"),      // the player's call
		mk("garbled", "not a date", "unspecified"),
	}, now)
	if len(got) != 1 || got[0] != "fresh" {
		t.Fatalf("candidates = %v", got)
	}

	d, f, store := newTestDota(t, map[string]any{
		"/players/42/matches": []any{map[string]any{"match_id": 555, "game_mode": 23, "lobby_type": 0}},
	})
	// Nothing linked, nothing untagged: no request at all.
	if _, err := d.BackfillGameTypes(); err != nil || len(f.queries) != 0 {
		t.Fatal("backfill touched the network with nothing to do")
	}
	linked(store, "dota", 42)
	recent := time.Now().UTC().Format(time.RFC3339)
	store.SaveHistory([]MatchSummary{mk("555", recent, "unspecified"), mk("556", recent, "ranked")})

	resolved, err := d.BackfillGameTypes()
	if err != nil || len(resolved) != 1 || resolved[0].GameType != "turbo" {
		t.Fatalf("backfill: %v %+v", err, resolved)
	}
	h := store.History()
	if h[0].GameType != "turbo" || h[1].GameType != "ranked" {
		t.Fatalf("history after backfill: %s %s", h[0].GameType, h[1].GameType)
	}
	if !strings.Contains(f.queries[0], "significant=0") {
		t.Fatal("the backfill would never see a Turbo game without significant=0")
	}
}

func TestPlayerProfile(t *testing.T) {
	d, _, store := newTestDota(t, map[string]any{
		"/players/42":    map[string]any{"rank_tier": 54, "profile": map[string]any{"personaname": "me", "avatarfull": "https://avatars.steamstatic.com/x.jpg"}},
		"/players/42/wl": map[string]any{"win": 60, "lose": 40},
		"/players/42/peers": []any{
			map[string]any{"account_id": 7, "personaname": "friend", "with_games": 50, "with_win": 30},
			map[string]any{"account_id": 8, "personaname": "stranger", "with_games": 1, "with_win": 1},
		},
		"/players/42/heroes": []any{
			map[string]any{"hero_id": 1, "games": 20, "win": 15},
			map[string]any{"hero_id": "2", "games": 80, "win": 40}, // the id was a string for years
			map[string]any{"hero_id": 5, "games": 0},
		},
	})
	linked(store, "dota", 42)
	p, err := d.Player(false)
	if err != nil {
		t.Fatal(err)
	}
	if p.Rank != "Legend 4" || p.Wins != 60 || p.WinRate != 60 {
		t.Fatalf("profile wrong: %+v", p)
	}
	if len(p.Peers) != 1 || p.Peers[0].Personaname != "friend" || p.Peers[0].WinRate != 60 {
		t.Fatalf("peers should drop one-off teammates: %+v", p.Peers)
	}
	if len(p.Heroes) != 2 || p.Heroes[0].HeroName != "Axe" || p.Heroes[0].WinRate != 50 || p.Heroes[1].HeroName != "Anti-Mage" {
		t.Fatalf("lifetime heroes wrong: %+v", p.Heroes)
	}
}

// ---------- Meta ----------

func TestThePartialFinalBucketIsExcludedFromTheTrend(t *testing.T) {
	picks := []int64{1000, 1000, 1000, 1000, 1000, 1000, 90}
	wins := []int64{500, 500, 500, 600, 600, 600, 10}
	if got := heroTrend(wins, picks); got != 10 {
		t.Fatalf("a hero winning 10 points more lately should read +10, got %v", got)
	}
	flat := []int64{500, 500, 500, 500, 500, 500, 500}
	half := []int64{250, 250, 250, 250, 250, 250, 250}
	if got := heroTrend(half, flat); got != 0 {
		t.Fatalf("a flat hero has no trend, got %v", got)
	}
	if bucketWinRate([]int64{0, 0}, []int64{0, 0}, 0, 2) != nil || bucketWinRate([]int64{1}, []int64{2}, 5, 9) != nil {
		t.Fatal("an empty or out-of-range window must yield nothing, not a division by zero")
	}
	if heroTrend(nil, nil) != 0 {
		t.Fatal("no trend data means no trend")
	}
}

func TestPositionEstimates(t *testing.T) {
	cases := []struct {
		name  string
		roles []string
		lanes map[int]int64
		want  string
	}{
		{"safe-lane carry", []string{"Carry", "Escape"}, map[int]int64{1: 800, 2: 60}, "carry"},
		{"mid", []string{"Carry", "Nuker"}, map[int]int64{2: 900, 1: 100}, "mid"},
		{"offlaner", []string{"Initiator", "Durable"}, map[int]int64{3: 2000, 1: 60}, "offlane"},
		{"support beside the carry", []string{"Support", "Disabler"}, map[int]int64{1: 890, 3: 228}, "hard_support"},
		{"roaming support", []string{"Support", "Initiator"}, map[int]int64{3: 700, 1: 300}, "support"},
		{"carry that is also tagged support", []string{"Carry", "Support"}, map[int]int64{1: 500}, "carry"},
		{"no lane data, support tags", []string{"Support", "Nuker"}, nil, "support"},
		{"no lane data, carry tags", []string{"Carry"}, nil, "carry"},
		{"no lane data, initiator", []string{"Initiator", "Disabler"}, nil, "offlane"},
	}
	for _, c := range cases {
		if got := estimatePosition(c.roles, c.lanes); got != c.want {
			t.Errorf("%s: got %s, want %s", c.name, got, c.want)
		}
	}
}

func TestDotaMeta(t *testing.T) {
	d, _, _ := newTestDota(t, map[string]any{
		"/heroStats": []any{
			map[string]any{"id": 1, "name": "npc_dota_hero_antimage", "localized_name": "Anti-Mage", "roles": []string{"Carry"}, "pub_pick": 600, "pub_win": 330,
				"7_pick": 100, "7_win": 55, "pro_pick": 10, "pro_win": 4, "pro_ban": 20, "turbo_picks": 50, "turbo_wins": 30},
			map[string]any{"id": 2, "name": "npc_dota_hero_axe", "localized_name": "Axe", "roles": []string{"Initiator"}, "pub_pick": 400, "pub_win": 180},
			map[string]any{"id": 3, "localized_name": "Unpicked", "pub_pick": 0},
		},
		"/scenarios/laneRoles": []any{
			map[string]any{"hero_id": 1, "lane_role": 1, "games": "14"},
			map[string]any{"hero_id": 2, "lane_role": 3, "games": "30"},
		},
	})
	m, err := d.Meta(false)
	if err != nil {
		t.Fatal(err)
	}
	if m.Matches != 100 || len(m.Heroes) != 2 {
		t.Fatalf("ten picks per match: 1000 picks is 100 matches; got %d matches, %d heroes", m.Matches, len(m.Heroes))
	}
	am := m.Heroes[0]
	if am.Name != "Anti-Mage" || am.WinRate != 55 || am.PickRate != 600 || am.Position != "carry" || *am.HighWinRate != 55 || *am.ProWinRate != 40 || *am.TurboWinRate != 60 {
		t.Fatalf("meta row wrong: %+v", am)
	}
	if m.Heroes[1].Position != "offlane" || m.Heroes[1].HighWinRate != nil {
		t.Fatalf("second row wrong: %+v", m.Heroes[1])
	}
}

// ---------- Deadlock ----------

func TestDeadlockOutcomeUsesTheTeamComparison(t *testing.T) {
	cases := []struct {
		m    jsonMap
		want string
	}{
		// player_match_outcome is 0 on most real records; the team
		// comparison still gives the answer.
		{jsonMap{"player_match_outcome": 0.0, "player_team": 1.0, "match_result": 1.0}, "win"},
		{jsonMap{"player_match_outcome": 0.0, "player_team": 0.0, "match_result": 1.0}, "loss"},
		{jsonMap{"player_match_outcome": 3.0, "player_team": 1.0, "match_result": 1.0}, "abandoned"},
		{jsonMap{"player_match_outcome": 1.0}, "win"},
		{jsonMap{"player_match_outcome": 2.0}, "loss"},
		{jsonMap{}, "unscored"},
	}
	for _, c := range cases {
		if got := deadlockOutcome(c.m); got != c.want {
			t.Errorf("%v: got %s, want %s", c.m, got, c.want)
		}
	}
}

func TestDeadlockSummary(t *testing.T) {
	if s := SummarizeDeadlock(nil); s.Matches != 0 || s.WinRate != 0 || s.AvgSouls != 0 || s.BestHero != nil {
		t.Fatalf("empty history: %+v", s)
	}
	s := SummarizeDeadlock([]DeadlockMatch{
		{HeroName: "Infernus", Kills: 5, Deaths: 0, Assists: 3, NetWorth: 1000, Outcome: "win"},
		{HeroName: "Haze", Outcome: "abandoned", NetWorth: 3000},
		{HeroName: "Haze", Outcome: "loss"},
	})
	if s.KDA != 8 {
		t.Fatalf("a deathless run counts as one death: KDA %v", s.KDA)
	}
	// Abandons do not count towards the win rate.
	if s.Wins != 1 || s.Losses != 1 || s.WinRate != 50 || *s.BestHero != "Infernus" {
		t.Fatalf("summary wrong: %+v", s)
	}
	if r := decodeBadge(26); r.Label != "Seeker 6" || r.Tier != 2 || r.Subrank != 6 {
		t.Fatalf("badge 26 is Seeker 6, got %+v", r)
	}
	if decodeBadge(0).Label != "Obscurus" || decodeBadge(990).TierName != "Unranked" {
		t.Fatal("badge edge cases")
	}
}

func TestDeadlockOverviewAndMeta(t *testing.T) {
	store := NewStore(t.TempDir())
	f := newFakeAPI(t, map[string]any{
		"/v1/assets/heroes": []any{
			map[string]any{"id": 1, "name": "Infernus", "images": map[string]any{"icon_image_small": "https://assets-bucket.deadlock-api.com/i.png"}},
			map[string]any{"id": 2, "name": "Seven", "images": map[string]any{"icon_hero_card": "https://assets-bucket.deadlock-api.com/s.png"}},
		},
		"/v1/players/42/match-history": []any{
			map[string]any{"match_id": 9, "hero_id": 1, "player_team": 0, "match_result": 0, "player_kills": 7, "net_worth": 30000, "match_duration_s": 1800},
			map[string]any{"match_id": 8, "hero_id": 50, "player_team": 0, "match_result": 1},
		},
		"/v1/players/42/rank": map[string]any{"badge": 61},
		"/v1/analytics/hero-stats": []any{
			map[string]any{"hero_id": 1, "matches": 600, "wins": 330, "total_kills": 3000, "total_deaths": 2000, "total_assists": 5000, "total_net_worth": 18000000, "total_player_damage": 12000000},
			map[string]any{"hero_id": 2, "matches": 600, "wins": 270, "total_deaths": 0},
			map[string]any{"hero_id": 53, "matches": 100, "wins": 99}, // not in the asset list
		},
		"/v1/analytics/item-stats": []any{
			map[string]any{"item_id": 10, "matches": 1000, "wins": 520, "avg_buy_time_s": 600},
			map[string]any{"item_id": 11, "matches": 250, "wins": 150},
			map[string]any{"item_id": 12, "matches": 900, "wins": 800}, // no name
		},
		"/v1/assets/items": []any{map[string]any{"id": 10, "name": "Extra Health"}, map[string]any{"id": 11, "name": "Mystic Burst"}},
	})
	d := NewDeadlock(store)
	d.api = &service{Name: "The Deadlock API", Base: f.srv.URL, Attempts: 1}
	linked(store, "deadlock", 42)

	o, err := d.Overview(0, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(o.Matches) != 2 || o.Matches[0].Outcome != "win" || o.Matches[0].HeroName != "Infernus" || o.Matches[1].HeroName != "Hero 50" || o.Matches[1].Outcome != "loss" {
		t.Fatalf("matches wrong: %+v", o.Matches)
	}
	if o.Rank == nil || o.Rank.Label != "Ritualist 1" {
		t.Fatalf("rank wrong: %+v", o.Rank)
	}

	m, err := d.Meta(false)
	if err != nil {
		t.Fatal(err)
	}
	if len(m.Heroes) != 2 || m.Heroes[0].Name != "Infernus" || m.Heroes[0].WinRate != 55 || m.Heroes[0].KDA != 4 || m.Heroes[0].AvgSouls != 30000 {
		t.Fatalf("an unknown hero id must be dropped, not shown as \"Hero 53\": %+v", m.Heroes)
	}
	if fmt.Sprint(m.Heroes[1].KDA) == "+Inf" {
		t.Fatal("zero deaths divided by zero")
	}
	if len(m.Items) != 2 || m.Items[0].Name != "Mystic Burst" || m.Items[0].Share != 25 || *m.Items[1].BuyMinute != 10 {
		t.Fatalf("items wrong (a nameless item must be dropped): %+v", m.Items)
	}
}
