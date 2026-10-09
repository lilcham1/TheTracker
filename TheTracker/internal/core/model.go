package core

import "sort"

// Field names match the JSON the app has always written to history.json, so
// history recorded by earlier versions loads unchanged.

var CheckpointMinutes = []int{5, 10, 15, 20, 25}

var GameTypes = []string{"ranked", "all_pick", "turbo", "other"}

func IsGameType(t string) bool {
	for _, g := range GameTypes {
		if g == t {
			return true
		}
	}
	return false
}

func GameTypeLabel(t string) string {
	switch t {
	case "ranked":
		return "Ranked"
	case "all_pick", "unranked": // "unranked" is from very old saves
		return "All Pick"
	case "turbo":
		return "Turbo"
	case "other":
		return "Other"
	}
	return "Unspecified"
}

type Death struct {
	Clock    string `json:"clock"`
	GoldLost *int64 `json:"goldLost"`
}

type KeyItemEntry struct {
	Clock string `json:"clock"`
	Item  string `json:"item"`
}

type Checkpoint struct {
	LastHits int64 `json:"lastHits"`
	Denies   int64 `json:"denies"`
}

type RoshanState struct {
	Deaths         int      `json:"deaths"`
	LastDeathClock *float64 `json:"lastDeathClock"`
	WasAlive       bool     `json:"wasAlive"`
}

type CompareMetric struct {
	Value   *float64 `json:"value"`
	Avg     *float64 `json:"avg"`
	Verdict string   `json:"verdict"` // better | worse | similar | no_data
	IsBest  bool     `json:"isBest"`
}

type Comparison struct {
	Deaths      CompareMetric         `json:"deaths"`
	GoldLost    CompareMetric         `json:"goldLost"`
	Checkpoints map[int]CompareMetric `json:"checkpoints"`
}

// MatchSummary is one finished match in history.json.
type MatchSummary struct {
	MatchID              string              `json:"matchid"`
	HeroName             *string             `json:"heroName"`
	Date                 string              `json:"date"`
	Duration             string              `json:"duration"`
	Kills                int64               `json:"kills"`
	TotalDeaths          int                 `json:"totalDeaths"`
	TotalGoldLost        int64               `json:"totalGoldLost"`
	Deaths               []Death             `json:"deaths"`
	KeyItems             []KeyItemEntry      `json:"keyItems"`
	Checkpoints          map[int]*Checkpoint `json:"checkpoints"`
	RoshanDeaths         int                 `json:"roshanDeaths"`
	GameType             string              `json:"gameType"`
	Comparison           *Comparison         `json:"comparison"`
	GamesComparedAgainst *int                `json:"gamesComparedAgainst"`

	// Newer than the original format; absent in older files. None of these
	// go to the cloud — the sync code sends an explicit field list.
	Won        *bool  `json:"won"`
	LastHits   *int64 `json:"lastHits"`
	Denies     *int64 `json:"denies"`
	Assists    *int64 `json:"assists"`
	GPM        *int64 `json:"gpm"`
	XPM        *int64 `json:"xpm"`
	Level      *int64 `json:"level,omitempty"`
	Incomplete bool   `json:"incomplete"`
	// Free text the player attaches to a session afterwards.
	Notes string `json:"notes,omitempty"`
}

// MatchState is the match being tracked right now.
type MatchState struct {
	MatchID         string              `json:"matchid"`
	HeroName        *string             `json:"heroName"`
	StartedAt       string              `json:"startedAt"`
	WasAlive        bool                `json:"wasAlive"`
	OwnedItemCounts map[string]int      `json:"ownedItemCounts"`
	Deaths          []Death             `json:"deaths"`
	KeyItemLog      []KeyItemEntry      `json:"keyItemLog"`
	Checkpoints     map[int]*Checkpoint `json:"checkpoints"`
	LastClockTime   float64             `json:"lastClockTime"`
	Daytime         *bool               `json:"daytime"`
	// True only while the horn has gone and the clock is the real game clock.
	// GSI reports a clock during the draft too.
	InProgress bool          `json:"inProgress"`
	LastHits   int64         `json:"lastHits"`
	Denies     int64         `json:"denies"`
	Kills      int64         `json:"kills"`
	PrevGold   *int64        `json:"prevGold"`
	Gold       *int64        `json:"gold"`
	Ended      bool          `json:"ended"`
	Summary    *MatchSummary `json:"summary"`
	GameType   string        `json:"gameType"`
	Roshan     RoshanState   `json:"roshan"`
	Team       *string       `json:"team"`
	Won        *bool         `json:"won"`
	Assists    *int64        `json:"assists"`
	GPM        *int64        `json:"gpm"`
	XPM        *int64        `json:"xpm"`
	Level      *int64        `json:"level"`
	// True once the horn has gone at least once; unlike InProgress it never
	// drops back, so it tells a played match from one abandoned in the draft.
	ReachedGame bool    `json:"reachedGame"`
	LastSeenAt  *string `json:"lastSeenAt"`
	// From the built-in simulator. Never saved or synced.
	Simulated bool `json:"simulated"`
	// Wards or a Blood Grenade were bought before 5:00 (see role.go). Kept
	// once seen: wards are placed, so the item itself goes.
	SupportItems bool `json:"supportItems"`
	// The role the player chose for this match: core | support, or empty
	// to let the app work it out.
	RoleChoice string `json:"roleChoice,omitempty"`
}

func newMatchState(matchID string, hero *string, startedAt string) *MatchState {
	cps := map[int]*Checkpoint{}
	for _, m := range CheckpointMinutes {
		cps[m] = nil
	}
	return &MatchState{
		MatchID:         matchID,
		HeroName:        hero,
		StartedAt:       startedAt,
		WasAlive:        true,
		OwnedItemCounts: map[string]int{},
		Deaths:          []Death{},
		KeyItemLog:      []KeyItemEntry{},
		Checkpoints:     cps,
		GameType:        "unspecified",
		Roshan:          RoshanState{WasAlive: true},
	}
}

func (m *MatchState) totalGoldLost() int64 {
	var sum int64
	for _, d := range m.Deaths {
		if d.GoldLost != nil {
			sum += *d.GoldLost
		}
	}
	return sum
}

// clone is a deep copy, so a snapshot handed to the UI cannot be changed by
// the next GSI update while it is being serialized.
func (m *MatchState) clone() *MatchState {
	if m == nil {
		return nil
	}
	c := *m
	c.OwnedItemCounts = make(map[string]int, len(m.OwnedItemCounts))
	for k, v := range m.OwnedItemCounts {
		c.OwnedItemCounts[k] = v
	}
	c.Deaths = append([]Death{}, m.Deaths...)
	c.KeyItemLog = append([]KeyItemEntry{}, m.KeyItemLog...)
	c.Checkpoints = make(map[int]*Checkpoint, len(m.Checkpoints))
	for k, v := range m.Checkpoints {
		if v != nil {
			cp := *v
			c.Checkpoints[k] = &cp
		} else {
			c.Checkpoints[k] = nil
		}
	}
	return &c
}

// KeyItems are the purchases worth a line in the match log. GSI reports
// Valve's internal names, which differ from shop names for several items
// (Daedalus is greater_crit, Linken's is sphere), and a wrong name here fails
// silently — the purchase just never appears.
var KeyItems = []string{
	"boots", "boots_of_elves", "phase_boots", "power_treads", "arcane_boots",
	"tranquil_boots", "travel_boots", "travel_boots_2", "guardian_greaves",
	"blink", "overwhelming_blink", "swift_blink", "arcane_blink",
	"black_king_bar", "ultimate_scepter", "ultimate_scepter_2", "aghanims_shard",
	"refresher", "refresher_shard", "heart", "assault", "shivas_guard",
	"satanic", "skadi", "manta", "sange_and_yasha", "yasha_and_kaya",
	"greater_crit", "butterfly", "silver_edge", "nullifier", "abyssal_blade",
	"monkey_king_bar", "sphere", "sheepstick", "rod_of_atos",
	"octarine_core", "bloodthorn", "dagon", "dagon_5",
	"mage_slayer",
}

var keyItemSet = func() map[string]bool {
	m := map[string]bool{}
	for _, k := range KeyItems {
		m[k] = true
	}
	return m
}()

func IsKeyItem(name string) bool { return keyItemSet[name] }

func sortedKeys(m map[string]int) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func ptr[T any](v T) *T { return &v }
