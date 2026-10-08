package core

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode"
)

// Deadlock Live: who is playing each hero right now, and which of them are
// streaming it on Twitch.
//
// The players come from the Deadlock API's copy of the game's Watch tab (the
// top live matches), their names from the same API's Steam profile lookup.
// The streams come from Twitch's official API through the cloud function in
// convex/twitch.ts, which holds the Twitch app credentials. A stream belongs
// to a player when their names agree: nothing links a Steam account to a
// Twitch channel, so a match is a good guess, and the page says so.

type LiveStream struct {
	Login     string `json:"login"`
	Name      string `json:"name"`
	Title     string `json:"title"`
	Viewers   int    `json:"viewers"`
	StartedAt string `json:"startedAt"`
	Thumbnail string `json:"thumbnail"`
	Language  string `json:"language"`
}

type LivePlayer struct {
	AccountID uint64  `json:"accountId"`
	Name      string  `json:"name"`
	Avatar    *string `json:"avatar"`
	MatchID   uint64  `json:"matchId"`
	Mode      string  `json:"mode"` // Ranked | Standard
	// The custom part of the player's Steam profile address
	// (steamcommunity.com/id/<this>), which streamers often set to their
	// Twitch name. Used for matching only.
	Vanity    string `json:"vanity,omitempty"`
	StartTime int64  `json:"startTime"`
	// The player's stream, when one was matched.
	Stream *LiveStream `json:"stream"`
	// True when the stream was linked to this account by hand, not guessed
	// from names.
	Linked bool `json:"linked,omitempty"`
	// How the stream was tied to this player: link | name | leaderboard.
	Via string `json:"via,omitempty"`
	// The player's rank, for streamers only.
	Rank *DeadlockRank `json:"rank,omitempty"`
}

type LiveHero struct {
	HeroID  int          `json:"heroId"`
	Name    string       `json:"name"`
	Image   *string      `json:"image"`
	Streams int          `json:"streams"`
	Ranked  int          `json:"ranked"`  // of Streams, how many in ranked matches
	Players []LivePlayer `json:"players"` // streamers first, by viewers
}

type DeadlockLiveBoard struct {
	Heroes []LiveHero `json:"heroes"`
	// Deadlock streams no live player could be matched to.
	Other   []LiveStream `json:"other"`
	Matches int          `json:"matches"` // standard matches only
	// Every streamer linked by hand, live or not.
	Linked []LinkedStatus `json:"linked"`
	// Live streamers found on a leaderboard whose match isn't listed.
	Ranked []RankedStreamer `json:"ranked"`
	// Whether Twitch could be asked; Reason says why not: not_set_up |
	// bad_credentials | unreachable.
	StreamsAvailable bool   `json:"streamsAvailable"`
	Reason           string `json:"reason,omitempty"`
	Freshness
}

// streamTags are words people add to a name to say where they stream.
var streamTags = map[string]bool{"ttv": true, "tv": true, "twitch": true, "twitchtv": true, "live": true, "yt": true, "youtube": true, "on": true, "kick": true}

// bracketed is a clan or region tag: [EU], (TTV), {x}, <3.
var bracketed = regexp.MustCompile(`\[[^\]]*\]|\([^)]*\)|\{[^}]*\}`)

// liveName reduces a name to what two spellings of the same name share:
// lowercase letters and digits, without bracketed tags or the words people
// add to show they stream ("TTV_Gibdin", "pandaego live", "MaleniaDL on
// twitch", "[EU] SoulTaker").
func liveName(s string) string {
	s = bracketed.ReplaceAllString(strings.ToLower(s), " ")
	words := strings.FieldsFunc(s, func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) })
	kept := []string{}
	for _, w := range words {
		if !streamTags[w] {
			kept = append(kept, w)
		}
	}
	if len(kept) == 0 {
		return ""
	}
	n := strings.Join(kept, "")
	// A tag run into the name: "LukieVibinYT", "ttvHazeMain".
	for _, affix := range []string{"twitchtv", "twitch", "ttv", "yt", "tv"} {
		if strings.HasPrefix(n, affix) && len(n)-len(affix) >= 4 {
			n = n[len(affix):]
		}
		if strings.HasSuffix(n, affix) && len(n)-len(affix) >= 4 {
			n = n[:len(n)-len(affix)]
		}
	}
	return n
}

// sameStreamer says whether a Steam player and a Twitch channel are the same
// person: the Twitch name, once tags are set aside, is exactly the player's
// Steam name or the custom part of their Steam profile address. Anything
// looser ("KenshinH" for "KenshiTTV", "metro_mann" for "Metro") pairs
// strangers too often, and showing the wrong person is worse than missing
// one.
func sameStreamer(steamName, vanity string, s LiveStream) bool {
	twitch := []string{liveName(s.Login), liveName(s.Name)}
	for _, mine := range []string{liveName(steamName), liveName(vanity)} {
		if len(mine) < 3 {
			continue
		}
		for _, tw := range twitch {
			if tw == mine {
				return true
			}
		}
	}
	return false
}

// steamVanity reads the custom part of a Steam profile address.
func steamVanity(profileURL string) string {
	_, rest, ok := strings.Cut(profileURL, "steamcommunity.com/id/")
	if !ok {
		return ""
	}
	return strings.Trim(rest, "/")
}

// RankedStreamer is a live streamer found on a leaderboard by their Twitch
// name, whose match is not among the listed ones.
type RankedStreamer struct {
	Stream   LiveStream    `json:"stream"`
	Region   string        `json:"region"`
	Position int           `json:"position"`
	Rank     *DeadlockRank `json:"rank,omitempty"`
	// Set when the leaderboard name points at a single Steam account.
	AccountID uint64 `json:"accountId,omitempty"`
}

type leaderEntry struct {
	region   string
	position int
	accounts []uint64
}

// leaderboardNames is every region's leaderboard by name, for matching
// streams. A region that can't be read is left out.
func (d *Deadlock) leaderboardNames(force bool) map[string][]leaderEntry {
	out := map[string][]leaderEntry{}
	for _, r := range DeadlockRegions {
		rows, _, err := d.leaderboardRaw(r.ID, false)
		if err != nil {
			continue
		}
		for _, e := range rows {
			// No candidate account (or more than five, already dropped):
			// the name can't be tied to anyone.
			if n := liveName(e.Name); len(n) >= 3 && len(e.Maybe) > 0 {
				out[n] = append(out[n], leaderEntry{region: r.Label, position: e.Rank, accounts: e.Maybe})
			}
		}
	}
	return out
}

// livePlayers is everyone in the Watch tab's matches, with names.
func (d *Deadlock) livePlayers(force bool) ([]LivePlayer, map[uint64]int, int, Freshness, error) {
	type cached struct {
		Matches int            `json:"matches"`
		Players []LivePlayer   `json:"players"`
		Heroes  map[string]int `json:"heroes"`
	}
	c, fresh, err := cachedFetch(d.store, "dl_live_players", 20*time.Second, force, func() (cached, error) {
		var rows []jsonMap
		if err := d.api.get("/v1/matches/active", &rows); err != nil {
			return cached{}, err
		}
		out := cached{Players: []LivePlayer{}, Heroes: map[string]int{}}
		ids := []string{}
		for _, m := range rows {
			// Standard 6v6 games only: Street Brawl and anything else is left
			// out.
			if !strings.HasSuffix(jStr(m, "game_mode_parsed", ""), "GameModeNormal") {
				continue
			}
			out.Matches++
			mode := "Standard"
			if jStr(m, "match_mode_parsed", "") == "Ranked" {
				mode = "Ranked"
			}
			for _, p := range jList(m["players"]) {
				id := jU64(p, "account_id")
				if id == 0 {
					continue
				}
				out.Players = append(out.Players, LivePlayer{AccountID: id, MatchID: jU64(m, "match_id"), Mode: mode, StartTime: jI64(m, "start_time")})
				out.Heroes[fmt.Sprint(id)] = int(jI64(p, "hero_id"))
				ids = append(ids, fmt.Sprint(id))
			}
		}
		// Names, a thousand at a time. Best effort: a player without a name
		// is still listed, just never matched to a stream.
		names := map[uint64]jsonMap{}
		for start := 0; start < len(ids); start += 1000 {
			end := min(start+1000, len(ids))
			var profiles []jsonMap
			if err := d.api.get("/v1/players/steam?account_ids="+strings.Join(ids[start:end], ","), &profiles); err == nil {
				for _, p := range profiles {
					names[jU64(p, "account_id")] = p
				}
			}
		}
		for i := range out.Players {
			if p, ok := names[out.Players[i].AccountID]; ok {
				out.Players[i].Name = jStr(p, "personaname", "")
				out.Players[i].Avatar = jStrPtr(p, "avatarmedium")
				out.Players[i].Vanity = steamVanity(jStr(p, "profileurl", ""))
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
	return c.Players, heroes, c.Matches, fresh, err
}

// DeadlockLive builds the Live page: every hero being played in the top live
// matches, with the streamers on it first.
func (a *App) DeadlockLive(force bool) (DeadlockLiveBoard, error) {
	players, heroOf, matches, fresh, err := a.Deadlock.livePlayers(force)
	if err != nil {
		return DeadlockLiveBoard{}, err
	}
	board := DeadlockLiveBoard{Matches: matches, Other: []LiveStream{}, Heroes: []LiveHero{}, Linked: []LinkedStatus{}, Ranked: []RankedStreamer{}, Freshness: fresh}

	streams, reason := a.liveStreams(force)
	board.StreamsAvailable, board.Reason = reason == "", reason
	byLogin := map[string]LiveStream{}
	for _, s := range streams {
		byLogin[strings.ToLower(s.Login)] = s
	}

	// Each stream goes to at most one player. Links made by hand come first:
	// they are certain, names are a guess.
	used := map[string]bool{}
	links := a.Store.StreamLinks()
	linkOf := map[uint64]string{}
	for _, l := range links {
		linkOf[l.AccountID] = l.Twitch
	}
	for i := range players {
		if login, ok := linkOf[players[i].AccountID]; ok {
			if s, live := byLogin[login]; live && !used[s.Login] {
				players[i].Stream, players[i].Linked, players[i].Via = &s, true, "link"
				used[s.Login] = true
			}
		}
	}
	for i := range players {
		if players[i].Stream != nil || (players[i].Name == "" && players[i].Vanity == "") {
			continue
		}
		for j := range streams {
			if !used[streams[j].Login] && sameStreamer(players[i].Name, players[i].Vanity, streams[j]) {
				s := streams[j]
				players[i].Stream, players[i].Via = &s, "name"
				used[s.Login] = true
				break
			}
		}
	}
	// Last, the leaderboards: a Twitch name that is a leaderboard name gives
	// the Steam accounts behind it. The stream goes to a player only when
	// exactly one of those accounts is in a listed match.
	if len(streams) > 0 {
		names := a.Deadlock.leaderboardNames(force)
		at := map[uint64]int{}
		for i, p := range players {
			at[p.AccountID] = i
		}
		for _, s := range streams {
			if used[s.Login] {
				continue
			}
			var hits []leaderEntry
			seen := map[string]bool{}
			for _, n := range []string{liveName(s.Login), liveName(s.Name)} {
				if !seen[n] {
					hits = append(hits, names[n]...)
					seen[n] = true
				}
			}
			if len(hits) == 0 {
				continue
			}
			accounts := map[uint64]bool{}
			for _, h := range hits {
				for _, id := range h.accounts {
					accounts[id] = true
				}
			}
			live := []int{}
			for id := range accounts {
				if i, ok := at[id]; ok && players[i].Stream == nil {
					live = append(live, i)
				}
			}
			switch {
			case len(live) == 1:
				st := s
				players[live[0]].Stream, players[live[0]].Via = &st, "leaderboard"
				used[s.Login] = true
			case len(live) == 0:
				best := hits[0]
				for _, h := range hits {
					if h.position < best.position {
						best = h
					}
				}
				rs := RankedStreamer{Stream: s, Region: best.region, Position: best.position}
				if len(accounts) == 1 {
					rs.AccountID = best.accounts[0]
				}
				board.Ranked = append(board.Ranked, rs)
				used[s.Login] = true
			}
		}
		sort.SliceStable(board.Ranked, func(i, j int) bool { return board.Ranked[i].Stream.Viewers > board.Ranked[j].Stream.Viewers })
	}
	for _, s := range streams {
		if !used[s.Login] {
			board.Other = append(board.Other, s)
		}
	}

	info := a.Deadlock.Heroes()

	// Ranks for the streamers and the linked accounts: a few dozen at most,
	// in one call.
	rankIDs := []uint64{}
	for _, p := range players {
		if p.Stream != nil {
			rankIDs = append(rankIDs, p.AccountID)
		}
	}
	for _, l := range links {
		rankIDs = append(rankIDs, l.AccountID)
	}
	for _, r := range board.Ranked {
		if r.AccountID != 0 {
			rankIDs = append(rankIDs, r.AccountID)
		}
	}
	ranks := a.Deadlock.RanksFor(rankIDs)
	for i := range board.Ranked {
		if r, ok := ranks[board.Ranked[i].AccountID]; ok && board.Ranked[i].AccountID != 0 {
			board.Ranked[i].Rank = &r
		}
	}
	for i := range players {
		if r, ok := ranks[players[i].AccountID]; ok && players[i].Stream != nil {
			players[i].Rank = &r
		}
	}

	// Where each linked streamer is right now.
	inMatch := map[uint64]LivePlayer{}
	for _, p := range players {
		inMatch[p.AccountID] = p
	}
	for _, l := range links {
		st := LinkedStatus{StreamLink: l}
		if r, ok := ranks[l.AccountID]; ok {
			st.Rank = &r
		}
		if s, live := byLogin[l.Twitch]; live {
			st.Stream = &s
		}
		if p, ok := inMatch[l.AccountID]; ok {
			id := heroOf[l.AccountID]
			st.HeroID, st.HeroName, st.Mode = id, info[id].Name, p.Mode
		}
		board.Linked = append(board.Linked, st)
	}

	byHero := map[int]*LiveHero{}
	for _, p := range players {
		id := heroOf[p.AccountID]
		h := byHero[id]
		if h == nil {
			name := info[id].Name
			if name == "" {
				name = fmt.Sprintf("Hero %d", id)
			}
			h = &LiveHero{HeroID: id, Name: name, Image: info[id].Image, Players: []LivePlayer{}}
			byHero[id] = h
		}
		h.Players = append(h.Players, p)
		if p.Stream != nil {
			h.Streams++
			if p.Mode == "Ranked" {
				h.Ranked++
			}
		}
	}
	for _, h := range byHero {
		sort.SliceStable(h.Players, func(i, j int) bool {
			a, b := h.Players[i], h.Players[j]
			if (a.Stream != nil) != (b.Stream != nil) {
				return a.Stream != nil
			}
			if a.Stream != nil {
				// Ranked games first, then the most watched.
				if (a.Mode == "Ranked") != (b.Mode == "Ranked") {
					return a.Mode == "Ranked"
				}
				return a.Stream.Viewers > b.Stream.Viewers
			}
			return a.StartTime < b.StartTime
		})
		board.Heroes = append(board.Heroes, *h)
	}
	sort.Slice(board.Heroes, func(i, j int) bool {
		a, b := board.Heroes[i], board.Heroes[j]
		if a.Streams != b.Streams {
			return a.Streams > b.Streams
		}
		if a.Ranked != b.Ranked {
			return a.Ranked > b.Ranked
		}
		if len(a.Players) != len(b.Players) {
			return len(a.Players) > len(b.Players)
		}
		return a.Name < b.Name
	})
	return board, nil
}

// liveStreams asks the cloud for Twitch's live Deadlock streams. The second
// value is why there are none, or "".
func (a *App) liveStreams(force bool) ([]LiveStream, string) {
	streams, _, err := cachedFetch(a.Store, "dl_live_streams", 45*time.Second, force, func() ([]LiveStream, error) {
		v, err := a.Cloud.call("action", "twitch:deadlockStreams", nil, "")
		if err != nil {
			return nil, err
		}
		m, _ := v.(map[string]any)
		if !jBool(m, "ok") {
			return nil, &apiError{"reason:" + jStr(m, "reason", "unreachable"), false}
		}
		out := []LiveStream{}
		for _, s := range jList(m["streams"]) {
			out = append(out, LiveStream{
				Login: jStr(s, "login", ""), Name: jStr(s, "name", ""), Title: jStr(s, "title", ""),
				Viewers: int(jI64(s, "viewers")), StartedAt: jStr(s, "startedAt", ""), Thumbnail: jStr(s, "thumbnail", ""), Language: jStr(s, "language", ""),
			})
		}
		return out, nil
	})
	if err != nil {
		if r, ok := strings.CutPrefix(err.Error(), "reason:"); ok {
			return nil, r
		}
		if strings.Contains(err.Error(), "Couldn't reach") {
			return nil, "unreachable"
		}
		// The deployment has no such function yet.
		return nil, "not_set_up"
	}
	return streams, ""
}
