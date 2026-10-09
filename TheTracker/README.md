# TheTracker

A desktop match tracker for Dota 2, Deadlock, Counter-Strike 2 and Overwatch. One Windows executable,
written in Go, with its interface drawn in the system's WebView2.

## What it does

**Dota 2**

- **Live** – follows your match as you play, from Valve's own Game State
  Integration feed: last hits at 5/10/15/20/25 minutes against your own
  averages, each death and the gold it cost, timings of key items, and
  progress against goals you set.
- **Overlay** – a small transparent window over the game that counts down to
  rune spawns, lotuses and the stack pull, and shows nothing otherwise.
- **Sessions** – every match recorded live, with comparisons against your
  other games of the same type, your own notes, and plain-language insights
  ("most of your deaths come between 10 and 20 minutes").
- **Matches, Heroes, Overview** – your match history, lifetime hero records,
  medal, and the people you play with most, from OpenDota. Recorded sessions
  OpenDota lacks are merged into the same list.
- **Draft** – add the enemy's heroes as they pick and see which heroes have
  done well against that lineup, with your own record on each.
- **Meta** – which heroes are winning, rising and falling, by estimated role.
- **Builds** – your saved item plans, beside what players buy most.
- **Leaderboard** – your best recorded games, and (with an account) a shared
  board.

**Deadlock** – overview, matches with scoreboards, heroes, meta (heroes and
items) and builds, from the community Deadlock API. Deadlock has no live
feed, so there is no live page or overlay for it.

**Today** – one page across every game you have switched on: today's and
this week's record, streaks and the last match in each.

**Deadlock Live** – the game's top live matches by hero, with the players
on each hero who are streaming it on Twitch first (viewers, title, a link to
the stream). Players come from the Deadlock API; streams from Twitch's
official API through `convex/twitch.ts`, which needs a Twitch app's client
ID and secret set once on the deployment (`npx convex env set
TWITCH_CLIENT_ID …` and `TWITCH_CLIENT_SECRET …`). Streams are matched to
players by name, as nothing links a Steam account to a Twitch channel.

**Dota 2 Streamers** – the same for Dota, as a tab under Live: Twitch streamers on each hero in Dota's top live games (from OpenDota), found by their in-game name, OpenDota's pro list, an exact Steam-name search run in the background, or your own links, with their medal. Dota's live list holds only the top ~100 games, so far fewer streamers can be placed than in Deadlock.

**Counter-Strike 2** – live tracking of your own matches from Valve's Game
State Integration, round by round: kills, damage (ADR), headshots, the weapon
behind each kill, the side played, what you bought (pistol, eco, force or
full buy) and how the round ended (elimination, bomb, defuse, time), saved as
you play. The overview adds win rate, CT/T and first/second-half round win
rates, win rate by buy, multi-kill rounds and per-map records, and a test
match shows the live page without opening the game. Valve publishes no CS2
match history or leaderboard for apps, so only matches played while the app
runs are listed.

**Overwatch** – career stats by mode, role and hero from your public profile,
each hero's full record (bests, averages, ability stats), and the hero meta
(pick, win and ban rates by mode, region, rank and map) with your own win
rate beside it, via the community OverFast API. Blizzard publishes no match
list, only running totals, so TheTracker remembers the totals each time it
reads your profile and shows the difference as sessions: games, wins, time
and heroes played, plus a history of your ranks, from the day tracking began.

**Compare** – a friend's public numbers next to yours for Dota 2, Deadlock
and Overwatch: win rate, KDA, farm or damage, and the heroes you both play.
Search by name, or paste a Steam profile link or account id.

**Share cards** – save your form, a CS2 match, a comparison or your week as
a PNG in Documents\TheTracker.

**Quick search** – Ctrl+K jumps to any page or hero. Ctrl+1–4 switch games,
Ctrl+T opens Today.

**Notifications** – optionally, a Windows notification when a Dota 2 or CS2
match has been saved. Off by default; Settings, General.

**Valorant is not supported.** Riot only allows match data through a
production API key it grants per application after review, with each player
signing in through Riot. Until this project holds such a key there is no
legitimate way to read Valorant matches, and the app does not pretend to.

Each game can be switched off under Settings; a first-run screen asks which
ones you play.

## Signing in

One method: **Sign in with Steam**. Steam's own login page opens in the
browser (OpenID); the app receives a signed statement of which account
logged in, and the cloud function in `convex/auth.ts` verifies it with Steam
before opening a session. That one sign-in links Dota 2 and Deadlock, names
the leaderboard profile, and never involves a password or API key. If the
cloud service is unreachable the app verifies the statement itself and
everything except the shared leaderboard works. Overwatch is connected
separately by BattleTag, since it is not a Steam account.

After changing anything in `convex/`, deploy it with `npx convex deploy`.

## Where the data comes from

| Source | Used for | Notes |
| --- | --- | --- |
| Valve Game State Integration | Live Dota tracking | Dota posts your own state to `127.0.0.1` on this PC. Nothing reads game memory. |
| [OpenDota](https://www.opendota.com) API | Dota history, heroes, meta, matchups | Public, no key. Keyed on your Steam account id. |
| [Deadlock API](https://deadlock-api.com) | Everything Deadlock | Community-run, rate-limited by Valve; can be late or incomplete. |
| Valve Game State Integration (CS2) | Live CS2 tracking | Same local feed as Dota; needs no launch option. |

| Valve's Dota leaderboard web API | Dota top players by region | Names and ranks only. |
| [OverFast API](https://overfast-api.tekrop.fr) | Overwatch career stats and hero meta | Community-run; reads Blizzard's public career pages. Profile must be public. |
| Convex (optional account) | Sync and the shared leaderboard | Only if you sign in. `convex/` holds the server functions. |

Nothing is scraped from any other tracker's site, and nothing is shown about
another player that the game does not already show you.

## Layout

```
main.go              desktop shell: windows, overlay, tray, start-with-Windows
internal/core/       everything the app does: tracker, storage, API clients,
                     sync, updater, insights. No window code; fully testable.
internal/api/        one HTTP handler; every UI action is POST /api/<command>
frontend/            the interface: plain HTML, CSS and JS, embedded in the exe
build/windows/       installer script and Windows resources
convex/              cloud functions (TypeScript, run on Convex's servers)
```

The UI talks to the backend only through `/api/*`, served inside the app's
own window (never on a network port). That is what lets every function be
tested without opening a window.

## Building

Needs Go 1.24+, [NSIS](https://nsis.sourceforge.io) and
[go-winres](https://github.com/tc-hib/go-winres). No C compiler.

```bash
./build.sh 1.0.0            # tests, then build/bin/thetracker.exe and the installer
./build.sh 1.0.0 --sign     # also signs the installer and writes latest.json
```

Signing uses the release key at `~/.tauri/thetracker.key` through the Tauri
CLI's signer (`npm install` provides it). The key and signature format are
the ones every release has used, so any installed version accepts the update.

To release: create a GitHub release tagged `v<version>` with the installer,
its `.sig`, and `latest.json` from `build/bin`. Installed copies check
`releases/latest/download/latest.json`.

## Testing

```bash
go test ./...
```

covers the tracker, the Dota config setup, every API client (against local
fake servers), sync, the updater's signature check, backup and restore, and
every `/api` command.

Two ways to run the app without touching real data or showing a window:

```bash
# The UI and API in a browser, backed by a throwaway data folder:
THETRACKER_LOG_DIR=/tmp/tt ./build/bin/thetracker.exe --serve 127.0.0.1:8765

# The real app checking itself (every page, the overlay, the tray), then quitting:
THETRACKER_LOG_DIR=/tmp/tt THETRACKER_NO_DOTA_SETUP=1 \
THETRACKER_SELFTEST=/tmp/selftest.txt ./build/bin/thetracker.exe --minimized
```

`THETRACKER_LOG_DIR` moves all app data; `THETRACKER_NO_DOTA_SETUP` stops the
app writing its config into Dota's folder.

## Data on disk

`%APPDATA%\TheTracker\logs` holds `history.json` (sessions), `prefs.json`,
`profile.json`, the linked accounts, and a `cache` folder. Uninstalling
leaves it in place. Settings → General can back it up to
`Documents\TheTracker` and restore it.
