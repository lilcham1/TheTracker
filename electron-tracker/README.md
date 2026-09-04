# Dota 2 Match Tracker

A clean live dashboard for Dota 2, built on Valve's official **Game State
Integration (GSI)** feature — the same system pro broadcast overlays use.
Nothing here touches game files or memory.

## What it tracks

- **Key items only** — boots tier, Blink, BKB, Aghs, refresher, and other
  core build items. Tracked by total owned count, so moving an item between
  inventory slots never re-logs it — only a real purchase (or rebuying after
  it's sold/destroyed) counts.
- **Deaths** — exact time and gold lost per death. Killer identity and map
  position aren't part of Dota's GSI feed, so those can't be shown.
- **Last hits** — snapshotted every 5 minutes, up to 25.
- **End-of-game summary** — auto-detected when the match ends, comparing
  this game's deaths / gold lost / last-hit checkpoints against your
  historical average, with Better / Worse / Average badges.
- **History tab** — every finished match is saved locally so future games
  have something to compare against.

- **Roshan tracker** — auto-detects Roshan's death when possible and shows
  a live respawn countdown (8–11 min window), plus a manual "Mark Roshan
  Death" button as a reliable fallback (Valve's own data on this is known
  to be inconsistent, so don't rely on auto-detection alone).
- **🏆 Personal Best** flags on the summary card when a stat beats your
  all-time record, not just your average.
- **Game type tags** (Ranked / Unranked / Turbo / Other) — set manually
  per match, since Dota's data feed has no field exposing queue type.
  Comparisons only ever measure Ranked-vs-Ranked, Turbo-vs-Turbo, etc. —
  never mixed, since Turbo's clock speed and gold/XP rates make it
  incomparable to a standard game.
- **Leaderboard tab** — your own top 3 games (🥇🥈🥉 highlighted), ranked by
  a stat you pick: most last hits at 25 min, fewest deaths, least gold lost,
  or most kills. Filterable by game type. This is personal only — it's your
  own history ranked against itself, not a real multiplayer leaderboard.
- **Profile tab** — a local username, rank, and main role you set once.
  This isn't an account or login system — it's just a label stored on your
  own PC, since there's no multiplayer or shared leaderboard here.
- **History detail view** — click any past match to expand its full
  breakdown: deaths, key items (with icons), checkpoints, and how it
  compared at the time.
- **Hero and item icons**, and proper hero display names (e.g. "Anti-Mage"
  instead of the internal `antimage`). Icons load live from Valve's own
  public game-asset CDN — the same one Dotabuff/OpenDota use — so an
  internet connection is needed for icons to appear (everything else works
  fully offline).

## Requires

**Node.js** (v18+) for the tracker itself — no dependencies needed there.
An internet connection is needed for hero/item icons and fonts to load
(the tracking itself works without one).

Building the standalone desktop app additionally needs **npm access to the
internet** once, to download Electron (~150–250MB first time only).

## Running as a standalone app (recommended)

1. Open a terminal in this folder.
2. `npm install` — installs Electron + the packaging tool (one-time, needs
   internet).
3. `npm start` — opens the tracker in its own window to test it.
4. `npm run dist` — builds a Windows installer into the `dist/` folder.
   Run that installer once; it adds a Start Menu / Desktop shortcut. From
   then on you just double-click **Dota Tracker** like any other app — no
   terminal, ever again.

## Running it without building (plain Node, browser tab)

1. Find your Dota 2 `cfg` folder:
   - Windows: `...\Steam\steamapps\common\dota 2 beta\game\dota\cfg`
   - Mac: `~/Library/Application Support/Steam/steamapps/common/dota 2 beta/game/dota/cfg`
   - Linux: `~/.steam/steam/steamapps/common/dota 2 beta/game/dota/cfg`
2. Inside `cfg`, create a folder named `gamestate_integration` (if it
   doesn't already exist).
3. Copy `gamestate_integration_lasthits.cfg` into that folder.
4. In Steam: right-click **Dota 2 → Properties → Launch Options**, add:
   ```
   -gamestateintegration
   ```
5. `node server.js`, then open **http://localhost:3000** in a browser tab.

Either way (standalone app or plain Node), every finished match is saved to
`logs/history.json`, and the `.cfg` setup steps above are required regardless
of which method you use to run it.

## Auto-updates (via GitHub Releases)

Once set up, the app checks GitHub on launch and installs new versions
automatically — no manual reinstalling.

**One-time setup:**

1. Create a GitHub account if you don't have one, and create a new
   repository (public is simplest — it just hosts the built app, not
   anything sensitive).
2. In `package.json`, replace `YOUR_GITHUB_USERNAME` and `YOUR_REPO_NAME`
   under `build.publish` with your actual username and repo name.
3. Create a GitHub Personal Access Token (Settings → Developer settings →
   Personal access tokens → generate one with `repo` scope). Keep it
   somewhere safe — you'll use it to publish releases, not commit it
   anywhere.

**Publishing an update, going forward:**

1. Bump the `version` field in `package.json` (e.g. `1.0.0` → `1.0.1`) —
   this is how the app recognizes there's something new.
2. Set your token for this terminal session:
   ```
   set GH_TOKEN=your_token_here
   ```
3. Run:
   ```
   npm run release
   ```
   This builds the installer **and** uploads it straight to a new GitHub
   Release — no need to visit GitHub's website.
4. That's it. Anyone running the app (including you) gets prompted to
   restart and install the update next time they open it — no manual
   reinstalling.

The very first version has to be installed manually the normal way
(`npm run dist` → run the installer once) — auto-update only kicks in for
versions *after* that first one, since the app needs to already be
installed to check for something newer.

## Custom app icon

The installer and app window use a generic icon by default. To use your
own (e.g. Kez artwork you have the rights to use — I can't embed Valve's
game art into the app myself, since an app icon has to be a real local
file, not a web link):

1. Find or create a square image (512x512 or so works well) and convert it
   to `.ico` format — there are many free online "png to ico" converters.
2. Name the file `icon.ico` and place it in the same folder as `main.js`
   and `package.json`.
3. Run `npm run dist` (or `npm run release`) again — `package.json` is
   already configured to pick it up automatically.

## The Tracking switch

There's an ON/OFF switch at the top of the dashboard. Dota's GSI feed
doesn't expose whether a match is ranked, unranked, a bot game, or being
spectated — Valve simply doesn't send that information. The app filters out
obvious non-matches automatically (main menu, sessions with no real match
ID), but the switch is the only fully reliable way to control what gets
tracked: flip it on right as you queue for a game you want logged, off for
anything else (Demo Hero, spectating, a casual bot match).

## Limitations

- Only your own hero's data is available via GSI — not full match data for
  all 10 players.
- No killer identity or map position on deaths (not exposed by GSI).
- No ranked/unranked detection (not exposed by GSI) — use the Tracking
  switch instead.
- The comparison badges need at least one prior finished match to have
  something to compare against; your very first game will show "New."
