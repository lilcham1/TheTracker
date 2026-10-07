package core

import (
	"errors"
	"fmt"
	"net/url"
	"sort"
	"time"
)

// Overwatch 2, via the community-run OverFast API, which reads the career
// profiles Blizzard publishes on its own site.
//
// Blizzard has no official API and publishes no match history, so there is
// no list of games here and nothing live: only the career totals the game
// itself shows on a profile — per mode, per role and per hero. A profile
// that is set to private in the game shows nothing at all.

type Overwatch struct {
	store *Store
	api   *service
	now   func() time.Time
}

func NewOverwatch(store *Store) *Overwatch {
	return &Overwatch{store: store, now: time.Now, api: &service{Name: "The Overwatch API", Base: "https://overfast-api.tekrop.fr", Attempts: 2}}
}

// OwLink is the Battle.net profile the Overwatch pages read.
type OwLink struct {
	PlayerID string  `json:"playerId"`
	Name     string  `json:"name"`
	Avatar   *string `json:"avatar"`
}

func (o *Overwatch) Link() OwLink {
	var l OwLink
	o.store.readJSON("overwatch.json", &l)
	return l
}

func (o *Overwatch) SetLink(l OwLink) error { return o.store.writeJSON("overwatch.json", l) }

type OwProfile struct {
	PlayerID string  `json:"playerId"`
	Name     string  `json:"name"`
	Avatar   *string `json:"avatar"`
	Title    string  `json:"title"`
	Public   bool    `json:"public"`
}

// Search finds profiles by name, or by full BattleTag (Name#1234).
func (o *Overwatch) Search(query string) ([]OwProfile, error) {
	var v jsonMap
	if err := o.api.get("/players?limit=20&name="+url.QueryEscape(query), &v); err != nil {
		return nil, err
	}
	out := []OwProfile{}
	for _, p := range jList(v["results"]) {
		id := jStr(p, "player_id", "")
		if id == "" {
			continue
		}
		// The API returns the id already percent-encoded; keep the plain
		// form and encode it where it is used.
		if dec, err := url.PathUnescape(id); err == nil {
			id = dec
		}
		out = append(out, OwProfile{PlayerID: id, Name: jStr(p, "name", "(no name)"), Avatar: jStrPtr(p, "avatar"), Title: jStr(p, "title", ""), Public: jBool(p, "is_public")})
	}
	return out, nil
}

type OwStats struct {
	GamesPlayed int     `json:"gamesPlayed"`
	GamesWon    int     `json:"gamesWon"`
	GamesLost   int     `json:"gamesLost"`
	WinRate     float64 `json:"winRate"`
	KDA         float64 `json:"kda"`
	// Seconds.
	TimePlayed int64 `json:"timePlayed"`
	// Per ten minutes, as the game reports them.
	AvgEliminations float64 `json:"avgEliminations"`
	AvgDeaths       float64 `json:"avgDeaths"`
	AvgDamage       float64 `json:"avgDamage"`
	AvgHealing      float64 `json:"avgHealing"`
}

type OwNamedStats struct {
	Key      string  `json:"key"`
	Name     string  `json:"name"`
	Role     string  `json:"role"`
	Portrait *string `json:"portrait"`
	OwStats
}

type OwRank struct {
	Role     string  `json:"role"`
	Division string  `json:"division"`
	Tier     int     `json:"tier"`
	Icon     *string `json:"icon"`
}

type OwOverview struct {
	Name        string         `json:"name"`
	Avatar      *string        `json:"avatar"`
	Title       string         `json:"title"`
	Namecard    *string        `json:"namecard"`
	Endorsement int            `json:"endorsement"`
	Ranks       []OwRank       `json:"ranks"`
	Mode        string         `json:"mode"` // all | competitive | quickplay
	General     OwStats        `json:"general"`
	Roles       []OwNamedStats `json:"roles"`
	Heroes      []OwNamedStats `json:"heroes"`
	// Career bests and per-ten-minute averages across every hero.
	Records []OwStatGroup `json:"records"`
	Freshness
}

func owStats(m jsonMap) OwStats {
	avg := sub(m, "average")
	return OwStats{
		GamesPlayed: int(jI64(m, "games_played")), GamesWon: int(jI64(m, "games_won")), GamesLost: int(jI64(m, "games_lost")),
		WinRate: jF64(m, "winrate"), KDA: jF64(m, "kda"), TimePlayed: jI64(m, "time_played"),
		AvgEliminations: jF64(avg, "eliminations"), AvgDeaths: jF64(avg, "deaths"), AvgDamage: jF64(avg, "damage"), AvgHealing: jF64(avg, "healing"),
	}
}

type owHero struct {
	Key      string  `json:"key"`
	Name     string  `json:"name"`
	Role     string  `json:"role"`
	Portrait *string `json:"portrait"`
}

func (o *Overwatch) heroes() map[string]owHero {
	list, _, _ := cachedFetch(o.store, "ow_heroes", 24*time.Hour, false, func() ([]owHero, error) {
		var rows []jsonMap
		if err := o.api.get("/heroes?locale=en-us", &rows); err != nil {
			return nil, err
		}
		out := []owHero{}
		for _, h := range rows {
			out = append(out, owHero{Key: jStr(h, "key", ""), Name: jStr(h, "name", ""), Role: jStr(h, "role", ""), Portrait: jStrPtr(h, "portrait")})
		}
		if len(out) == 0 {
			return nil, &apiError{"The Overwatch API returned no heroes.", true}
		}
		return out, nil
	})
	m := map[string]owHero{}
	for _, h := range list {
		m[h.Key] = h
	}
	return m
}

var owRoleNames = map[string]string{"tank": "Tank", "damage": "Damage", "support": "Support", "open": "Open queue"}

// Overview is the linked profile's career: ranks, and totals overall, by
// role and by hero, for one game mode.
func (o *Overwatch) Overview(mode string, force bool) (OwOverview, error) {
	link := o.Link()
	if link.PlayerID == "" {
		return OwOverview{}, errors.New("No Overwatch profile connected yet.")
	}
	return o.overviewFor(link, mode, force, true)
}

// overviewFor reads any public profile. Only the player's own profile is
// remembered for Progress.
func (o *Overwatch) overviewFor(link OwLink, mode string, force, remember bool) (OwOverview, error) {
	if mode != "competitive" && mode != "quickplay" {
		mode = "all"
	}
	id := url.PathEscape(link.PlayerID)
	ov, fresh, err := cachedFetch(o.store, "ow_overview_"+safeKey(link.PlayerID)+"_"+mode, 10*time.Minute, force, func() (OwOverview, error) {
		out := OwOverview{Mode: mode, Ranks: []OwRank{}, Roles: []OwNamedStats{}, Heroes: []OwNamedStats{}, Records: []OwStatGroup{}}
		var summary jsonMap
		if err := o.api.get("/players/"+id+"/summary", &summary); err != nil {
			return out, err
		}
		out.Name, out.Avatar, out.Title = jStr(summary, "username", link.Name), jStrPtr(summary, "avatar"), jStr(summary, "title", "")
		out.Endorsement = int(jI64(sub(summary, "endorsement"), "level"))
		out.Namecard = jStrPtr(summary, "namecard")
		if pc := sub(sub(summary, "competitive"), "pc"); pc != nil {
			for _, role := range []string{"tank", "damage", "support", "open"} {
				if r := sub(pc, role); r != nil {
					out.Ranks = append(out.Ranks, OwRank{Role: owRoleNames[role], Division: jStr(r, "division", ""), Tier: int(jI64(r, "tier")), Icon: jStrPtr(r, "rank_icon")})
				}
			}
		}

		path := "/players/" + id + "/stats/summary"
		if mode != "all" {
			path += "?gamemode=" + mode
		}
		var stats jsonMap
		if err := o.api.get(path, &stats); err != nil {
			return out, err
		}
		if general := sub(stats, "general"); general != nil {
			out.General = owStats(general)
		}
		for _, role := range []string{"tank", "damage", "support"} {
			if r := sub(sub(stats, "roles"), role); r != nil {
				out.Roles = append(out.Roles, OwNamedStats{Key: role, Name: owRoleNames[role], Role: role, OwStats: owStats(r)})
			}
		}
		heroes := o.heroes()
		for key, raw := range sub(stats, "heroes") {
			h, _ := raw.(map[string]any)
			if h == nil {
				continue
			}
			info := heroes[key]
			name := info.Name
			if name == "" {
				name = key
			}
			out.Heroes = append(out.Heroes, OwNamedStats{Key: key, Name: name, Role: info.Role, Portrait: info.Portrait, OwStats: owStats(h)})
		}
		// Best effort: the overview is worth showing without its records.
		out.Records, _ = o.career(id, mode, "all-heroes")
		sort.Slice(out.Heroes, func(i, j int) bool {
			if out.Heroes[i].TimePlayed != out.Heroes[j].TimePlayed {
				return out.Heroes[i].TimePlayed > out.Heroes[j].TimePlayed
			}
			return out.Heroes[i].Name < out.Heroes[j].Name
		})
		if remember {
			o.remember(link.PlayerID, out)
		}
		return out, nil
	})
	ov.Freshness = fresh
	if err != nil {
		return ov, fmt.Errorf("%v If your career profile is private in Overwatch, set it to public under Options, Social.", err)
	}
	return ov, nil
}

// safeKey makes an id usable as a cache file name.
func safeKey(s string) string {
	out := []rune{}
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
			out = append(out, r)
		} else {
			out = append(out, '_')
		}
	}
	return string(out)
}
