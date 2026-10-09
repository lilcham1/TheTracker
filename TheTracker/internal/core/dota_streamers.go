package core

import (
	"fmt"
	"net/url"
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

func (d *Dota) searchCache() map[string]dotaSearched {
	dotaSearchMu.Lock()
	defer dotaSearchMu.Unlock()
	cache := map[string]dotaSearched{}
	d.store.readJSON("od_name_search.json", &cache)
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
	q := dotaSearches
	q.mu.Lock()
	for _, n := range names {
		if key := liveName(n); len(key) >= 3 && !q.queued[key] && len(q.pending) < dotaSearchQueueMax {
			q.queued[key] = true
			q.pending = append(q.pending, n)
		}
	}
	start := !q.running && len(q.pending) > 0
	if start {
		q.running = true
	}
	q.mu.Unlock()
	if start {
		go d.runSearches()
	}
}

func (d *Dota) runSearches() {
	q := dotaSearches
	for {
		q.mu.Lock()
		if len(q.pending) == 0 {
			q.running = false
			q.mu.Unlock()
			return
		}
		name := q.pending[0]
		q.pending = q.pending[1:]
		every := q.every
		q.mu.Unlock()

		d.searchName(name)
		q.mu.Lock()
		delete(q.queued, liveName(name))
		q.mu.Unlock()
		time.Sleep(every)
	}
}

// searchName looks one name up and remembers which accounts carry exactly
// that name.
func (d *Dota) searchName(name string) {
	key := liveName(name)
	var rows []jsonMap
	if err := d.api.get("/search?q="+url.QueryEscape(name), &rows); err != nil {
		return
	}
	found := []uint64{}
	for _, r := range rows {
		if liveName(jStr(r, "personaname", "")) == key {
			if id := jU64(r, "account_id"); id != 0 {
				found = append(found, id)
			}
		}
	}
	dotaSearchMu.Lock()
	defer dotaSearchMu.Unlock()
	cache := map[string]dotaSearched{}
	d.store.readJSON("od_name_search.json", &cache)
	cache[key] = dotaSearched{Accounts: found, At: time.Now().Unix()}
	// Forget lookups that have run out, so the file doesn't grow forever.
	for k, c := range cache {
		if time.Since(time.Unix(c.At, 0)) > dotaSearchFor {
			delete(cache, k)
		}
	}
	_ = d.store.writeJSON("od_name_search.json", cache)
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
	// looked up yet are queued for the background, most watched first.
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
			if found, ok := a.Dota.accountsNamed(s.Login); ok {
				placeAmong(s, found, "search")
			} else if s.Viewers >= dotaSearchMinViewers {
				toSearch = append(toSearch, s.Login)
			}
		}
		a.Dota.queueSearches(toSearch)
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
	board.Heroes = groupByHero(players, heroOf, func(id int) (string, *string) { return info[id].Name, heroImage(id) })
	return board, nil
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
