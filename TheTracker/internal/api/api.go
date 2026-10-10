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
	"strconv"
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
	OwLink     core.OwLink             `json:"overwatchLink"`
	Auth       core.AuthState          `json:"auth"`
	Background core.BackgroundSettings `json:"background"`
	Gsi        core.GsiStatus          `json:"gsi"`
	Overlay    bool                    `json:"overlayVisible"`
	DataDir    string                  `json:"dataDir"`
	ExportDir  string                  `json:"exportDir"`
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

type regionArg struct {
	Region string `json:"region"`
	Force  bool   `json:"force"`
}

type pathArg struct {
	Path string `json:"path"`
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
	"https://www.twitch.tv/", "https://dev.twitch.tv/console",
}

func commands(a *core.App) map[string]command {
	s := a.Store
	return map[string]command{
		// ----- Boot -----
		"boot": none(func() (any, error) {
			return Boot{
				Version: core.Version, Prefs: s.LoadPrefs(), Profile: s.LoadProfile(),
				DotaLink: s.LoadLink("dota"), DlLink: s.LoadLink("deadlock"), OwLink: a.Ow.Link(), Auth: a.Cloud.Auth(),
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
		"set_live_role": in(func(p struct {
			Role string `json:"role"`
		}) (any, error) {
			a.Tracker.SetRole(p.Role)
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
		"save_games": in(func(g core.Games) (any, error) { return a.SetGames(g) }),

		// ----- Running in the background -----
		"background_settings": none(func() (any, error) { return a.Background(), nil }),
		"set_start_with_windows": in(func(p enabledArg) (any, error) {
			return a.SetStartWithWindows(p.Enabled)
		}),
		"set_close_to_tray": in(func(p enabledArg) (any, error) {
			return a.SetCloseToTray(p.Enabled), nil
		}),
		"set_notify":               in(func(p enabledArg) (any, error) { return a.SetNotify(p.Enabled), nil }),
		"test_notify":              none(func() (any, error) { a.TestNotification(); return ok, nil }),
		"dismiss_autostart_prompt": none(func() (any, error) { return a.DismissAutostartPrompt(), nil }),

		// ----- Dota live feed setup -----
		"gsi_status":  none(func() (any, error) { return a.Gsi.Status(), nil }),
		"gsi_install": none(func() (any, error) { _, err := a.Gsi.Install(); return a.Gsi.Status(), err }),
		"gsi_remove":  none(func() (any, error) { a.Gsi.Remove(); return a.Gsi.Status(), nil }),
		"launch_dota": none(func() (any, error) { return ok, a.Gsi.LaunchDota() }),

		// ----- Dota (OpenDota) -----
		"dota_link_status":    none(func() (any, error) { return s.LoadLink("dota"), nil }),
		"dota_history":        in(func(p forceArg) (any, error) { return a.Dota.History(p.Limit, p.Force) }),
		"dota_match_detail":   in(func(p matchArg) (any, error) { return a.Dota.Detail(p.MatchID) }),
		"dota_player":         in(func(p forceArg) (any, error) { return a.Dota.Player(p.Force) }),
		"dota_refresh":        none(func() (any, error) { return ok, a.Dota.RequestRefresh() }),
		"dota_heroes":         none(func() (any, error) { return a.Dota.HeroList(), nil }),
		"dota_items":          none(func() (any, error) { return a.Dota.ItemCatalog(), nil }),
		"dota_meta":           in(func(p forceArg) (any, error) { return a.Dota.Meta(p.Force) }),
		"dota_matchups":       in(func(p heroArg) (any, error) { return a.Dota.Matchups(p.HeroID) }),
		"dota_popular_builds": in(func(p heroArg) (any, error) { return a.Dota.PopularBuilds(p.HeroID) }),
		"dota_leaderboard":    in(func(p regionArg) (any, error) { return a.Dota.Leaderboard(p.Region, p.Force) }),

		// ----- Deadlock -----
		"deadlock_link_status":   none(func() (any, error) { return s.LoadLink("deadlock"), nil }),
		"deadlock_overview":      in(func(p forceArg) (any, error) { return a.Deadlock.Overview(p.Limit, p.Force) }),
		"deadlock_live":          none(func() (any, error) { return a.Deadlock.Live() }),
		"deadlock_match_detail":  in(func(p matchArg) (any, error) { return a.Deadlock.Detail(p.MatchID) }),
		"deadlock_heroes":        none(func() (any, error) { return a.Deadlock.HeroList(), nil }),
		"deadlock_meta":          in(func(p forceArg) (any, error) { return a.Deadlock.Meta(p.Force) }),
		"deadlock_popular_items": in(func(p heroArg) (any, error) { return a.Deadlock.PopularItems(p.HeroID) }),
		"deadlock_leaderboard":   in(func(p regionArg) (any, error) { return a.Deadlock.Leaderboard(p.Region, p.Force) }),

		// ----- Counter-Strike 2 -----
		"cs2_status":  none(func() (any, error) { return a.Cs2.Status(), nil }),
		"cs2_history": none(func() (any, error) { return a.Cs2.History(), nil }),
		"cs2_delete": in(func(p struct {
			ID string `json:"id"`
		}) (any, error) {
			return a.Cs2.Delete(p.ID)
		}),
		"cs2_sim_start": none(func() (any, error) { return ok, a.Cs2.StartSimulation() }),
		"cs2_sim_stop":  none(func() (any, error) { a.Cs2.StopSimulation(); return ok, nil }),

		"cs2_setup": none(func() (any, error) { return a.Cs2.Setup(a.Gsi.Port(), a.Gsi.Token()), nil }),
		"cs2_install": none(func() (any, error) {
			err := a.Cs2.Install(a.Gsi.Port(), a.Gsi.Token())
			return a.Cs2.Setup(a.Gsi.Port(), a.Gsi.Token()), err
		}),

		// ----- Overwatch -----
		"ow_link_status": none(func() (any, error) { return a.Ow.Link(), nil }),
		"ow_search": in(func(p queryArg) (any, error) {
			q, err := searchQuery(p.Query)
			if err != nil {
				return nil, err
			}
			return a.Ow.Search(q)
		}),
		"ow_link": in(func(l core.OwLink) (any, error) {
			if l.PlayerID == "" {
				return nil, errors.New("That profile has no id.")
			}
			return l, a.Ow.SetLink(l)
		}),
		"ow_unlink": none(func() (any, error) { return core.OwLink{}, a.Ow.SetLink(core.OwLink{}) }),
		"ow_meta": in(func(p struct {
			Mode     string `json:"mode"`
			Region   string `json:"region"`
			Division string `json:"division"`
			Map      string `json:"map"`
			Force    bool   `json:"force"`
		}) (any, error) {
			return a.Ow.Meta(p.Mode, p.Region, p.Division, p.Map, p.Force)
		}),
		"ow_hero": in(func(p struct {
			Hero string `json:"hero"`
			Mode string `json:"mode"`
		}) (any, error) {
			if p.Hero == "" {
				return nil, errors.New("Choose a hero.")
			}
			return a.Ow.HeroCareer(p.Hero, p.Mode)
		}),
		"ow_overview": in(func(p struct {
			Mode  string `json:"mode"`
			Force bool   `json:"force"`
		}) (any, error) {
			return a.Ow.Overview(p.Mode, p.Force)
		}),

		"ow_progress": in(func(p struct {
			Mode string `json:"mode"`
		}) (any, error) {
			return a.Ow.Progress(p.Mode), nil
		}),

		"stream_links": none(func() (any, error) { return s.StreamLinks(), nil }),
		"stream_link_add": in(func(p struct {
			Game      string  `json:"game"`
			Twitch    string  `json:"twitch"`
			AccountID string  `json:"accountId"`
			SteamName string  `json:"steamName"`
			Avatar    *string `json:"avatar"`
		}) (any, error) {
			id, _ := strconv.ParseUint(p.AccountID, 10, 64)
			return s.LinkStreamer(p.Game, p.Twitch, id, p.SteamName, p.Avatar)
		}),
		"stream_link_remove": in(func(p struct {
			Game   string `json:"game"`
			Twitch string `json:"twitch"`
		}) (any, error) {
			return s.UnlinkStreamer(p.Game, p.Twitch), nil
		}),
		"deadlock_live_board": in(func(p forceArg) (any, error) { return a.DeadlockLive(p.Force) }),

		// ----- Compare and share -----
		"friends":       none(func() (any, error) { return s.Friends(), nil }),
		"friend_forget": in(func(f core.Friend) (any, error) { return s.ForgetFriend(f.Game, f.ID), nil }),
		"friend_search": in(func(p struct {
			Game  string `json:"game"`
			Query string `json:"query"`
		}) (any, error) {
			return a.FindFriend(p.Game, p.Query)
		}),
		"compare": in(func(p struct {
			Friend core.Friend `json:"friend"`
			Mode   string      `json:"mode"`
			Force  bool        `json:"force"`
		}) (any, error) {
			return a.Compare(p.Friend, p.Mode, p.Force)
		}),
		"save_image": in(func(p struct {
			Name    string `json:"name"`
			DataURL string `json:"dataUrl"`
		}) (any, error) {
			path, err := core.SaveImage(core.ExportDir(), p.Name, p.DataURL)
			return map[string]string{"path": path}, err
		}),

		// ----- Account and cloud -----
		"auth_status": none(func() (any, error) { return a.Cloud.Auth(), nil }),
		// One way in. The browser does the signing in; these start it, report
		// on it, and call it off.
		"steam_login_start":  none(func() (any, error) { return a.SteamLoginStatus(), a.StartSteamLogin() }),
		"steam_login_status": none(func() (any, error) { return a.SteamLoginStatus(), nil }),
		"steam_login_cancel": none(func() (any, error) { a.CancelSteamLogin(); return a.SteamLoginStatus(), nil }),
		"sign_out":           none(func() (any, error) { return a.SignOut(), nil }),
		"sync_status":        none(func() (any, error) { return a.Cloud.Status(), nil }),
		"sync_all":           none(func() (any, error) { return map[string]int{"queued": a.Cloud.SyncAll()}, nil }),
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
		"install_update": in(func(p struct {
			DuringMatch bool `json:"duringMatch"`
		}) (any, error) {
			return ok, a.InstallUpdate(p.DuringMatch)
		}),
		"run_diagnostics": none(func() (any, error) { return a.Diagnostics(), nil }),
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
	"img-src 'self' data: https://cdn.cloudflare.steamstatic.com https://assets-bucket.deadlock-api.com https://api.deadlock-api.com https://www.opendota.com " +
	"https://avatars.steamstatic.com https://avatars.akamai.steamstatic.com https://avatars.cloudflare.steamstatic.com " +
	"https://d15f34w2p8l1cc.cloudfront.net https://static.playoverwatch.com https://blz-contentstack-images.akamaized.net " +
	"https://static-cdn.jtvnw.net; " +
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
