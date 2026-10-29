package core

// VALORANT, through HenrikDev's community API (api.henrikdev.xyz): Riot's
// own match API is open only to approved production apps. The API key stays
// on the cloud service (convex/valorant.ts), like PUBG's. Names, pictures,
// ranks and map callouts come from valorant-api.com, public and keyless.
//
// HenrikDev lists a player's last ten matches in full (every kill with the
// players' positions, round by round); each one read is kept here for good,
// so the history grows the more the app is open.

import (
	"errors"
	"fmt"
	"math"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
)

type Valorant struct {
	store   *Store
	cloud   *Cloud
	content *service
	mu      sync.Mutex
}

func NewValorant(store *Store, cloud *Cloud) *Valorant {
	return &Valorant{store: store, cloud: cloud, content: &service{Name: "valorant-api.com", Base: "https://valorant-api.com", Attempts: 2}}
}

// ValLink is the Riot account the pages read.
type ValLink struct {
	Puuid    string `json:"puuid"`
	Name     string `json:"name"`
	Tag      string `json:"tag"`
	Region   string `json:"region"`
	Platform string `json:"platform"`
	Card     string `json:"card,omitempty"`
	Level    int    `json:"level,omitempty"`
}

func (v *Valorant) Link() ValLink {
	var l ValLink
	v.store.readJSON("valorant.json", &l)
	return l
}

func (v *Valorant) SetLink(l ValLink) error { return v.store.writeJSON("valorant.json", l) }

var valRegions = map[string]string{"eu": "Europe", "na": "North America", "ap": "Asia Pacific", "kr": "Korea", "latam": "Latin America", "br": "Brazil"}

var valPuuid = regexp.MustCompile(`^[0-9a-f\-]{36}$`)

var errValNotSetUp = errors.New("Valorant isn't set up on the cloud service yet.")

// get reads one path of HenrikDev's API through the cloud service.
func (v *Valorant) get(path string) (any, error) {
	r, err := v.cloud.call("action", "valorant:get", map[string]any{"path": path}, "")
	if err != nil {
		if strings.Contains(err.Error(), "Could not find public function") || strings.Contains(err.Error(), "Server Error") {
			return nil, errValNotSetUp
		}
		return nil, err
	}
	m, _ := r.(map[string]any)
	if !jBool(m, "ok") {
		switch reason := jStr(m, "reason", ""); reason {
		case "not_set_up":
			return nil, errValNotSetUp
		case "not_found":
			return nil, errors.New("Riot doesn't know that player. Check the name and the tag after the #.")
		case "rate_limited":
			return nil, errors.New("The Valorant API is busy for a minute. Try again shortly.")
		case "bad_key":
			return nil, errors.New("HenrikDev refused the cloud service's API key.")
		default:
			return nil, fmt.Errorf("The Valorant API answered: %s", reason)
		}
	}
	return m["data"], nil
}

// Find looks up a Riot ID ("Name#TAG").
func (v *Valorant) Find(riotID string) (ValLink, error) {
	name, tag, ok := strings.Cut(strings.TrimSpace(riotID), "#")
	name, tag = strings.TrimSpace(name), strings.TrimSpace(tag)
	if !ok || name == "" || tag == "" || len(name) > 16 || len(tag) > 5 {
		return ValLink{}, errors.New("Type your Riot ID as it shows in Valorant, with the tag: Name#TAG.")
	}
	d, err := v.get(fmt.Sprintf("/valorant/v2/account/%s/%s", url.PathEscape(name), url.PathEscape(tag)))
	if err != nil {
		return ValLink{}, err
	}
	m, _ := d.(map[string]any)
	l := ValLink{Puuid: jStr(m, "puuid", ""), Name: jStr(m, "name", name), Tag: jStr(m, "tag", tag), Region: strings.ToLower(jStr(m, "region", "eu")),
		Platform: "pc", Card: jStr(m, "card", ""), Level: int(jI64(m, "account_level"))}
	if !valPuuid.MatchString(l.Puuid) {
		return ValLink{}, errors.New("Riot didn't return that account.")
	}
	if _, ok := valRegions[l.Region]; !ok {
		l.Region = "eu"
	}
	return l, nil
}

// ---------- Content: agents, maps, ranks ----------

type valAgent struct {
	Name string `json:"name"`
	Icon string `json:"icon"`
	Role string `json:"role"`
}

type valCallout struct {
	Name string  `json:"name"`
	X    float64 `json:"x"`
	Y    float64 `json:"y"`
}

type valMap struct {
	Name     string       `json:"name"`
	Image    string       `json:"image"`
	Callouts []valCallout `json:"callouts"`
}

type valTier struct {
	Name  string `json:"name"`
	Icon  string `json:"icon"`
	Color string `json:"color"`
}

type valContent struct {
	Agents map[string]valAgent `json:"agents"` // by lower-case name
	Maps   map[string]valMap   `json:"maps"`   // by lower-case name
	Tiers  map[int]valTier     `json:"tiers"`
}

func (v *Valorant) contentData() valContent {
	c, _, _ := cachedFetch(v.store, "val_content", 7*24*time.Hour, false, func() (valContent, error) {
		out := valContent{Agents: map[string]valAgent{}, Maps: map[string]valMap{}, Tiers: map[int]valTier{}}
		var agents struct {
			Data []struct {
				DisplayName string `json:"displayName"`
				DisplayIcon string `json:"displayIcon"`
				Role        *struct {
					DisplayName string `json:"displayName"`
				} `json:"role"`
			} `json:"data"`
		}
		if err := v.content.get("/v1/agents?isPlayableCharacter=true", &agents); err != nil {
			return out, err
		}
		for _, a := range agents.Data {
			ag := valAgent{Name: a.DisplayName, Icon: a.DisplayIcon}
			if a.Role != nil {
				ag.Role = a.Role.DisplayName
			}
			out.Agents[strings.ToLower(a.DisplayName)] = ag
		}
		var maps struct {
			Data []struct {
				DisplayName  string `json:"displayName"`
				ListViewIcon string `json:"listViewIcon"`
				Callouts     []struct {
					RegionName      string `json:"regionName"`
					SuperRegionName string `json:"superRegionName"`
					Location        struct{ X, Y float64 }
				} `json:"callouts"`
			} `json:"data"`
		}
		if err := v.content.get("/v1/maps", &maps); err == nil {
			for _, m := range maps.Data {
				vm := valMap{Name: m.DisplayName, Image: m.ListViewIcon}
				for _, c := range m.Callouts {
					vm.Callouts = append(vm.Callouts, valCallout{Name: calloutName(c.SuperRegionName, c.RegionName), X: c.Location.X, Y: c.Location.Y})
				}
				out.Maps[strings.ToLower(m.DisplayName)] = vm
			}
		}
		var tiers struct {
			Data []struct {
				Tiers []struct {
					Tier      int    `json:"tier"`
					TierName  string `json:"tierName"`
					SmallIcon string `json:"smallIcon"`
					Color     string `json:"color"`
				} `json:"tiers"`
			} `json:"data"`
		}
		if err := v.content.get("/v1/competitivetiers", &tiers); err == nil && len(tiers.Data) > 0 {
			for _, t := range tiers.Data[len(tiers.Data)-1].Tiers {
				out.Tiers[t.Tier] = valTier{Name: titleCase(t.TierName), Icon: t.SmallIcon, Color: "#" + strings.TrimSuffix(t.Color, "ff")}
			}
		}
		return out, nil
	})
	if c.Agents == nil {
		c.Agents = map[string]valAgent{}
	}
	return c
}

// calloutName says a place the way players call it: "A Main", "B Site",
// "Mid Top", "Attacker Spawn".
func calloutName(super, region string) string {
	if super == "" || strings.Contains(strings.ToLower(region), strings.ToLower(super)) {
		return region
	}
	return super + " " + region
}

// nearestCallout names the map place closest to a point (game units).
func (m valMap) nearestCallout(x, y float64) string {
	best, bestD := "", math.MaxFloat64
	for _, c := range m.Callouts {
		if d := math.Hypot(c.X-x, c.Y-y); d < bestD {
			best, bestD = c.Name, d
		}
	}
	return best
}

func titleCase(s string) string {
	words := strings.Fields(strings.ToLower(s))
	for i, w := range words {
		if w != "" {
			words[i] = strings.ToUpper(w[:1]) + w[1:]
		}
	}
	return strings.Join(words, " ")
}

// ---------- Matches ----------

type ValPlayer struct {
	Name      string  `json:"name"`
	Agent     string  `json:"agent"`
	Me        bool    `json:"me,omitempty"`
	Tier      string  `json:"tier,omitempty"`
	Kills     int     `json:"kills"`
	Deaths    int     `json:"deaths"`
	Assists   int     `json:"assists"`
	ACS       float64 `json:"acs"`
	ADR       float64 `json:"adr"`
	HSPct     float64 `json:"hsPct"`
	Damage    int     `json:"damage"`
	TeamOfMe  bool    `json:"mine,omitempty"`
	partyID   string
	teamID    string
	puuid     string
	headshots int
	shots     int
	score     int
}

// ValDeath is one of the player's deaths, with what's known about it.
type ValDeath struct {
	Round  int    `json:"round"`
	Clock  string `json:"clock"` // time into the round
	Killer string `json:"killer"`
	Agent  string `json:"agent"`
	Weapon string `json:"weapon"`
	Place  string `json:"place,omitempty"`
	First  bool   `json:"first,omitempty"`  // the round's first death
	Traded bool   `json:"traded,omitempty"` // a teammate killed the killer within 5 s
	// Distance to the nearest living teammate, in metres; -1 none alive.
	Mate float64 `json:"mate"`
	// The round was lost.
	Lost bool `json:"lost,omitempty"`
}

type ValMatch struct {
	ID          string      `json:"id"`
	StartTime   int64       `json:"startTime"`
	Duration    int         `json:"duration"`
	Map         string      `json:"map"`
	Mode        string      `json:"mode"`
	Ranked      bool        `json:"ranked"`
	Season      string      `json:"season,omitempty"`
	Completed   bool        `json:"completed"`
	Won         *bool       `json:"won"`
	TeamRounds  int         `json:"teamRounds"`
	EnemyRounds int         `json:"enemyRounds"`
	Rounds      int         `json:"rounds"`
	Agent       string      `json:"agent"`
	Tier        string      `json:"tier,omitempty"`
	Kills       int         `json:"kills"`
	Deaths      int         `json:"deaths"`
	Assists     int         `json:"assists"`
	ACS         float64     `json:"acs"`
	ADR         float64     `json:"adr"`
	HSPct       float64     `json:"hsPct"`
	KAST        float64     `json:"kast"`
	FirstBloods int         `json:"firstBloods"`
	FirstDeaths int         `json:"firstDeaths"`
	Clutches    int         `json:"clutches"`
	MultiKills  int         `json:"multiKills"` // rounds with 3 or more kills
	Plants      int         `json:"plants"`
	Defuses     int         `json:"defuses"`
	Utility     int         `json:"utility"` // ability casts
	TeamPlace   int         `json:"teamPlace"`
	MatchPlace  int         `json:"matchPlace"`
	Team        []ValPlayer `json:"team"`
	Enemies     []ValPlayer `json:"enemies"`
	DeathLog    []ValDeath  `json:"deathLog"`
}

// valMatchFrom reads one HenrikDev v4 match for one player.
func valMatchFrom(m jsonMap, puuid string, content valContent) (ValMatch, bool) {
	meta := sub(m, "metadata")
	out := ValMatch{ID: jStr(meta, "match_id", ""), Duration: int(jI64(meta, "game_length_in_ms") / 1000), Map: jStr(sub(meta, "map"), "name", ""),
		Mode: jStr(sub(meta, "queue"), "name", jStr(sub(meta, "queue"), "id", "")), Season: jStr(sub(meta, "season"), "short", ""),
		Completed: jBool(meta, "is_completed"), Team: []ValPlayer{}, Enemies: []ValPlayer{}, DeathLog: []ValDeath{}}
	out.Ranked = strings.EqualFold(jStr(sub(meta, "queue"), "id", ""), "competitive")
	if t, err := time.Parse(time.RFC3339, jStr(meta, "started_at", "")); err == nil {
		out.StartTime = t.Unix()
	}
	rounds := jList(m["rounds"])
	out.Rounds = max(len(rounds), 1)
	var me *ValPlayer
	players := []ValPlayer{}
	for _, p := range jList(m["players"]) {
		st := sub(p, "stats")
		vp := ValPlayer{Name: jStr(p, "name", ""), Agent: jStr(sub(p, "agent"), "name", ""), Tier: jStr(sub(p, "tier"), "name", ""),
			Kills: int(jI64(st, "kills")), Deaths: int(jI64(st, "deaths")), Assists: int(jI64(st, "assists")), Damage: int(jI64(sub(st, "damage"), "dealt")),
			score: int(jI64(st, "score")), headshots: int(jI64(st, "headshots")), puuid: jStr(p, "puuid", ""), teamID: jStr(p, "team_id", ""), partyID: jStr(p, "party_id", "")}
		vp.shots = vp.headshots + int(jI64(st, "bodyshots")) + int(jI64(st, "legshots"))
		vp.ACS, vp.ADR = round1(float64(vp.score)/float64(out.Rounds)), round1(float64(vp.Damage)/float64(out.Rounds))
		if vp.shots > 0 {
			vp.HSPct = round1(float64(vp.headshots) * 100 / float64(vp.shots))
		}
		if vp.puuid == puuid {
			vp.Me = true
			out.Utility = int(jI64(sub(p, "ability_casts"), "ability1") + jI64(sub(p, "ability_casts"), "ability2") + jI64(sub(p, "ability_casts"), "grenade") + jI64(sub(p, "ability_casts"), "ultimate"))
		}
		players = append(players, vp)
	}
	for i := range players {
		if players[i].Me {
			me = &players[i]
		}
	}
	if me == nil {
		return out, false
	}
	myTeam := me.teamID
	out.Agent, out.Tier, out.Kills, out.Deaths, out.Assists = me.Agent, me.Tier, me.Kills, me.Deaths, me.Assists
	out.ACS, out.ADR, out.HSPct = me.ACS, me.ADR, me.HSPct
	for _, t := range jList(m["teams"]) {
		r := sub(t, "rounds")
		if jStr(t, "team_id", "") == myTeam {
			out.TeamRounds, out.EnemyRounds = int(jI64(r, "won")), int(jI64(r, "lost"))
			if out.Completed && out.TeamRounds != out.EnemyRounds {
				won := jBool(t, "won")
				out.Won = &won
			}
		}
	}
	for _, p := range players {
		p.TeamOfMe = p.teamID == myTeam
		if p.TeamOfMe {
			out.Team = append(out.Team, p)
		} else {
			out.Enemies = append(out.Enemies, p)
		}
	}
	byACS := func(l []ValPlayer) { sort.SliceStable(l, func(i, j int) bool { return l[i].ACS > l[j].ACS }) }
	byACS(out.Team)
	byACS(out.Enemies)
	all := append(append([]ValPlayer{}, out.Team...), out.Enemies...)
	byACS(all)
	for i, p := range out.Team {
		if p.Me {
			out.TeamPlace = i + 1
		}
	}
	for i, p := range all {
		if p.Me {
			out.MatchPlace = i + 1
		}
	}

	// Round by round: first bloods, KAST, trades, clutches, multi-kills.
	vmap := content.Maps[strings.ToLower(out.Map)]
	teamOf := map[string]string{}
	for _, p := range players {
		teamOf[p.puuid] = p.teamID
	}
	agentOf, nameOf := map[string]string{}, map[string]string{}
	for _, p := range players {
		agentOf[p.puuid], nameOf[p.puuid] = p.Agent, p.Name
	}
	type kill struct {
		round, at      int
		killer, victim string
		weapon         string
		x, y           float64
		assists        []string
		mates          []jsonMap
	}
	byRound := map[int][]kill{}
	for _, k := range jList(m["kills"]) {
		kk := kill{round: int(jI64(k, "round")), at: int(jI64(k, "time_in_round_in_ms")), killer: jStr(sub(k, "killer"), "puuid", ""),
			victim: jStr(sub(k, "victim"), "puuid", ""), weapon: jStr(sub(k, "weapon"), "name", ""),
			x: jF64(sub(k, "location"), "x"), y: jF64(sub(k, "location"), "y"), mates: jList(k["player_locations"])}
		for _, a := range jList(k["assistants"]) {
			kk.assists = append(kk.assists, jStr(a, "puuid", ""))
		}
		byRound[kk.round] = append(byRound[kk.round], kk)
	}
	kastRounds := 0
	for ri, r := range rounds {
		id := int(jI64(r, "id"))
		if _, ok := r["id"]; !ok {
			id = ri
		}
		ks := byRound[id]
		sort.SliceStable(ks, func(i, j int) bool { return ks[i].at < ks[j].at })
		won := jStr(r, "winning_team", "") == myTeam
		if pl := sub(r, "plant"); pl != nil && jStr(sub(pl, "player"), "puuid", "") == puuid {
			out.Plants++
		}
		if df := sub(r, "defuse"); df != nil && jStr(sub(df, "player"), "puuid", "") == puuid {
			out.Defuses++
		}
		killed, assisted, died, traded := 0, false, false, false
		alive := map[string]int{}
		for _, p := range players {
			alive[p.teamID]++
		}
		clutch := false
		for i, k := range ks {
			if i == 0 {
				if k.killer == puuid {
					out.FirstBloods++
				}
				if k.victim == puuid {
					out.FirstDeaths++
				}
			}
			if k.killer == puuid && teamOf[k.victim] != myTeam {
				killed++
			}
			for _, a := range k.assists {
				if a == puuid {
					assisted = true
				}
			}
			alive[teamOf[k.victim]]--
			if k.victim == puuid {
				died = true
				d := ValDeath{Round: id + 1, Clock: FmtClock(float64(k.at) / 1000), Killer: nameOf[k.killer], Agent: agentOf[k.killer], Weapon: k.weapon,
					First: i == 0, Lost: !won, Mate: -1}
				if len(vmap.Callouts) > 0 {
					d.Place = vmap.nearestCallout(k.x, k.y)
				}
				for _, e := range ks[i+1:] {
					if e.at-k.at > 5000 {
						break
					}
					if e.victim == k.killer && teamOf[e.killer] == myTeam {
						d.Traded, traded = true, true
						break
					}
				}
				best := -1.0
				for _, pl := range k.mates {
					who := jStr(sub(pl, "player"), "puuid", "")
					if who == puuid || teamOf[who] != myTeam {
						continue
					}
					dist := math.Hypot(jF64(sub(pl, "location"), "x")-k.x, jF64(sub(pl, "location"), "y")-k.y) / 100
					if best < 0 || dist < best {
						best = dist
					}
				}
				d.Mate = math.Round(best)
				out.DeathLog = append(out.DeathLog, d)
			}
			// The last one standing for the team, against at least one.
			if !died && alive[myTeam] == 1 && teamOf[k.victim] == myTeam && alive[teamOfEnemy(teamOf, myTeam)] >= 1 {
				clutch = true
			}
		}
		if clutch && won {
			out.Clutches++
		}
		if killed >= 3 {
			out.MultiKills++
		}
		if killed > 0 || assisted || !died || traded {
			kastRounds++
		}
	}
	if len(rounds) > 0 {
		out.KAST = round1(float64(kastRounds) * 100 / float64(len(rounds)))
	}
	return out, true
}

func teamOfEnemy(teamOf map[string]string, mine string) string {
	for _, t := range teamOf {
		if t != mine {
			return t
		}
	}
	return ""
}

// ---------- Overview ----------

type ValRank struct {
	Tier        string        `json:"tier"`
	TierID      int           `json:"tierId"`
	Icon        string        `json:"icon,omitempty"`
	RR          int           `json:"rr"`
	LastChange  int           `json:"lastChange"`
	Elo         int           `json:"elo"`
	Peak        string        `json:"peak,omitempty"`
	PeakIcon    string        `json:"peakIcon,omitempty"`
	PeakSeason  string        `json:"peakSeason,omitempty"`
	Leaderboard int           `json:"leaderboard,omitempty"`
	Seasons     []ValSeason   `json:"seasons"`
	History     []ValRRChange `json:"history"`
}

type ValSeason struct {
	Season string `json:"season"`
	Games  int    `json:"games"`
	Wins   int    `json:"wins"`
	Tier   string `json:"tier"`
}

type ValRRChange struct {
	Date   int64  `json:"date"`
	Change int    `json:"change"`
	Tier   string `json:"tier"`
	RR     int    `json:"rr"`
	Map    string `json:"map"`
}

type ValOverview struct {
	Link      ValLink             `json:"link"`
	Rank      *ValRank            `json:"rank,omitempty"`
	Matches   []ValMatch          `json:"matches"`
	Agents    map[string]valAgent `json:"agents"`
	Maps      map[string]string   `json:"maps"` // name → picture
	Tiers     map[int]valTier     `json:"tiers"`
	Freshness Freshness           `json:"freshness"`
}

// Overview is the linked account's rank and its matches: the latest ten from
// the API and every one read before.
func (v *Valorant) Overview(force bool) (ValOverview, error) {
	link := v.Link()
	if link.Puuid == "" {
		return ValOverview{}, errors.New("No Riot account connected yet.")
	}
	content := v.contentData()
	ov, fresh, err := cachedFetch(v.store, "val_overview_"+safeKey(link.Puuid), 3*time.Minute, force, func() (ValOverview, error) {
		out := ValOverview{Link: link, Matches: []ValMatch{}}
		d, err := v.get(fmt.Sprintf("/valorant/v4/by-puuid/matches/%s/%s/%s?size=10", link.Region, link.Platform, link.Puuid))
		if err != nil {
			return out, err
		}
		for _, raw := range anyList(d) {
			if vm, ok := valMatchFrom(raw, link.Puuid, content); ok && vm.ID != "" {
				v.keep(link.Puuid, vm)
			}
		}
		out.Rank = v.rank(link, content)
		return out, nil
	})
	ov.Link, ov.Freshness = link, fresh
	ov.Matches = v.kept(link.Puuid)
	ov.Agents, ov.Tiers = content.Agents, content.Tiers
	ov.Maps = map[string]string{}
	for k, m := range content.Maps {
		ov.Maps[k] = m.Image
	}
	return ov, err
}

func anyList(v any) []jsonMap {
	return jList(v)
}

// keep saves a match for good and adds it to the player's list.
func (v *Valorant) keep(puuid string, m ValMatch) {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.store.WriteCache("val_match_"+safeKey(m.ID)+"_"+safeKey(puuid)[:8], m)
	var ids []string
	v.store.readJSON("valorant_matches_"+safeKey(puuid)[:8]+".json", &ids)
	for _, id := range ids {
		if id == m.ID {
			return
		}
	}
	ids = append([]string{m.ID}, ids...)
	if len(ids) > 200 {
		ids = ids[:200]
	}
	_ = v.store.writeJSON("valorant_matches_"+safeKey(puuid)[:8]+".json", ids)
}

// kept lists every match read for the player, newest first.
func (v *Valorant) kept(puuid string) []ValMatch {
	v.mu.Lock()
	var ids []string
	v.store.readJSON("valorant_matches_"+safeKey(puuid)[:8]+".json", &ids)
	v.mu.Unlock()
	out := []ValMatch{}
	for _, id := range ids {
		var m ValMatch
		if _, ok := v.store.ReadCacheStale("val_match_"+safeKey(id)+"_"+safeKey(puuid)[:8], &m); ok {
			out = append(out, m)
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].StartTime > out[j].StartTime })
	return out
}

func (v *Valorant) rank(link ValLink, content valContent) *ValRank {
	d, err := v.get(fmt.Sprintf("/valorant/v3/by-puuid/mmr/%s/%s/%s", link.Region, link.Platform, link.Puuid))
	if err != nil {
		return nil
	}
	m, _ := d.(map[string]any)
	cur := sub(m, "current")
	r := &ValRank{TierID: int(jI64(sub(cur, "tier"), "id")), Tier: jStr(sub(cur, "tier"), "name", ""), RR: int(jI64(cur, "rr")),
		LastChange: int(jI64(cur, "last_change")), Elo: int(jI64(cur, "elo")), Seasons: []ValSeason{}, History: []ValRRChange{}}
	r.Icon = content.Tiers[r.TierID].Icon
	if lp := sub(cur, "leaderboard_placement"); lp != nil {
		r.Leaderboard = int(jI64(lp, "rank"))
	}
	if pk := sub(m, "peak"); pk != nil {
		r.Peak = jStr(sub(pk, "tier"), "name", "")
		r.PeakIcon = content.Tiers[int(jI64(sub(pk, "tier"), "id"))].Icon
		r.PeakSeason = jStr(sub(pk, "season"), "short", "")
	}
	for _, s := range jList(m["seasonal"]) {
		r.Seasons = append(r.Seasons, ValSeason{Season: jStr(sub(s, "season"), "short", ""), Games: int(jI64(s, "games")), Wins: int(jI64(s, "wins")),
			Tier: jStr(sub(s, "end_tier"), "name", "")})
	}
	sort.SliceStable(r.Seasons, func(i, j int) bool { return r.Seasons[i].Season > r.Seasons[j].Season })
	if h, err := v.get(fmt.Sprintf("/valorant/v2/by-puuid/mmr-history/%s/%s/%s", link.Region, link.Platform, link.Puuid)); err == nil {
		hm, _ := h.(map[string]any)
		for _, e := range jList(hm["history"]) {
			c := ValRRChange{Change: int(jI64(e, "last_change")), Tier: jStr(sub(e, "tier"), "name", ""), RR: int(jI64(e, "rr")), Map: jStr(sub(e, "map"), "name", "")}
			if t, err := time.Parse(time.RFC3339, jStr(e, "date", "")); err == nil {
				c.Date = t.Unix()
			}
			r.History = append(r.History, c)
		}
	}
	return r
}

// ---------- Leaderboard ----------

type ValBoardRow struct {
	Rank   int    `json:"rank"`
	Name   string `json:"name"`
	RR     int    `json:"rr"`
	Wins   int    `json:"wins"`
	Tier   string `json:"tier"`
	Me     bool   `json:"me,omitempty"`
	Hidden bool   `json:"hidden,omitempty"`
}

type ValBoard struct {
	Region string        `json:"region"`
	Rows   []ValBoardRow `json:"rows"`
}

func (v *Valorant) Leaderboard(region string) (ValBoard, error) {
	if _, ok := valRegions[region]; !ok {
		region = "eu"
	}
	content := v.contentData()
	me := v.Link()
	b, _, err := cachedFetch(v.store, "val_board_"+region, 30*time.Minute, false, func() (ValBoard, error) {
		out := ValBoard{Region: region, Rows: []ValBoardRow{}}
		d, err := v.get(fmt.Sprintf("/valorant/v3/leaderboard/%s/pc?size=200&page=1", region))
		if err != nil {
			return out, err
		}
		m, _ := d.(map[string]any)
		for _, p := range jList(m["players"]) {
			row := ValBoardRow{Rank: int(jI64(p, "leaderboard_rank")), RR: int(jI64(p, "rr")), Wins: int(jI64(p, "wins")),
				Tier: content.Tiers[int(jI64(p, "tier"))].Name, Hidden: jBool(p, "is_anonymized")}
			row.Name = jStr(p, "name", "")
			if tag := jStr(p, "tag", ""); tag != "" && row.Name != "" {
				row.Name += "#" + tag
			}
			if row.Hidden || row.Name == "" {
				row.Name = "Secret Agent"
			}
			out.Rows = append(out.Rows, row)
		}
		return out, nil
	})
	for i := range b.Rows {
		b.Rows[i].Me = me.Name != "" && strings.EqualFold(b.Rows[i].Name, me.Name+"#"+me.Tag)
	}
	return b, err
}

// ValorantStreams lists live Valorant streams on Twitch.
func (a *App) ValorantStreams() map[string]any {
	streams, reason := a.liveStreams("valorant", false)
	if streams == nil {
		streams = []LiveStream{}
	}
	return map[string]any{"streams": streams, "reason": reason}
}
