package core

// The PUBG match review, from the match's telemetry: the event log PUBG
// publishes for every match on its CDN (no key needed). Read once per match
// and boiled down to what a review needs: where the player landed and who
// else did, each time they were knocked or killed (by what, from how far,
// in or out of the zone, how far their squad was), the knocks they made and
// finished, the damage the blue zone did, and the damage by weapon.

import (
	"compress/gzip"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"sort"
	"strings"
	"time"
)

type pgLoc struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
}

type pgChar struct {
	Name        string   `json:"name"`
	TeamID      int      `json:"teamId"`
	AccountID   string   `json:"accountId"`
	Location    pgLoc    `json:"location"`
	IsInBlue    bool     `json:"isInBlueZone"`
	IsInVehicle bool     `json:"isInVehicle"`
	Zone        []string `json:"zone"`
}

type pgDamage struct {
	Category string  `json:"damageTypeCategory"`
	Causer   string  `json:"damageCauserName"`
	Distance float64 `json:"distance"`
}

// pgEvent holds the fields of every event type the review reads.
type pgEvent struct {
	T         string  `json:"_T"`
	D         string  `json:"_D"`
	Character *pgChar `json:"character"`
	// Damage, knocks, kills.
	Attacker  *pgChar  `json:"attacker"`
	Victim    *pgChar  `json:"victim"`
	Killer    *pgChar  `json:"killer"`
	Finisher  *pgChar  `json:"finisher"`
	DBNOMaker *pgChar  `json:"dBNOMaker"`
	Damage    float64  `json:"damage"`
	Category  string   `json:"damageTypeCategory"`
	Causer    string   `json:"damageCauserName"`
	Distance  float64  `json:"distance"`
	KillInfo  pgDamage `json:"killerDamageInfo"`
	KnockInfo pgDamage `json:"dBNODamageInfo"`
	DBNOID    int      `json:"dBNOId"`
	// Periodic game state.
	GameState *struct {
		ElapsedTime  float64 `json:"elapsedTime"`
		SafePos      pgLoc   `json:"safetyZonePosition"`
		SafeRadius   float64 `json:"safetyZoneRadius"`
		AliveTeams   int     `json:"numAliveTeams"`
		AlivePlayers int     `json:"numAlivePlayers"`
	} `json:"gameState"`
	Common *struct {
		IsGame float64 `json:"isGame"`
	} `json:"common"`
}

// PgDeath is one knock or death of the player.
type PgDeath struct {
	At       float64 `json:"at"` // seconds since the match started
	Knock    bool    `json:"knock"`
	By       string  `json:"by,omitempty"`
	Weapon   string  `json:"weapon,omitempty"`
	Category string  `json:"category,omitempty"`
	Distance float64 `json:"distance"` // metres
	InBlue   bool    `json:"inBlue,omitempty"`
	OutSafe  bool    `json:"outSafe,omitempty"` // outside the white circle
	Vehicle  bool    `json:"vehicle,omitempty"`
	// Nearest teammate still standing, metres; -1 when none was.
	Mate  float64 `json:"mate"`
	Phase float64 `json:"phase"`
	Place string  `json:"place,omitempty"`
}

// PgFacts is a match's telemetry boiled down for the player.
type PgFacts struct {
	LandedAt    float64 `json:"landedAt"`
	LandedPlace string  `json:"landedPlace,omitempty"`
	// When the landing was away from named places: the first one reached.
	FirstPlace   string             `json:"firstPlace,omitempty"`
	FirstPlaceAt float64            `json:"firstPlaceAt,omitempty"`
	NearbyLand   int                `json:"nearbyLand"` // enemies landing within 300 m
	Downs        []PgDeath          `json:"downs"`
	KnocksMade   int                `json:"knocksMade"`
	KnocksKilled int                `json:"knocksKilled"` // of those, finished by the squad
	BlueDamage   float64            `json:"blueDamage"`
	DamageTaken  float64            `json:"damageTaken"`
	ByWeapon     map[string]float64 `json:"byWeapon"`
	TeamWiped    bool               `json:"teamWiped"`
	Duration     float64            `json:"duration"`
}

// pubgTelemetryFetch reads a telemetry file; swapped in tests.
var pubgTelemetryFetch = func(url string) (io.ReadCloser, error) {
	if !strings.HasPrefix(url, "https://telemetry-cdn.pubg.com/") {
		return nil, errors.New("That isn't PUBG's telemetry address.")
	}
	req, _ := http.NewRequest(http.MethodGet, url, nil)
	req.Header.Set("Accept-Encoding", "gzip")
	req.Header.Set("User-Agent", userAgent())
	resp, err := (&http.Client{Timeout: 2 * time.Minute}).Do(req)
	if err != nil {
		return nil, errors.New("PUBG's match data couldn't be downloaded.")
	}
	if resp.StatusCode != 200 {
		resp.Body.Close()
		return nil, fmt.Errorf("PUBG's match data answered HTTP %d.", resp.StatusCode)
	}
	if resp.Header.Get("Content-Encoding") == "gzip" {
		gz, err := gzip.NewReader(resp.Body)
		if err != nil {
			resp.Body.Close()
			return nil, err
		}
		return struct {
			io.Reader
			io.Closer
		}{gz, resp.Body}, nil
	}
	return resp.Body, nil
}

func pgDist(a, b pgLoc) float64 { return math.Hypot(a.X-b.X, a.Y-b.Y) / 100 }

// pgFactsFrom reads the telemetry stream for one account.
func pgFactsFrom(r io.Reader, accountID string) (PgFacts, error) {
	dec := json.NewDecoder(r)
	if _, err := dec.Token(); err != nil {
		return PgFacts{}, err
	}
	f := PgFacts{ByWeapon: map[string]float64{}, Downs: []PgDeath{}}
	var start time.Time
	myTeam := -1
	pos := map[string]pgLoc{}  // last known location, by account
	alive := map[string]bool{} // standing (not knocked or dead), by account
	team := map[string]int{}
	var safe pgLoc
	var safeR float64
	landed := map[string]pgLoc{}
	knockedBy := map[int]bool{} // dBNO ids the player made
	at := func(e pgEvent) float64 {
		t, err := time.Parse(time.RFC3339Nano, e.D)
		if err != nil || start.IsZero() {
			return 0
		}
		return t.Sub(start).Seconds()
	}
	see := func(c *pgChar) {
		if c == nil || c.AccountID == "" {
			return
		}
		pos[c.AccountID], team[c.AccountID] = c.Location, c.TeamID
		if c.AccountID == accountID {
			myTeam = c.TeamID
		}
	}
	mateNear := func(me pgLoc) float64 {
		best := -1.0
		for acc, tid := range team {
			if acc == accountID || tid != myTeam || !alive[acc] {
				continue
			}
			if d := pgDist(me, pos[acc]); best < 0 || d < best {
				best = d
			}
		}
		return best
	}
	down := func(e pgEvent, victim, by *pgChar, info pgDamage, knock bool) {
		d := PgDeath{At: at(e), Knock: knock, Weapon: pgWeaponName(info.Causer), Category: info.Category, Distance: info.Distance / 100,
			InBlue: victim.IsInBlue, Vehicle: victim.IsInVehicle, Mate: mateNear(victim.Location)}
		if by != nil && by.AccountID != accountID {
			d.By = by.Name
		}
		if e.Common != nil {
			d.Phase = e.Common.IsGame
		}
		if safeR > 0 && pgDist(victim.Location, safe) > safeR/100 {
			d.OutSafe = true
		}
		if len(victim.Zone) > 0 {
			d.Place = pgPlaceName(victim.Zone[0])
		}
		f.Downs = append(f.Downs, d)
	}
	for dec.More() {
		var e pgEvent
		if err := dec.Decode(&e); err != nil {
			return f, err
		}
		switch e.T {
		case "LogMatchStart":
			start, _ = time.Parse(time.RFC3339Nano, e.D)
		case "LogPlayerPosition", "LogParachuteLanding":
			see(e.Character)
			if c := e.Character; c != nil {
				if _, ok := alive[c.AccountID]; !ok {
					alive[c.AccountID] = true
				}
				if e.T == "LogParachuteLanding" {
					landed[c.AccountID] = c.Location
					if c.AccountID == accountID {
						f.LandedAt = at(e)
						if len(c.Zone) > 0 {
							f.LandedPlace = pgPlaceName(c.Zone[0])
						}
					}
				}
			}
		case "LogGameStatePeriodic":
			if g := e.GameState; g != nil {
				safe, safeR = g.SafePos, g.SafeRadius
			}
		case "LogPlayerTakeDamage":
			see(e.Attacker)
			see(e.Victim)
			if e.Victim != nil && e.Victim.AccountID == accountID {
				f.DamageTaken += e.Damage
				if e.Category == "Damage_BlueZone" {
					f.BlueDamage += e.Damage
				}
			}
			if e.Attacker != nil && e.Attacker.AccountID == accountID && e.Victim != nil && e.Victim.TeamID != e.Attacker.TeamID {
				f.ByWeapon[pgWeaponName(e.Causer)] += e.Damage
			}
		case "LogPlayerMakeGroggy":
			see(e.Attacker)
			see(e.Victim)
			if e.Victim != nil {
				alive[e.Victim.AccountID] = false
				if e.Victim.AccountID == accountID {
					down(e, e.Victim, e.Attacker, pgDamage{Category: e.Category, Causer: e.Causer, Distance: e.Distance}, true)
				}
			}
			if e.Attacker != nil && e.Attacker.AccountID == accountID && e.Victim != nil && e.Victim.TeamID != e.Attacker.TeamID {
				f.KnocksMade++
				knockedBy[e.DBNOID] = true
			}
		case "LogPlayerRevive":
			if e.Victim != nil {
				alive[e.Victim.AccountID] = true
			}
		case "LogPlayerKillV2", "LogPlayerKill":
			see(e.Victim)
			if e.Victim == nil {
				continue
			}
			alive[e.Victim.AccountID] = false
			if e.Victim.AccountID == accountID {
				by, info := e.Killer, e.KillInfo
				if by == nil && e.Finisher != nil {
					by = e.Finisher
				}
				if info.Causer == "" {
					info = e.KnockInfo
				}
				down(e, e.Victim, by, info, false)
			}
			if knockedBy[e.DBNOID] && e.DBNOID != 0 {
				f.KnocksKilled++
			}
		case "LogMatchEnd":
			f.Duration = at(e)
		}
		// Real telemetry often has no place on the landing itself: take the
		// first named place the player is seen in during the next 90 s.
		if f.LandedPlace == "" && f.FirstPlace == "" && f.LandedAt > 0 {
			for _, c := range []*pgChar{e.Character, e.Attacker, e.Victim} {
				if c == nil || c.AccountID != accountID || len(c.Zone) == 0 {
					continue
				}
				if t := at(e); t-f.LandedAt <= 90 {
					f.LandedPlace = pgPlaceName(c.Zone[0])
				} else {
					f.FirstPlace, f.FirstPlaceAt = pgPlaceName(c.Zone[0]), t
				}
				break
			}
		}
	}
	if myTeam >= 0 {
		f.TeamWiped = true
		for acc, tid := range team {
			if tid == myTeam && alive[acc] {
				f.TeamWiped = false
			}
		}
	}
	if me, ok := landed[accountID]; ok {
		for acc, loc := range landed {
			if team[acc] != myTeam && pgDist(me, loc) <= 300 {
				f.NearbyLand++
			}
		}
	}
	return f, nil
}

// pgWeaponName turns telemetry's causer names into the game's names.
func pgWeaponName(causer string) string {
	known := map[string]string{
		"WeapHK416_C": "M416", "WeapAK47_C": "AKM", "WeapBerylM762_C": "Beryl M762", "WeapSCAR-L_C": "SCAR-L", "WeapM16A4_C": "M16A4",
		"WeapAUG_C": "AUG", "WeapGroza_C": "Groza", "WeapQBZ95_C": "QBZ", "WeapG36C_C": "G36C", "WeapMk47Mutant_C": "Mk47 Mutant",
		"WeapACE32_C": "ACE32", "WeapK2_C": "K2", "WeapFamasG2_C": "FAMAS", "WeapKar98k_C": "Kar98k", "WeapM24_C": "M24", "WeapAWM_C": "AWM",
		"WeapMosinNagant_C": "Mosin Nagant", "WeapWin1894_C": "Win94", "WeapLynxAMR_C": "Lynx AMR", "WeapMini14_C": "Mini 14", "WeapSKS_C": "SKS",
		"WeapMk14_C": "Mk14", "WeapMk12_C": "Mk12", "WeapFNFal_C": "SLR", "WeapQBU88_C": "QBU", "WeapVSS_C": "VSS", "WeapDragunov_C": "Dragunov",
		"WeapUMP_C": "UMP45", "WeapVector_C": "Vector", "WeapUZI_C": "Micro UZI", "WeapThompson_C": "Tommy Gun", "WeapBizonPP19_C": "PP-19 Bizon",
		"WeapMP5K_C": "MP5K", "WeapP90_C": "P90", "WeapJS9_C": "JS9", "WeapM249_C": "M249", "WeapDP28_C": "DP-28", "WeapMG3_C": "MG3",
		"WeapSaiga12_C": "S12K", "WeapBerreta686_C": "S686", "WeapWinchester_C": "S1897", "WeapDP12_C": "DBS", "WeapOriginS12_C": "O12",
		"WeapCrossbow_1_C": "Crossbow", "WeapPanzerFaust100M1_C": "Panzerfaust", "WeapMortar_C": "Mortar",
		"ProjGrenade_C": "Frag grenade", "ProjMolotov_C": "Molotov", "ProjC4_C": "C4", "ProjStickyGrenade_C": "Sticky bomb",
		"PlayerMale_A_C": "Fists", "PlayerFemale_A_C": "Fists", "WeapPan_C": "Pan", "WeapMachete_C": "Machete", "WeapSickle_C": "Sickle", "WeapCowbar_C": "Crowbar",
		"BlueZoneBomb_EffectActor_C": "Blue zone grenade", "RedZoneBomb_C": "Red zone", "BP_Mirado_A_01_C": "Vehicle", "Bluezonebomb_EffectActor_C": "Blue zone grenade",
	}
	if n, ok := known[causer]; ok {
		return n
	}
	switch {
	case causer == "":
		return ""
	case strings.HasPrefix(causer, "Weap"):
		return strings.TrimSuffix(strings.TrimPrefix(causer, "Weap"), "_C")
	case strings.HasPrefix(causer, "BP_") || strings.Contains(causer, "Vehicle"):
		return "Vehicle"
	}
	return strings.TrimSuffix(causer, "_C")
}

func pgClock(s float64) string { return FmtClock(s) }

// ---------- The review ----------

// pubgReviewFacts reads (once) a match's telemetry facts.
func (p *Pubg) reviewFacts(m PubgMatch, accountID string) (PgFacts, error) {
	key := "pubg_facts2_" + safeKey(m.ID)
	var f PgFacts
	if _, ok := p.store.ReadCacheStale(key, &f); ok && f.ByWeapon != nil {
		return f, nil
	}
	if m.Telemetry == "" {
		return f, errors.New("This match has no telemetry link.")
	}
	body, err := pubgTelemetryFetch(m.Telemetry)
	if err != nil {
		return f, err
	}
	defer body.Close()
	f, err = pgFactsFrom(body, accountID)
	if err != nil {
		return f, fmt.Errorf("PUBG's match data couldn't be read: %v", err)
	}
	p.store.WriteCache(key, f)
	return f, nil
}

// PubgReview reviews one match ("" means the latest).
func (a *App) PubgReview(matchKey string) (MatchReview, error) {
	link := a.Pubg.Link()
	if link.AccountID == "" {
		return MatchReview{}, errors.New("Connect your PUBG account to review matches.")
	}
	ov, err := a.Pubg.Overview(false)
	if err != nil {
		return MatchReview{}, err
	}
	var m *PubgMatch
	for i := range ov.Matches {
		mm := &ov.Matches[i]
		if mm.Custom {
			continue
		}
		if matchKey == "" || mm.ID == matchKey {
			m = mm
			break
		}
	}
	if m == nil {
		return MatchReview{}, errNoReviewMatch
	}
	won := m.Place == 1
	r := MatchReview{Game: "pubg", MatchKey: m.ID, Hero: m.Map + " · " + m.ModeName, Won: &won, Duration: m.Duration, StartTime: m.StartTime,
		Kills: m.Kills, Deaths: 0, Assists: m.Assists, Wrong: []ReviewPoint{}, Good: []ReviewPoint{}, DeathLog: []ReviewDeath{}, Notes: []string{}}
	var wrong, good []ReviewPoint
	add := func(kind, title, detail, habit string, w, v float64) {
		wrong = append(wrong, ReviewPoint{Kind: kind, Title: title, Detail: detail, Habit: habit, Weight: w, Value: &v})
	}
	r.Facts = append(r.Facts, fmt.Sprintf("Placed #%d of %d, alive %s of %s", m.Place, m.Teams, FmtClock(m.Survived), FmtClock(float64(m.Duration))))
	if m.Place == 1 {
		good = append(good, ReviewPoint{Kind: "dinner", Title: "Chicken dinner", Detail: fmt.Sprintf("Won with %d kill%s and %.0f damage", m.Kills, plural(m.Kills), m.Damage), Weight: 1})
	} else if m.Place <= 10 {
		good = append(good, ReviewPoint{Kind: "top10", Title: "Top 10", Detail: fmt.Sprintf("#%d of %d", m.Place, m.Teams), Weight: 0.5})
	}
	// Against the player's own other matches.
	var others []PubgMatch
	for _, o := range ov.Matches {
		if !o.Custom && o.ID != m.ID {
			others = append(others, o)
		}
	}
	if len(others) >= 5 {
		dmg := make([]float64, 0, len(others))
		for _, o := range others {
			dmg = append(dmg, o.Damage)
		}
		sort.Float64s(dmg)
		if q := quantile(dmg, 0.25); m.Damage < q && m.Survived > 600 {
			add("low_damage", "Little damage for the time alive", fmt.Sprintf("%.0f damage in %s alive; your usual is %.0f+", m.Damage, FmtClock(m.Survived), quantile(dmg, 0.5)),
				"Take the fights you can win from cover: an alive player who never shoots doesn't help the squad.", 0.3, m.Damage)
		} else if q := quantile(dmg, 0.75); m.Damage >= q && m.Damage > 0 {
			good = append(good, ReviewPoint{Kind: "damage", Title: "Big damage", Detail: fmt.Sprintf("%.0f damage, more than three quarters of your recent matches", m.Damage), Weight: 0.7})
		}
	}
	f, err := a.Pubg.reviewFacts(*m, link.AccountID)
	if err != nil {
		r.Notes = append(r.Notes, err.Error()+" The review uses the match totals only.")
	} else {
		if f.LandedPlace != "" || f.LandedAt > 0 {
			where := "Landed at " + f.LandedPlace
			if f.LandedPlace == "" {
				where = "Landed away from the named places"
				if f.FirstPlace != "" {
					where = fmt.Sprintf("Landed away from the named places (reached %s at %s)", f.FirstPlace, FmtClock(f.FirstPlaceAt))
				}
			}
			r.Facts = append(r.Facts, fmt.Sprintf("%s at %s, %d enemies landed within 300 m", where, FmtClock(f.LandedAt), f.NearbyLand))
		}
		for _, d := range f.Downs {
			rd := ReviewDeath{Clock: FmtClock(d.At), Killer: d.By, Region: d.Place}
			what := "knocked"
			if !d.Knock {
				what = "killed"
			}
			parts := []string{what}
			if d.Weapon != "" {
				parts = append(parts, "by "+d.Weapon)
			}
			if d.Distance >= 1 {
				parts = append(parts, fmt.Sprintf("from %.0f m", d.Distance))
			}
			rd.Fight = strings.Join(parts, " ")
			var notes []string
			if d.Mate < 0 {
				notes = append(notes, "no teammate standing")
			} else if d.Mate > 0 {
				notes = append(notes, fmt.Sprintf("nearest teammate %.0f m away", d.Mate))
			}
			if d.InBlue {
				notes = append(notes, "in the blue zone")
			} else if d.OutSafe {
				notes = append(notes, "outside the white circle")
			}
			if d.Vehicle {
				notes = append(notes, "in a vehicle")
			}
			rd.Note = strings.Join(notes, ", ")
			r.DeathLog = append(r.DeathLog, rd)
			if !d.Knock {
				r.Deaths++
			}
		}
		// The death that ended the match, if any.
		var last *PgDeath
		for i := range f.Downs {
			if !f.Downs[i].Knock {
				last = &f.Downs[i]
			}
		}
		early := last != nil && last.At-f.LandedAt < 240 && f.LandedAt > 0
		if early && f.NearbyLand >= 3 {
			add("hot_drop", "Died in a contested drop", fmt.Sprintf("Dead %s after landing with %d enemies around", FmtClock(last.At-f.LandedAt), f.NearbyLand),
				"Land where you can loot a weapon in the first 20 seconds, or a little away from the crowd and take the fight on your terms.", 0.55, float64(f.NearbyLand))
		} else if last != nil && f.LandedAt > 0 && last.At-f.LandedAt < 300 && m.Place*2 > m.Teams {
			add("early_death", "Out in the first minutes", fmt.Sprintf("Dead %s after landing, with %.0f damage dealt", FmtClock(last.At-f.LandedAt), m.Damage),
				"The first minutes decide whether you play the match at all: loot with a teammate in sight and cross open ground only after checking the ridges and windows around it.", 0.5, last.At-f.LandedAt)
		}
		// Knocked or killed by a gun from far away: caught in the open.
		for _, d := range f.Downs {
			if d.Distance >= 80 && d.Weapon != "" && !d.Vehicle {
				what := "Knocked"
				if !d.Knock {
					what = "Killed"
				}
				add("range", "Picked off at range", fmt.Sprintf("%s by %s from %.0f m", what, d.Weapon, d.Distance),
					"Rotate cover to cover: run the low ground or behind a ridge, and smoke the open stretches you can't avoid.", 0.3, d.Distance)
				break
			}
		}
		far := 0
		for _, d := range f.Downs {
			if d.Mate > 150 || (d.Mate < 0 && !f.TeamWiped) {
				far++
			}
		}
		if far >= 1 && m.Mode != "solo" && m.Mode != "solo-fpp" {
			add("alone", "Caught away from your squad", fmt.Sprintf("%d of your knocks or deaths with no teammate within 150 m", far),
				"Move with your squad and stay within revive distance; trade the solo loot run for being there when the fight starts.", 0.45+0.1*float64(far), float64(far))
		}
		if f.BlueDamage >= 40 {
			add("blue_zone", "Damage from the blue zone", fmt.Sprintf("%.0f health lost to the zone", f.BlueDamage),
				"Start rotating when the next circle shows, not when the blue arrives: arrive early and hold the edge.", 0.35+f.BlueDamage/400, f.BlueDamage)
		}
		for _, d := range f.Downs {
			if d.Vehicle {
				add("vehicle", "Caught in a vehicle", "Knocked or killed while driving or riding",
					"Leave the vehicle before you reach the fight; park behind cover and walk the last stretch.", 0.35, 1)
				break
			}
		}
		if f.KnocksMade >= 3 && f.KnocksKilled*2 < f.KnocksMade {
			add("unfinished", "Knocks not finished", fmt.Sprintf("%d knocks, %d finished", f.KnocksMade, f.KnocksKilled),
				"When you knock someone and their team is far, finish them; if their team is close, hold the angle for the revive.", 0.3, float64(f.KnocksMade-f.KnocksKilled))
		} else if f.KnocksMade >= 3 {
			good = append(good, ReviewPoint{Kind: "knocks", Title: "Knocks finished", Detail: fmt.Sprintf("%d knocks, %d finished", f.KnocksMade, f.KnocksKilled), Weight: 0.5})
		}
		if len(f.ByWeapon) > 0 {
			type wd struct {
				w string
				d float64
			}
			var ws []wd
			for w, d := range f.ByWeapon {
				if w != "" {
					ws = append(ws, wd{w, d})
				}
			}
			sort.Slice(ws, func(i, j int) bool { return ws[i].d > ws[j].d })
			var parts []string
			for i, x := range ws {
				if i == 3 {
					break
				}
				parts = append(parts, fmt.Sprintf("%s %.0f", x.w, x.d))
			}
			r.Facts = append(r.Facts, "Damage by weapon: "+strings.Join(parts, ", "))
		}
		if f.DamageTaken > 0 {
			r.Facts = append(r.Facts, fmt.Sprintf("Damage taken %.0f, dealt %.0f", f.DamageTaken, m.Damage))
		}
	}
	// The goals from the player's own recent matches, checked against this one.
	if set, err := a.PubgGoals(); err == nil && len(set.Goals) > 0 {
		checked := checkPubgGoals(set, *m)
		r.Goals = &checked
	}
	r.Wrong, r.Good = sortPoints(wrong, 4), sortPoints(good, 3)
	r.Verdict = pubgVerdict(*m, r.Wrong, r.Good)
	return r, nil
}

func pubgVerdict(m PubgMatch, wrong, good []ReviewPoint) string {
	res := fmt.Sprintf("#%d of %d", m.Place, m.Teams)
	if m.Place == 1 {
		res = "Chicken dinner"
	}
	lower := func(s string) string {
		if s == "" {
			return s
		}
		return strings.ToLower(s[:1]) + s[1:]
	}
	switch {
	case len(wrong) > 0 && len(good) > 0:
		return fmt.Sprintf("%s. Biggest issue: %s. Best part: %s.", res, lower(wrong[0].Title), lower(good[0].Title))
	case len(wrong) > 0:
		return fmt.Sprintf("%s. Biggest issue: %s.", res, lower(wrong[0].Title))
	case len(good) > 0:
		return fmt.Sprintf("%s. Nothing stood out as a mistake. Best part: %s.", res, lower(good[0].Title))
	}
	return res + ". Nothing stood out either way."
}

// PubgWorkOn tallies the issues of the last ten matches.
func (a *App) PubgWorkOn() (WorkOn, error) {
	ov, err := a.Pubg.Overview(false)
	if err != nil {
		return WorkOn{}, err
	}
	var reviews []MatchReview
	n := 0
	for _, m := range ov.Matches {
		if m.Custom || n >= 10 {
			continue
		}
		n++
		if r, err := a.PubgReview(m.ID); err == nil {
			reviews = append(reviews, r)
		}
	}
	return tallyWorkOn("pubg", reviews), nil
}

// pgPlaceNames spells telemetry's place keys the way the maps do.
var pgPlaceNames = map[string]string{
	"yasnayapolyana": "Yasnaya Polyana", "militarybase": "Military Base", "myltapower": "Mylta Power", "novorepnoye": "Novorepnoye",
	"shootingrange": "Shooting Range", "losleones": "Los Leones", "elpozo": "El Pozo", "sanmartin": "San Martín", "elazahar": "El Azahar",
	"montenuevo": "Monte Nuevo", "cruzdelvalle": "Cruz del Valle", "lahacienda": "La Hacienda", "lacobreria": "La Cobrería",
	"tierrabronca": "Tierra Bronca", "pecado": "Pecado", "chumacera": "Chumacera", "campojunction": "Campo Junction",
	"bootcamp": "Bootcamp", "paradiseresort": "Paradise Resort", "ruins": "Ruins", "hatinh": "Ha Tinh", "tatmok": "Tat Mok",
	"podvosto": "Podvosto", "goroka": "Goroka", "castle": "Castle", "cosmodrome": "Cosmodrome", "villa": "Villa",
	"dinoland": "Dino Park", "dinopark": "Dino Park", "volnova": "Volnova", "milnar": "Milnar",
}

// pgPlaceName names a place; unknown keys are capitalised.
func pgPlaceName(key string) string {
	if n, ok := pgPlaceNames[strings.ToLower(key)]; ok {
		return n
	}
	if key == "" {
		return ""
	}
	return strings.ToUpper(key[:1]) + key[1:]
}
