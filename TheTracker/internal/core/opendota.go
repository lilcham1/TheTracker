package core

import (
	"errors"
	"fmt"
	"net/url"
	"sort"
	"strconv"
	"time"
)

// Dota match history and reference data, from the public OpenDota API.
//
// The live GSI feed only ever describes the local player while a match
// runs. OpenDota fills in everything after the fact: results, game modes,
// scoreboards, lifetime hero records, teammates. It needs no API key and is
// keyed on the Steam32 account id.

type Dota struct {
	store *Store
	api   *service
	// Valve's own web API, for the official leaderboard.
	valve *service
}

func NewDota(store *Store) *Dota {
	return &Dota{
		store: store,
		api:   &service{Name: "OpenDota", Base: "https://api.opendota.com/api", Attempts: 3},
		valve: &service{Name: "Dota's leaderboard", Base: "https://www.dota2.com/webapi", Attempts: 2},
	}
}

func (d *Dota) account() (uint64, error) {
	if id := d.store.LoadLink("dota").AccountID; id != nil {
		return *id, nil
	}
	return 0, errors.New("No Steam account linked yet.")
}

// ---------- Heroes ----------

type DotaHero struct {
	ID    int      `json:"id"`
	Name  string   `json:"name"`
	Slug  string   `json:"slug"` // internal name without npc_dota_hero_, for portraits
	Attr  string   `json:"attr"` // str | agi | int | all
	Roles []string `json:"roles"`
}

func heroSlug(raw string) string {
	const p = "npc_dota_hero_"
	if len(raw) > len(p) && raw[:len(p)] == p {
		return raw[len(p):]
	}
	return raw
}

// Heroes maps hero id to name and portrait slug. These change only when
// Valve adds a hero, so a day-old copy is fine.
func (d *Dota) Heroes() map[int]DotaHero {
	list, _, _ := cachedFetch(d.store, "od_heroes", 24*time.Hour, false, func() ([]DotaHero, error) {
		var rows []jsonMap
		if err := d.api.get("/heroes", &rows); err != nil {
			return nil, err
		}
		out := make([]DotaHero, 0, len(rows))
		for _, h := range rows {
			roles := []string{}
			if arr, ok := h["roles"].([]any); ok {
				for _, r := range arr {
					if s, ok := r.(string); ok {
						roles = append(roles, s)
					}
				}
			}
			out = append(out, DotaHero{
				ID:    int(jI64(h, "id")),
				Name:  jStr(h, "localized_name", "Unknown"),
				Slug:  heroSlug(jStr(h, "name", "")),
				Attr:  jStr(h, "primary_attr", ""),
				Roles: roles,
			})
		}
		if len(out) == 0 {
			return nil, &apiError{"OpenDota returned no heroes.", true}
		}
		return out, nil
	})
	m := make(map[int]DotaHero, len(list))
	for _, h := range list {
		m[h.ID] = h
	}
	return m
}

// HeroList is Heroes as a name-sorted list, for pickers.
func (d *Dota) HeroList() []DotaHero {
	out := []DotaHero{}
	for _, h := range d.Heroes() {
		out = append(out, h)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// ---------- Mode and queue ----------
//
// Mode (how heroes are picked) and queue (ranked or not) are two independent
// facts about a match. Both are kept, and the UI filters on each separately.

// Classify buckets a match for the Sessions comparison groups. Turbo is
// decided first: ranked Turbo exists, and it plays like Turbo.
func Classify(gameMode, lobbyType int) string {
	switch {
	case gameMode == 23:
		return "turbo"
	case lobbyType == 7:
		return "ranked"
	case gameMode == 1 || gameMode == 22:
		return "all_pick"
	}
	return "other"
}

var modeKeys = map[int]string{
	1: "all_pick", 22: "all_pick", 2: "captains_mode", 3: "random_draft", 4: "single_draft",
	5: "all_random", 12: "least_played", 16: "captains_draft", 17: "balanced_draft",
	18: "ability_draft", 20: "ardm", 21: "1v1_mid", 23: "turbo", 24: "mutation",
}

var modeNames = map[int]string{
	1: "All Pick", 22: "All Pick", 2: "Captains Mode", 3: "Random Draft", 4: "Single Draft",
	5: "All Random", 6: "Intro", 7: "Diretide", 8: "Reverse Captains Mode", 9: "Greeviling",
	10: "Tutorial", 11: "Mid Only", 12: "Least Played", 13: "Limited Heroes",
	14: "Compendium Matchmaking", 15: "Custom Game", 16: "Captains Draft", 17: "Balanced Draft",
	18: "Ability Draft", 19: "Event", 20: "All Random Deathmatch", 21: "1v1 Mid", 23: "Turbo",
	24: "Mutation", 25: "Coaches Challenge",
}

var lobbyNames = map[int]string{
	0: "Unranked", 1: "Practice", 2: "Tournament", 3: "Tutorial", 4: "Bots", 5: "Ranked",
	6: "Ranked", 7: "Ranked", 8: "1v1 Mid", 9: "Battle Cup", 10: "Local Bots", 11: "Spectator",
	12: "Event", 13: "Gauntlet", 14: "New Player", 15: "Featured",
}

func GameModeKey(id int) string {
	if k, ok := modeKeys[id]; ok {
		return k
	}
	return "other"
}

func GameModeName(id int) string {
	if n, ok := modeNames[id]; ok {
		return n
	}
	return "Unknown Mode"
}

func LobbyName(id int) string {
	if n, ok := lobbyNames[id]; ok {
		return n
	}
	return "Other"
}

// IsRankedLobby is true only for the queues that affect rank. Battle Cup and
// tournaments are competitive but are not the ladder.
func IsRankedLobby(lobby int) bool { return lobby == 5 || lobby == 6 || lobby == 7 }

// ---------- Match history ----------

type DotaMatch struct {
	MatchID         uint64  `json:"matchId"`
	HeroID          int     `json:"heroId"`
	HeroName        string  `json:"heroName"`
	HeroSlug        string  `json:"heroSlug"`
	StartTime       int64   `json:"startTime"`
	DurationSeconds int     `json:"durationSeconds"`
	Won             bool    `json:"won"`
	Radiant         bool    `json:"radiant"`
	Kills           int     `json:"kills"`
	Deaths          int     `json:"deaths"`
	Assists         int     `json:"assists"`
	KDA             float64 `json:"kda"`
	LastHits        int     `json:"lastHits"`
	Denies          int     `json:"denies"`
	GoldPerMin      int     `json:"goldPerMin"`
	XpPerMin        int     `json:"xpPerMin"`
	HeroDamage      int64   `json:"heroDamage"`
	TowerDamage     int64   `json:"towerDamage"`
	HeroHealing     int64   `json:"heroHealing"`
	GameType        string  `json:"gameType"`
	ModeKey         string  `json:"modeKey"`
	ModeName        string  `json:"modeName"`
	LobbyName       string  `json:"lobbyName"`
	Ranked          bool    `json:"ranked"`
	PartySize       *int    `json:"partySize"`
	Abandoned       bool    `json:"abandoned"`
}

func kda(k, d, a int) float64 {
	// A deathless game would divide by zero; the usual convention is to
	// count it as one death.
	return float64(k+a) / float64(max(d, 1))
}

func parseDotaMatch(m jsonMap, heroes map[int]DotaHero) (DotaMatch, bool) {
	id := jU64(m, "match_id")
	if id == 0 {
		return DotaMatch{}, false
	}
	heroID := int(jI64(m, "hero_id"))
	hero, known := heroes[heroID]
	name := hero.Name
	if !known {
		name = fmt.Sprintf("Hero %d", heroID)
	}
	// Slots 0–4 are Radiant, 128–132 Dire; the winner arrives as a single
	// radiant_win flag, so the player's side decides the result.
	radiant := jI64(m, "player_slot") < 128
	mode, lobby := int(jI64(m, "game_mode")), int(jI64(m, "lobby_type"))
	k, de, a := int(jI64(m, "kills")), int(jI64(m, "deaths")), int(jI64(m, "assists"))

	out := DotaMatch{
		MatchID: id, HeroID: heroID, HeroName: name, HeroSlug: hero.Slug,
		StartTime: jI64(m, "start_time"), DurationSeconds: int(jI64(m, "duration")),
		Won: radiant == jBool(m, "radiant_win"), Radiant: radiant,
		Kills: k, Deaths: de, Assists: a, KDA: kda(k, de, a),
		LastHits: int(jI64(m, "last_hits")), Denies: int(jI64(m, "denies")),
		GoldPerMin: int(jI64(m, "gold_per_min")), XpPerMin: int(jI64(m, "xp_per_min")),
		HeroDamage: jI64(m, "hero_damage"), TowerDamage: jI64(m, "tower_damage"), HeroHealing: jI64(m, "hero_healing"),
		GameType: Classify(mode, lobby), ModeKey: GameModeKey(mode), ModeName: GameModeName(mode),
		LobbyName: LobbyName(lobby), Ranked: IsRankedLobby(lobby),
		Abandoned: jI64(m, "leaver_status") > 1,
	}
	if jHas(m, "party_size") {
		out.PartySize = ptr(int(jI64(m, "party_size")))
	}
	return out, true
}

type DotaSummary struct {
	Matches     int     `json:"matches"`
	Wins        int     `json:"wins"`
	Losses      int     `json:"losses"`
	WinRate     float64 `json:"winRate"`
	Kills       int     `json:"kills"`
	Deaths      int     `json:"deaths"`
	Assists     int     `json:"assists"`
	KDA         float64 `json:"kda"`
	AvgGpm      int     `json:"avgGpm"`
	AvgXpm      int     `json:"avgXpm"`
	AvgLastHits int     `json:"avgLastHits"`
}

func SummarizeDota(matches []DotaMatch) DotaSummary {
	s := DotaSummary{Matches: len(matches)}
	var gpm, xpm, lh int
	for _, m := range matches {
		if m.Won {
			s.Wins++
		}
		s.Kills += m.Kills
		s.Deaths += m.Deaths
		s.Assists += m.Assists
		gpm += m.GoldPerMin
		xpm += m.XpPerMin
		lh += m.LastHits
	}
	s.Losses = s.Matches - s.Wins
	n := max(s.Matches, 1)
	s.WinRate = pct(float64(s.Wins), float64(s.Matches))
	s.KDA = kda(s.Kills, s.Deaths, s.Assists)
	s.AvgGpm, s.AvgXpm, s.AvgLastHits = gpm/n, xpm/n, lh/n
	return s
}

type DotaHistory struct {
	Matches []DotaMatch `json:"matches"`
	Summary DotaSummary `json:"summary"`
	Freshness
}

// every field the UI renders is requested explicitly: asking OpenDota for any
// projection drops the rest, and an unrequested column reads as zero.
var historyFields = []string{
	"hero_id", "start_time", "duration", "kills", "deaths", "assists", "gold_per_min", "xp_per_min",
	"last_hits", "denies", "hero_damage", "tower_damage", "hero_healing", "party_size",
	"leaver_status", "game_mode", "lobby_type", "player_slot", "radiant_win",
}

func clampLimit(limit, def, hi int) int {
	if limit <= 0 {
		return def
	}
	return min(limit, hi)
}

// History is the linked account's recent matches.
//
// significant=0 matters. OpenDota hides matches it considers "not
// significant" by default, and that includes every Turbo game — which is why
// an account that mostly plays Turbo used to look almost empty here.
func (d *Dota) History(limit int, force bool) (DotaHistory, error) {
	account, err := d.account()
	if err != nil {
		return DotaHistory{}, err
	}
	return d.historyFor(account, limit, force)
}

// historyFor is any public account's recent matches.
func (d *Dota) historyFor(account uint64, limit int, force bool) (DotaHistory, error) {
	limit = clampLimit(limit, 100, 500)
	key := fmt.Sprintf("od_matches_%d_%d", account, limit)
	matches, fresh, err := cachedFetch(d.store, key, 2*time.Minute, force, func() ([]DotaMatch, error) {
		q := url.Values{"limit": {strconv.Itoa(limit)}, "significant": {"0"}, "project": historyFields}
		var rows []jsonMap
		if err := d.api.get(fmt.Sprintf("/players/%d/matches?%s", account, q.Encode()), &rows); err != nil {
			return nil, err
		}
		heroes := d.Heroes()
		out := make([]DotaMatch, 0, len(rows))
		for _, r := range rows {
			if m, ok := parseDotaMatch(r, heroes); ok {
				out = append(out, m)
			}
		}
		return out, nil
	})
	if err != nil {
		return DotaHistory{}, err
	}
	if matches == nil {
		matches = []DotaMatch{}
	}
	return DotaHistory{Matches: matches, Summary: SummarizeDota(matches), Freshness: fresh}, nil
}

// ---------- Single match ----------

type ScoreboardPlayer struct {
	AccountID  *uint64 `json:"accountId"`
	Name       string  `json:"name"`
	HeroName   string  `json:"heroName"`
	HeroSlug   string  `json:"heroSlug"`
	Radiant    bool    `json:"radiant"`
	Kills      int     `json:"kills"`
	Deaths     int     `json:"deaths"`
	Assists    int     `json:"assists"`
	LastHits   int     `json:"lastHits"`
	Denies     int     `json:"denies"`
	GoldPerMin int     `json:"goldPerMin"`
	XpPerMin   int     `json:"xpPerMin"`
	HeroDamage int64   `json:"heroDamage"`
	NetWorth   int64   `json:"netWorth"`
	Level      int     `json:"level"`
	IsMe       bool    `json:"isMe"`
	// Internal item names, for icons.
	Items []string `json:"items"`
}

type DotaMatchDetail struct {
	MatchID         uint64             `json:"matchId"`
	DurationSeconds int                `json:"durationSeconds"`
	StartTime       int64              `json:"startTime"`
	RadiantWin      bool               `json:"radiantWin"`
	RadiantScore    int                `json:"radiantScore"`
	DireScore       int                `json:"direScore"`
	ModeName        string             `json:"modeName"`
	LobbyName       string             `json:"lobbyName"`
	GameType        string             `json:"gameType"`
	Players         []ScoreboardPlayer `json:"players"`
}

// Detail is the ten-player scoreboard for one match. A finished match never
// changes, so it is cached for a month.
func (d *Dota) Detail(matchID uint64) (DotaMatchDetail, error) {
	me := d.store.LoadLink("dota").AccountID
	detail, _, err := cachedFetch(d.store, fmt.Sprintf("od_match_%d", matchID), 30*24*time.Hour, false, func() (DotaMatchDetail, error) {
		var m jsonMap
		if err := d.api.get(fmt.Sprintf("/matches/%d", matchID), &m); err != nil {
			return DotaMatchDetail{}, err
		}
		heroes := d.Heroes()
		items := d.itemNames()
		mode, lobby := int(jI64(m, "game_mode")), int(jI64(m, "lobby_type"))
		out := DotaMatchDetail{
			MatchID: matchID, DurationSeconds: int(jI64(m, "duration")), StartTime: jI64(m, "start_time"),
			RadiantWin: jBool(m, "radiant_win"), RadiantScore: int(jI64(m, "radiant_score")), DireScore: int(jI64(m, "dire_score")),
			ModeName: GameModeName(mode), LobbyName: LobbyName(lobby), GameType: Classify(mode, lobby),
			Players: []ScoreboardPlayer{},
		}
		for _, p := range jList(m["players"]) {
			heroID := int(jI64(p, "hero_id"))
			hero, known := heroes[heroID]
			name := hero.Name
			if !known {
				name = fmt.Sprintf("Hero %d", heroID)
			}
			sp := ScoreboardPlayer{
				// Anonymous profiles legitimately have no name attached.
				Name: jStr(p, "personaname", "Anonymous"), HeroName: name, HeroSlug: hero.Slug,
				Radiant: jI64(p, "player_slot") < 128,
				Kills:   int(jI64(p, "kills")), Deaths: int(jI64(p, "deaths")), Assists: int(jI64(p, "assists")),
				LastHits: int(jI64(p, "last_hits")), Denies: int(jI64(p, "denies")),
				GoldPerMin: int(jI64(p, "gold_per_min")), XpPerMin: int(jI64(p, "xp_per_min")),
				HeroDamage: jI64(p, "hero_damage"), NetWorth: jI64(p, "net_worth"), Level: int(jI64(p, "level")),
				Items: []string{},
			}
			if id := jU64(p, "account_id"); id != 0 {
				sp.AccountID = &id
			}
			for i := 0; i < 6; i++ {
				if id := jI64(p, fmt.Sprintf("item_%d", i)); id > 0 {
					if it, ok := items[int(id)]; ok {
						sp.Items = append(sp.Items, it.Key)
					}
				}
			}
			out.Players = append(out.Players, sp)
		}
		if len(out.Players) == 0 {
			return out, &apiError{"OpenDota hasn't finished processing that match yet.", false}
		}
		return out, nil
	})
	if err != nil {
		return detail, err
	}
	// "Me" depends on which account is linked now, not when it was cached.
	for i := range detail.Players {
		p := &detail.Players[i]
		p.IsMe = me != nil && p.AccountID != nil && *p.AccountID == *me
	}
	return detail, nil
}

// ---------- Search ----------

type SteamProfile struct {
	AccountID   uint64  `json:"accountId"`
	Personaname string  `json:"personaname"`
	Avatar      *string `json:"avatar"`
}

func (d *Dota) Search(query string) ([]SteamProfile, error) {
	var rows []jsonMap
	if err := d.api.get("/search?q="+url.QueryEscape(query), &rows); err != nil {
		return nil, err
	}
	out := []SteamProfile{}
	for _, p := range rows {
		id := jU64(p, "account_id")
		if id == 0 {
			continue
		}
		avatar := jStrPtr(p, "avatarfull")
		if avatar == nil {
			avatar = jStrPtr(p, "avatar")
		}
		out = append(out, SteamProfile{AccountID: id, Personaname: jStr(p, "personaname", "(no name)"), Avatar: avatar})
		if len(out) == 20 {
			break
		}
	}
	return out, nil
}

// ---------- Player profile ----------

var rankTiers = []string{"", "Herald", "Guardian", "Crusader", "Archon", "Legend", "Ancient", "Divine", "Immortal"}

// RankLabel decodes OpenDota's rank_tier: tens digit is the medal, units the
// stars. 54 is Legend 4.
func RankLabel(tier int) string {
	medal, stars := tier/10, tier%10
	if medal < 1 || medal >= len(rankTiers) {
		return ""
	}
	if medal == 8 || stars == 0 {
		return rankTiers[medal]
	}
	return fmt.Sprintf("%s %d", rankTiers[medal], stars)
}

type DotaPeer struct {
	AccountID   uint64  `json:"accountId"`
	Personaname string  `json:"personaname"`
	Avatar      *string `json:"avatar"`
	Games       int     `json:"games"`
	Wins        int     `json:"wins"`
	WinRate     float64 `json:"winRate"`
	LastPlayed  int64   `json:"lastPlayed"`
}

type DotaLifetimeHero struct {
	HeroID     int     `json:"heroId"`
	HeroName   string  `json:"heroName"`
	HeroSlug   string  `json:"heroSlug"`
	Games      int     `json:"games"`
	Wins       int     `json:"wins"`
	WinRate    float64 `json:"winRate"`
	LastPlayed int64   `json:"lastPlayed"`
}

type DotaPlayer struct {
	AccountID   uint64  `json:"accountId"`
	Personaname string  `json:"personaname"`
	Avatar      *string `json:"avatar"`
	// Medal name, e.g. "Legend 4". Empty when the account is uncalibrated or
	// its rank is hidden.
	Rank            string `json:"rank"`
	RankTier        int    `json:"rankTier"`
	LeaderboardRank int    `json:"leaderboardRank"`
	// Lifetime record across every mode OpenDota holds.
	Wins    int                `json:"wins"`
	Losses  int                `json:"losses"`
	WinRate float64            `json:"winRate"`
	Peers   []DotaPeer         `json:"peers"`
	Heroes  []DotaLifetimeHero `json:"heroes"`
	Freshness
}

// Player is the linked account's lifetime picture: medal, total record,
// every hero's record and the people most played with.
func (d *Dota) Player(force bool) (DotaPlayer, error) {
	account, err := d.account()
	if err != nil {
		return DotaPlayer{}, err
	}
	p, fresh, err := cachedFetch(d.store, fmt.Sprintf("od_player_%d", account), 30*time.Minute, force, func() (DotaPlayer, error) {
		out := DotaPlayer{AccountID: account, Peers: []DotaPeer{}, Heroes: []DotaLifetimeHero{}}

		var prof jsonMap
		if err := d.api.get(fmt.Sprintf("/players/%d", account), &prof); err != nil {
			return out, err
		}
		if inner := sub(prof, "profile"); inner != nil {
			out.Personaname = jStr(inner, "personaname", "")
			out.Avatar = jStrPtr(inner, "avatarfull")
		}
		out.RankTier = int(jI64(prof, "rank_tier"))
		out.Rank = RankLabel(out.RankTier)
		out.LeaderboardRank = int(jI64(prof, "leaderboard_rank"))

		// The rest is best-effort: a profile without its teammates is still
		// worth showing.
		var wl jsonMap
		if d.api.get(fmt.Sprintf("/players/%d/wl?significant=0", account), &wl) == nil {
			out.Wins, out.Losses = int(jI64(wl, "win")), int(jI64(wl, "lose"))
			out.WinRate = pct(float64(out.Wins), float64(out.Wins+out.Losses))
		}

		var peers []jsonMap
		if d.api.get(fmt.Sprintf("/players/%d/peers?significant=0", account), &peers) == nil {
			for _, r := range peers {
				games := int(jI64(r, "with_games"))
				if games < 3 {
					continue
				}
				wins := int(jI64(r, "with_win"))
				out.Peers = append(out.Peers, DotaPeer{
					AccountID: jU64(r, "account_id"), Personaname: jStr(r, "personaname", "Anonymous"),
					Avatar: jStrPtr(r, "avatarfull"), Games: games, Wins: wins,
					WinRate: pct(float64(wins), float64(games)), LastPlayed: jI64(r, "last_played"),
				})
			}
			sort.Slice(out.Peers, func(i, j int) bool { return out.Peers[i].Games > out.Peers[j].Games })
			if len(out.Peers) > 30 {
				out.Peers = out.Peers[:30]
			}
		}

		var heroRows []jsonMap
		if d.api.get(fmt.Sprintf("/players/%d/heroes?significant=0", account), &heroRows) == nil {
			heroes := d.Heroes()
			for _, r := range heroRows {
				games := int(jI64(r, "games"))
				if games == 0 {
					continue
				}
				// hero_id arrives as a number on this endpoint today but was
				// a string for years; accept both.
				id := int(jI64(r, "hero_id"))
				if s, ok := r["hero_id"].(string); ok {
					id, _ = strconv.Atoi(s)
				}
				h := heroes[id]
				name := h.Name
				if name == "" {
					name = fmt.Sprintf("Hero %d", id)
				}
				wins := int(jI64(r, "win"))
				out.Heroes = append(out.Heroes, DotaLifetimeHero{
					HeroID: id, HeroName: name, HeroSlug: h.Slug, Games: games, Wins: wins,
					WinRate: pct(float64(wins), float64(games)), LastPlayed: jI64(r, "last_played"),
				})
			}
			sort.Slice(out.Heroes, func(i, j int) bool { return out.Heroes[i].Games > out.Heroes[j].Games })
		}
		return out, nil
	})
	p.Freshness = fresh
	if p.Peers == nil {
		p.Peers = []DotaPeer{}
	}
	if p.Heroes == nil {
		p.Heroes = []DotaLifetimeHero{}
	}
	return p, err
}

// RequestRefresh asks OpenDota to re-scan the account's recent matches, for
// when a game just played has not shown up yet.
func (d *Dota) RequestRefresh() error {
	account, err := d.account()
	if err != nil {
		return err
	}
	return d.api.do("POST", fmt.Sprintf("/players/%d/refresh", account), nil, nil)
}

// ---------- Matchups ----------

type Matchup struct {
	HeroID   int     `json:"heroId"`
	HeroName string  `json:"heroName"`
	HeroSlug string  `json:"heroSlug"`
	Games    int     `json:"games"`
	Wins     int     `json:"wins"`
	WinRate  float64 `json:"winRate"`
}

// Matchups is how one hero has fared against every other, from OpenDota's
// matchup sample. WinRate is the chosen hero's win rate against that
// opponent: low means the opponent is a counter.
func (d *Dota) Matchups(heroID int) ([]Matchup, error) {
	list, _, err := cachedFetch(d.store, fmt.Sprintf("od_matchups_%d", heroID), 12*time.Hour, false, func() ([]Matchup, error) {
		var rows []jsonMap
		if err := d.api.get(fmt.Sprintf("/heroes/%d/matchups", heroID), &rows); err != nil {
			return nil, err
		}
		heroes := d.Heroes()
		out := []Matchup{}
		for _, r := range rows {
			id, games, wins := int(jI64(r, "hero_id")), int(jI64(r, "games_played")), int(jI64(r, "wins"))
			h, ok := heroes[id]
			if !ok || games == 0 {
				continue
			}
			out = append(out, Matchup{HeroID: id, HeroName: h.Name, HeroSlug: h.Slug, Games: games, Wins: wins, WinRate: pct(float64(wins), float64(games))})
		}
		return out, nil
	})
	if list == nil {
		list = []Matchup{}
	}
	return list, err
}

// ---------- Items and popular builds ----------

type dotaItem struct {
	Key  string `json:"key"`  // internal name, for icons
	Name string `json:"name"` // display name
	Cost int    `json:"cost"`
}

func (d *Dota) itemNames() map[int]dotaItem {
	m, _, _ := cachedFetch(d.store, "od_items", 24*time.Hour, false, func() (map[int]dotaItem, error) {
		var raw map[string]jsonMap
		if err := d.api.get("/constants/items", &raw); err != nil {
			return nil, err
		}
		out := map[int]dotaItem{}
		for key, v := range raw {
			if id := int(jI64(v, "id")); id > 0 {
				out[id] = dotaItem{Key: key, Name: jStr(v, "dname", key), Cost: int(jI64(v, "cost"))}
			}
		}
		if len(out) == 0 {
			return nil, &apiError{"OpenDota returned no items.", true}
		}
		return out, nil
	})
	return m
}

type CatalogItem struct {
	Key  string `json:"key"`
	Name string `json:"name"`
	Cost int    `json:"cost"`
}

// ItemCatalog is every purchasable item, for the build editor's search.
func (d *Dota) ItemCatalog() []CatalogItem {
	out := []CatalogItem{}
	for _, it := range d.itemNames() {
		// Recipes and zero-cost entries are not things anyone plans a build
		// around.
		if it.Cost <= 0 || len(it.Key) > 7 && it.Key[:7] == "recipe_" {
			continue
		}
		out = append(out, CatalogItem(it))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

type PopularItem struct {
	Key   string `json:"key"`
	Name  string `json:"name"`
	Count int    `json:"count"`
}

type PopularBuild struct {
	Phase string        `json:"phase"`
	Items []PopularItem `json:"items"`
}

// PopularBuilds is what players most often buy on a hero, by stage of the
// game, from OpenDota's item popularity data.
func (d *Dota) PopularBuilds(heroID int) ([]PopularBuild, error) {
	builds, _, err := cachedFetch(d.store, fmt.Sprintf("od_popular_%d", heroID), 12*time.Hour, false, func() ([]PopularBuild, error) {
		var raw map[string]map[string]any
		if err := d.api.get(fmt.Sprintf("/heroes/%d/itemPopularity", heroID), &raw); err != nil {
			return nil, err
		}
		names := d.itemNames()
		phases := [][2]string{
			{"start_game_items", "Starting"}, {"early_game_items", "Early game"},
			{"mid_game_items", "Mid game"}, {"late_game_items", "Late game"},
		}
		out := []PopularBuild{}
		for _, ph := range phases {
			items := []PopularItem{}
			for idStr, count := range raw[ph[0]] {
				id, _ := strconv.Atoi(idStr)
				n, _ := count.(float64)
				if it, ok := names[id]; ok && n > 0 {
					items = append(items, PopularItem{Key: it.Key, Name: it.Name, Count: int(n)})
				}
			}
			sort.Slice(items, func(i, j int) bool {
				if items[i].Count != items[j].Count {
					return items[i].Count > items[j].Count
				}
				return items[i].Name < items[j].Name
			})
			if len(items) > 6 {
				items = items[:6]
			}
			if len(items) > 0 {
				out = append(out, PopularBuild{Phase: ph[1], Items: items})
			}
		}
		return out, nil
	})
	if builds == nil {
		builds = []PopularBuild{}
	}
	return builds, err
}

// ---------- Automatic game-type tagging ----------

// How long a locally tracked match keeps being looked up. OpenDota ingests a
// finished game within minutes to hours; one it still lacks after two days
// is not coming, and asking forever is a request every five minutes for
// nothing.
const backfillWindow = 48 * time.Hour

func backfillCandidates(history []MatchSummary, now time.Time) []string {
	var out []string
	for _, m := range history {
		if m.GameType != "unspecified" {
			continue
		}
		at, err := time.Parse(time.RFC3339, m.Date)
		if err != nil || now.Sub(at) >= backfillWindow {
			continue
		}
		out = append(out, m.MatchID)
	}
	return out
}

// BackfillGameTypes fills in the game type of locally tracked matches still
// marked "unspecified" — GSI never says which mode a match is — and returns
// the matches it resolved. A type the player set by hand is never touched.
func (d *Dota) BackfillGameTypes() ([]MatchSummary, error) {
	account, err := d.account()
	if err != nil {
		return nil, nil
	}
	candidates := backfillCandidates(d.store.LoadHistory(), time.Now())
	if len(candidates) == 0 {
		return nil, nil
	}

	var rows []jsonMap
	path := fmt.Sprintf("/players/%d/matches?limit=50&significant=0&project=game_mode&project=lobby_type", account)
	if err := d.api.get(path, &rows); err != nil {
		return nil, err
	}
	types := map[string]string{}
	for _, r := range rows {
		if id := jU64(r, "match_id"); id != 0 {
			types[strconv.FormatUint(id, 10)] = Classify(int(jI64(r, "game_mode")), int(jI64(r, "lobby_type")))
		}
	}

	want := map[string]bool{}
	for _, c := range candidates {
		want[c] = true
	}
	var resolved []MatchSummary
	_, err = d.store.UpdateHistory(func(h []MatchSummary) ([]MatchSummary, bool) {
		for i := range h {
			// Re-checked under the lock: the player may have tagged it by
			// hand since the candidates were listed.
			if t, ok := types[h[i].MatchID]; ok && want[h[i].MatchID] && h[i].GameType == "unspecified" {
				h[i].GameType = t
				resolved = append(resolved, h[i])
			}
		}
		if len(resolved) == 0 {
			return h, false
		}
		RecomputeComparisons(h)
		return h, true
	})
	return resolved, err
}
