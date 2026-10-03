package core

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"golang.org/x/sys/windows/registry"
)

// Steam account detection for one-click linking.
//
// What is read: the Steam install path and the signed-in account id from the
// registry, and account ids and display names from loginusers.vdf. What is
// never read: passwords, auth tokens, session files or anything else in that
// folder. A Steam32 account id is public — it is in every profile URL.

const steamID64Base = 76561197960265728

type SteamAccount struct {
	AccountID   uint64  `json:"accountId"`
	Personaname *string `json:"personaname"`
	Source      string  `json:"source"`
}

// SteamPath is where Steam is installed, or "" if it is not.
func SteamPath() string {
	key, err := registry.OpenKey(registry.CURRENT_USER, `Software\Valve\Steam`, registry.QUERY_VALUE)
	if err != nil {
		return ""
	}
	defer key.Close()
	path, _, err := key.GetStringValue("SteamPath")
	if err != nil {
		return ""
	}
	return filepath.FromSlash(path)
}

func activeSteamAccount() uint64 {
	key, err := registry.OpenKey(registry.CURRENT_USER, `Software\Valve\Steam\ActiveProcess`, registry.QUERY_VALUE)
	if err != nil {
		return 0
	}
	defer key.Close()
	v, _, err := key.GetIntegerValue("ActiveUser")
	if err != nil {
		return 0 // zero also means "nobody is signed in"
	}
	return v
}

// quoted returns the quoted tokens on a VDF line: `"key" "value"` -> [key value].
func quoted(line string) []string {
	var out []string
	parts := strings.Split(line, `"`)
	for i := 1; i < len(parts); i += 2 {
		out = append(out, parts[i])
	}
	return out
}

func parseLoginUsers(text string) []SteamAccount {
	var out []SteamAccount
	recent := map[uint64]bool{}
	current := -1
	for _, line := range strings.Split(text, "\n") {
		fields := quoted(strings.TrimSpace(line))
		// A block header is a lone quoted SteamID64.
		if len(fields) == 1 {
			if id64, err := strconv.ParseUint(fields[0], 10, 64); err == nil && id64 > steamID64Base {
				out = append(out, SteamAccount{AccountID: id64 - steamID64Base})
				current = len(out) - 1
			}
			continue
		}
		if current < 0 || len(fields) < 2 {
			continue
		}
		switch {
		case strings.EqualFold(fields[0], "PersonaName"):
			out[current].Personaname = ptr(fields[1])
		case strings.EqualFold(fields[0], "MostRecent") && fields[1] == "1":
			recent[out[current].AccountID] = true
		}
	}
	for i := range out {
		out[i].Source = "previously signed in"
		if recent[out[i].AccountID] {
			out[i].Source = "most recent"
		}
	}
	return out
}

// DetectSteamAccounts lists the Steam accounts known to this PC, leading with
// whichever one Steam is running as.
func DetectSteamAccounts() []SteamAccount {
	accounts := []SteamAccount{}
	if root := SteamPath(); root != "" {
		if raw, err := os.ReadFile(filepath.Join(root, "config", "loginusers.vdf")); err == nil {
			accounts = append(accounts, parseLoginUsers(string(raw))...)
		}
	}
	front := func(i int) {
		a := accounts[i]
		copy(accounts[1:i+1], accounts[:i])
		accounts[0] = a
	}
	if active := activeSteamAccount(); active != 0 {
		for i := range accounts {
			if accounts[i].AccountID == active {
				accounts[i].Source = "signed in now"
				front(i)
				return accounts
			}
		}
		return append([]SteamAccount{{AccountID: active, Source: "signed in now"}}, accounts...)
	}
	for i := range accounts {
		if accounts[i].Source == "most recent" {
			front(i)
			break
		}
	}
	return accounts
}
