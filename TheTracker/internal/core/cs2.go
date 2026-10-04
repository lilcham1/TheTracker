package core

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Counter-Strike 2, from Valve's Game State Integration — the same official
// local feed Dota uses. The game posts the local player's own state to this
// PC while a match runs: map, score, each round's kills and damage, the
// weapon in hand.
//
// Valve publishes no match-history API for CS2 that works without each
// player's private match-sharing code, so matches are what the app recorded
// live; one played while the app was closed is not recoverable.

const (
	cs2AppID   = 730
	cs2CfgName = "gamestate_integration_thetracker.cfg"
)

func cs2ConfigText(port int, token string) string {
	return fmt.Sprintf(`"TheTracker"
{
    "uri"           "http://127.0.0.1:%d/"
    "timeout"       "5.0"
    "buffer"        "0.1"
    "throttle"      "0.5"
    "heartbeat"     "30.0"
    "auth"
    {
        "token"         "%s"
    }
    "data"
    {
        "provider"              "1"
        "map"                   "1"
        "map_round_wins"        "1"
        "round"                 "1"
        "player_id"             "1"
        "player_state"          "1"
        "player_match_stats"    "1"
        "player_weapons"        "1"
    }
}
`, port, token)
}

// Cs2Round is one finished round, from the player's side of it.
type Cs2Round struct {
	N      int    `json:"n"`
	Won    bool   `json:"won"`
	Side   string `json:"side"` // CT | T
	Kills  int    `json:"kills"`
	HS     int    `json:"hs"`
	Damage int    `json:"damage"`
	Died   bool   `json:"died"`
	// What the player carried into the round, in dollars, and what kind of
	// buy that makes it: pistol | eco | force | full. Empty when the round
	// was joined too late to see.
	Equip int    `json:"equip,omitempty"`
	Buy   string `json:"buy,omitempty"`
	// planted | exploded | defused, when the bomb went down.
	Bomb string `json:"bomb,omitempty"`
	// How the round was decided: elimination | bomb | defuse | time.
	End string `json:"end,omitempty"`
}

// cs2Buy names a buy from the value of what the player is carrying. The
// first round of a half is a pistol round only while money is short: an
// overtime half starts rich.
func cs2Buy(equip int, firstOfHalf bool) string {
	switch {
	case equip <= 0:
		return ""
	case firstOfHalf && equip < 1500:
		return "pistol"
	case equip < 1500:
		return "eco"
	case equip < 3500:
		return "force"
	}
	return "full"
}

// cs2End reads the game's own reason for a round, like "ct_win_defuse".
func cs2End(reason, bomb string) string {
	if _, why, ok := strings.Cut(reason, "_win_"); ok {
		switch why {
		case "elimination", "bomb", "defuse", "time":
			return why
		}
	}
	switch bomb {
	case "exploded":
		return "bomb"
	case "defused":
		return "defuse"
	}
	return ""
}

// Cs2Match is one match, live or finished.
type Cs2Match struct {
	ID         string `json:"id"`
	Date       string `json:"date"`
	Map        string `json:"map"`
	Mode       string `json:"mode"`
	Phase      string `json:"phase"` // warmup | live | intermission | gameover
	Team       string `json:"team"`  // CT | T, the side being played now
	Round      int    `json:"round"`
	MyScore    int    `json:"myScore"`
	TheirScore int    `json:"theirScore"`
	Kills      int    `json:"kills"`
	Deaths     int    `json:"deaths"`
	Assists    int    `json:"assists"`
	MVPs       int    `json:"mvps"`
	Score      int    `json:"score"`
	// Headshot kills, summed round by round.
	HeadshotKills int `json:"headshotKills"`
	// Damage dealt, summed over the rounds recorded.
	Damage int `json:"damage"`
	// Every round the app saw finish, in order. A match joined late has
	// fewer rounds here than its score.
	Rounds []Cs2Round `json:"rounds"`
	// Kills by the weapon that was in hand when the kill landed.
	WeaponKills map[string]int `json:"weaponKills"`
	// Right now, for the Live page.
	Health int    `json:"health"`
	Armor  int    `json:"armor"`
	Money  int    `json:"money"`
	Weapon string `json:"weapon"`
	// win | loss | draw, set when the game says the match is over. Empty for
	// a match that was left before the end.
	Result     string `json:"result"`
	Ended      bool   `json:"ended"`
	Incomplete bool   `json:"incomplete"`
	Simulated  bool   `json:"simulated,omitempty"`

	// The round in progress.
	lastRoundHS int
	roundKills  int
	roundHS     int
	roundDamage int
	roundDied   bool
	roundSide   string
	roundEquip  int
	roundBomb   string
	seenKills   int // round_kills as last reported, to spot each new kill
	awaitReset  bool
	lastTotal   int
	deathsMark  int
	haveTotal   bool
}

type Cs2Status struct {
	Current    *Cs2Match `json:"current"`
	GsiAgeSecs *int64    `json:"gsiAgeSecs"`
	Live       bool      `json:"live"`
	Simulating bool      `json:"simulating"`
}

type Cs2 struct {
	mu      sync.Mutex
	store   *Store
	current *Cs2Match

	lastPayloadAt      time.Time
	lastMatchPayloadAt time.Time
	now                func() time.Time

	steamRoot func() string

	simStop chan struct{}

	// Called, off the tracker's lock, with each match that is saved.
	OnSaved func(Cs2Match)
}

func NewCs2(store *Store) *Cs2 {
	return &Cs2{store: store, now: time.Now, steamRoot: SteamPath}
}

// ---------- History ----------

func (c *Cs2) History() []Cs2Match {
	out := []Cs2Match{}
	c.store.readJSON("cs2_history.json", &out)
	for i := range out {
		if out[i].Rounds == nil {
			out[i].Rounds = []Cs2Round{}
		}
		if out[i].WeaponKills == nil {
			out[i].WeaponKills = map[string]int{}
		}
	}
	return out
}

func (c *Cs2) save(m Cs2Match) {
	c.store.historyMu.Lock()
	defer c.store.historyMu.Unlock()
	h := c.History()
	for _, old := range h {
		if old.ID == m.ID {
			return
		}
	}
	_ = c.store.writeJSON("cs2_history.json", append(h, m))
}

func (c *Cs2) Delete(id string) ([]Cs2Match, error) {
	c.store.historyMu.Lock()
	defer c.store.historyMu.Unlock()
	h := c.History()
	out := []Cs2Match{}
	for _, m := range h {
		if m.ID != id {
			out = append(out, m)
		}
	}
	if len(out) == len(h) {
		return h, errors.New("That match isn't in your CS2 history.")
	}
	return out, c.store.writeJSON("cs2_history.json", out)
}

// ---------- Live ----------

func (c *Cs2) Status() Cs2Status {
	c.mu.Lock()
	defer c.mu.Unlock()
	s := Cs2Status{Simulating: c.simStop != nil}
	if c.current != nil {
		m := *c.current
		m.Rounds = append([]Cs2Round{}, m.Rounds...)
		wk := make(map[string]int, len(m.WeaponKills))
		for k, v := range m.WeaponKills {
			wk[k] = v
		}
		m.WeaponKills = wk
		s.Current = &m
		s.Live = !m.Ended && m.Phase != "" && !c.lastMatchPayloadAt.IsZero() && c.now().Sub(c.lastMatchPayloadAt) < matchFeedTimeout
	}
	if !c.lastPayloadAt.IsZero() {
		s.GsiAgeSecs = ptr(int64(c.now().Sub(c.lastPayloadAt).Seconds()))
	}
	return s
}

// A match shorter than this that never reached "gameover" is a warmup, a
// practice server or a join-and-leave, and is not kept.
const cs2MinRounds = 5

func (c *Cs2) finalizeLocked(incomplete bool) {
	m := c.current
	if m == nil || m.Ended {
		return
	}
	m.Ended, m.Incomplete = true, incomplete
	if !incomplete {
		switch {
		case m.MyScore > m.TheirScore:
			m.Result = "win"
		case m.MyScore < m.TheirScore:
			m.Result = "loss"
		default:
			m.Result = "draw"
		}
	}
	if m.Simulated || (incomplete && m.MyScore+m.TheirScore < cs2MinRounds) {
		return
	}
	c.save(*m)
	if hook := c.OnSaved; hook != nil {
		saved := *m
		go hook(saved)
	}
}

func weaponName(raw string) string {
	return strings.TrimPrefix(raw, "weapon_")
}

// activeWeapon is the weapon in the player's hands.
func activeWeapon(player jsonMap) string {
	for _, raw := range sub(player, "weapons") {
		w, _ := raw.(map[string]any)
		if state, _ := getStr(w, "state"); state == "active" {
			name, _ := getStr(w, "name")
			return weaponName(name)
		}
	}
	return ""
}

// HandleUpdate applies one CS2 GSI payload.
func (c *Cs2) HandleUpdate(body jsonMap) {
	c.mu.Lock()
	defer c.mu.Unlock()
	now := c.now()
	c.lastPayloadAt = now

	gmap, player, provider := sub(body, "map"), sub(body, "player"), sub(body, "provider")
	if gmap == nil {
		// Back in the menus: whatever was running is over.
		if c.current != nil && !c.current.Ended {
			c.finalizeLocked(true)
		}
		return
	}
	c.lastMatchPayloadAt = now

	name, _ := getStr(gmap, "name")
	phase, _ := getStr(gmap, "phase")
	simulated, _ := getBool(body, SimulatedMarker)

	// A different map, or a fresh warmup after a finished game, is a new
	// match. CS2's feed carries no match id.
	cur := c.current
	if cur != nil && !cur.Ended && cur.Map != name {
		c.finalizeLocked(true)
		cur = nil
	}
	if cur == nil || cur.Ended {
		if phase == "gameover" {
			return // the scoreboard of a match already recorded
		}
		cur = &Cs2Match{
			ID:   "cs" + strconv.FormatInt(now.UnixMilli(), 10) + randomToken(2),
			Date: now.UTC().Format("2006-01-02T15:04:05.000Z"), Map: name, Simulated: simulated,
			Rounds: []Cs2Round{}, WeaponKills: map[string]int{},
		}
		c.current = cur
	}

	cur.Phase = phase
	cur.Mode, _ = getStr(gmap, "mode")
	if r, ok := getInt(gmap, "round"); ok {
		cur.Round = int(r)
	}
	roundPhase, _ := getStr(sub(body, "round"), "phase")
	if roundPhase == "freezetime" {
		// The next round's buy time: the game has zeroed its per-round
		// counters, so they can be trusted again.
		cur.awaitReset = false
		cur.seenKills, cur.lastRoundHS = 0, 0
	}
	// The bomb's fate is reported with the round it happened in, and stays
	// on the feed until the next buy time.
	if b, ok := getStr(sub(body, "round"), "bomb"); ok && b != "" {
		if cur.awaitReset && len(cur.Rounds) > 0 {
			cur.Rounds[len(cur.Rounds)-1].Bomb = b
		} else {
			cur.roundBomb = b
		}
	}

	// While dead the feed describes whoever is being spectated. Only the
	// local player's own numbers count.
	mine := true
	if pid, ok := getStr(player, "steamid"); ok {
		if own, ok := getStr(provider, "steamid"); ok && own != "" {
			mine = pid == own
		}
	}
	if mine && player != nil {
		if team, ok := getStr(player, "team"); ok {
			cur.Team = team
		}
		if ms := sub(player, "match_stats"); ms != nil {
			cur.Kills, cur.Deaths, cur.Assists = int(jI64(ms, "kills")), int(jI64(ms, "deaths")), int(jI64(ms, "assists"))
			cur.MVPs, cur.Score = int(jI64(ms, "mvps")), int(jI64(ms, "score"))
			// A death usually shows up late: once dead the feed follows a
			// teammate, and the player's own count only arrives with the
			// next round's buy time. It belongs to the round just recorded.
			if cur.Deaths > cur.deathsMark {
				if (cur.awaitReset || roundPhase == "freezetime") && len(cur.Rounds) > 0 {
					cur.Rounds[len(cur.Rounds)-1].Died = true
				} else {
					cur.roundDied = true
				}
				cur.deathsMark = cur.Deaths
			}
		}
		if w := activeWeapon(player); w != "" {
			cur.Weapon = w
		}
		if st := sub(player, "state"); st != nil {
			cur.Health, cur.Armor, cur.Money = int(jI64(st, "health")), int(jI64(st, "armor")), int(jI64(st, "money"))
			if cur.awaitReset && int(jI64(st, "round_kills")) < cur.seenKills {
				cur.awaitReset, cur.seenKills, cur.lastRoundHS = false, 0, 0
			}
			if !cur.awaitReset && phase == "live" {
				cur.roundSide = cur.Team
				// The first sight of the round in play is what was bought.
				if cur.roundEquip == 0 && roundPhase == "live" {
					cur.roundEquip = int(jI64(st, "equip_value"))
				}
				// round_kills and round_killhs count up within a round;
				// each increase is a kill with the weapon now in hand.
				if k := int(jI64(st, "round_kills")); k > cur.seenKills {
					if cur.Weapon != "" {
						cur.WeaponKills[cur.Weapon] += k - cur.seenKills
					}
					cur.seenKills = k
				}
				cur.roundKills = max(cur.roundKills, cur.seenKills)
				if hs := int(jI64(st, "round_killhs")); hs > cur.lastRoundHS {
					cur.HeadshotKills += hs - cur.lastRoundHS
					cur.roundHS += hs - cur.lastRoundHS
					cur.lastRoundHS = hs
				}
				cur.roundDamage = max(cur.roundDamage, int(jI64(st, "round_totaldmg")))
				if cur.Health == 0 {
					cur.roundDied = true
				}
			}
		}
	}

	// Scores follow the side, and sides swap at half time, so "my score" is
	// always the score of the side being played now.
	ct, t := int(jI64(sub(gmap, "team_ct"), "score")), int(jI64(sub(gmap, "team_t"), "score"))
	prevMine := cur.MyScore
	switch cur.Team {
	case "CT":
		cur.MyScore, cur.TheirScore = ct, t
	case "T":
		cur.MyScore, cur.TheirScore = t, ct
	}

	// A round has finished when the total score goes up. At the half-time
	// side swap the two scores trade places but the total does not move.
	total := ct + t
	if cur.haveTotal && total == cur.lastTotal+1 && cur.Team != "" {
		side := cur.roundSide
		if side == "" {
			side = cur.Team
		}
		firstOfHalf := len(cur.Rounds) == 0 && total == 1
		if n := len(cur.Rounds); n > 0 {
			firstOfHalf = cur.Rounds[n-1].Side != side
		}
		cur.Rounds = append(cur.Rounds, Cs2Round{
			N: total, Won: cur.MyScore > prevMine, Side: side,
			Kills: cur.roundKills, HS: cur.roundHS, Damage: cur.roundDamage, Died: cur.roundDied,
			Equip: cur.roundEquip, Buy: cs2Buy(cur.roundEquip, firstOfHalf), Bomb: cur.roundBomb,
		})
		cur.Damage += cur.roundDamage
		cur.roundKills, cur.roundHS, cur.roundDamage, cur.roundDied, cur.roundSide = 0, 0, 0, false, ""
		cur.roundEquip, cur.roundBomb = 0, ""
		// Until the next buy time the feed still shows the finished round's
		// counters; reading them again would count its kills twice.
		cur.awaitReset = true
	}
	cur.lastTotal, cur.haveTotal = total, true

	// Why the last round ended can arrive a post after the score does.
	if n := len(cur.Rounds); n > 0 && cur.Rounds[n-1].End == "" {
		last := &cur.Rounds[n-1]
		reason, _ := getStr(sub(gmap, "round_wins"), strconv.Itoa(last.N))
		last.End = cs2End(reason, last.Bomb)
	}

	if phase == "gameover" {
		c.finalizeLocked(false)
	}
}

// ---------- Test match ----------

// StartSimulation plays a short scripted match through the tracker so the
// CS2 pages can be seen working without launching the game. It is marked
// simulated and never saved.
func (c *Cs2) StartSimulation() error {
	c.mu.Lock()
	if c.current != nil && !c.current.Ended && !c.current.Simulated && !c.lastMatchPayloadAt.IsZero() && c.now().Sub(c.lastMatchPayloadAt) < matchFeedTimeout {
		c.mu.Unlock()
		return errors.New("A real match is running. The test match would interrupt it.")
	}
	if c.simStop != nil {
		c.mu.Unlock()
		return nil
	}
	stop := make(chan struct{})
	c.simStop = stop
	c.current = nil
	c.mu.Unlock()

	go func() {
		ct, t, kills, deaths := 0, 0, 0, 0
		weapons := []string{"weapon_ak47", "weapon_m4a1", "weapon_awp", "weapon_deagle"}
		wins := jsonMap{}
		send := func(phase, roundPhase, team string, rk, hs, dmg, health, equip int, weapon, bomb string) {
			myCT, myT := ct, t
			if team == "T" {
				myCT, myT = t, ct
			}
			roundWins := jsonMap{}
			for k, v := range wins {
				roundWins[k] = v
			}
			round := jsonMap{"phase": roundPhase}
			if bomb != "" {
				round["bomb"] = bomb
			}
			c.HandleUpdate(jsonMap{
				SimulatedMarker: true,
				"provider":      jsonMap{"appid": float64(cs2AppID), "steamid": "sim"},
				"map":           jsonMap{"name": "de_mirage", "mode": "competitive", "phase": phase, "round": float64(ct + t), "team_ct": jsonMap{"score": float64(myCT)}, "team_t": jsonMap{"score": float64(myT)}, "round_wins": roundWins},
				"round":         round,
				"player": jsonMap{"steamid": "sim", "team": team, "activity": "playing",
					"state":       jsonMap{"health": float64(health), "armor": 100.0, "money": float64(2400 + 350*(ct+t)%5000), "equip_value": float64(equip), "round_kills": float64(rk), "round_killhs": float64(hs), "round_totaldmg": float64(dmg)},
					"match_stats": jsonMap{"kills": float64(kills), "deaths": float64(deaths), "assists": float64((ct + t) / 4), "mvps": float64(ct / 3), "score": float64(kills*2 + ct)},
					"weapons":     jsonMap{"weapon_0": jsonMap{"name": "weapon_knife", "state": "holstered"}, "weapon_1": jsonMap{"name": weapon, "state": "active"}}},
			})
		}
		pause := func(d time.Duration) bool {
			select {
			case <-stop:
				return false
			case <-time.After(d):
				return true
			}
		}
		// "ct" is the player's own score throughout; the side they play
		// swaps at half time.
		for r := 0; r < 20; r++ {
			team, other := "CT", "T"
			if r >= 12 {
				team, other = "T", "CT"
			}
			w := weapons[r%len(weapons)]
			// A pistol round opens each half; after it, a mix of buys.
			equip := []int{4700, 1200, 2600, 5200}[r%4]
			if r == 0 || r == 12 {
				equip = 850
			}
			send("live", "freezetime", team, 0, 0, 0, 100, equip, w, "")
			if !pause(500 * time.Millisecond) {
				break
			}
			rk := (r*7 + 1) % 4
			died := r%3 == 2
			kills += rk
			health := map[bool]int{true: 0, false: 100}[died]
			send("live", "live", team, rk, rk/2, 40+rk*85, health, equip, w, "")
			if died {
				deaths++
			}
			if !pause(700 * time.Millisecond) {
				break
			}
			winner := team
			if r%5 == 3 {
				t++
				winner = other
			} else {
				ct++
			}
			reason, bomb := "elimination", ""
			switch {
			case r%3 == 0 && winner == "T":
				reason, bomb = "bomb", "exploded"
			case r%3 == 0:
				reason, bomb = "defuse", "defused"
			case r%7 == 5 && winner == "CT":
				reason = "time"
			}
			wins[strconv.Itoa(ct+t)] = strings.ToLower(winner) + "_win_" + reason
			send("live", "over", team, rk, rk/2, 40+rk*85, health, equip, w, bomb)
			if !pause(300 * time.Millisecond) {
				break
			}
		}
		send("gameover", "over", "T", 0, 0, 0, 100, 0, "weapon_ak47", "")
		c.mu.Lock()
		c.simStop = nil
		c.mu.Unlock()
	}()
	return nil
}

func (c *Cs2) StopSimulation() {
	c.mu.Lock()
	stop := c.simStop
	c.mu.Unlock()
	if stop == nil {
		return
	}
	select {
	case <-stop:
	default:
		close(stop)
	}
	for i := 0; i < 40; i++ {
		c.mu.Lock()
		done := c.simStop == nil
		c.mu.Unlock()
		if done {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// ---------- Setup ----------

type Cs2Setup struct {
	CfgDirs   []string `json:"cfgDirs"`
	Installed bool     `json:"installed"`
	Port      int      `json:"port"`
}

func (c *Cs2) cfgDirs() []string {
	g := &Gsi{steamRoot: c.steamRoot}
	dirs := []string{}
	for _, lib := range g.steamLibraries() {
		cfg := filepath.Join(lib, "steamapps", "common", "Counter-Strike Global Offensive", "game", "csgo", "cfg")
		if st, err := os.Stat(cfg); err == nil && st.IsDir() {
			dirs = append(dirs, cfg)
		}
	}
	return dirs
}

func (c *Cs2) Setup(port int, token string) Cs2Setup {
	s := Cs2Setup{CfgDirs: c.cfgDirs(), Port: port}
	want := strings.TrimSpace(cs2ConfigText(port, token))
	for _, dir := range s.CfgDirs {
		raw, err := os.ReadFile(filepath.Join(dir, cs2CfgName))
		if err == nil && strings.TrimSpace(strings.ReplaceAll(string(raw), "\r\n", "\n")) == want {
			s.Installed = true
		}
	}
	return s
}

// Install writes the config into every CS2 install found. CS2 reads it at
// launch and needs no launch option.
func (c *Cs2) Install(port int, token string) error {
	dirs := c.cfgDirs()
	if len(dirs) == 0 {
		return errors.New("Couldn't find Counter-Strike 2. Is it installed through Steam?")
	}
	wrote := false
	var last error
	for _, dir := range dirs {
		if err := os.WriteFile(filepath.Join(dir, cs2CfgName), []byte(cs2ConfigText(port, token)), 0o644); err != nil {
			last = err
			continue
		}
		wrote = true
	}
	if !wrote {
		return fmt.Errorf("Couldn't write the config: %v", last)
	}
	return nil
}

func (c *Cs2) Remove() {
	for _, dir := range c.cfgDirs() {
		_ = os.Remove(filepath.Join(dir, cs2CfgName))
	}
}
