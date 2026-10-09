package core

import (
	"errors"
	"fmt"
	"net/url"
	"sort"
	"time"
)

// Deadlock, via the community-run Deadlock API.
//
// Valve publishes no live feed for Deadlock, so nothing here is live the way
// the Dota side is: matches appear once the API has ingested them. It is a
// third-party service that Valve rate-limits, so data can be late or missing,
// and failures are shown plainly rather than dressed up as empty state.
//
// Deliberately not done: nothing reads the game's memory or surfaces anything
// a player could not already see in-game.

type Deadlock struct {
	store *Store
	api   *service
}

func NewDeadlock(store *Store) *Deadlock {
	return &Deadlock{store: store, api: &service{Name: "The Deadlock API", Base: "https://api.deadlock-api.com", Attempts: 2}}
}

func (d *Deadlock) account() (uint64, error) {
	if id := d.store.LoadLink("deadlock").AccountID; id != nil {
		return *id, nil
	}
	return 0, errors.New("No Deadlock account linked yet.")
}

type DeadlockHero struct {
	ID    int     `json:"id"`
	Name  string  `json:"name"`
	Image *string `json:"image"`
}

func (d *Deadlock) Heroes() map[int]DeadlockHero {
	list, _, _ := cachedFetch(d.store, "dl_heroes", 6*time.Hour, false, func() ([]DeadlockHero, error) {
		var rows []jsonMap
		if err := d.api.get("/v1/assets/heroes?only_active=true", &rows); err != nil {
			return nil, err
		}
		out := []DeadlockHero{}
		for _, h := range rows {
			id := int(jI64(h, "id"))
			if id == 0 {
				continue
			}
			images := sub(h, "images")
			img := jStrPtr(images, "icon_image_small")
			if img == nil {
				img = jStrPtr(images, "icon_hero_card")
			}
			out = append(out, DeadlockHero{ID: id, Name: jStr(h, "name", "Unknown"), Image: img})
		}
		if len(out) == 0 {
			return nil, &apiError{"The Deadlock API returned no heroes.", true}
		}
		return out, nil
	})
	m := make(map[int]DeadlockHero, len(list))
	for _, h := range list {
		m[h.ID] = h
	}
	return m
}

func (d *Deadlock) HeroList() []DeadlockHero {
	out := []DeadlockHero{}
	for _, h := range d.Heroes() {
		out = append(out, h)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

type DeadlockProfile struct {
	AccountID   uint64  `json:"accountId"`
	Personaname string  `json:"personaname"`
	Avatar      *string `json:"avatar"`
	ProfileURL  *string `json:"profileUrl"`
}

func (d *Deadlock) Search(query string) ([]DeadlockProfile, error) {
	var rows []jsonMap
	if err := d.api.get("/v1/players/steam-search?search_query="+url.QueryEscape(query), &rows); err != nil {
		return nil, err
	}
	out := []DeadlockProfile{}
	for _, p := range rows {
		id := jU64(p, "account_id")
		if id == 0 {
			continue
		}
		avatar := jStrPtr(p, "avatarmedium")
		if avatar == nil {
			avatar = jStrPtr(p, "avatar")
		}
		out = append(out, DeadlockProfile{AccountID: id, Personaname: jStr(p, "personaname", "(no name)"), Avatar: avatar, ProfileURL: jStrPtr(p, "profileurl")})
		if len(out) == 20 {
			break
		}
	}
	return out, nil
}

// ---------- Rank ----------

var deadlockTiers = []string{
	"Obscurus", "Initiate", "Seeker", "Acolyte", "Sentinel", "Mystic", "Ritualist", "Emissary",
	"Oracle", "Phantom", "Ascendant", "Eternus",
}

type DeadlockRank struct {
	Badge    int    `json:"badge"`
	Tier     int    `json:"tier"`
	Subrank  int    `json:"subrank"`
	TierName string `json:"tierName"`
	Label    string `json:"label"`
	// The game's icon for this rank, when known.
	Icon *string `json:"icon,omitempty"`
	// Dota medals draw their stars as a second image over the medal.
	Star *string `json:"star,omitempty"`
}

// decodeBadge unpacks tier*10 + subrank: badge 26 is Seeker 6.
func decodeBadge(badge int) DeadlockRank {
	r := DeadlockRank{Badge: badge, Tier: badge / 10, Subrank: badge % 10, TierName: "Unranked"}
	if r.Tier >= 0 && r.Tier < len(deadlockTiers) {
		r.TierName = deadlockTiers[r.Tier]
	}
	r.Label = r.TierName
	if r.Subrank > 0 {
		r.Label = fmt.Sprintf("%s %d", r.TierName, r.Subrank)
	}
	return r
}

func (d *Deadlock) rank(account uint64) *DeadlockRank {
	var v jsonMap
	if d.api.get(fmt.Sprintf("/v1/players/%d/rank", account), &v) != nil || !jHas(v, "badge") {
		return nil
	}
	r := decodeBadge(int(jI64(v, "badge")))
	return &r
}

// ---------- Match history ----------

type DeadlockMatch struct {
	MatchID         uint64  `json:"matchId"`
	HeroID          int     `json:"heroId"`
	HeroName        string  `json:"heroName"`
	HeroImage       *string `json:"heroImage"`
	StartTime       int64   `json:"startTime"`
	DurationSeconds int     `json:"durationSeconds"`
	Kills           int     `json:"kills"`
	Deaths          int     `json:"deaths"`
	Assists         int     `json:"assists"`
	NetWorth        int64   `json:"netWorth"`
	LastHits        int     `json:"lastHits"`
	Denies          int     `json:"denies"`
	HeroLevel       int     `json:"heroLevel"`
	Outcome         string  `json:"outcome"` // win | loss | abandoned | unscored
	Abandoned       bool    `json:"abandoned"`
}

// deadlockOutcome decides win or loss by comparing the player's team with
// the winning team, not from player_match_outcome. That field is documented
// as authoritative but was 0 on three quarters of a real 232-match history;
// the team comparison was present on every record and agreed with it wherever
// both existed. player_match_outcome is used only for the abandon cases it
// uniquely reports.
func deadlockOutcome(m jsonMap) string {
	code, hasCode := getInt(m, "player_match_outcome")
	if hasCode && (code == 3 || code == 4) {
		return "abandoned"
	}
	team, hasTeam := getInt(m, "player_team")
	result, hasResult := getInt(m, "match_result")
	if hasTeam && hasResult {
		if team == result {
			return "win"
		}
		return "loss"
	}
	switch code {
	case 1:
		return "win"
	case 2:
		return "loss"
	}
	return "unscored"
}

type DeadlockSummary struct {
	Matches  int     `json:"matches"`
	Wins     int     `json:"wins"`
	Losses   int     `json:"losses"`
	WinRate  float64 `json:"winRate"`
	Kills    int     `json:"kills"`
	Deaths   int     `json:"deaths"`
	Assists  int     `json:"assists"`
	KDA      float64 `json:"kda"`
	AvgSouls int64   `json:"avgSouls"`
	BestHero *string `json:"bestHero"`
}

// SummarizeDeadlock rolls matches into headline numbers. Only scored games
// count towards the win rate — abandons would otherwise drag it down.
func SummarizeDeadlock(matches []DeadlockMatch) DeadlockSummary {
	s := DeadlockSummary{Matches: len(matches)}
	var souls int64
	type rec struct{ played, won int }
	perHero := map[string]*rec{}
	var order []string
	for _, m := range matches {
		switch m.Outcome {
		case "win":
			s.Wins++
		case "loss":
			s.Losses++
		}
		s.Kills += m.Kills
		s.Deaths += m.Deaths
		s.Assists += m.Assists
		souls += m.NetWorth
		r := perHero[m.HeroName]
		if r == nil {
			r = &rec{}
			perHero[m.HeroName] = r
			order = append(order, m.HeroName)
		}
		r.played++
		if m.Outcome == "win" {
			r.won++
		}
	}
	s.WinRate = pct(float64(s.Wins), float64(s.Wins+s.Losses))
	s.KDA = kda(s.Kills, s.Deaths, s.Assists)
	if len(matches) > 0 {
		s.AvgSouls = souls / int64(len(matches))
	}
	// Most-won hero, falling back to most-played; ties go to whichever was
	// played most recently so the answer is stable.
	best := ""
	for _, name := range order {
		r := perHero[name]
		if best == "" || r.won > perHero[best].won || (r.won == perHero[best].won && r.played > perHero[best].played) {
			best = name
		}
	}
	if best != "" {
		s.BestHero = &best
	}
	return s
}

type DeadlockOverview struct {
	Matches []DeadlockMatch `json:"matches"`
	Summary DeadlockSummary `json:"summary"`
	Rank    *DeadlockRank   `json:"rank"`
	Freshness
}

type deadlockCached struct {
	Matches []DeadlockMatch `json:"matches"`
	Rank    *DeadlockRank   `json:"rank"`
}

func (d *Deadlock) Overview(limit int, force bool) (DeadlockOverview, error) {
	account, err := d.account()
	if err != nil {
		return DeadlockOverview{}, err
	}
	return d.overviewFor(account, limit, force)
}

// overviewFor is any public account's recent matches and rank.
func (d *Deadlock) overviewFor(account uint64, limit int, force bool) (DeadlockOverview, error) {
	limit = clampLimit(limit, 100, 500)
	c, fresh, err := cachedFetch(d.store, fmt.Sprintf("dl_matches_%d_%d", account, limit), 2*time.Minute, force, func() (deadlockCached, error) {
		var rows []jsonMap
		if err := d.api.get(fmt.Sprintf("/v1/players/%d/match-history", account), &rows); err != nil {
			return deadlockCached{}, err
		}
		heroes := d.Heroes()
		out := deadlockCached{Matches: []DeadlockMatch{}}
		for i, m := range rows {
			if i >= limit {
				break
			}
			heroID := int(jI64(m, "hero_id"))
			hero, known := heroes[heroID]
			name := hero.Name
			if !known {
				name = fmt.Sprintf("Hero %d", heroID)
			}
			out.Matches = append(out.Matches, DeadlockMatch{
				MatchID: jU64(m, "match_id"), HeroID: heroID, HeroName: name, HeroImage: hero.Image,
				StartTime: jI64(m, "start_time"), DurationSeconds: int(jI64(m, "match_duration_s")),
				Kills: int(jI64(m, "player_kills")), Deaths: int(jI64(m, "player_deaths")), Assists: int(jI64(m, "player_assists")),
				NetWorth: jI64(m, "net_worth"), LastHits: int(jI64(m, "last_hits")), Denies: int(jI64(m, "denies")),
				HeroLevel: int(jI64(m, "hero_level")), Outcome: deadlockOutcome(m), Abandoned: jBool(m, "team_abandoned"),
			})
		}
		// A missing rank should not sink the view: plenty of accounts are
		// simply unranked.
		out.Rank = d.rank(account)
		return out, nil
	})
	if err != nil {
		return DeadlockOverview{}, err
	}
	if c.Matches == nil {
		c.Matches = []DeadlockMatch{}
	}
	return DeadlockOverview{Matches: c.Matches, Summary: SummarizeDeadlock(c.Matches), Rank: c.Rank, Freshness: fresh}, nil
}

// ---------- Live presence ----------

type DeadlockLive struct {
	MatchID   uint64  `json:"matchId"`
	StartTime int64   `json:"startTime"`
	HeroID    int     `json:"heroId"`
	HeroName  string  `json:"heroName"`
	HeroImage *string `json:"heroImage"`
	// Heroes on each side. The game shows every player the full lineup, so
	// this reveals nothing hidden — and it carries no identities or ranks.
	AllyHeroes  []string `json:"allyHeroes"`
	EnemyHeroes []string `json:"enemyHeroes"`
}

// Live reports whether the linked account is in a match the API lists as
// active. Nil means not in one.
func (d *Deadlock) Live() (*DeadlockLive, error) {
	account, err := d.account()
	if err != nil {
		return nil, nil
	}
	var rows []jsonMap
	if err := d.api.get("/v1/matches/active", &rows); err != nil {
		return nil, err
	}
	heroes := d.Heroes()
	nameOf := func(p jsonMap) string {
		id := int(jI64(p, "hero_id"))
		if h, ok := heroes[id]; ok {
			return h.Name
		}
		return fmt.Sprintf("Hero %d", id)
	}
	for _, m := range rows {
		players := jList(m["players"])
		var me jsonMap
		for _, p := range players {
			if jU64(p, "account_id") == account {
				me = p
				break
			}
		}
		if me == nil {
			continue
		}
		hero := heroes[int(jI64(me, "hero_id"))]
		live := &DeadlockLive{
			MatchID: jU64(m, "match_id"), StartTime: jI64(m, "start_time"),
			HeroID: int(jI64(me, "hero_id")), HeroName: nameOf(me), HeroImage: hero.Image,
			AllyHeroes: []string{}, EnemyHeroes: []string{},
		}
		for _, p := range players {
			if jU64(p, "account_id") == account {
				continue
			}
			if jI64(p, "team") == jI64(me, "team") {
				live.AllyHeroes = append(live.AllyHeroes, nameOf(p))
			} else {
				live.EnemyHeroes = append(live.EnemyHeroes, nameOf(p))
			}
		}
		return live, nil
	}
	return nil, nil
}

// ---------- Single match ----------

type DeadlockPlayer struct {
	AccountID uint64  `json:"accountId"`
	HeroName  string  `json:"heroName"`
	HeroImage *string `json:"heroImage"`
	Team      int     `json:"team"`
	Kills     int     `json:"kills"`
	Deaths    int     `json:"deaths"`
	Assists   int     `json:"assists"`
	NetWorth  int64   `json:"netWorth"`
	LastHits  int     `json:"lastHits"`
	Denies    int     `json:"denies"`
	Level     int     `json:"level"`
	IsMe      bool    `json:"isMe"`
}

type DeadlockMatchDetail struct {
	MatchID         uint64           `json:"matchId"`
	DurationSeconds int              `json:"durationSeconds"`
	WinningTeam     int              `json:"winningTeam"`
	Players         []DeadlockPlayer `json:"players"`
}

// Detail is the scoreboard for one match. The metadata payload is around a
// megabyte, so it is fetched only when a row is opened, and only the
// scoreboard is kept.
func (d *Deadlock) Detail(matchID uint64) (DeadlockMatchDetail, error) {
	me := d.store.LoadLink("deadlock").AccountID
	detail, _, err := cachedFetch(d.store, fmt.Sprintf("dl_match_%d", matchID), 30*24*time.Hour, false, func() (DeadlockMatchDetail, error) {
		var v jsonMap
		if err := d.api.get(fmt.Sprintf("/v1/matches/%d/metadata", matchID), &v); err != nil {
			return DeadlockMatchDetail{}, err
		}
		info := sub(v, "match_info")
		if info == nil {
			info = v
		}
		heroes := d.Heroes()
		out := DeadlockMatchDetail{MatchID: matchID, DurationSeconds: int(jI64(info, "duration_s")), WinningTeam: int(jI64(info, "winning_team")), Players: []DeadlockPlayer{}}
		for _, p := range jList(info["players"]) {
			heroID := int(jI64(p, "hero_id"))
			hero, known := heroes[heroID]
			name := hero.Name
			if !known {
				name = fmt.Sprintf("Hero %d", heroID)
			}
			out.Players = append(out.Players, DeadlockPlayer{
				AccountID: jU64(p, "account_id"), HeroName: name, HeroImage: hero.Image, Team: int(jI64(p, "team")),
				Kills: int(jI64(p, "kills")), Deaths: int(jI64(p, "deaths")), Assists: int(jI64(p, "assists")),
				NetWorth: jI64(p, "net_worth"), LastHits: int(jI64(p, "last_hits")), Denies: int(jI64(p, "denies")), Level: int(jI64(p, "level")),
			})
		}
		if len(out.Players) == 0 {
			return out, &apiError{"The Deadlock API has no scoreboard for that match yet.", false}
		}
		// Team, then net worth: how a scoreboard is read.
		sort.SliceStable(out.Players, func(i, j int) bool {
			if out.Players[i].Team != out.Players[j].Team {
				return out.Players[i].Team < out.Players[j].Team
			}
			return out.Players[i].NetWorth > out.Players[j].NetWorth
		})
		return out, nil
	})
	if err != nil {
		return detail, err
	}
	for i := range detail.Players {
		detail.Players[i].IsMe = me != nil && detail.Players[i].AccountID == *me
	}
	return detail, nil
}

// ---------- Popular items ----------

func (d *Deadlock) itemNames() map[int64]string {
	m, _, _ := cachedFetch(d.store, "dl_items", 24*time.Hour, false, func() (map[int64]string, error) {
		var rows []jsonMap
		if err := d.api.get("/v1/assets/items", &rows); err != nil {
			return nil, err
		}
		out := map[int64]string{}
		for _, it := range rows {
			if id := jI64(it, "id"); id != 0 {
				out[id] = jStr(it, "name", "Unknown item")
			}
		}
		if len(out) == 0 {
			return nil, &apiError{"The Deadlock API returned no items.", true}
		}
		return out, nil
	})
	return m
}

type DeadlockPopularItem struct {
	ItemID int64  `json:"itemId"`
	Name   string `json:"name"`
	Builds int64  `json:"builds"`
}

// PopularItems is which items appear in the most published builds for a hero.
func (d *Deadlock) PopularItems(heroID int) ([]DeadlockPopularItem, error) {
	items, _, err := cachedFetch(d.store, fmt.Sprintf("dl_popular_%d", heroID), 12*time.Hour, false, func() ([]DeadlockPopularItem, error) {
		var rows []jsonMap
		if err := d.api.get(fmt.Sprintf("/v1/analytics/build-item-stats?hero_id=%d", heroID), &rows); err != nil {
			return nil, err
		}
		names := d.itemNames()
		out := []DeadlockPopularItem{}
		for _, r := range rows {
			id, builds := jI64(r, "item_id"), jI64(r, "builds")
			if id == 0 || builds == 0 {
				continue
			}
			name, ok := names[id]
			if !ok {
				continue // an id with no name is not something to show
			}
			out = append(out, DeadlockPopularItem{ItemID: id, Name: name, Builds: builds})
		}
		sort.Slice(out, func(i, j int) bool { return out[i].Builds > out[j].Builds })
		if len(out) > 12 {
			out = out[:12]
		}
		return out, nil
	})
	if items == nil {
		items = []DeadlockPopularItem{}
	}
	return items, err
}
