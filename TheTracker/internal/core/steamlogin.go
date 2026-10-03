package core

import (
	"context"
	"errors"
	"fmt"
	"html"
	"io"
	"net"
	"net/http"
	"net/url"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Sign in with Steam.
//
// The one way into the app. It opens Steam's own login page in the player's
// browser (OpenID 2.0); Steam sends the browser back to a listener on this PC
// with a signed statement of which account logged in. That statement is
// confirmed with Steam — by the cloud service when it is reachable, which
// also opens the leaderboard session, and otherwise from here — and only then
// is the account treated as the player's.
//
// The app never sees a password, needs no Steam API key, and reads nothing
// from the Steam client.

var steamOpenIDEndpoint = "https://steamcommunity.com/openid/login"

var steamClaimedID = regexp.MustCompile(`^https://steamcommunity\.com/openid/id/(\d{17})$`)

const steamLoginTimeout = 5 * time.Minute

type steamLogin struct {
	mu      sync.Mutex
	pending bool
	err     string
	cancel  func()
}

// SteamLoginStatus is what the UI polls while the browser is open.
type SteamLoginStatus struct {
	Pending bool      `json:"pending"`
	Error   string    `json:"error"`
	Auth    AuthState `json:"auth"`
}

func (a *App) SteamLoginStatus() SteamLoginStatus {
	a.login.mu.Lock()
	defer a.login.mu.Unlock()
	return SteamLoginStatus{Pending: a.login.pending, Error: a.login.err, Auth: a.Cloud.Auth()}
}

func openInBrowser(u string) error {
	return exec.Command("rundll32", "url.dll,FileProtocolHandler", u).Start()
}

// StartSteamLogin opens Steam's login page and waits, in the background, for
// the browser to come back. Progress is read with SteamLoginStatus.
func (a *App) StartSteamLogin() error {
	a.CancelSteamLogin()

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return errors.New("Couldn't open a local port for Steam to answer on.")
	}
	base := "http://" + ln.Addr().String()
	state := randomToken(16)
	returnTo := base + "/steam/callback?state=" + state

	ctx, cancel := context.WithTimeout(context.Background(), steamLoginTimeout)
	srv := &http.Server{ReadHeaderTimeout: 10 * time.Second}

	finish := func(msg string) {
		a.login.mu.Lock()
		a.login.pending, a.login.err = false, msg
		a.login.mu.Unlock()
		cancel()
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/steam/callback", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		// The state ties this answer to the sign-in this app started; without
		// it any page could bounce a browser at the listener.
		if q.Get("state") != state {
			http.Error(w, "This isn't the sign-in TheTracker is waiting for.", http.StatusBadRequest)
			return
		}
		params := map[string]string{}
		for key, vals := range q {
			if strings.HasPrefix(key, "openid.") && len(vals) > 0 {
				params[key] = vals[0]
			}
		}
		var msg string
		switch {
		case params["openid.mode"] == "cancel":
			msg = "Steam sign-in was cancelled."
		case params["openid.return_to"] != returnTo:
			msg = "That isn't the sign-in TheTracker started."
		default:
			if err := a.completeSteamLogin(params); err != nil {
				msg = err.Error()
			}
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, steamCallbackPage(msg))
		finish(msg)
		a.Shell.ShowMainWindow()
	})
	srv.Handler = mux

	a.login.mu.Lock()
	a.login.pending, a.login.err, a.login.cancel = true, "", cancel
	a.login.mu.Unlock()

	go func() { _ = srv.Serve(ln) }()
	go func() {
		<-ctx.Done()
		// Give the callback's reply a moment to reach the browser.
		time.Sleep(500 * time.Millisecond)
		_ = srv.Close()
		a.login.mu.Lock()
		if a.login.pending {
			a.login.pending = false
			if errors.Is(ctx.Err(), context.DeadlineExceeded) {
				a.login.err = "Steam sign-in timed out. Try again."
			}
		}
		a.login.mu.Unlock()
	}()

	q := url.Values{
		"openid.ns":         {"http://specs.openid.net/auth/2.0"},
		"openid.mode":       {"checkid_setup"},
		"openid.return_to":  {returnTo},
		"openid.realm":      {base},
		"openid.identity":   {"http://specs.openid.net/auth/2.0/identifier_select"},
		"openid.claimed_id": {"http://specs.openid.net/auth/2.0/identifier_select"},
	}
	if err := a.OpenURL(steamOpenIDEndpoint + "?" + q.Encode()); err != nil {
		finish("Couldn't open your browser for the Steam sign-in.")
		return errors.New("Couldn't open your browser for the Steam sign-in.")
	}
	return nil
}

func (a *App) CancelSteamLogin() {
	a.login.mu.Lock()
	cancel := a.login.cancel
	a.login.pending, a.login.cancel = false, nil
	a.login.mu.Unlock()
	if cancel != nil {
		cancel()
	}
}

func steamCallbackPage(errMsg string) string {
	title, body := "You're signed in", "You can close this tab and go back to TheTracker."
	if errMsg != "" {
		title, body = "Sign-in didn't finish", html.EscapeString(errMsg)+" Go back to TheTracker and try again."
	}
	return `<!doctype html><meta charset="utf-8"><title>TheTracker</title>
<body style="margin:0;display:grid;place-items:center;height:100vh;background:#0c0e13;color:#e8ebf0;font:16px 'Segoe UI',sans-serif">
<div style="text-align:center;max-width:460px;padding:24px"><h1 style="font:600 26px Bahnschrift,'Segoe UI',sans-serif;margin:0 0 10px">` + title + `</h1>
<p style="color:#8891a0;margin:0">` + body + `</p></div>`
}

// verifySteamLocally asks Steam whether it really issued this statement.
func verifySteamLocally(params map[string]string) error {
	form := url.Values{}
	for k, v := range params {
		form.Set(k, v)
	}
	form.Set("openid.mode", "check_authentication")
	req, _ := http.NewRequest(http.MethodPost, steamOpenIDEndpoint, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("User-Agent", userAgent())
	resp, err := httpClient.Do(req)
	if err != nil {
		return errors.New("Couldn't reach Steam to confirm the sign-in. Check your connection.")
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<16))
	if resp.StatusCode != http.StatusOK || !regexp.MustCompile(`is_valid\s*:\s*true`).Match(raw) {
		return errors.New("Steam did not confirm that sign-in.")
	}
	return nil
}

func (a *App) completeSteamLogin(params map[string]string) error {
	m := steamClaimedID.FindStringSubmatch(params["openid.claimed_id"])
	if m == nil || params["openid.mode"] != "id_res" {
		return errors.New("Steam didn't say which account signed in.")
	}
	id64, _ := strconv.ParseUint(m[1], 10, 64)
	if id64 <= steamID64Base {
		return errors.New("Steam returned an account id TheTracker doesn't recognise.")
	}
	id := SteamIdentity{SteamID: m[1], AccountID: id64 - steamID64Base, Name: "Steam user"}

	// Name, avatar and medal come from OpenDota's public profile. Best
	// effort: an account OpenDota has never seen still signs in.
	rank := ""
	if p, err := a.Dota.Profile(id.AccountID); err == nil {
		if p.Name != "" {
			id.Name = p.Name
		}
		id.Avatar, rank = p.Avatar, RankLabel(p.RankTier)
	}

	// The cloud service confirms the statement with Steam and opens the
	// leaderboard session in one step. If it cannot be used at all, the
	// statement is confirmed from here instead, so the sign-in still works
	// and only the shared leaderboard waits.
	if err := a.Cloud.SteamSignIn(id, params); err != nil {
		if !cloudUnavailable(err) {
			return errors.New(cleanServerError(err.Error()))
		}
		if err := verifySteamLocally(params); err != nil {
			return err
		}
		a.Cloud.SetLocalIdentity(id, "The shared leaderboard isn't available right now, so your games stay on this PC. Everything else works.")
	}

	// One sign-in sets up everything: both games read this account, and the
	// leaderboard shows this name.
	link := Link{AccountID: &id.AccountID, Personaname: &id.Name, Avatar: id.Avatar}
	_ = a.Store.SaveLink("dota", link)
	_ = a.Store.SaveLink("deadlock", link)
	profile := Profile{Username: id.Name}
	if rank != "" {
		profile.Rank = &rank
	}
	_ = a.Store.SaveProfile(profile)
	a.Cloud.PushProfile(profile)
	return nil
}

// SignOut ends the session and disconnects both games. Recorded sessions and
// settings stay on this PC.
func (a *App) SignOut() AuthState {
	a.CancelSteamLogin()
	auth := a.Cloud.SignOut()
	_ = a.Store.SaveLink("dota", Link{})
	_ = a.Store.SaveLink("deadlock", Link{})
	return auth
}
