package core

import (
	"encoding/json"
	"sync"
	"time"
)

// Preferences that are not match data: favourites, saved builds, overlay
// appearance, goals and how the app behaves in the background. One
// prefs.json; missing keys take their defaults, so an older file never wipes
// a newer setting and vice versa.

type DotaPanels struct {
	// Runes switches the four rune reminders together (the only rune switch
	// before 1.5.1); each also has its own.
	Runes  bool `json:"runes"`
	Bounty bool `json:"bounty"`
	Water  bool `json:"water"`
	Power  bool `json:"power"`
	Wisdom bool `json:"wisdom"`
	Lotus  bool `json:"lotus"`
	Stacks bool `json:"stacks"`
	// How many minutes into a game each reminder runs (stack, bounty,
	// lotus, power, wisdom); 0 is the whole game. Turbo halves them.
	Until map[string]int `json:"until"`
	// The lotus reminder comes back from 35:00, when the pools grow Great
	// and Greater Lotuses.
	LotusLate bool `json:"lotusLate"`
	// "auto": the windows follow the role the app works out each match (the
	// Core and Support presets); "fixed": Until as set.
	RoleMode string `json:"roleMode"`
}

// The Core and Support windows, also in overlay.js and settings.js.
var rolePresets = map[string]map[string]int{
	"core":    {"stack": 10, "bounty": 12, "lotus": 15, "power": 0, "wisdom": 0},
	"support": {"stack": 20, "bounty": 20, "lotus": 20, "power": 0, "wisdom": 0},
}

// settleRoleMode fills in RoleMode for a settings file from before it
// existed: windows left at a role preset follow the role from now on;
// hand-tuned ones stay as they are.
func (d *DotaPanels) settleRoleMode() {
	if d.RoleMode == "auto" || d.RoleMode == "fixed" {
		return
	}
	d.RoleMode = "fixed"
	for _, preset := range rolePresets {
		same := true
		for k, v := range preset {
			if got, ok := d.Until[k]; ok && got != v || !ok && v != 0 {
				same = false
			}
		}
		if same {
			d.RoleMode = "auto"
			return
		}
	}
}

// The defaults: each reminder for as long as it usually matters.
func defaultUntil() map[string]int {
	return map[string]int{"stack": 10, "bounty": 12, "lotus": 15, "power": 0, "wisdom": 0}
}

type OverlaySettings struct {
	Opacity     float64 `json:"opacity"`     // 0.25–1.0
	Scale       float64 `json:"scale"`       // 0.75–1.5
	LeadSeconds int     `json:"leadSeconds"` // 1–30
	Corner      string  `json:"corner"`      // top-left | top-right | bottom-left | bottom-right
	// Pixels further in from the corner's top or bottom edge, to clear the
	// game's own HUD there. 0–600.
	OffsetY      int  `json:"offsetY"`
	ClickThrough bool `json:"clickThrough"`
	// Show when a match starts, hide when it ends.
	Auto bool `json:"auto"`
	// Display name to pin the overlay to; empty follows the main window.
	Monitor string `json:"monitor"`
	// Icon and seconds only, without the words.
	Compact bool `json:"compact"`
	// One quiet line naming the next two events.
	NextUp bool       `json:"nextUp"`
	Dota   DotaPanels `json:"dota"`
}

type Build struct {
	ID        string   `json:"id"`
	Game      string   `json:"game"`
	Hero      string   `json:"hero"`
	Name      string   `json:"name"`
	Items     []string `json:"items"`
	Notes     string   `json:"notes"`
	UpdatedAt string   `json:"updatedAt"`
}

type Favorites struct {
	Dota     *string `json:"dota"`
	Deadlock *string `json:"deadlock"`
}

type GeneralPrefs struct {
	// Closing the window keeps the app in the tray. On by default: live
	// tracking only works while the app runs.
	CloseToTray    bool `json:"closeToTray"`
	AutostartAsked bool `json:"autostartAsked"`
	// A desktop notification when a match has been saved.
	Notify bool `json:"notify"`
}

// Goals are per-match targets the player sets for themselves. Zero means
// "no goal". Sessions are scored against them and the Live page shows
// progress while a match runs.
type Goals struct {
	LastHits10 int `json:"lastHits10"` // at least this many last hits at 10:00
	MaxDeaths  int `json:"maxDeaths"`  // die at most this many times
	MinGPM     int `json:"minGpm"`     // finish with at least this GPM
}

// Games is which games the player uses the app for. A game that is off has
// no pages, and for Dota no live listener and no config in the game's folder.
type Games struct {
	Dota      bool `json:"dota"`
	Deadlock  bool `json:"deadlock"`
	CS2       bool `json:"cs2"`
	Overwatch bool `json:"overwatch"`
	// False until the player has answered the first-run question.
	Chosen bool `json:"chosen"`
}

// WindowState is the main window's last size and position.
type WindowState struct {
	X      int  `json:"x"`
	Y      int  `json:"y"`
	Width  int  `json:"width"`
	Height int  `json:"height"`
	Set    bool `json:"set"`
}

type Prefs struct {
	Favorites Favorites       `json:"favorites"`
	Builds    []Build         `json:"builds"`
	Overlay   OverlaySettings `json:"overlay"`
	General   GeneralPrefs    `json:"general"`
	Goals     Goals           `json:"goals"`
	Window    WindowState     `json:"window"`
	Games     Games           `json:"games"`
}

func defaultOverlay() OverlaySettings {
	return OverlaySettings{
		Opacity: 0.85, Scale: 1.0, LeadSeconds: 5, Corner: "top-left",
		ClickThrough: true, Auto: true,
		Dota: DotaPanels{Runes: true, Bounty: true, Water: true, Power: true, Wisdom: true, Lotus: true, Stacks: true, Until: defaultUntil(), LotusLate: true},
	}
}

func defaultPrefs() Prefs {
	return Prefs{
		Builds:  []Build{},
		Overlay: defaultOverlay(),
		General: GeneralPrefs{CloseToTray: true},
		Games:   Games{Dota: true, Deadlock: true},
	}
}

func clampF(v, lo, hi float64) float64 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

func clampI(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

// sanitized clamps values that arrive from the UI — an opacity of 0 would
// leave an invisible window with no way back.
func (o OverlaySettings) sanitized() OverlaySettings {
	o.Opacity = clampF(o.Opacity, 0.25, 1.0)
	o.Scale = clampF(o.Scale, 0.75, 1.5)
	o.LeadSeconds = clampI(o.LeadSeconds, 1, 30)
	o.OffsetY = clampI(o.OffsetY, 0, 600)
	until := defaultUntil()
	for k := range until {
		if v, ok := o.Dota.Until[k]; ok {
			until[k] = clampI(v, 0, 90)
		}
	}
	o.Dota.Until = until
	o.Dota.settleRoleMode()
	switch o.Corner {
	case "top-left", "top-right", "bottom-left", "bottom-right":
	default:
		o.Corner = "top-left"
	}
	return o
}

func (g Goals) sanitized() Goals {
	g.LastHits10 = clampI(g.LastHits10, 0, 200)
	g.MaxDeaths = clampI(g.MaxDeaths, 0, 50)
	g.MinGPM = clampI(g.MinGPM, 0, 2000)
	return g
}

var prefsMu sync.Mutex

func parsePrefs(raw []byte) (Prefs, error) {
	// Unmarshalling over the defaults is what makes a missing key keep its
	// default rather than reading as false or zero.
	p := defaultPrefs()
	if err := json.Unmarshal(raw, &p); err != nil {
		return defaultPrefs(), err
	}
	p.Overlay.Dota.settleRoleMode()
	if p.Builds == nil {
		p.Builds = []Build{}
	}
	for i := range p.Builds {
		if p.Builds[i].Items == nil {
			p.Builds[i].Items = []string{}
		}
	}
	return p, nil
}

func (s *Store) LoadPrefs() Prefs {
	p := defaultPrefs()
	s.readJSON("prefs.json", &p)
	p.Overlay.Dota.settleRoleMode()
	if p.Builds == nil {
		p.Builds = []Build{}
	}
	for i := range p.Builds {
		if p.Builds[i].Items == nil {
			p.Builds[i].Items = []string{}
		}
	}
	return p
}

// UpdatePrefs applies one change to prefs.json under a lock, so two settings
// screens saving at once cannot overwrite each other.
func (s *Store) UpdatePrefs(change func(*Prefs)) Prefs {
	prefsMu.Lock()
	defer prefsMu.Unlock()
	p := s.LoadPrefs()
	change(&p)
	_ = s.writeJSON("prefs.json", p)
	return p
}

func (s *Store) SetFavorite(game string, hero *string) Prefs {
	return s.UpdatePrefs(func(p *Prefs) {
		if game == "deadlock" {
			p.Favorites.Deadlock = hero
		} else {
			p.Favorites.Dota = hero
		}
	})
}

func (s *Store) SaveOverlay(o OverlaySettings) Prefs {
	return s.UpdatePrefs(func(p *Prefs) { p.Overlay = o.sanitized() })
}

func (s *Store) SaveGoals(g Goals) Prefs {
	return s.UpdatePrefs(func(p *Prefs) { p.Goals = g.sanitized() })
}

// UpsertBuild inserts a build, or replaces the one with the same id.
func (s *Store) UpsertBuild(b Build) Prefs {
	return s.UpdatePrefs(func(p *Prefs) {
		b.UpdatedAt = time.Now().Format(time.RFC3339)
		if b.Items == nil {
			b.Items = []string{}
		}
		if b.ID == "" {
			b.ID = newUUID()
		}
		for i := range p.Builds {
			if p.Builds[i].ID == b.ID {
				p.Builds[i] = b
				return
			}
		}
		p.Builds = append(p.Builds, b)
	})
}

func (s *Store) DeleteBuild(id string) Prefs {
	return s.UpdatePrefs(func(p *Prefs) {
		out := []Build{}
		for _, b := range p.Builds {
			if b.ID != id {
				out = append(out, b)
			}
		}
		p.Builds = out
	})
}
