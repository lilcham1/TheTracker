package core

import (
	"errors"
	"net/url"
	"sort"
	"strings"
	"time"
)

// Overwatch: the hero meta, and the detailed career stats behind a profile.

// ---------- Detailed career stats ----------

type OwStat struct {
	Key   string  `json:"key"`
	Label string  `json:"label"`
	Value float64 `json:"value"`
	// number | time (seconds) | percent
	Kind string `json:"kind"`
}

type OwStatGroup struct {
	Key   string   `json:"key"`
	Label string   `json:"label"`
	Stats []OwStat `json:"stats"`
}

var owGroupLabels = map[string]string{
	"best": "Best in one game", "average": "Per 10 minutes", "combat": "Combat", "assists": "Assists and healing",
	"game": "Games", "hero_specific": "Hero abilities", "match_awards": "Match awards",
}

// The order groups are shown in: what a player looks for first, first.
var owGroupOrder = []string{"best", "average", "hero_specific", "combat", "assists", "game", "match_awards"}

// owStatLabel turns the API's key into words: "eliminations_avg_per_10_min"
// reads "Eliminations", because the group heading already says per 10 min.
func owStatLabel(key string) string {
	k := key
	for _, suffix := range []string{"_avg_per_10_min", "_most_in_game"} {
		k = strings.TrimSuffix(k, suffix)
	}
	// "Most in one life" sits in the same group as "most in one game", so it
	// keeps a qualifier or the two rows would read the same.
	note := ""
	if cut, ok := strings.CutSuffix(k, "_most_in_life"); ok {
		k, note = cut, ", in one life"
	}
	k = strings.ReplaceAll(k, "_", " ")
	if k == "" {
		return key
	}
	return strings.ToUpper(k[:1]) + k[1:] + note
}

func owStatKind(key string) string {
	switch {
	case strings.Contains(key, "percentage") || strings.Contains(key, "accuracy") || strings.HasSuffix(key, "_pct"):
		return "percent"
	case strings.Contains(key, "time"):
		return "time"
	}
	return "number"
}

// career reads one hero's (or "all-heroes") detailed stats for a mode.
func (o *Overwatch) career(escapedID, mode, hero string) ([]OwStatGroup, error) {
	// The career endpoint has no "all modes": quick play is the fuller
	// record for most players.
	if mode != "competitive" {
		mode = "quickplay"
	}
	var v map[string]map[string]map[string]any
	if err := o.api.get("/players/"+escapedID+"/stats/career?gamemode="+mode+"&hero="+url.QueryEscape(hero), &v); err != nil {
		return []OwStatGroup{}, err
	}
	groups := []OwStatGroup{}
	raw := v[hero]
	for _, gk := range owGroupOrder {
		stats := raw[gk]
		if len(stats) == 0 {
			continue
		}
		g := OwStatGroup{Key: gk, Label: owGroupLabels[gk], Stats: []OwStat{}}
		for key, val := range stats {
			n, ok := val.(float64)
			if !ok {
				continue
			}
			g.Stats = append(g.Stats, OwStat{Key: key, Label: owStatLabel(key), Value: n, Kind: owStatKind(key)})
		}
		sort.Slice(g.Stats, func(i, j int) bool { return g.Stats[i].Label < g.Stats[j].Label })
		if len(g.Stats) > 0 {
			groups = append(groups, g)
		}
	}
	return groups, nil
}

type OwHeroCareer struct {
	Key      string        `json:"key"`
	Name     string        `json:"name"`
	Role     string        `json:"role"`
	Portrait *string       `json:"portrait"`
	Mode     string        `json:"mode"`
	Groups   []OwStatGroup `json:"groups"`
}

// HeroCareer is everything the profile records about one hero.
func (o *Overwatch) HeroCareer(hero, mode string) (OwHeroCareer, error) {
	link := o.Link()
	if link.PlayerID == "" {
		return OwHeroCareer{}, errors.New("No Overwatch profile connected yet.")
	}
	if mode != "competitive" {
		mode = "quickplay"
	}
	info := o.heroes()[hero]
	out, _, err := cachedFetch(o.store, "ow_hero_"+safeKey(link.PlayerID)+"_"+safeKey(hero)+"_"+mode, 10*time.Minute, false, func() (OwHeroCareer, error) {
		groups, err := o.career(url.PathEscape(link.PlayerID), mode, hero)
		return OwHeroCareer{Key: hero, Mode: mode, Groups: groups}, err
	})
	out.Name, out.Role, out.Portrait = info.Name, info.Role, info.Portrait
	if out.Name == "" {
		out.Name = hero
	}
	if out.Groups == nil {
		out.Groups = []OwStatGroup{}
	}
	return out, err
}

// ---------- Meta ----------

type OwMetaHero struct {
	Key      string  `json:"key"`
	Name     string  `json:"name"`
	Role     string  `json:"role"`
	Portrait *string `json:"portrait"`
	PickRate float64 `json:"pickRate"`
	WinRate  float64 `json:"winRate"`
	BanRate  float64 `json:"banRate"`
}

type OwOption struct {
	ID    string `json:"id"`
	Label string `json:"label"`
}

type OwMeta struct {
	Heroes []OwMetaHero `json:"heroes"`
	// What was asked for, after anything unknown was replaced by a default.
	Mode     string `json:"mode"`
	Region   string `json:"region"`
	Division string `json:"division"`
	Map      string `json:"map"`
	// The choices the page offers.
	Regions   []OwOption `json:"regions"`
	Divisions []OwOption `json:"divisions"`
	Maps      []OwOption `json:"maps"`
	Freshness
}

var owRegions = []OwOption{{"europe", "Europe"}, {"americas", "Americas"}, {"asia", "Asia"}}

var owDivisions = []OwOption{
	{"all", "All ranks"}, {"bronze", "Bronze"}, {"silver", "Silver"}, {"gold", "Gold"}, {"platinum", "Platinum"},
	{"diamond", "Diamond"}, {"master", "Master"}, {"grandmaster", "Grandmaster"},
}

func owPick(options []OwOption, id string) string {
	for _, o := range options {
		if o.ID == id {
			return id
		}
	}
	return options[0].ID
}

func (o *Overwatch) maps() []OwOption {
	list, _, _ := cachedFetch(o.store, "ow_maps", 24*time.Hour, false, func() ([]OwOption, error) {
		var rows []jsonMap
		if err := o.api.get("/maps", &rows); err != nil {
			return nil, err
		}
		out := []OwOption{}
		for _, m := range rows {
			if key := jStr(m, "key", ""); key != "" {
				out = append(out, OwOption{ID: key, Label: jStr(m, "name", key)})
			}
		}
		sort.Slice(out, func(i, j int) bool { return out[i].Label < out[j].Label })
		return out, nil
	})
	return list
}

// Meta is how often each hero is picked, wins and is banned, as Blizzard
// publishes it, for one mode, region, rank and (optionally) map.
func (o *Overwatch) Meta(mode, region, division, mapKey string, force bool) (OwMeta, error) {
	if mode != "quickplay" {
		mode = "competitive"
	}
	region = owPick(owRegions, region)
	division = owPick(owDivisions, division)
	maps := append([]OwOption{{"all", "All maps"}}, o.maps()...)
	mapKey = owPick(maps, mapKey)
	// Rank only exists in competitive.
	if mode == "quickplay" {
		division = "all"
	}

	key := "ow_meta_" + mode + "_" + region + "_" + division + "_" + safeKey(mapKey)
	heroes, fresh, err := cachedFetch(o.store, key, 30*time.Minute, force, func() ([]OwMetaHero, error) {
		q := url.Values{"platform": {"pc"}, "gamemode": {mode}, "region": {region}}
		if division != "all" {
			q.Set("competitive_division", division)
		}
		if mapKey != "all" {
			q.Set("map", mapKey)
		}
		var rows []jsonMap
		if err := o.api.get("/heroes/stats?"+q.Encode(), &rows); err != nil {
			return nil, err
		}
		info := o.heroes()
		out := []OwMetaHero{}
		for _, r := range rows {
			k := jStr(r, "hero", "")
			h, known := info[k]
			if !known {
				continue
			}
			out = append(out, OwMetaHero{Key: k, Name: h.Name, Role: h.Role, Portrait: h.Portrait, PickRate: jF64(r, "pickrate"), WinRate: jF64(r, "winrate"), BanRate: jF64(r, "banrate")})
		}
		if len(out) == 0 {
			return nil, &apiError{"There is no hero data for that combination yet.", false}
		}
		sort.SliceStable(out, func(i, j int) bool { return out[i].WinRate > out[j].WinRate })
		return out, nil
	})
	if heroes == nil {
		heroes = []OwMetaHero{}
	}
	return OwMeta{Heroes: heroes, Mode: mode, Region: region, Division: division, Map: mapKey, Regions: owRegions, Divisions: owDivisions, Maps: maps, Freshness: fresh}, err
}
