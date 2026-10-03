// Package api is the bridge between the UI and the backend: every button in
// the app ends up as one POST to /api/<command>. It is plain HTTP on purpose.
// Served through the desktop window it never touches a network port, and
// because it is just a handler, every command can be driven by a test or a
// script without opening a window.
package api

import (
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"net/http"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"thetracker/internal/core"
)

type command func(body []byte) (any, error)

// in adapts a typed function to a command, decoding the JSON body into T.
func in[T any](fn func(T) (any, error)) command {
	return func(body []byte) (any, error) {
		var args T
		if len(body) > 0 {
			if err := json.Unmarshal(body, &args); err != nil {
				return nil, errors.New("That request wasn't understood.")
			}
		}
		return fn(args)
	}
}

// none adapts a function that takes no arguments.
func none(fn func() (any, error)) command {
	return func([]byte) (any, error) { return fn() }
}

var ok = map[string]bool{"ok": true}

// Boot is everything the UI needs to draw its first frame, in one call.
type Boot struct {
	Version    string                  `json:"version"`
	Prefs      core.Prefs              `json:"prefs"`
	Profile    core.Profile            `json:"profile"`
	DotaLink   core.Link               `json:"dotaLink"`
	DlLink     core.Link               `json:"deadlockLink"`
	Auth       core.AuthState          `json:"auth"`
	Background core.BackgroundSettings `json:"background"`
	Gsi        core.GsiStatus          `json:"gsi"`
	Overlay    bool                    `json:"overlayVisible"`
	DataDir    string                  `json:"dataDir"`
	ExportDir  string                  `json:"exportDir"`
}

type linkArgs struct {
	AccountID   uint64  `json:"accountId"`
	Personaname string  `json:"personaname"`
	Avatar      *string `json:"avatar"`
}

type enabledArg struct {
	Enabled bool `json:"enabled"`
}

type forceArg struct {
	Limit int  `json:"limit"`
	Force bool `json:"force"`
}

type heroArg struct {
	HeroID int `json:"heroId"`
}

type matchArg struct {
	MatchID uint64 `json:"matchId"`
}

type queryArg struct {
	Query string `json:"query"`
}

type pathArg struct {
	Path string `json:"path"`
}

func linkOf(a linkArgs) (core.Link, error) {
	if a.AccountID == 0 {
		return core.Link{}, errors.New("That account has no id.")
	}
	return core.Link{AccountID: &a.AccountID, Personaname: &a.Personaname, Avatar: a.Avatar}, nil
}

func searchQuery(q string) (string, error) {
	q = strings.TrimSpace(q)
	if len(q) < 2 {
		return "", errors.New("Type at least two characters to search.")
	}
	return q, nil
}

// allowedLinks are the only sites the app will open in the browser.
var allowedLinks = []string{
	"https://www.opendota.com/", "https://steamcommunity.com/", "https://github.com/lilcham1/TheTracker",
	"https://deadlock-api.com/", "https://store.steampowered.com/",
}

func commands(a *core.App) map[string]command {
	s := a.Store
	return map[string]command{
		// ----- Boot -----
		"boot": none(func() (any, error) {
			return Boot{
				Version: core.Version, Prefs: s.LoadPrefs(), Profile: s.LoadProfile(),
				DotaLink: s.LoadLink("dota"), DlLink: s.LoadLink("deadlock"), Auth: a.Cloud.Auth(),
				Background: a.Background(), Gsi: a.Gsi.Status(), Overlay: a.Shell.OverlayVisible(),
				DataDir: s.Dir, ExportDir: core.ExportDir(),
			}, nil
		}),

		// ----- Live -----
		"get_live": in(func(p struct {
			Log bool `json:"log"`
		}) (any, error) {
			return a.Live(p.Log), nil
		}),
		"set_tracking": in(func(p enabledArg) (any, error) {
			a.Tracker.SetEnabled(p.Enabled)
			return a.Live(false), nil
		}),
		"set_live_game_type": in(func(p struct {
			GameType string `json:"gameType"`
		}) (any, error) {
			a.Tracker.SetGameType(p.GameType)
			return a.Live(false), nil
		}),
		"mark_roshan_death": none(func() (any, error) { a.Tracker.MarkRoshanDeath(); return a.Live(false), nil }),
		"sim_start": in(func(p struct {
			Seconds int `json:"seconds"`
		}) (any, error) {
			if p.Seconds == 0 {
				p.Seconds = 90
			}
			return ok, a.StartSimulation(p.Seconds)
		}),
		"sim_stop": none(func() (any, error) { a.StopSimulation(); return ok, nil }),

		// ----- Sessions -----
		"get_history": none(func() (any, error) { return s.History(), nil }),
		"set_history_game_type": in(func(p struct {
			MatchID  string `json:"matchid"`
			GameType string `json:"gameType"`
		}) (any, error) {
			h, updated, err := s.SetHistoryGameType(p.MatchID, p.GameType)
			if err != nil {
				return nil, err
			}
			// The cloud row carries the game type; keep it in step.
			a.Cloud.PushMatch(*updated)
			return h, nil
		}),
		"set_history_notes": in(func(p struct {
			MatchID string `json:"matchid"`
			Notes   string `json:"notes"`
		}) (any, error) {
			return s.SetHistoryNotes(p.MatchID, p.Notes)
		}),
		"delete_history": in(func(p struct {
			MatchID string `json:"matchid"`
		}) (any, error) {
			return s.DeleteHistoryMatch(p.MatchID)
		}),
		"insights": none(func() (any, error) {
			h := s.History()
			return map[string]any{
				"insights": core.BuildInsights(h),
				"goals":    core.GoalsProgress(s.LoadPrefs().Goals, h, 20),
				"sessions": len(h),
			}, nil
		}),
		"export_sessions": in(func(p struct {
			Format string `json:"format"`
		}) (any, error) {
			path, err := a.ExportSessions(p.Format, "")
			return map[string]string{"path": path}, err
		}),
		"backup": none(func() (any, error) {
			path, err := a.Backup("")
			return map[string]string{"path": path}, err
		}),
		"list_backups": none(func() (any, error) {
			matches, _ := filepath.Glob(filepath.Join(core.ExportDir(), "TheTracker-backup_*.zip"))
			sort.Sort(sort.Reverse(sort.StringSlice(matches)))
			if len(matches) > 10 {
				matches = matches[:10]
			}
			if matches == nil {
				matches = []string{}
			}
			return matches, nil
		}),
		"restore": in(func(p pathArg) (any, error) {
			// Only a backup the app itself wrote, from its own folder.
			if filepath.Dir(filepath.Clean(p.Path)) != filepath.Clean(core.ExportDir()) ||
				!strings.HasPrefix(filepath.Base(p.Path), "TheTracker-backup_") {
				return nil, errors.New("Choose one of the backups listed.")
			}
			n, err := a.Restore(p.Path)
			return map[string]int{"restored": n}, err
		}),
		"reveal": in(func(p pathArg) (any, error) {
			clean := filepath.Clean(p.Path)
			if !strings.HasPrefix(clean, filepath.Clean(core.ExportDir())) && !strings.HasPrefix(clean, filepath.Clean(s.Dir)) {
				return nil, errors.New("That isn't one of TheTracker's files.")
			}
			return ok, core.Reveal(clean)
		}),

		// ----- Profile and preferences -----
		"get_profile": none(func() (any, error) { return s.LoadProfile(), nil }),
		"save_profile": in(func(p core.Profile) (any, error) {
			p.Username = strings.TrimSpace(p.Username)
			if len(p.Username) > 40 {
				return nil, errors.New("That name is too long. Keep it to 40 characters.")
			}
			if err := s.SaveProfile(p); err != nil {
				return nil, err
			}
			a.Cloud.PushProfile(p)
			return p, nil
		}),
		"get_prefs": none(func() (any, error) { return s.LoadPrefs(), nil }),
		"set_favorite_hero": in(func(p struct {
			Game string  `json:"game"`
			Hero *string `json:"hero"`
		}) (any, error) {
			return s.SetFavorite(p.Game, p.Hero), nil
		}),
		"save_build": in(func(b core.Build) (any, error) {
			b.Name, b.Hero = strings.TrimSpace(b.Name), strings.TrimSpace(b.Hero)
			if b.Name == "" || b.Hero == "" {
				return nil, errors.New("A build needs a hero and a name.")
			}
			if b.Game != "deadlock" {
				b.Game = "dota"
			}
			return s.UpsertBuild(b), nil
		}),
		"delete_build": in(func(p struct {
			ID string `json:"id"`
		}) (any, error) {
			return s.DeleteBuild(p.ID), nil
		}),
		"save_overlay_settings": in(func(o core.OverlaySettings) (any, error) {
			p := s.SaveOverlay(o)
			a.Shell.ApplyOverlay(p.Overlay)
			return p, nil
		}),
		"save_goals": in(func(g core.Goals) (any, error) { return s.SaveGoals(g), nil }),

		// ----- Running in the background -----
		"background_settings": none(func() (any, error) { return a.Background(), nil }),
		"set_start_with_windows": in(func(p enabledArg) (any, error) {
			return a.SetStartWithWindows(p.Enabled)
		}),
		"set_close_to_tray": in(func(p enabledArg) (any, error) {
			return a.SetCloseToTray(p.Enabled), nil
		}),
		"dismiss_autostart_prompt": none(func() (any, error) { return a.DismissAutostartPrompt(), nil }),

		// ----- Dota live feed setup -----
		"gsi_status":     none(func() (any, error) { return a.Gsi.Status(), nil }),
		"gsi_install":    none(func() (any, error) { _, err := a.Gsi.Install(); return a.Gsi.Status(), err }),
		"gsi_remove":     none(func() (any, error) { a.Gsi.Remove(); return a.Gsi.Status(), nil }),
		"launch_dota":    none(func() (any, error) { return ok, a.Gsi.LaunchDota() }),
		"steam_accounts": none(func() (any, error) { return core.DetectSteamAccounts(), nil }),

		// ----- Dota (OpenDota) -----
		"dota_link_status": none(func() (any, error) { return s.LoadLink("dota"), nil }),
		"dota_search": in(func(p queryArg) (any, error) {
			q, err := searchQuery(p.Query)
			if err != nil {
				return nil, err
			}
			return a.Dota.Search(q)
		}),
		"dota_link": in(func(p linkArgs) (any, error) {
			l, err := linkOf(p)
			if err != nil {
				return nil, err
			}
			return l, s.SaveLink("dota", l)
		}),
		"dota_unlink":       none(func() (any, error) { return core.Link{}, s.SaveLink("dota", core.Link{}) }),
		"dota_history":      in(func(p forceArg) (any, error) { return a.Dota.History(p.Limit, p.Force) }),
		"dota_match_detail": in(func(p matchArg) (any, error) { return a.Dota.Detail(p.MatchID) }),
		"dota_player":       in(func(p forceArg) (any, error) { return a.Dota.Player(p.Force) }),
		"dota_refresh":      none(func() (any, error) { return ok, a.Dota.RequestRefresh() }),
		"dota_heroes":       none(func() (any, error) { return a.Dota.HeroList(), nil }),
		"dota_items":        none(func() (any, error) { return a.Dota.ItemCatalog(), nil }),
		"dota_meta":         in(func(p forceArg) (any, error) { return a.Dota.Meta(p.Force) }),
		"dota_matchups":     in(func(p heroArg) (any, error) { return a.Dota.Matchups(p.HeroID) }),
		"dota_draft": in(func(p struct {
			Enemies []int `json:"enemies"`
		}) (any, error) {
			return a.Dota.DraftAdvice(p.Enemies)
		}),
		"dota_popular_builds": in(func(p heroArg) (any, error) { return a.Dota.PopularBuilds(p.HeroID) }),

		// ----- Deadlock -----
		"deadlock_link_status": none(func() (any, error) { return s.LoadLink("deadlock"), nil }),
		"deadlock_search": in(func(p queryArg) (any, error) {
			q, err := searchQuery(p.Query)
			if err != nil {
				return nil, err
			}
			return a.Deadlock.Search(q)
		}),
		"deadlock_link": in(func(p linkArgs) (any, error) {
			l, err := linkOf(p)
			if err != nil {
				return nil, err
			}
			return l, s.SaveLink("deadlock", l)
		}),
		"deadlock_unlink":        none(func() (any, error) { return core.Link{}, s.SaveLink("deadlock", core.Link{}) }),
		"deadlock_overview":      in(func(p forceArg) (any, error) { return a.Deadlock.Overview(p.Limit, p.Force) }),
		"deadlock_live":          none(func() (any, error) { return a.Deadlock.Live() }),
		"deadlock_match_detail":  in(func(p matchArg) (any, error) { return a.Deadlock.Detail(p.MatchID) }),
		"deadlock_heroes":        none(func() (any, error) { return a.Deadlock.HeroList(), nil }),
		"deadlock_meta":          in(func(p forceArg) (any, error) { return a.Deadlock.Meta(p.Force) }),
		"deadlock_popular_items": in(func(p heroArg) (any, error) { return a.Deadlock.PopularItems(p.HeroID) }),

		// ----- Account and cloud -----
		"auth_status": none(func() (any, error) { return a.Cloud.Auth(), nil }),
		"sign_in": in(func(p struct {
			Email    string `json:"email"`
			Password string `json:"password"`
			Flow     string `json:"flow"`
		}) (any, error) {
			return a.Cloud.SignIn(p.Email, p.Password, p.Flow)
		}),
		"sign_out":    none(func() (any, error) { return a.Cloud.SignOut(), nil }),
		"sync_status": none(func() (any, error) { return a.Cloud.Status(), nil }),
		"sync_all":    none(func() (any, error) { return map[string]int{"queued": a.Cloud.SyncAll()}, nil }),
		"delete_cloud_data": none(func() (any, error) {
			n, err := a.Cloud.DeleteCloudData()
			return map[string]int{"deleted": n}, err
		}),
		"global_leaderboard": in(func(p struct {
			Metric   string `json:"metric"`
			GameType string `json:"gameType"`
			Limit    int    `json:"limit"`
		}) (any, error) {
			return a.Cloud.GlobalLeaderboard(p.Metric, p.GameType, p.Limit)
		}),

		// ----- Overlay -----
		"overlay_show":    none(func() (any, error) { return ok, a.Shell.ShowOverlay() }),
		"overlay_hide":    none(func() (any, error) { a.Shell.HideOverlay(); return ok, nil }),
		"overlay_visible": none(func() (any, error) { return a.Shell.OverlayVisible(), nil }),
		"list_monitors":   none(func() (any, error) { return a.Shell.Monitors(), nil }),

		// ----- App -----
		"check_for_update": none(func() (any, error) { return a.Updater.Check() }),
		"install_update":   none(func() (any, error) { return ok, a.InstallUpdate() }),
		"run_diagnostics":  none(func() (any, error) { return a.Diagnostics(), nil }),
		"open_url": in(func(p struct {
			URL string `json:"url"`
		}) (any, error) {
			for _, prefix := range allowedLinks {
				if strings.HasPrefix(p.URL, prefix) {
					return ok, exec.Command("rundll32", "url.dll,FileProtocolHandler", p.URL).Start()
				}
			}
			return nil, errors.New("TheTracker doesn't open links to that site.")
		}),
		"quit": none(func() (any, error) { go a.Shell.Quit(); return ok, nil }),
	}
}

// csp: scripts and styles only from the app itself; images from the game
// CDNs the portraits and icons live on; no outbound connections from the page
// at all — the backend makes those.
const csp = "default-src 'self'; " +
	"img-src 'self' data: https://cdn.cloudflare.steamstatic.com https://assets-bucket.deadlock-api.com " +
	"https://avatars.steamstatic.com https://avatars.akamai.steamstatic.com https://avatars.cloudflare.steamstatic.com; " +
	"style-src 'self' 'unsafe-inline'; script-src 'self'; connect-src 'self'"

// New returns the handler the window loads: /api/* for commands, everything
// else from the embedded UI files.
func New(a *core.App, assets fs.FS) http.Handler {
	cmds := commands(a)
	static := http.FileServerFS(assets)

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		name, isAPI := strings.CutPrefix(r.URL.Path, "/api/")
		if !isAPI {
			w.Header().Set("Cache-Control", "no-cache")
			w.Header().Set("X-Content-Type-Options", "nosniff")
			w.Header().Set("Content-Security-Policy", csp)
			static.ServeHTTP(w, r)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		cmd, found := cmds[name]
		if r.Method != http.MethodPost || !found {
			w.WriteHeader(http.StatusNotFound)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "Unknown command: " + name})
			return
		}
		body, _ := io.ReadAll(io.LimitReader(r.Body, 4<<20))
		out, err := cmd(body)
		if err != nil {
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
			return
		}
		_ = json.NewEncoder(w).Encode(out)
	})
}

// Commands lists every command name, sorted, for tests that check coverage.
func Commands(a *core.App) []string {
	out := []string{}
	for name := range commands(a) {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}
