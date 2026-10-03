package core

import (
	"sort"
	"sync"
	"time"
)

// Meta: which heroes are strong right now and which way they are moving.
//
// Everything here is computed from the public APIs the rest of the app
// already reads — OpenDota's /heroStats and deadlock-api's analytics. Nothing
// is taken from any other tracker's site: their ratings are their own work
// over their own samples.

const metaTTL = 30 * time.Minute

type MetaHero struct {
	ID    int      `json:"id"`
	Name  string   `json:"name"`
	Slug  string   `json:"slug"`
	Roles []string `json:"roles"`
	// carry | mid | offlane | support | hard_support — an estimate, see
	// estimatePosition.
	Position string  `json:"position"`
	Picks    int64   `json:"picks"`
	WinRate  float64 `json:"winRate"`
	// Share of matches the hero appears in.
	PickRate float64 `json:"pickRate"`
	// Win-rate change across the sample window, in points.
	Trend        float64  `json:"trend"`
	HighWinRate  *float64 `json:"highWinRate"`
	ProPicks     int64    `json:"proPicks"`
	ProBans      int64    `json:"proBans"`
	ProWinRate   *float64 `json:"proWinRate"`
	TurboWinRate *float64 `json:"turboWinRate"`
}

type DotaMeta struct {
	Matches int64      `json:"matches"`
	Heroes  []MetaHero `json:"heroes"`
	Freshness
}

func numList(v any) []int64 {
	arr, _ := v.([]any)
	out := make([]int64, 0, len(arr))
	for _, e := range arr {
		if f, ok := e.(float64); ok {
			out = append(out, int64(f))
		}
	}
	return out
}

// bucketWinRate is the win rate over buckets [from, to), or nil if the range
// is out of bounds or empty.
func bucketWinRate(wins, picks []int64, from, to int) *float64 {
	if from < 0 || to > len(wins) || to > len(picks) || from >= to {
		return nil
	}
	var w, p int64
	for i := from; i < to; i++ {
		w += wins[i]
		p += picks[i]
	}
	if p == 0 {
		return nil
	}
	return ptr(float64(w) / float64(p) * 100)
}

// heroTrend is the win-rate change between the earlier and recent halves of
// OpenDota's trend buckets. The final bucket is the week in progress and is
// always short, so it is dropped — including it reads as every hero falling.
func heroTrend(wins, picks []int64) float64 {
	usable := len(picks) - 1
	if usable < 2 || len(wins) < usable {
		return 0
	}
	mid := usable / 2
	earlier, recent := bucketWinRate(wins, picks, 0, mid), bucketWinRate(wins, picks, mid, usable)
	if earlier == nil || recent == nil {
		return 0
	}
	return *recent - *earlier
}

func hasRole(roles []string, want string) bool {
	for _, r := range roles {
		if r == want {
			return true
		}
	}
	return false
}

// estimatePosition guesses which of the five positions a hero is usually
// played in.
//
// OpenDota publishes no per-position hero stats. What it does publish is
// which lane each hero goes to (safe, mid, off) and Valve's role tags, and
// the two together are enough for a reasonable estimate: a mid-lane hero is
// a Mid; a safe-lane hero is the Carry unless it is tagged Support, in which
// case it is the Hard Support beside the carry; an off-lane hero is the
// Offlane unless tagged Support, in which case it is the roaming Support.
// Heroes without lane data fall back to their tags alone. It is labelled an
// estimate in the UI because that is what it is.
func estimatePosition(roles []string, lanes map[int]int64) string {
	support := hasRole(roles, "Support")
	carryFirst := len(roles) > 0 && roles[0] == "Carry"
	// A hero tagged both ways (Carry first, Support somewhere later) is
	// played as a core far more often than not.
	pureSupport := support && !carryFirst

	var best int
	var bestGames int64
	for lane, games := range lanes {
		if lane >= 1 && lane <= 3 && (games > bestGames || (games == bestGames && lane < best)) {
			best, bestGames = lane, games
		}
	}
	switch best {
	case 2:
		if pureSupport && !hasRole(roles, "Nuker") {
			return "support"
		}
		return "mid"
	case 1:
		if pureSupport {
			return "hard_support"
		}
		return "carry"
	case 3:
		if pureSupport {
			return "support"
		}
		return "offlane"
	}

	switch {
	case pureSupport:
		return "support"
	case carryFirst:
		return "carry"
	case hasRole(roles, "Initiator") || hasRole(roles, "Durable"):
		return "offlane"
	case hasRole(roles, "Carry"):
		return "carry"
	}
	return "mid"
}

// laneRoles is games per lane for each hero, from OpenDota's lane scenarios.
// Best-effort: without it, positions fall back to role tags.
func (d *Dota) laneRoles() map[int]map[int]int64 {
	out := map[int]map[int]int64{}
	var rows []jsonMap
	if d.api.get("/scenarios/laneRoles", &rows) != nil {
		return out
	}
	for _, r := range rows {
		hero, lane := int(jI64(r, "hero_id")), int(jI64(r, "lane_role"))
		// games arrives as a string on this endpoint.
		games := jI64(r, "games")
		if s, ok := r["games"].(string); ok {
			for _, c := range s {
				if c < '0' || c > '9' {
					games = 0
					break
				}
				games = games*10 + int64(c-'0')
			}
		}
		if out[hero] == nil {
			out[hero] = map[int]int64{}
		}
		out[hero][lane] += games
	}
	return out
}

func (d *Dota) Meta(force bool) (DotaMeta, error) {
	meta, fresh, err := cachedFetch(d.store, "od_meta", metaTTL, force, func() (DotaMeta, error) {
		var rows []jsonMap
		if err := d.api.get("/heroStats", &rows); err != nil {
			return DotaMeta{}, err
		}
		lanes := d.laneRoles()

		var totalPicks int64
		for _, r := range rows {
			totalPicks += jI64(r, "pub_pick")
		}
		// Ten heroes are picked per match.
		matches := max(totalPicks/10, 1)

		heroes := []MetaHero{}
		for _, r := range rows {
			picks := jI64(r, "pub_pick")
			name := jStr(r, "localized_name", "")
			if picks == 0 || name == "" {
				continue
			}
			roles := []string{}
			if arr, ok := r["roles"].([]any); ok {
				for _, x := range arr {
					if s, ok := x.(string); ok {
						roles = append(roles, s)
					}
				}
			}
			id := int(jI64(r, "id"))
			h := MetaHero{
				ID: id, Name: name, Slug: heroSlug(jStr(r, "name", "")), Roles: roles,
				Position: estimatePosition(roles, lanes[id]),
				Picks:    picks, WinRate: pct(float64(jI64(r, "pub_win")), float64(picks)),
				PickRate: pct(float64(picks), float64(matches)),
				Trend:    heroTrend(numList(r["pub_win_trend"]), numList(r["pub_pick_trend"])),
				ProPicks: jI64(r, "pro_pick"), ProBans: jI64(r, "pro_ban"),
			}
			// Bracket 8 is Immortal and often empty in the snapshot; 7 is
			// Divine. Prefer the higher one that has data.
			for _, b := range []string{"8", "7"} {
				if p := jI64(r, b+"_pick"); p > 0 {
					h.HighWinRate = ptr(pct(float64(jI64(r, b+"_win")), float64(p)))
					break
				}
			}
			if h.ProPicks > 0 {
				h.ProWinRate = ptr(pct(float64(jI64(r, "pro_win")), float64(h.ProPicks)))
			}
			if tp := jI64(r, "turbo_picks"); tp > 0 {
				h.TurboWinRate = ptr(pct(float64(jI64(r, "turbo_wins")), float64(tp)))
			}
			heroes = append(heroes, h)
		}
		if len(heroes) == 0 {
			return DotaMeta{}, &apiError{"OpenDota returned no hero stats.", true}
		}
		sort.SliceStable(heroes, func(i, j int) bool { return heroes[i].WinRate > heroes[j].WinRate })
		return DotaMeta{Matches: matches, Heroes: heroes}, nil
	})
	meta.Freshness = fresh
	if meta.Heroes == nil {
		meta.Heroes = []MetaHero{}
	}
	return meta, err
}

// ---------- Deadlock ----------

type MetaDlHero struct {
	ID        int     `json:"id"`
	Name      string  `json:"name"`
	Image     *string `json:"image"`
	Matches   int64   `json:"matches"`
	WinRate   float64 `json:"winRate"`
	PickRate  float64 `json:"pickRate"`
	KDA       float64 `json:"kda"`
	AvgSouls  int64   `json:"avgSouls"`
	AvgDamage int64   `json:"avgDamage"`
}

type MetaDlItem struct {
	ID int64 `json:"id"`
	// Empty when the item list could not be loaded; such rows are dropped.
	Name string `json:"name"`
	// Matches the item was bought in — a raw count, because the item and
	// hero endpoints cover different samples and dividing one by the other
	// produced "pick rates" over 100%.
	Matches int64   `json:"matches"`
	WinRate float64 `json:"winRate"`
	// How common next to the most-bought item, 0–100.
	Share     float64  `json:"share"`
	BuyMinute *float64 `json:"buyMinute"`
}

type DeadlockMeta struct {
	Matches int64        `json:"matches"`
	Heroes  []MetaDlHero `json:"heroes"`
	Items   []MetaDlItem `json:"items"`
	Freshness
}

func (d *Deadlock) Meta(force bool) (DeadlockMeta, error) {
	meta, fresh, err := cachedFetch(d.store, "dl_meta", metaTTL, force, func() (DeadlockMeta, error) {
		// Four requests, run together: sequentially the page sat loading for
		// the better part of half a minute.
		var heroRows, itemRows []jsonMap
		var heroErr error
		var names map[int]DeadlockHero
		var itemNames map[int64]string
		var wg sync.WaitGroup
		wg.Add(4)
		go func() { defer wg.Done(); heroErr = d.api.get("/v1/analytics/hero-stats", &heroRows) }()
		go func() { defer wg.Done(); names = d.Heroes() }()
		go func() { defer wg.Done(); _ = d.api.get("/v1/analytics/item-stats", &itemRows) }()
		go func() { defer wg.Done(); itemNames = d.itemNames() }()
		wg.Wait()
		if heroErr != nil {
			return DeadlockMeta{}, heroErr
		}

		var total int64
		for _, r := range heroRows {
			total += jI64(r, "matches")
		}
		// Twelve players per Deadlock match.
		matches := max(total/12, 1)

		out := DeadlockMeta{Matches: matches, Heroes: []MetaDlHero{}, Items: []MetaDlItem{}}
		for _, r := range heroRows {
			played := jI64(r, "matches")
			id := int(jI64(r, "hero_id"))
			hero, known := names[id]
			// A hero id the asset list does not know is unreleased or test
			// content; a row called "Hero 53" helps nobody.
			if played == 0 || !known {
				continue
			}
			out.Heroes = append(out.Heroes, MetaDlHero{
				ID: id, Name: hero.Name, Image: hero.Image, Matches: played,
				WinRate:  pct(float64(jI64(r, "wins")), float64(played)),
				PickRate: pct(float64(played), float64(matches)),
				KDA:      float64(jI64(r, "total_kills")+jI64(r, "total_assists")) / float64(max(jI64(r, "total_deaths"), 1)),
				AvgSouls: jI64(r, "total_net_worth") / played, AvgDamage: jI64(r, "total_player_damage") / played,
			})
		}
		if len(out.Heroes) == 0 {
			return out, &apiError{"The Deadlock API returned no hero stats.", true}
		}
		sort.SliceStable(out.Heroes, func(i, j int) bool { return out.Heroes[i].WinRate > out.Heroes[j].WinRate })

		var busiest int64 = 1
		for _, r := range itemRows {
			busiest = max(busiest, jI64(r, "matches"))
		}
		for _, r := range itemRows {
			played, id := jI64(r, "matches"), jI64(r, "item_id")
			name, known := itemNames[id]
			if played == 0 || id == 0 || !known {
				continue
			}
			it := MetaDlItem{ID: id, Name: name, Matches: played, WinRate: pct(float64(jI64(r, "wins")), float64(played)), Share: pct(float64(played), float64(busiest))}
			if s := jF64(r, "avg_buy_time_s"); s > 0 {
				it.BuyMinute = ptr(s / 60)
			}
			out.Items = append(out.Items, it)
		}
		sort.SliceStable(out.Items, func(i, j int) bool { return out.Items[i].WinRate > out.Items[j].WinRate })
		return out, nil
	})
	meta.Freshness = fresh
	if meta.Heroes == nil {
		meta.Heroes = []MetaDlHero{}
	}
	if meta.Items == nil {
		meta.Items = []MetaDlItem{}
	}
	return meta, err
}
