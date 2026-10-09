package core

import (
	"fmt"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"
)

// Dota 2 streamers: who is streaming each hero right now.
//
// The live games are Dota's top ~100 (the in-game Watch list) from OpenDota's
// /live, every player with account and hero. Unlike Deadlock there is no way
// to get every player's Steam name without a Steam key, so Dota works from
// the Twitch side: for each live stream, the Steam accounts behind its name
// come from a hand link, the live game itself (pros carry their pro name), a
// Steam name we do know, OpenDota's pro list, or an OpenDota name search.
// The stream goes on a hero only when exactly one of those accounts is in a
// listed game.

// Standard Dota: ranked and unranked lobbies in the usual draft modes. No
// Turbo, Ability Draft, custom or league lobbies.
var dotaStandardModes = map[int64]bool{1: true, 2: true, 3: true, 4: true, 5: true, 16: true, 22: true}

const dotaHeroCDN = "https://cdn.cloudflare.steamstatic.com/apps/dota2/images/dota_react/heroes/"

// liveGames is everyone in Dota's listed live games, with a name where one
// is known.
func (d *Dota) liveGames(force bool) ([]LivePlayer, map[uint64]int, int, Freshness, error) {
	type cached struct {
		Games   int            `json:"games"`
		Players []LivePlayer   `json:"players"`
		Heroes  map[string]int `json:"heroes"`
	}
	c, fresh, err := cachedFetch(d.store, "od_live_games", 45*time.Second, force, func() (cached, error) {
		var rows []jsonMap
		if err := d.api.get("/live", &rows); err != nil {
			return cached{}, err
		}
		out := cached{Players: []LivePlayer{}, Heroes: map[string]int{}}
		for _, g := range rows {
			lobby := jI64(g, "lobby_type")
			if (lobby != 0 && lobby != 7) || !dotaStandardModes[jI64(g, "game_mode")] {
				continue
			}
			out.Games++
			mode := "Unranked"
			if lobby == 7 {
				mode = "Ranked"
			}
			for _, p := range jList(g["players"]) {
				id, hero := jU64(p, "account_id"), int(jI64(p, "hero_id"))
				if id == 0 || hero == 0 {
					continue
				}
				out.Players = append(out.Players, LivePlayer{AccountID: id, Name: jStr(p, "name", ""), MatchID: jU64(g, "match_id"), Mode: mode, StartTime: jI64(g, "activate_time")})
				out.Heroes[fmt.Sprint(id)] = hero
			}
		}
		return out, nil
	})
	heroes := map[uint64]int{}
	for k, v := range c.Heroes {
		var id uint64
		fmt.Sscan(k, &id)
		heroes[id] = v
	}
	return c.Players, heroes, c.Games, fresh, err
}

type dotaPro struct {
	AccountID   uint64 `json:"accountId"`
	Name        string `json:"name"`
	Personaname string `json:"personaname"`
}

// proNames is OpenDota's pro list by name (pro name and Steam name).
func (d *Dota) proNames() map[string][]uint64 {
	pros, _, _ := cachedFetch(d.store, "od_pro_players", 24*time.Hour, false, func() ([]dotaPro, error) {
		var rows []jsonMap
		if err := d.api.get("/proPlayers", &rows); err != nil {
			return nil, err
		}
		out := []dotaPro{}
		for _, r := range rows {
			if id := jU64(r, "account_id"); id != 0 {
				out = append(out, dotaPro{AccountID: id, Name: jStr(r, "name", ""), Personaname: jStr(r, "personaname", "")})
			}
		}
		return out, nil
	})
	out := map[string][]uint64{}
	for _, p := range pros {
		seen := map[string]bool{}
		for _, n := range []string{liveName(p.Name), liveName(p.Personaname)} {
			if len(n) >= 3 && !seen[n] {
				out[n] = append(out[n], p.AccountID)
				seen[n] = true
			}
		}
	}
	return out
}

// Name searches cost OpenDota requests, which are shared with the rest of
// the app (60 a minute, 3,000 a day). They run in the background, one every
// couple of seconds, and each name is looked up once every three days. A
// page refresh only reads what has been found so far.
const (
	dotaSearchEvery = 2 * time.Second
	dotaSearchFor   = 72 * time.Hour
	// Streams watched by fewer people than this aren't looked up.
	dotaSearchMinViewers = 3
	dotaSearchQueueMax   = 150
)

type dotaSearched struct {
	Accounts []uint64 `json:"accounts"`
	At       int64    `json:"at"`
}

var dotaSearchMu sync.Mutex

// searchQueue holds names waiting to be looked up, most watched first.
type searchQueue struct {
	mu      sync.Mutex
	pending []string
	queued  map[string]bool
	running bool
	every   time.Duration
}

var dotaSearches = &searchQueue{queued: map[string]bool{}, every: dotaSearchEvery}

// dotaLastGames reads streamers' last finished games in the background.
var dotaLastGames = &searchQueue{queued: map[string]bool{}, every: dotaSearchEvery}

// enqueue adds names (or ids) to a queue and starts it working through them
// with work, one every q.every.
func enqueue(q *searchQueue, names []string, key func(string) string, work func(string)) {
	q.mu.Lock()
	for _, n := range names {
		if k := key(n); len(k) >= 1 && !q.queued[k] && len(q.pending) < dotaSearchQueueMax {
			q.queued[k] = true
			q.pending = append(q.pending, n)
		}
	}
	start := !q.running && len(q.pending) > 0
	if start {
		q.running = true
	}
	q.mu.Unlock()
	if !start {
		return
	}
	go func() {
		for {
			q.mu.Lock()
			if len(q.pending) == 0 {
				q.running = false
				q.mu.Unlock()
				return
			}
			n := q.pending[0]
			q.pending = q.pending[1:]
			every := q.every
			q.mu.Unlock()
			work(n)
			q.mu.Lock()
			delete(q.queued, key(n))
			q.mu.Unlock()
			time.Sleep(every)
		}
	}()
}

func (d *Dota) searchCache() map[string]dotaSearched {
	dotaSearchMu.Lock()
	defer dotaSearchMu.Unlock()
	cache := map[string]dotaSearched{}
	d.store.readJSON("od_name_search2.json", &cache)
	return cache
}

// accountsNamed is the Steam accounts already found under this exact name.
// ok is false when the name hasn't been looked up yet.
func (d *Dota) accountsNamed(name string) ([]uint64, bool) {
	key := liveName(name)
	if len(key) < 3 {
		return nil, false
	}
	if c, hit := d.searchCache()[key]; hit && time.Since(time.Unix(c.At, 0)) < dotaSearchFor {
		return c.Accounts, true
	}
	return nil, false
}

// queueSearches asks for names to be looked up in the background.
func (d *Dota) queueSearches(names []string) {
	enqueue(dotaSearches, names, liveName, d.searchName)
}

// ---------- Last game ----------

type dotaLastGame struct {
	HeroID  int   `json:"heroId"`
	EndedAt int64 `json:"endedAt"`
	Won     bool  `json:"won"`
	Known   bool  `json:"known"`
}

// RecentStreamer is a live streamer whose account is known but who isn't
// placed in a live game: the hero of their last finished game.
type RecentStreamer struct {
	Stream    LiveStream    `json:"stream"`
	AccountID uint64        `json:"accountId"`
	HeroID    int           `json:"heroId"`
	HeroName  string        `json:"heroName"`
	HeroImage *string       `json:"heroImage"`
	EndedAt   int64         `json:"endedAt"`
	Won       bool          `json:"won"`
	Rank      *DeadlockRank `json:"rank,omitempty"`
}

const dotaLastGameFor = 10 * time.Minute

// A searched account counts as the streamer's only if it played within this.
const dotaActiveWithin = 30 * 24 * time.Hour

// A last game older than this says nothing about what they play now.
const dotaLastGameRecent = 12 * time.Hour

type lastGameSeen struct {
	At   int64        `json:"at"`
	Game dotaLastGame `json:"game"`
}

var dotaLastMu sync.Mutex

func (d *Dota) lastGames() map[string]lastGameSeen {
	dotaLastMu.Lock()
	defer dotaLastMu.Unlock()
	all := map[string]lastGameSeen{}
	d.store.readJSON("od_last_games.json", &all)
	return all
}

// lastGame is an account's last finished game, if it has been read within
// ten minutes; otherwise ok is false.
func (d *Dota) lastGame(id uint64) (dotaLastGame, bool) {
	if c, hit := d.lastGames()[fmt.Sprint(id)]; hit && time.Since(time.Unix(c.At, 0)) < dotaLastGameFor {
		return c.Game, true
	}
	return dotaLastGame{}, false
}

func (d *Dota) queueLastGames(ids []uint64) {
	names := make([]string, 0, len(ids))
	for _, id := range ids {
		names = append(names, fmt.Sprint(id))
	}
	enqueue(dotaLastGames, names, func(s string) string { return s }, func(s string) {
		var id uint64
		fmt.Sscan(s, &id)
		var rows []jsonMap
		if err := d.api.get(fmt.Sprintf("/players/%d/recentMatches", id), &rows); err != nil {
			return
		}
		g := dotaLastGame{}
		if len(rows) > 0 {
			m := rows[0]
			radiant := jI64(m, "player_slot") < 128
			g = dotaLastGame{HeroID: int(jI64(m, "hero_id")), EndedAt: jI64(m, "start_time") + jI64(m, "duration"), Won: radiant == jBool(m, "radiant_win"), Known: true}
		}
		dotaLastMu.Lock()
		defer dotaLastMu.Unlock()
		all := map[string]lastGameSeen{}
		d.store.readJSON("od_last_games.json", &all)
		all[s] = lastGameSeen{At: time.Now().Unix(), Game: g}
		// Drop what's a day old, so the file stays small.
		for k, c := range all {
			if time.Since(time.Unix(c.At, 0)) > 24*time.Hour {
				delete(all, k)
			}
		}
		_ = d.store.writeJSON("od_last_games.json", all)
	})
}

// ---------- Live through Steam ----------

type steamInGame struct {
	HeroID    int
	MatchID   uint64
	GameMode  int64
	LobbyType int64 // -1 when Steam didn't say
	GameTime  int64
}

type steamSeen struct {
	game *steamInGame
	at   time.Time
}

var steamMemo = struct {
	sync.Mutex
	seen   map[uint64]steamSeen
	reason string
}{seen: map[uint64]steamSeen{}}

// steamLive asks the cloud, through Steam's official API, which of these
// accounts are in a Dota game right now and on which hero. Each answer is
// kept a minute. The second value is why it couldn't, or "".
func (a *App) steamLive(ids []uint64, force bool) (map[uint64]steamInGame, string) {
	out := map[uint64]steamInGame{}
	want := []string{}
	steamMemo.Lock()
	for _, id := range ids {
		if s, ok := steamMemo.seen[id]; ok && !force && time.Since(s.at) < time.Minute {
			if s.game != nil {
				out[id] = *s.game
			}
			continue
		}
		want = append(want, fmt.Sprint(id))
	}
	reason := steamMemo.reason
	steamMemo.Unlock()
	if len(want) == 0 {
		return out, reason
	}
	if len(want) > 200 {
		want = want[:200]
	}
	v, err := a.Cloud.call("action", "steamlive:dotaLive", map[string]any{"accountIds": want}, "")
	reason = ""
	var rows []jsonMap
	switch {
	case err != nil && strings.Contains(err.Error(), "Couldn't reach"):
		reason = "unreachable"
	case err != nil:
		reason = "not_set_up" // a deployment without the function
	default:
		m, _ := v.(map[string]any)
		if !jBool(m, "ok") {
			reason = jStr(m, "reason", "unreachable")
		} else {
			rows = jList(m["players"])
		}
	}
	now := time.Now()
	steamMemo.Lock()
	steamMemo.reason = reason
	if reason == "" {
		got := map[uint64]*steamInGame{}
		for _, r := range rows {
			var id, match uint64
			fmt.Sscan(jStr(r, "accountId", ""), &id)
			fmt.Sscan(jStr(r, "matchId", ""), &match)
			got[id] = &steamInGame{HeroID: int(jI64(r, "heroId")), MatchID: match, GameMode: jI64(r, "gameMode"), LobbyType: jI64(r, "lobbyType"), GameTime: jI64(r, "gameTime")}
		}
		for _, s := range want {
			var id uint64
			fmt.Sscan(s, &id)
			steamMemo.seen[id] = steamSeen{got[id], now}
			if g := got[id]; g != nil {
				out[id] = *g
			}
		}
	}
	steamMemo.Unlock()
	return out, reason
}

// searchName looks one name up and remembers which accounts carry exactly
// that name.
func (d *Dota) searchName(name string) {
	key := liveName(name)
	var rows []jsonMap
	if err := d.api.get("/search?q="+url.QueryEscape(name), &rows); err != nil {
		return
	}
	// Only accounts that have played lately: an old, abandoned account with
	// the same name is someone else, or no longer the streamer's.
	found := []uint64{}
	for _, r := range rows {
		if liveName(jStr(r, "personaname", "")) != key {
			continue
		}
		last, err := time.Parse(time.RFC3339, jStr(r, "last_match_time", ""))
		if err != nil || time.Since(last) > dotaActiveWithin {
			continue
		}
		if id := jU64(r, "account_id"); id != 0 {
			found = append(found, id)
		}
	}
	dotaSearchMu.Lock()
	defer dotaSearchMu.Unlock()
	cache := map[string]dotaSearched{}
	d.store.readJSON("od_name_search2.json", &cache)
	cache[key] = dotaSearched{Accounts: found, At: time.Now().Unix()}
	// Forget lookups that have run out, so the file doesn't grow forever.
	for k, c := range cache {
		if time.Since(time.Unix(c.At, 0)) > dotaSearchFor {
			delete(cache, k)
		}
	}
	_ = d.store.writeJSON("od_name_search2.json", cache)
}

// dotaMedal is a Dota rank as the page shows it, with the medal and star
// images OpenDota hosts.
func dotaMedal(tier, leaderboard int) *DeadlockRank {
	medal, stars := tier/10, tier%10
	if medal < 1 || medal > 8 {
		return nil
	}
	icon := fmt.Sprintf("https://www.opendota.com/assets/images/dota2/rank_icons/rank_icon_%d.png", medal)
	r := &DeadlockRank{Badge: tier, Tier: medal, Subrank: stars, TierName: RankLabel(medal * 10), Label: RankLabel(tier), Icon: &icon}
	if medal == 8 && leaderboard > 0 {
		r.Label = fmt.Sprintf("Immortal #%d", leaderboard)
	}
	if medal < 8 && stars >= 1 && stars <= 5 {
		star := fmt.Sprintf("https://www.opendota.com/assets/images/dota2/rank_icons/rank_star_%d.png", stars)
		r.Star = &star
	}
	return r
}

// dotaRanks is the medal of each account, from OpenDota, kept an hour,
// looked up four at a time.
func (d *Dota) dotaRanks(ids []uint64) map[uint64]DeadlockRank {
	type rank struct {
		Tier        int `json:"tier"`
		Leaderboard int `json:"leaderboard"`
	}
	out := map[uint64]DeadlockRank{}
	var mu sync.Mutex
	var wg sync.WaitGroup
	slots := make(chan struct{}, 4)
	seen := map[uint64]bool{}
	for _, id := range ids {
		if seen[id] {
			continue
		}
		seen[id] = true
		wg.Add(1)
		slots <- struct{}{}
		go func(id uint64) {
			defer func() { <-slots; wg.Done() }()
			r, _, err := cachedFetch(d.store, fmt.Sprintf("od_rank_%d", id), time.Hour, false, func() (rank, error) {
				var v jsonMap
				if err := d.api.get(fmt.Sprintf("/players/%d", id), &v); err != nil {
					return rank{}, err
				}
				return rank{Tier: int(jI64(v, "rank_tier")), Leaderboard: int(jI64(v, "leaderboard_rank"))}, nil
			})
			if err != nil {
				return
			}
			if m := dotaMedal(r.Tier, r.Leaderboard); m != nil {
				mu.Lock()
				out[id] = *m
				mu.Unlock()
			}
		}(id)
	}
	wg.Wait()
	return out
}

// DotaStreamers builds the Dota streamers page, in the same shape as
// Deadlock's Live page.
func (a *App) DotaStreamers(force bool) (DeadlockLiveBoard, error) {
	players, heroOf, games, fresh, err := a.Dota.liveGames(force)
	if err != nil {
		return DeadlockLiveBoard{}, err
	}
	board := DeadlockLiveBoard{Matches: games, Other: []LiveStream{}, Heroes: []LiveHero{}, Linked: []LinkedStatus{}, Ranked: []RankedStreamer{}, Freshness: fresh}

	// Steam names the Deadlock API happens to know (players of both games).
	ids := []string{}
	for _, p := range players {
		if p.Name == "" {
			ids = append(ids, fmt.Sprint(p.AccountID))
		}
	}
	if len(ids) > 0 {
		var profiles []jsonMap
		if a.Deadlock.api.get("/v1/players/steam?account_ids="+strings.Join(ids, ","), &profiles) == nil {
			names := map[uint64]jsonMap{}
			for _, p := range profiles {
				names[jU64(p, "account_id")] = p
			}
			for i := range players {
				if p, ok := names[players[i].AccountID]; ok && players[i].Name == "" {
					players[i].Name = jStr(p, "personaname", "")
					players[i].Avatar = jStrPtr(p, "avatarmedium")
					players[i].Vanity = steamVanity(jStr(p, "profileurl", ""))
				}
			}
		}
	}

	streams, reason := a.liveStreams("dota", force)
	board.StreamsAvailable, board.Reason = reason == "", reason
	byLogin := map[string]LiveStream{}
	for _, s := range streams {
		byLogin[strings.ToLower(s.Login)] = s
	}
	at := map[uint64]int{}
	for i, p := range players {
		at[p.AccountID] = i
	}
	used := map[string]bool{}
	place := func(i int, s LiveStream, via string) {
		st := s
		players[i].Stream, players[i].Via = &st, via
		players[i].Linked = via == "link"
		used[s.Login] = true
	}
	// placeAmong gives the stream to the one live, unplaced account among
	// candidates; none or several, and it stays where it is.
	placeAmong := func(s LiveStream, accounts []uint64, via string) bool {
		live := -1
		for _, id := range accounts {
			if i, ok := at[id]; ok && players[i].Stream == nil {
				if live >= 0 && live != i {
					return false
				}
				live = i
			}
		}
		if live < 0 {
			return false
		}
		place(live, s, via)
		return true
	}

	// 1. Links made by hand.
	links := a.Store.StreamLinksFor("dota")
	for _, l := range links {
		if s, live := byLogin[l.Twitch]; live && !used[s.Login] {
			placeAmong(s, []uint64{l.AccountID}, "link")
		}
	}
	// 2. A known name in the game: pro names, Steam names.
	for i := range players {
		if players[i].Stream != nil || (players[i].Name == "" && players[i].Vanity == "") {
			continue
		}
		for _, s := range streams {
			if !used[s.Login] && sameStreamer(players[i].Name, players[i].Vanity, s) {
				place(i, s, "name")
				break
			}
		}
	}
	// 3. OpenDota's pro list, then 4. names already looked up; names not
	// looked up yet are queued for the background, most watched first. The
	// accounts each stream could be are kept for the passes after.
	candidates := map[string][]uint64{}
	linkFor := map[string]uint64{}
	for _, l := range links {
		linkFor[l.Twitch] = l.AccountID
	}
	if len(streams) > 0 {
		pros := a.Dota.proNames()
		toSearch := []string{}
		for _, s := range streams {
			if used[s.Login] {
				continue
			}
			var accounts []uint64
			for _, n := range uniqueNames(s) {
				accounts = append(accounts, pros[n]...)
			}
			if placeAmong(s, accounts, "pro") {
				continue
			}
			found, ok := a.Dota.accountsNamed(s.Login)
			if ok && placeAmong(s, found, "search") {
				continue
			}
			if !ok && s.Viewers >= dotaSearchMinViewers {
				toSearch = append(toSearch, s.Login)
			}
			if id, linked := linkFor[strings.ToLower(s.Login)]; linked {
				candidates[s.Login] = []uint64{id}
				continue
			}
			all := uniqueIDs(append(accounts, found...))
			if len(all) > 0 && len(all) <= 3 {
				candidates[s.Login] = all
			}
		}
		a.Dota.queueSearches(toSearch)
	}

	// 5. Steam: which of those accounts are in a Dota game anywhere, and on
	// which hero, through Steam's official API (public profiles only).
	if len(candidates) > 0 {
		ids := []uint64{}
		for _, list := range candidates {
			ids = append(ids, list...)
		}
		live, why := a.steamLive(uniqueIDs(ids), force)
		board.SteamAvailable, board.SteamReason = why == "", why
		for _, s := range streams {
			list, ok := candidates[s.Login]
			if !ok || used[s.Login] {
				continue
			}
			var hit uint64
			n := 0
			for _, id := range list {
				if g, in := live[id]; in && g.HeroID > 0 && dotaStandardModes[g.GameMode] && (g.LobbyType == -1 || g.LobbyType == 0 || g.LobbyType == 7) {
					hit, n = id, n+1
				}
			}
			if n != 1 {
				continue
			}
			g := live[hit]
			i, listed := at[hit]
			if !listed {
				mode := "Unranked"
				if g.LobbyType == 7 || (g.LobbyType == -1 && g.GameMode == 22) {
					mode = "Ranked"
				}
				players = append(players, LivePlayer{AccountID: hit, MatchID: g.MatchID, Mode: mode, StartTime: time.Now().Unix() - g.GameTime})
				i = len(players) - 1
				at[hit] = i
				heroOf[hit] = g.HeroID
			}
			if players[i].Stream == nil {
				place(i, s, "steam")
			}
		}
	}

	// Streamers on Valve's leaderboard whose game isn't listed.
	if len(streams) > 0 {
		type spot struct {
			region   string
			position int
		}
		boards := make([]DotaLeaderboard, len(DotaRegions))
		var wg sync.WaitGroup
		for i, r := range DotaRegions {
			wg.Add(1)
			go func(i int, id string) {
				defer wg.Done()
				boards[i], _ = a.Dota.Leaderboard(id, false)
			}(i, r.ID)
		}
		wg.Wait()
		board2 := map[string]spot{}
		for i, r := range DotaRegions {
			for _, p := range boards[i].Players {
				if n := liveName(p.Name); len(n) >= 3 {
					if old, ok := board2[n]; !ok || p.Rank < old.position {
						board2[n] = spot{r.Label, p.Rank}
					}
				}
			}
		}
		for _, s := range streams {
			if used[s.Login] {
				continue
			}
			for _, n := range uniqueNames(s) {
				if sp, ok := board2[n]; ok {
					icon := "https://www.opendota.com/assets/images/dota2/rank_icons/rank_icon_8.png"
					rank := &DeadlockRank{Tier: 8, TierName: "Immortal", Label: fmt.Sprintf("Immortal #%d", sp.position), Icon: &icon}
					board.Ranked = append(board.Ranked, RankedStreamer{Stream: s, Region: sp.region, Position: sp.position, Rank: rank})
					used[s.Login] = true
					break
				}
			}
		}
	}
	// 6. The hero of the last finished game, for streamers whose single
	// account is known but who aren't placed live.
	lastWanted := []uint64{}
	for _, s := range streams {
		list, ok := candidates[s.Login]
		if !ok || used[s.Login] || len(list) != 1 {
			continue
		}
		if g, fresh := a.Dota.lastGame(list[0]); fresh {
			if g.Known && g.HeroID > 0 && time.Since(time.Unix(g.EndedAt, 0)) < dotaLastGameRecent {
				board.Recent = append(board.Recent, RecentStreamer{Stream: s, AccountID: list[0], HeroID: g.HeroID, EndedAt: g.EndedAt, Won: g.Won})
				used[s.Login] = true
			}
		} else {
			lastWanted = append(lastWanted, list[0])
		}
	}
	a.Dota.queueLastGames(lastWanted)

	for _, s := range streams {
		if !used[s.Login] {
			board.Other = append(board.Other, s)
		}
	}

	// Medals for the streamers on a hero and the linked accounts.
	rankIDs := []uint64{}
	for _, p := range players {
		if p.Stream != nil {
			rankIDs = append(rankIDs, p.AccountID)
		}
	}
	for _, l := range links {
		rankIDs = append(rankIDs, l.AccountID)
	}
	for _, r := range board.Recent {
		rankIDs = append(rankIDs, r.AccountID)
	}
	ranks := a.Dota.dotaRanks(rankIDs)
	for i := range players {
		if r, ok := ranks[players[i].AccountID]; ok && players[i].Stream != nil {
			players[i].Rank = &r
		}
	}

	info := a.Dota.Heroes()
	heroImage := func(id int) *string {
		if h, ok := info[id]; ok && h.Slug != "" {
			u := dotaHeroCDN + h.Slug + ".png"
			return &u
		}
		return nil
	}
	for _, l := range links {
		st := LinkedStatus{StreamLink: l}
		if r, ok := ranks[l.AccountID]; ok {
			st.Rank = &r
		}
		if s, live := byLogin[l.Twitch]; live {
			st.Stream = &s
		}
		if i, ok := at[l.AccountID]; ok {
			id := heroOf[l.AccountID]
			st.HeroID, st.HeroName, st.Mode = id, info[id].Name, players[i].Mode
		}
		board.Linked = append(board.Linked, st)
	}
	for i := range board.Recent {
		r := &board.Recent[i]
		r.HeroName, r.HeroImage = info[r.HeroID].Name, heroImage(r.HeroID)
		if rk, ok := ranks[r.AccountID]; ok {
			r.Rank = &rk
		}
	}
	sort.SliceStable(board.Recent, func(i, j int) bool { return board.Recent[i].Stream.Viewers > board.Recent[j].Stream.Viewers })
	board.Heroes = groupByHero(players, heroOf, func(id int) (string, *string) { return info[id].Name, heroImage(id) })
	return board, nil
}

func uniqueIDs(ids []uint64) []uint64 {
	seen := map[uint64]bool{}
	out := []uint64{}
	for _, id := range ids {
		if !seen[id] {
			seen[id] = true
			out = append(out, id)
		}
	}
	return out
}

// uniqueNames is a stream's login and display name, reduced and without
// repeats.
func uniqueNames(s LiveStream) []string {
	out := []string{}
	for _, n := range []string{liveName(s.Login), liveName(s.Name)} {
		if len(n) >= 3 && (len(out) == 0 || out[0] != n) {
			out = append(out, n)
		}
	}
	return out
}
