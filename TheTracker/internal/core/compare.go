package core

import (
	"errors"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Compare: another player's public numbers next to the player's own. It uses
// the same public sources as the rest of the app, and only what those
// sources already show to anyone: a private profile stays private.

// Friend is someone the player has compared with, kept so they can be picked
// again without searching.
type Friend struct {
	Game   string  `json:"game"` // dota | deadlock | overwatch
	ID     string  `json:"id"`   // Steam account id, or Battle.net player id
	Name   string  `json:"name"`
	Avatar *string `json:"avatar"`
	At     int64   `json:"at"`
}

const maxFriends = 8

func (s *Store) Friends() []Friend {
	out := []Friend{}
	s.readJSON("friends.json", &out)
	return out
}

// RememberFriend puts a friend at the top of the recent list.
func (s *Store) RememberFriend(f Friend) []Friend {
	f.At = time.Now().Unix()
	list := []Friend{f}
	for _, old := range s.Friends() {
		if old.Game != f.Game || old.ID != f.ID {
			list = append(list, old)
		}
	}
	if len(list) > maxFriends {
		list = list[:maxFriends]
	}
	_ = s.writeJSON("friends.json", list)
	return list
}

func (s *Store) ForgetFriend(game, id string) []Friend {
	list := []Friend{}
	for _, old := range s.Friends() {
		if old.Game != game || old.ID != id {
			list = append(list, old)
		}
	}
	_ = s.writeJSON("friends.json", list)
	return list
}

// A Steam profile link or a bare id: steamcommunity.com/profiles/7656…, a
// 64-bit id, or the short 32-bit account id Dota shows.
var steamProfileURL = regexp.MustCompile(`steamcommunity\.com/profiles/(\d{17})`)

// steamAccountFrom reads an account id out of what a player might paste.
// Custom profile names (steamcommunity.com/id/name) cannot be resolved
// without a Steam API key, so those are searched by name instead.
func steamAccountFrom(q string) (uint64, bool) {
	q = strings.TrimSpace(q)
	if m := steamProfileURL.FindStringSubmatch(q); m != nil {
		q = m[1]
	}
	n, err := strconv.ParseUint(q, 10, 64)
	if err != nil || n == 0 {
		return 0, false
	}
	if n > steamID64Base {
		return n - steamID64Base, true
	}
	if n < 1<<32 {
		return n, true
	}
	return 0, false
}

// FindFriend looks a player up by name, profile link or id.
func (a *App) FindFriend(game, query string) ([]Friend, error) {
	query = strings.TrimSpace(query)
	if len(query) < 2 {
		return nil, errors.New("Type at least two characters to search.")
	}
	out := []Friend{}
	switch game {
	case "dota", "deadlock":
		if id, ok := steamAccountFrom(query); ok {
			p, err := a.Dota.Profile(id)
			if err != nil {
				return nil, err
			}
			name := p.Name
			if name == "" {
				name = "Account " + strconv.FormatUint(id, 10)
			}
			return []Friend{{Game: game, ID: strconv.FormatUint(id, 10), Name: name, Avatar: p.Avatar}}, nil
		}
		if game == "deadlock" {
			rows, err := a.Deadlock.Search(query)
			if err != nil {
				return nil, err
			}
			for _, r := range rows {
				out = append(out, Friend{Game: game, ID: strconv.FormatUint(r.AccountID, 10), Name: r.Personaname, Avatar: r.Avatar})
			}
			return out, nil
		}
		rows, err := a.Dota.Search(query)
		if err != nil {
			return nil, err
		}
		for _, r := range rows {
			out = append(out, Friend{Game: game, ID: strconv.FormatUint(r.AccountID, 10), Name: r.Personaname, Avatar: r.Avatar})
		}
	case "overwatch":
		rows, err := a.Ow.Search(query)
		if err != nil {
			return nil, err
		}
		for _, r := range rows {
			if r.Public {
				out = append(out, Friend{Game: game, ID: r.PlayerID, Name: r.Name, Avatar: r.Avatar})
			}
		}
	default:
		return nil, errors.New("Compare works for Dota 2, Deadlock and Overwatch.")
	}
	return out, nil
}

// HeroLine is one hero in a comparison.
type HeroLine struct {
	Name    string  `json:"name"`
	Image   *string `json:"image"`
	Slug    string  `json:"slug,omitempty"`
	Games   int     `json:"games"`
	Wins    int     `json:"wins"`
	WinRate float64 `json:"winRate"`
}

// Side is one player's half of a comparison.
type Side struct {
	Name    string     `json:"name"`
	Avatar  *string    `json:"avatar"`
	Rank    string     `json:"rank"`
	Games   int        `json:"games"`
	Wins    int        `json:"wins"`
	Losses  int        `json:"losses"`
	WinRate float64    `json:"winRate"`
	KDA     float64    `json:"kda"`
	Form    []*bool    `json:"form"` // newest first
	Stats   []float64  `json:"stats"`
	Heroes  []HeroLine `json:"heroes"`
}

type FriendComparison struct {
	Game string `json:"game"`
	// What each entry of Side.Stats is.
	StatLabels []string `json:"statLabels"`
	Me         Side     `json:"me"`
	Them       Side     `json:"them"`
}

func heroLines(byName map[string]*HeroLine, order []string) []HeroLine {
	out := make([]HeroLine, 0, len(order))
	for _, n := range order {
		h := byName[n]
		h.WinRate = pct(float64(h.Wins), float64(h.Games))
		out = append(out, *h)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Games > out[j].Games })
	return out
}

func dotaSide(h DotaHistory) Side {
	s := Side{Games: h.Summary.Matches, Wins: h.Summary.Wins, Losses: h.Summary.Losses, WinRate: h.Summary.WinRate, KDA: h.Summary.KDA, Form: []*bool{}}
	byName, order := map[string]*HeroLine{}, []string{}
	var dmg int64
	for i, m := range h.Matches {
		if i < 20 {
			s.Form = append(s.Form, ptr(m.Won))
		}
		dmg += m.HeroDamage
		hl := byName[m.HeroName]
		if hl == nil {
			hl = &HeroLine{Name: m.HeroName, Slug: m.HeroSlug}
			byName[m.HeroName] = hl
			order = append(order, m.HeroName)
		}
		hl.Games++
		if m.Won {
			hl.Wins++
		}
	}
	n := float64(max(len(h.Matches), 1))
	s.Stats = []float64{float64(h.Summary.AvgGpm), float64(h.Summary.AvgXpm), float64(h.Summary.AvgLastHits), float64(dmg) / n}
	s.Heroes = heroLines(byName, order)
	return s
}

func deadlockSide(o DeadlockOverview) Side {
	s := Side{Games: o.Summary.Matches, Wins: o.Summary.Wins, Losses: o.Summary.Losses, WinRate: o.Summary.WinRate, KDA: o.Summary.KDA, Form: []*bool{}}
	if o.Rank != nil {
		s.Rank = o.Rank.Label
	}
	byName, order := map[string]*HeroLine{}, []string{}
	var lh int
	for i, m := range o.Matches {
		if i < 20 {
			switch m.Outcome {
			case "win":
				s.Form = append(s.Form, ptr(true))
			case "loss":
				s.Form = append(s.Form, ptr(false))
			default:
				s.Form = append(s.Form, nil)
			}
		}
		lh += m.LastHits
		hl := byName[m.HeroName]
		if hl == nil {
			hl = &HeroLine{Name: m.HeroName, Image: m.HeroImage}
			byName[m.HeroName] = hl
			order = append(order, m.HeroName)
		}
		hl.Games++
		if m.Outcome == "win" {
			hl.Wins++
		}
	}
	n := float64(max(len(o.Matches), 1))
	s.Stats = []float64{float64(o.Summary.AvgSouls), float64(lh) / n}
	s.Heroes = heroLines(byName, order)
	return s
}

func owSide(ov OwOverview) Side {
	g := ov.General
	s := Side{Name: ov.Name, Avatar: ov.Avatar, Games: g.GamesPlayed, Wins: g.GamesWon, Losses: g.GamesLost, WinRate: g.WinRate, KDA: g.KDA, Form: []*bool{}}
	for _, r := range ov.Ranks {
		if s.Rank != "" {
			s.Rank += ", "
		}
		s.Rank += r.Role + " " + strings.ToUpper(r.Division[:min(1, len(r.Division))]) + r.Division[min(1, len(r.Division)):] + " " + strconv.Itoa(r.Tier)
	}
	s.Stats = []float64{g.AvgEliminations, g.AvgDeaths, g.AvgDamage, g.AvgHealing}
	for _, h := range ov.Heroes {
		s.Heroes = append(s.Heroes, HeroLine{Name: h.Name, Image: h.Portrait, Games: h.GamesPlayed, Wins: h.GamesWon, WinRate: h.WinRate})
	}
	if s.Heroes == nil {
		s.Heroes = []HeroLine{}
	}
	return s
}

// Compare puts a friend's numbers next to the player's own, over the same
// number of recent matches (Dota, Deadlock) or the same mode (Overwatch).
func (a *App) Compare(f Friend, mode string, force bool) (FriendComparison, error) {
	switch f.Game {
	case "dota":
		id, err := strconv.ParseUint(f.ID, 10, 64)
		if err != nil {
			return FriendComparison{}, errors.New("That isn't a Steam account.")
		}
		mine, err := a.Dota.History(100, force)
		if err != nil {
			return FriendComparison{}, err
		}
		theirs, err := a.Dota.historyFor(id, 100, force)
		if err != nil {
			return FriendComparison{}, err
		}
		if len(theirs.Matches) == 0 {
			return FriendComparison{}, errors.New(f.Name + " has no public Dota matches. Their match data may be private.")
		}
		c := FriendComparison{Game: "dota", StatLabels: []string{"Gold per minute", "XP per minute", "Last hits", "Hero damage"}, Me: dotaSide(mine), Them: dotaSide(theirs)}
		link := a.Store.LoadLink("dota")
		if link.Personaname != nil {
			c.Me.Name = *link.Personaname
		}
		c.Me.Avatar = link.Avatar
		c.Them.Name, c.Them.Avatar = f.Name, f.Avatar
		if p, err := a.Dota.Profile(id); err == nil {
			c.Them.Rank = RankLabel(p.RankTier)
		}
		if acc := link.AccountID; acc != nil {
			if p, err := a.Dota.Profile(*acc); err == nil {
				c.Me.Rank = RankLabel(p.RankTier)
			}
		}
		a.Store.RememberFriend(f)
		return c, nil
	case "deadlock":
		id, err := strconv.ParseUint(f.ID, 10, 64)
		if err != nil {
			return FriendComparison{}, errors.New("That isn't a Steam account.")
		}
		mine, err := a.Deadlock.Overview(100, force)
		if err != nil {
			return FriendComparison{}, err
		}
		theirs, err := a.Deadlock.overviewFor(id, 100, force)
		if err != nil {
			return FriendComparison{}, err
		}
		if len(theirs.Matches) == 0 {
			return FriendComparison{}, errors.New(f.Name + " has no Deadlock matches the API can see.")
		}
		c := FriendComparison{Game: "deadlock", StatLabels: []string{"Souls per match", "Last hits"}, Me: deadlockSide(mine), Them: deadlockSide(theirs)}
		link := a.Store.LoadLink("deadlock")
		if link.Personaname != nil {
			c.Me.Name = *link.Personaname
		}
		c.Me.Avatar = link.Avatar
		c.Them.Name, c.Them.Avatar = f.Name, f.Avatar
		a.Store.RememberFriend(f)
		return c, nil
	case "overwatch":
		mine, err := a.Ow.Overview(mode, force)
		if err != nil {
			return FriendComparison{}, err
		}
		theirs, err := a.Ow.overviewFor(OwLink{PlayerID: f.ID, Name: f.Name, Avatar: f.Avatar}, mode, force, false)
		if err != nil {
			return FriendComparison{}, err
		}
		c := FriendComparison{Game: "overwatch", StatLabels: []string{"Eliminations per 10 min", "Deaths per 10 min", "Damage per 10 min", "Healing per 10 min"}, Me: owSide(mine), Them: owSide(theirs)}
		a.Store.RememberFriend(f)
		return c, nil
	}
	return FriendComparison{}, errors.New("Compare works for Dota 2, Deadlock and Overwatch.")
}
