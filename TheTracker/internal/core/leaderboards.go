package core

import (
	"fmt"
	"time"
)

// Global leaderboards: each game's own ranked ladder, by region.

// ---------- Public profile ----------

type SteamProfileInfo struct {
	Name     string
	Avatar   *string
	RankTier int
}

// Profile is an account's public name, avatar and medal, from OpenDota.
func (d *Dota) Profile(account uint64) (SteamProfileInfo, error) {
	var v jsonMap
	if err := d.api.get(fmt.Sprintf("/players/%d", account), &v); err != nil {
		return SteamProfileInfo{}, err
	}
	p := SteamProfileInfo{RankTier: int(jI64(v, "rank_tier"))}
	if inner := sub(v, "profile"); inner != nil {
		p.Name = jStr(inner, "personaname", "")
		p.Avatar = jStrPtr(inner, "avatarfull")
	}
	return p, nil
}

// ---------- Dota ----------

type Region struct {
	ID    string `json:"id"`
	Label string `json:"label"`
}

// DotaRegions are the divisions of Valve's official leaderboard.
var DotaRegions = []Region{{"europe", "Europe"}, {"americas", "Americas"}, {"se_asia", "SE Asia"}, {"china", "China"}}

func validRegion(list []Region, id string) string {
	for _, r := range list {
		if r.ID == id {
			return id
		}
	}
	return list[0].ID
}

type DotaLeader struct {
	Rank    int    `json:"rank"`
	Name    string `json:"name"`
	Team    string `json:"team"`
	Country string `json:"country"`
}

type DotaLeaderboard struct {
	Region  string       `json:"region"`
	Regions []Region     `json:"regions"`
	Players []DotaLeader `json:"players"`
	// When Valve last posted this board, Unix seconds.
	PostedAt int64 `json:"postedAt"`
	Freshness
}

// leaderboardDepth: the boards run thousands deep; the top thousand is what
// anyone reads.
const leaderboardDepth = 1000

// Leaderboard is Valve's official ranked leaderboard for one region: the
// same list the Dota client and dota2.com show.
func (d *Dota) Leaderboard(region string, force bool) (DotaLeaderboard, error) {
	region = validRegion(DotaRegions, region)
	board, fresh, err := cachedFetch(d.store, "dota_leaderboard_"+region, 30*time.Minute, force, func() (DotaLeaderboard, error) {
		var v jsonMap
		if err := d.valve.get("/ILeaderboard/GetDivisionLeaderboard/v0001?division="+region+"&leaderboard=0", &v); err != nil {
			return DotaLeaderboard{}, err
		}
		out := DotaLeaderboard{PostedAt: jI64(v, "time_posted"), Players: []DotaLeader{}}
		for _, p := range jList(v["leaderboard"]) {
			out.Players = append(out.Players, DotaLeader{Rank: int(jI64(p, "rank")), Name: jStr(p, "name", ""), Team: jStr(p, "team_tag", ""), Country: jStr(p, "country", "")})
			if len(out.Players) == leaderboardDepth {
				break
			}
		}
		if len(out.Players) == 0 {
			return out, &apiError{"Dota's leaderboard came back empty.", true}
		}
		return out, nil
	})
	board.Region, board.Regions, board.Freshness = region, DotaRegions, fresh
	if board.Players == nil {
		board.Players = []DotaLeader{}
	}
	return board, err
}

// ---------- Deadlock ----------

// DeadlockRegions are the regions of Deadlock's ranked leaderboard.
var DeadlockRegions = []Region{{"Europe", "Europe"}, {"NAmerica", "North America"}, {"SAmerica", "South America"}, {"Asia", "Asia"}, {"Oceania", "Oceania"}}

type DeadlockLeader struct {
	Rank   int      `json:"rank"`
	Name   string   `json:"name"`
	Heroes []string `json:"heroes"`
	Images []string `json:"images"`
	// True when the linked account is one this entry could belong to.
	IsMe bool `json:"isMe"`
}

type DeadlockLeaderboard struct {
	Region  string           `json:"region"`
	Regions []Region         `json:"regions"`
	Players []DeadlockLeader `json:"players"`
	Freshness
}

type dlLeaderRaw struct {
	Rank    int      `json:"rank"`
	Name    string   `json:"name"`
	HeroIDs []int    `json:"heroIds"`
	Maybe   []uint64 `json:"maybe"`
}

// Leaderboard is Deadlock's ranked leaderboard for one region.
func (d *Deadlock) Leaderboard(region string, force bool) (DeadlockLeaderboard, error) {
	region = validRegion(DeadlockRegions, region)
	raw, fresh, err := cachedFetch(d.store, "dl_leaderboard_"+region, 30*time.Minute, force, func() ([]dlLeaderRaw, error) {
		var v jsonMap
		if err := d.api.get("/v1/leaderboard/"+region, &v); err != nil {
			return nil, err
		}
		out := []dlLeaderRaw{}
		for _, e := range jList(v["entries"]) {
			r := dlLeaderRaw{Rank: int(jI64(e, "rank")), Name: jStr(e, "account_name", "")}
			if arr, ok := e["top_hero_ids"].([]any); ok {
				for _, x := range arr {
					if f, ok := x.(float64); ok {
						r.HeroIDs = append(r.HeroIDs, int(f))
					}
				}
			}
			// The game publishes names, not accounts; the API lists which
			// accounts a name could be. Kept only to mark the player's own
			// row, and only when the list is short: a common name matches
			// hundreds of accounts and proves nothing.
			if arr, ok := e["possible_account_ids"].([]any); ok && len(arr) <= 5 {
				for _, x := range arr {
					if f, ok := x.(float64); ok {
						r.Maybe = append(r.Maybe, uint64(f))
					}
				}
			}
			out = append(out, r)
		}
		if len(out) == 0 {
			return nil, &apiError{"The Deadlock leaderboard came back empty.", true}
		}
		return out, nil
	})
	board := DeadlockLeaderboard{Region: region, Regions: DeadlockRegions, Players: []DeadlockLeader{}, Freshness: fresh}
	if err != nil {
		return board, err
	}
	heroes := d.Heroes()
	me := d.store.LoadLink("deadlock").AccountID
	for _, r := range raw {
		p := DeadlockLeader{Rank: r.Rank, Name: r.Name, Heroes: []string{}, Images: []string{}}
		for _, id := range r.HeroIDs {
			if h, ok := heroes[id]; ok {
				p.Heroes = append(p.Heroes, h.Name)
				if h.Image != nil {
					p.Images = append(p.Images, *h.Image)
				}
			}
		}
		for _, id := range r.Maybe {
			p.IsMe = p.IsMe || (me != nil && id == *me)
		}
		board.Players = append(board.Players, p)
	}
	return board, nil
}
