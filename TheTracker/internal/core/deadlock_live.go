package core

import (
	"fmt"
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
	Mode      string  `json:"mode"`
	StartTime int64   `json:"startTime"`
	// The player's stream, when one was matched.
	Stream *LiveStream `json:"stream"`
}

type LiveHero struct {
	HeroID  int          `json:"heroId"`
	Name    string       `json:"name"`
	Image   *string      `json:"image"`
	Streams int          `json:"streams"`
	Players []LivePlayer `json:"players"` // streamers first, by viewers
}

type DeadlockLiveBoard struct {
	Heroes []LiveHero `json:"heroes"`
	// Deadlock streams no live player could be matched to.
	Other   []LiveStream `json:"other"`
	Matches int          `json:"matches"`
	// Whether Twitch could be asked; Reason says why not: not_set_up |
	// bad_credentials | unreachable.
	StreamsAvailable bool   `json:"streamsAvailable"`
	Reason           string `json:"reason,omitempty"`
	Freshness
}

// liveName reduces a name to what two spellings of it share: lowercase
// letters and digits, without the "ttv"/"twitch" people add to show they
// stream.
func liveName(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(s) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
		}
	}
	n := b.String()
	for _, affix := range []string{"twitchtv", "twitch", "ttv", "tv"} {
		if strings.HasPrefix(n, affix) && len(n)-len(affix) >= 3 {
			n = n[len(affix):]
		}
		if strings.HasSuffix(n, affix) && len(n)-len(affix) >= 3 {
			n = n[:len(n)-len(affix)]
		}
	}
	return n
}

// sameStreamer says whether a Steam name and a Twitch channel are probably
// the same person. Short names match too much by accident, so a partial
// match needs at least five characters.
func sameStreamer(steamName string, s LiveStream) bool {
	steam := liveName(steamName)
	if len(steam) < 3 {
		return false
	}
	for _, tw := range []string{liveName(s.Login), liveName(s.Name)} {
		if tw == "" {
			continue
		}
		if tw == steam {
			return true
		}
		if len(tw) >= 5 && strings.Contains(steam, tw) {
			return true
		}
	}
	return false
}

// livePlayers is everyone in the Watch tab's matches, with names.
func (d *Deadlock) livePlayers(force bool) ([]LivePlayer, map[uint64]int, int, Freshness, error) {
	type cached struct {
		Matches int            `json:"matches"`
		Players []LivePlayer   `json:"players"`
		Heroes  map[string]int `json:"heroes"`
	}
	c, fresh, err := cachedFetch(d.store, "dl_live_players", time.Minute, force, func() (cached, error) {
		var rows []jsonMap
		if err := d.api.get("/v1/matches/active", &rows); err != nil {
			return cached{}, err
		}
		out := cached{Matches: len(rows), Players: []LivePlayer{}, Heroes: map[string]int{}}
		ids := []string{}
		for _, m := range rows {
			mode := jStr(m, "match_mode_parsed", "")
			if strings.Contains(jStr(m, "game_mode_parsed", ""), "StreetBrawl") {
				mode = "Street Brawl"
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
	board := DeadlockLiveBoard{Matches: matches, Other: []LiveStream{}, Heroes: []LiveHero{}, Freshness: fresh}

	streams, reason := a.liveStreams(force)
	board.StreamsAvailable, board.Reason = reason == "", reason

	// Each stream goes to at most one player: the first whose name agrees.
	used := map[string]bool{}
	for i := range players {
		if players[i].Name == "" {
			continue
		}
		for j := range streams {
			if !used[streams[j].Login] && sameStreamer(players[i].Name, streams[j]) {
				s := streams[j]
				players[i].Stream = &s
				used[s.Login] = true
				break
			}
		}
	}
	for _, s := range streams {
		if !used[s.Login] {
			board.Other = append(board.Other, s)
		}
	}

	info := a.Deadlock.Heroes()
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
		}
	}
	for _, h := range byHero {
		sort.SliceStable(h.Players, func(i, j int) bool {
			a, b := h.Players[i], h.Players[j]
			if (a.Stream != nil) != (b.Stream != nil) {
				return a.Stream != nil
			}
			if a.Stream != nil {
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
	streams, _, err := cachedFetch(a.Store, "dl_live_streams", 90*time.Second, force, func() ([]LiveStream, error) {
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
