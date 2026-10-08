package core

import (
	"errors"
	"regexp"
	"strings"
	"time"
)

// Linked streamers: a Twitch channel tied by hand to the Steam account its
// streamer plays on. Names are only a guess; a link is certain, so a linked
// streamer is found in a live match whatever their Steam name is. Links are
// kept on this PC only.

type StreamLink struct {
	Twitch    string  `json:"twitch"` // the channel's login, lowercase
	AccountID uint64  `json:"accountId"`
	SteamName string  `json:"steamName"`
	Avatar    *string `json:"avatar"`
	AddedAt   int64   `json:"addedAt"`
}

// What the Live page says about each linked streamer.
type LinkedStatus struct {
	StreamLink
	// The channel as Twitch shows it, when live in the Deadlock category.
	Stream *LiveStream `json:"stream"`
	// Set when the account is in one of the listed live matches.
	HeroID   int           `json:"heroId,omitempty"`
	HeroName string        `json:"heroName,omitempty"`
	Rank     *DeadlockRank `json:"rank,omitempty"`
	Mode     string        `json:"mode,omitempty"`
}

var twitchLogin = regexp.MustCompile(`^[a-z0-9_]{3,25}$`)

// twitchFrom reads a channel name out of what someone pastes: a name, or a
// twitch.tv link.
func twitchFrom(s string) (string, bool) {
	s = strings.ToLower(strings.TrimSpace(s))
	if _, rest, ok := strings.Cut(s, "twitch.tv/"); ok {
		s = rest
	}
	s = strings.TrimPrefix(s, "@")
	if i := strings.IndexAny(s, "/?#"); i >= 0 {
		s = s[:i]
	}
	return s, twitchLogin.MatchString(s)
}

func (s *Store) StreamLinks() []StreamLink {
	out := []StreamLink{}
	s.readJSON("stream_links.json", &out)
	return out
}

// LinkStreamer ties a Twitch channel to a Steam account. A channel has one
// account; linking it again replaces the old one.
func (s *Store) LinkStreamer(channel string, accountID uint64, steamName string, avatar *string) ([]StreamLink, error) {
	login, ok := twitchFrom(channel)
	if !ok {
		return s.StreamLinks(), errors.New("That isn't a Twitch channel name. Paste the channel's name or its twitch.tv link.")
	}
	if accountID == 0 {
		return s.StreamLinks(), errors.New("Pick the Steam account this streamer plays on.")
	}
	list := []StreamLink{{Twitch: login, AccountID: accountID, SteamName: steamName, Avatar: avatar, AddedAt: time.Now().Unix()}}
	for _, l := range s.StreamLinks() {
		if l.Twitch != login {
			list = append(list, l)
		}
	}
	return list, s.writeJSON("stream_links.json", list)
}

func (s *Store) UnlinkStreamer(channel string) []StreamLink {
	login, _ := twitchFrom(channel)
	list := []StreamLink{}
	for _, l := range s.StreamLinks() {
		if l.Twitch != login {
			list = append(list, l)
		}
	}
	_ = s.writeJSON("stream_links.json", list)
	return list
}
