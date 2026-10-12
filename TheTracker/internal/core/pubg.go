package core

// PUBG: Battlegrounds, from PUBG's official API (developer.pubg.com). PUBG
// requires the API key to stay on a server, so every read goes through the
// cloud service's pubg:get action, which holds the key and caches what the
// free key's 10-requests-a-minute limit can't afford to ask twice.
//
// The API keeps a player's matches for 14 days; each match lists every
// player's stats and team placement. Season and ranked totals come per
// mode (solo, duo, squad, first- or third-person).

import (
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
)

type Pubg struct {
	store *Store
	cloud *Cloud
}

func NewPubg(store *Store, cloud *Cloud) *Pubg { return &Pubg{store: store, cloud: cloud} }

// PubgLink is the PUBG account the pages read.
type PubgLink struct {
	AccountID string `json:"accountId"`
	Name      string `json:"name"`
	Shard     string `json:"shard"`
}

func (p *Pubg) Link() PubgLink {
	var l PubgLink
	p.store.readJSON("pubg.json", &l)
	return l
}

func (p *Pubg) SetLink(l PubgLink) error { return p.store.writeJSON("pubg.json", l) }

var pubgShards = map[string]string{"steam": "Steam", "kakao": "Kakao", "psn": "PlayStation", "xbox": "Xbox", "console": "Console"}

var pubgAccount = regexp.MustCompile(`^account\.[0-9a-f]{32}$`)

// errPubgNotSetUp: the cloud service has no PUBG key yet.
var errPubgNotSetUp = errors.New("PUBG isn't set up on the cloud service yet.")

// get reads one path of PUBG's API through the cloud service.
func (p *Pubg) get(path string) (jsonMap, error) {
	v, err := p.cloud.call("action", "pubg:get", map[string]any{"path": path}, "")
	if err != nil {
		if strings.Contains(err.Error(), "Could not find public function") || strings.Contains(err.Error(), "Server Error") {
			return nil, errPubgNotSetUp
		}
		return nil, err
	}
	m, _ := v.(map[string]any)
	if !jBool(m, "ok") {
		switch reason := jStr(m, "reason", ""); reason {
		case "not_set_up":
			return nil, errPubgNotSetUp
		case "not_found":
			return nil, errors.New("PUBG doesn't know that player on that platform. Names are case-sensitive.")
		case "rate_limited":
			return nil, errors.New("PUBG's API is busy for a minute (10 requests a minute on a free key). Try again shortly.")
		case "bad_key":
			return nil, errors.New("PUBG refused the cloud service's API key.")
		default:
			return nil, fmt.Errorf("PUBG's API answered: %s", reason)
		}
	}
	data, _ := m["data"].(map[string]any)
	return data, nil
}

// Search finds an account by its exact in-game name.
func (p *Pubg) Search(name, shard string) ([]PubgLink, error) {
	name = strings.TrimSpace(name)
	if name == "" || len(name) > 64 {
		return nil, errors.New("Type your PUBG name exactly as it shows in the game.")
	}
	if _, ok := pubgShards[shard]; !ok {
		shard = "steam"
	}
	m, err := p.get(fmt.Sprintf("/shards/%s/players?filter[playerNames]=%s", shard, url.QueryEscape(name)))
	if err != nil {
		return nil, err
	}
	out := []PubgLink{}
	for _, d := range jList(m["data"]) {
		id := jStr(d, "id", "")
		if pubgAccount.MatchString(id) {
			out = append(out, PubgLink{AccountID: id, Name: jStr(sub(d, "attributes"), "name", name), Shard: shard})
		}
	}
	return out, nil
}

// ---------- Matches ----------

type PubgMate struct {
	Name     string  `json:"name"`
	Me       bool    `json:"me,omitempty"`
	Kills    int     `json:"kills"`
	Assists  int     `json:"assists"`
	Damage   float64 `json:"damage"`
	Knocks   int     `json:"knocks"`
	Revives  int     `json:"revives"`
	Survived float64 `json:"survived"`
}

type PubgMatch struct {
	ID        string     `json:"id"`
	StartTime int64      `json:"startTime"`
	Duration  int        `json:"duration"`
	Mode      string     `json:"mode"`
	ModeName  string     `json:"modeName"`
	Map       string     `json:"map"`
	Ranked    bool       `json:"ranked"`
	Custom    bool       `json:"custom"`
	Place     int        `json:"place"`
	Teams     int        `json:"teams"`
	Kills     int        `json:"kills"`
	Assists   int        `json:"assists"`
	Damage    float64    `json:"damage"`
	Knocks    int        `json:"knocks"`
	Headshots int        `json:"headshots"`
	Longest   float64    `json:"longest"`
	Survived  float64    `json:"survived"`
	Walked    float64    `json:"walked"`
	Rode      float64    `json:"rode"`
	Revives   int        `json:"revives"`
	Heals     int        `json:"heals"`
	Boosts    int        `json:"boosts"`
	KillPlace int        `json:"killPlace"`
	DeathType string     `json:"deathType"`
	Team      []PubgMate `json:"team"`
}

var pubgMaps = map[string]string{
	"Baltic_Main": "Erangel", "Erangel_Main": "Erangel", "Desert_Main": "Miramar", "Savage_Main": "Sanhok",
	"DihorOtok_Main": "Vikendi", "Summerland_Main": "Karakin", "Chimera_Main": "Paramo", "Heaven_Main": "Haven",
	"Tiger_Main": "Taego", "Kiki_Main": "Deston", "Neon_Main": "Rondo", "Range_Main": "Camp Jackal",
}

// pubgModeName says a mode the way the game does: "squad-fpp" is Squad FPP.
func pubgModeName(mode string) string {
	switch mode {
	case "tdm":
		return "Team Deathmatch"
	case "ibr":
		return "Intense Battle Royale"
	}
	fpp := strings.HasSuffix(mode, "-fpp")
	base := strings.TrimSuffix(mode, "-fpp")
	if base == "" {
		return mode
	}
	name := strings.ToUpper(base[:1]) + base[1:]
	if fpp {
		return name + " FPP"
	}
	return name + " TPP"
}

// pubgMatchFrom reads one match for one account.
func pubgMatchFrom(m jsonMap, accountID string) (PubgMatch, bool) {
	d := sub(m, "data")
	attrs := sub(d, "attributes")
	out := PubgMatch{ID: jStr(d, "id", ""), Duration: int(jI64(attrs, "duration")), Mode: jStr(attrs, "gameMode", ""),
		Ranked: jStr(attrs, "matchType", "") == "competitive", Custom: jBool(attrs, "isCustomMatch")}
	out.ModeName = pubgModeName(out.Mode)
	if mapName := jStr(attrs, "mapName", ""); pubgMaps[mapName] != "" {
		out.Map = pubgMaps[mapName]
	} else {
		out.Map = strings.TrimSuffix(mapName, "_Main")
	}
	if t, err := time.Parse(time.RFC3339, jStr(attrs, "createdAt", "")); err == nil {
		out.StartTime = t.Unix()
	}
	participants := map[string]jsonMap{}
	var mine string
	for _, inc := range jList(m["included"]) {
		if jStr(inc, "type", "") != "participant" {
			continue
		}
		stats := sub(sub(inc, "attributes"), "stats")
		participants[jStr(inc, "id", "")] = stats
		if jStr(stats, "playerId", "") == accountID {
			mine = jStr(inc, "id", "")
		}
	}
	if mine == "" {
		return out, false
	}
	for _, inc := range jList(m["included"]) {
		if jStr(inc, "type", "") != "roster" {
			continue
		}
		out.Teams++
		ids := []string{}
		for _, ref := range jList(sub(sub(inc, "relationships"), "participants")["data"]) {
			ids = append(ids, jStr(ref, "id", ""))
		}
		in := false
		for _, id := range ids {
			if id == mine {
				in = true
			}
		}
		if !in {
			continue
		}
		for _, id := range ids {
			s := participants[id]
			out.Team = append(out.Team, PubgMate{Name: jStr(s, "name", ""), Me: id == mine, Kills: int(jI64(s, "kills")), Assists: int(jI64(s, "assists")),
				Damage: jF64(s, "damageDealt"), Knocks: int(jI64(s, "DBNOs")), Revives: int(jI64(s, "revives")), Survived: jF64(s, "timeSurvived")})
		}
	}
	s := participants[mine]
	out.Place, out.Kills, out.Assists = int(jI64(s, "winPlace")), int(jI64(s, "kills")), int(jI64(s, "assists"))
	out.Damage, out.Knocks, out.Headshots = jF64(s, "damageDealt"), int(jI64(s, "DBNOs")), int(jI64(s, "headshotKills"))
	out.Longest, out.Survived = jF64(s, "longestKill"), jF64(s, "timeSurvived")
	out.Walked, out.Rode = jF64(s, "walkDistance"), jF64(s, "rideDistance")
	out.Revives, out.Heals, out.Boosts = int(jI64(s, "revives")), int(jI64(s, "heals")), int(jI64(s, "boosts"))
	out.KillPlace, out.DeathType = int(jI64(s, "killPlace")), jStr(s, "deathType", "")
	sort.SliceStable(out.Team, func(i, j int) bool { return out.Team[i].Damage > out.Team[j].Damage })
	return out, true
}

// match reads one match; a match never changes, so it's kept for good.
func (p *Pubg) match(shard, id, accountID string) (PubgMatch, error) {
	key := "pubg_match_" + safeKey(id) + "_" + safeKey(accountID)
	var cached PubgMatch
	if _, ok := p.store.ReadCacheStale(key, &cached); ok && cached.ID == id {
		return cached, nil
	}
	m, err := p.get(fmt.Sprintf("/shards/%s/matches/%s", shard, id))
	if err != nil {
		return PubgMatch{}, err
	}
	out, ok := pubgMatchFrom(m, accountID)
	if !ok {
		return PubgMatch{}, errors.New("You aren't in that match.")
	}
	p.store.WriteCache(key, out)
	return out, nil
}

// ---------- Season and ranked totals ----------

type PubgModeStats struct {
	Mode      string  `json:"mode"`
	ModeName  string  `json:"modeName"`
	Rounds    int     `json:"rounds"`
	Wins      int     `json:"wins"`
	Top10s    int     `json:"top10s"`
	Kills     int     `json:"kills"`
	Assists   int     `json:"assists"`
	Damage    float64 `json:"damage"`
	Headshots int     `json:"headshots"`
	Knocks    int     `json:"knocks"`
	Longest   float64 `json:"longest"`
	Survived  float64 `json:"survived"`
	MostKills int     `json:"mostKills"`
	Revives   int     `json:"revives"`
}

type PubgRanked struct {
	Mode      string  `json:"mode"`
	ModeName  string  `json:"modeName"`
	Tier      string  `json:"tier"`
	SubTier   string  `json:"subTier"`
	Points    int     `json:"points"`
	BestTier  string  `json:"bestTier"`
	BestSub   string  `json:"bestSubTier"`
	Best      int     `json:"bestPoints"`
	Rounds    int     `json:"rounds"`
	Wins      int     `json:"wins"`
	AvgRank   float64 `json:"avgRank"`
	Top10     float64 `json:"top10Ratio"`
	KDA       float64 `json:"kda"`
	Damage    float64 `json:"damage"`
	Kills     int     `json:"kills"`
	Deaths    int     `json:"deaths"`
	Headshots int     `json:"headshots"`
}

type PubgOverview struct {
	Link    PubgLink        `json:"link"`
	Season  string          `json:"season"`
	Modes   []PubgModeStats `json:"modes"`
	Ranked  []PubgRanked    `json:"ranked"`
	Matches []PubgMatch     `json:"matches"`
	// Matches the API listed that couldn't be read (yet).
	Missing   int       `json:"missing"`
	Freshness Freshness `json:"freshness"`
}

var pubgModeOrder = []string{"squad-fpp", "squad", "duo-fpp", "duo", "solo-fpp", "solo"}

func (p *Pubg) currentSeason(shard string) (string, error) {
	id, _, err := cachedFetch(p.store, "pubg_season_"+shard, 24*time.Hour, false, func() (string, error) {
		m, err := p.get(fmt.Sprintf("/shards/%s/seasons", shard))
		if err != nil {
			return "", err
		}
		for _, s := range jList(m["data"]) {
			if jBool(sub(s, "attributes"), "isCurrentSeason") {
				return jStr(s, "id", ""), nil
			}
		}
		return "", errors.New("PUBG listed no current season.")
	})
	return id, err
}

// Overview is the linked account's season and ranked totals and its last
// 14 days of matches.
func (p *Pubg) Overview(force bool) (PubgOverview, error) {
	link := p.Link()
	if link.AccountID == "" {
		return PubgOverview{}, errors.New("No PUBG account connected yet.")
	}
	ov, fresh, err := cachedFetch(p.store, "pubg_overview_"+safeKey(link.AccountID), 5*time.Minute, force, func() (PubgOverview, error) {
		out := PubgOverview{Link: link, Modes: []PubgModeStats{}, Ranked: []PubgRanked{}, Matches: []PubgMatch{}}
		player, err := p.get(fmt.Sprintf("/shards/%s/players/%s", link.Shard, link.AccountID))
		if err != nil {
			return out, err
		}
		var ids []string
		for _, ref := range jList(sub(sub(sub(player, "data"), "relationships"), "matches")["data"]) {
			if id := jStr(ref, "id", ""); id != "" {
				ids = append(ids, id)
			}
		}
		if season, err := p.currentSeason(link.Shard); err == nil {
			out.Season = season
			if m, err := p.get(fmt.Sprintf("/shards/%s/players/%s/seasons/%s", link.Shard, link.AccountID, season)); err == nil {
				out.Modes = pubgModesFrom(sub(sub(sub(m, "data"), "attributes"), "gameModeStats"))
			}
			if m, err := p.get(fmt.Sprintf("/shards/%s/players/%s/seasons/%s/ranked", link.Shard, link.AccountID, season)); err == nil {
				out.Ranked = pubgRankedFrom(sub(sub(sub(m, "data"), "attributes"), "rankedGameModeStats"))
			}
		}
		out.Matches, out.Missing = p.matches(link, ids, 40)
		return out, nil
	})
	ov.Freshness = fresh
	return ov, err
}

// matches reads up to limit of the listed matches, four at a time, newest
// first.
func (p *Pubg) matches(link PubgLink, ids []string, limit int) ([]PubgMatch, int) {
	if len(ids) > limit {
		ids = ids[:limit]
	}
	got := make([]*PubgMatch, len(ids))
	var wg sync.WaitGroup
	sem := make(chan struct{}, 4)
	for i, id := range ids {
		wg.Add(1)
		go func(i int, id string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			if m, err := p.match(link.Shard, id, link.AccountID); err == nil {
				got[i] = &m
			}
		}(i, id)
	}
	wg.Wait()
	out, missing := []PubgMatch{}, 0
	for _, m := range got {
		if m == nil {
			missing++
			continue
		}
		out = append(out, *m)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].StartTime > out[j].StartTime })
	return out, missing
}

func pubgModesFrom(stats jsonMap) []PubgModeStats {
	out := []PubgModeStats{}
	for _, mode := range pubgModeOrder {
		s := sub(stats, mode)
		if s == nil || jI64(s, "roundsPlayed") == 0 {
			continue
		}
		out = append(out, PubgModeStats{Mode: mode, ModeName: pubgModeName(mode), Rounds: int(jI64(s, "roundsPlayed")), Wins: int(jI64(s, "wins")),
			Top10s: int(jI64(s, "top10s")), Kills: int(jI64(s, "kills")), Assists: int(jI64(s, "assists")), Damage: jF64(s, "damageDealt"),
			Headshots: int(jI64(s, "headshotKills")), Knocks: int(jI64(s, "dBNOs")), Longest: jF64(s, "longestKill"), Survived: jF64(s, "timeSurvived"),
			MostKills: int(jI64(s, "roundMostKills")), Revives: int(jI64(s, "revives"))})
	}
	return out
}

func pubgRankedFrom(stats jsonMap) []PubgRanked {
	out := []PubgRanked{}
	for _, mode := range pubgModeOrder {
		s := sub(stats, mode)
		if s == nil || jI64(s, "roundsPlayed") == 0 {
			continue
		}
		cur, best := sub(s, "currentTier"), sub(s, "bestTier")
		out = append(out, PubgRanked{Mode: mode, ModeName: pubgModeName(mode), Tier: jStr(cur, "tier", ""), SubTier: jStr(cur, "subTier", ""),
			Points: int(jI64(s, "currentRankPoint")), BestTier: jStr(best, "tier", ""), BestSub: jStr(best, "subTier", ""), Best: int(jI64(s, "bestRankPoint")),
			Rounds: int(jI64(s, "roundsPlayed")), Wins: int(jI64(s, "wins")), AvgRank: jF64(s, "avgRank"), Top10: jF64(s, "top10Ratio"),
			KDA: jF64(s, "kda"), Damage: jF64(s, "damageDealt"), Kills: int(jI64(s, "kills")), Deaths: int(jI64(s, "deaths")), Headshots: int(jI64(s, "headshotKills"))})
	}
	return out
}
