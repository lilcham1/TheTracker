// Dota pages: Overview, Live, Matches, Heroes, Sessions, Leaderboard.

const dHist = resource("dotaHistory", "dota_history", { args: () => ({ limit: D.limit }) });
const dPlayer = resource("dotaPlayer", "dota_player", { ttl: 30 * 60000 });
const dHeroes = resource("dotaHeroes", "dota_heroes", { ttl: 6 * 3600000 });
const dInsights = resource("insights", "insights", { ttl: 30000 });

const D = {
  limit: 100,
  mode: "all",
  queue: "all",
  result: "all",
  heroQuery: "",
  sortKey: "startTime",
  sortDir: "desc",
  open: new Set(),
  details: new Map(),
  heroSort: "games",
  openSessions: new Set(),
  typeMenu: null,
  lbScope: "personal",
  lbMetric: "last_hits_25",
  lbType: "all",
  lbGlobal: { rows: null, error: null, loading: false, key: "" },
  showLog: false,
};

const dotaLinked = () => !!(S.boot && S.boot.dotaLink && S.boot.dotaLink.accountId);

async function ensureHeroList() {
  await dHeroes.load();
  if (dHeroes.data && !S.heroBySlug) {
    S.heroBySlug = {};
    S.heroById = {};
    for (const h of dHeroes.data) {
      S.heroBySlug[h.slug] = h;
      S.heroById[h.id] = h;
    }
    rerender();
  }
}

// ---------- Linking a Steam account (shared with Deadlock) ----------

const LINK = { accounts: null, busy: false, results: {}, searching: false, error: {} };

function linkPanelHtml(game) {
  const label = game === "deadlock" ? "Deadlock" : "Dota 2";
  const source = game === "deadlock" ? "the community Deadlock API" : "OpenDota";
  const accounts = LINK.accounts || [];
  const results = LINK.results[game] || [];
  return `<div class="panel narrow">
    <h2>Connect your Steam account</h2>
    <p class="muted">Your ${label} match history comes from ${source}, looked up by your Steam account. It's public data: no password, no API key, and nothing is read from the game.</p>

    <h3>Accounts on this PC</h3>
    ${
      LINK.accounts === null
        ? `<p class="muted">Looking for Steam…</p>`
        : accounts.length
          ? `<div class="pick-list">${accounts
              .map(
                (a) => `<button class="pick" data-act="link-account" data-game="${game}" data-id="${a.accountId}" data-name="${esc(a.personaname || "")}" type="button">
                  <span><b>${esc(a.personaname || "Account " + a.accountId)}</b><span class="muted"> ${esc(a.source)}</span></span>
                  <span class="muted">Use this account</span></button>`
              )
              .join("")}</div>`
          : `<p class="muted">No Steam account was found on this PC. Search by name instead.</p>`
    }

    <h3>Or search by Steam name</h3>
    <div class="row">
      <input class="input grow" id="linkQuery-${game}" type="text" placeholder="Steam display name" data-enter="link-search" data-game="${game}" />
      <button class="btn" data-act="link-search" data-game="${game}" type="button" ${LINK.searching ? "disabled" : ""}>${LINK.searching ? "Searching…" : "Search"}</button>
    </div>
    ${LINK.error[game] ? `<div class="note err">${esc(LINK.error[game])}</div>` : ""}
    <div class="pick-list">${results
      .map(
        (r) => `<button class="pick" data-act="link-account" data-game="${game}" data-id="${r.accountId}" data-name="${esc(r.personaname)}" data-avatar="${esc(r.avatar || "")}" type="button">
          <span class="row">${imgHtml(r.avatar, "avatar small")}<span><b>${esc(r.personaname)}</b><span class="muted"> account ${r.accountId}</span></span></span>
          <span class="muted">Use this account</span></button>`
      )
      .join("")}</div>
    ${
      game === "dota"
        ? `<p class="hint">Can't find your matches after linking? In Dota 2 open Settings, then Social, and turn on "Expose Public Match Data".</p>`
        : ""
    }
  </div>`;
}

async function loadSteamAccounts() {
  if (LINK.accounts !== null || LINK.busy) return;
  LINK.busy = true;
  try {
    LINK.accounts = await invoke("steam_accounts");
  } catch (_) {
    LINK.accounts = [];
  }
  LINK.busy = false;
  rerender();
}

act("link-search", async (el) => {
  const game = el.dataset.game;
  const q = ($(`#linkQuery-${game}`) || {}).value || "";
  LINK.searching = true;
  LINK.error[game] = null;
  LINK.results[game] = [];
  rerender();
  try {
    LINK.results[game] = await invoke(game === "deadlock" ? "deadlock_search" : "dota_search", { query: q });
    if (!LINK.results[game].length) LINK.error[game] = "No Steam profiles match that name.";
  } catch (e) {
    LINK.error[game] = e.message;
  }
  LINK.searching = false;
  rerender();
});

act("link-account", async (el) => {
  const game = el.dataset.game;
  const args = { accountId: Number(el.dataset.id), personaname: el.dataset.name || `Account ${el.dataset.id}`, avatar: el.dataset.avatar || null };
  const link = await attempt(() => invoke(game === "deadlock" ? "deadlock_link" : "dota_link", args));
  if (!link) return;
  if (game === "deadlock") {
    S.boot.deadlockLink = link;
    dlOverview.clear();
  } else {
    S.boot.dotaLink = link;
    dHist.clear();
    dPlayer.clear();
  }
  toast(`${game === "deadlock" ? "Deadlock" : "Dota 2"} connected to ${args.personaname}.`);
  ACTIONS.refresh();
});

// ---------- The merged timeline ----------
//
// OpenDota's list plus the matches this app recorded that OpenDota lacks, in
// one shape, so one table and one set of figures covers everything played.

function localKind(gameType) {
  switch (gameType) {
    case "turbo": return { modeKey: "turbo", modeName: "Turbo", ranked: false };
    case "all_pick":
    case "unranked": return { modeKey: "all_pick", modeName: "All Pick", ranked: false };
    case "ranked": return { modeKey: null, modeName: null, ranked: true };
    case "other": return { modeKey: null, modeName: null, ranked: false };
    default: return { modeKey: null, modeName: null, ranked: null };
  }
}

function localOnlyMatches() {
  const od = (dHist.data && dHist.data.matches) || [];
  const listed = new Set(od.map((m) => String(m.matchId)));
  // Only inside the span OpenDota's list covers: an older recorded match is
  // most likely on OpenDota too, just past the last row it returned.
  const since = od.length >= D.limit ? Math.min(...od.map((m) => m.startTime)) : 0;
  const rows = [];
  for (const h of S.history || []) {
    if (listed.has(String(h.matchid))) continue;
    const ended = Date.parse(h.date) / 1000;
    if (!Number.isFinite(ended)) continue;
    const duration = clockSeconds(h.duration);
    const startTime = Math.round(ended - (duration || 0));
    if (startTime < since) continue;
    const assists = known(h.assists) ? h.assists : null;
    rows.push({
      matchId: String(h.matchid), local: true, tag: h.gameType,
      heroName: heroName(h.heroName), heroSlug: heroSlug(h.heroName),
      startTime, durationSeconds: duration, won: known(h.won) ? h.won : null, abandoned: false, incomplete: !!h.incomplete,
      kills: h.kills, deaths: h.totalDeaths, assists,
      kda: assists === null ? null : (h.kills + assists) / Math.max(1, h.totalDeaths),
      lastHits: known(h.lastHits) ? h.lastHits : null, goldPerMin: known(h.gpm) ? h.gpm : null, xpPerMin: known(h.xpm) ? h.xpm : null,
      heroDamage: null, ...localKind(h.gameType),
    });
  }
  return rows;
}

function timeline() {
  const od = (dHist.data && dHist.data.matches) || [];
  return [...od, ...localOnlyMatches()].sort((a, b) => b.startTime - a.startTime);
}

const avgOf = (rows, key) => {
  const vals = rows.map((m) => m[key]).filter(known);
  return vals.length ? vals.reduce((a, b) => a + b, 0) / vals.length : null;
};

function recordOf(rows) {
  const decided = rows.filter((m) => !m.abandoned && known(m.won));
  const wins = decided.filter((m) => m.won).length;
  return { wins, losses: decided.length - wins, games: decided.length, rate: decided.length ? (wins * 100) / decided.length : null };
}

function kdaOf(rows) {
  const r = rows.filter((m) => known(m.assists));
  const sum = (k) => r.reduce((a, m) => a + m[k], 0);
  return r.length ? (sum("kills") + sum("assists")) / Math.max(1, sum("deaths")) : null;
}

function heroAggregate(rows) {
  const by = new Map();
  for (const m of rows) {
    if (!m.heroSlug) continue;
    let h = by.get(m.heroSlug);
    if (!h) by.set(m.heroSlug, (h = { slug: m.heroSlug, name: m.heroName, rows: [] }));
    h.rows.push(m);
  }
  return [...by.values()].map((h) => ({ ...h, ...recordOf(h.rows), played: h.rows.length, kda: kdaOf(h.rows), gpm: avgOf(h.rows, "goldPerMin"), last: h.rows[0].startTime }));
}

const toneOfRate = (rate) => (!known(rate) ? "" : rate >= 50 ? "win" : "loss");

// ---------- Overview ----------

view("overview", {
  game: "dota", nav: true, icon: "overview", title: "Overview",
  sub: () => (dotaLinked() ? "Your recent form and lifetime record" : "Connect Steam to see your matches"),
  load(force) {
    if (!dotaLinked()) return loadSteamAccounts();
    ensureHeroList();
    dInsights.load(force);
    return Promise.all([dHist.load(force), dPlayer.load(force), loadHistory()]);
  },
  render() {
    if (!dotaLinked()) return linkPanelHtml("dota");
    const wait = gate(dHist, "Loading your matches…");
    if (wait) return wait;

    const rows = timeline();
    const p = dPlayer.data;
    const link = S.boot.dotaLink;
    if (!rows.length) {
      return emptyState("No matches found for this account", "If you've played recently, your match data may be private. In Dota 2 open Settings, then Social, and turn on \"Expose Public Match Data\".",
        `<button class="btn" data-act="go" data-view="settings" type="button">Change account</button>`);
    }

    const rec = recordOf(rows);
    const results = rows.filter((m) => !m.abandoned).map((m) => m.won);
    const streak = streakOf(results);
    const midnight = new Date();
    midnight.setHours(0, 0, 0, 0);
    const today = recordOf(rows.filter((m) => m.startTime >= midnight.getTime() / 1000));
    const chrono = [...rows].reverse();

    const heroes = heroAggregate(rows).sort((a, b) => b.played - a.played);
    const solid = heroes.filter((h) => h.games >= 3);
    const best = solid.filter((h) => h.rate >= 50).sort((a, b) => b.rate - a.rate || b.games - a.games).slice(0, 3);
    const worst = solid.filter((h) => h.rate < 50).sort((a, b) => a.rate - b.rate || b.games - a.games).slice(0, 3);

    const modes = new Map();
    for (const m of rows) {
      const key = m.modeName || "Mode not known";
      if (!modes.has(key)) modes.set(key, []);
      modes.get(key).push(m);
    }
    const modeRows = [...modes.entries()].map(([name, list]) => ({ name, ...recordOf(list), played: list.length })).sort((a, b) => b.played - a.played);

    const fav = S.boot.prefs.favorites.dota;
    const favHero = fav ? heroes.find((h) => h.slug === fav) : null;
    const ins = (dInsights.data && dInsights.data.insights) || [];
    const local = rows.filter((m) => m.local).length;

    const heroLine = (h) => `<button class="line" data-act="go" data-view="heroes" data-params='${JSON.stringify({ hero: h.slug })}' type="button">
      ${heroImgHtml(h.slug)}<span class="grow"><b>${esc(h.name)}</b><span class="muted"> ${h.played} games</span></span>
      <span class="num ${toneOfRate(h.rate)}">${pct(h.rate)}</span></button>`;

    return `
      ${staleNote(dHist)}
      <section class="hero-head">
        ${imgHtml((p && p.avatar) || link.avatar, "avatar large")}
        <div class="grow">
          <h2 class="display">${esc((p && p.personaname) || link.personaname || "Your account")}</h2>
          <p class="muted">${[
            p && p.rank ? esc(p.rank) + (p.leaderboardRank ? ` (rank ${p.leaderboardRank})` : "") : null,
            p && p.wins + p.losses ? `${fmtNum(p.wins)} wins and ${fmtNum(p.losses)} losses all-time (${pct(p.winRate, 1)})` : null,
          ].filter(Boolean).join(", ") || "Lifetime record loading…"}</p>
          ${formStrip(results, 30)}
        </div>
        <div class="today">
          <div class="stat-label">Today</div>
          <div class="display ${today.games ? toneOfRate(today.rate) : ""}">${today.games ? `${today.wins} – ${today.losses}` : "No games yet"}</div>
          ${streak && streak.n >= 2 ? `<div class="muted">${streak.n} ${streak.won ? "wins" : "losses"} in a row</div>` : ""}
        </div>
      </section>

      ${statRow([
        { label: `Win rate, last ${rows.length}`, value: pct(rec.rate), tone: toneOfRate(rec.rate), sub: `${rec.wins} won, ${rec.losses} lost${local ? `, ${local} recorded here` : ""}`,
          extra: sparkline(chrono.filter((m) => known(m.won)).map((_, i, arr) => { const w = arr.slice(Math.max(0, i - 9), i + 1); return (w.filter((x) => x.won).length / w.length) * 100; })) },
        { label: "KDA", value: dash(kdaOf(rows), (v) => v.toFixed(2)), sub: `${dash(avgOf(rows, "kills"), (v) => v.toFixed(1))} / ${dash(avgOf(rows, "deaths"), (v) => v.toFixed(1))} / ${dash(avgOf(rows, "assists"), (v) => v.toFixed(1))}`, extra: sparkline(chrono.map((m) => m.kda)) },
        { label: "Gold per minute", value: dash(avgOf(rows, "goldPerMin"), Math.round), sub: `${dash(avgOf(rows, "xpPerMin"), Math.round)} XP per minute`, extra: sparkline(chrono.map((m) => m.goldPerMin)) },
        { label: "Last hits", value: dash(avgOf(rows, "lastHits"), Math.round), sub: "per match", extra: sparkline(chrono.map((m) => m.lastHits)) },
      ])}

      <div class="cols">
        <section>
          <div class="sec-head"><h3>Recent matches</h3><button class="link" data-act="go" data-view="matches" type="button">All matches</button></div>
          <div class="lines">${rows.slice(0, 7).map((m) => `
            <button class="line ${resultClass(m)}" data-act="go" data-view="matches" type="button">
              ${heroImgHtml(m.heroSlug)}
              <span class="grow"><b>${esc(m.heroName)}</b><span class="muted"> ${esc(m.modeName || gameTypeLabel(m.tag))}</span></span>
              <span class="num">${m.kills}/${m.deaths}/${dash(m.assists)}</span>
              <span class="res ${resultClass(m)}">${resultText(m)}</span>
              <span class="muted when">${ago(m.startTime)}</span>
            </button>`).join("")}</div>

          ${ins.length ? `<div class="sec-head"><h3>From your recorded games</h3><button class="link" data-act="go" data-view="sessions" type="button">Sessions</button></div>
            <div class="insights">${ins.slice(0, 3).map(insightHtml).join("")}</div>` : ""}
        </section>

        <section>
          ${favHero ? `<div class="sec-head"><h3>Your hero</h3></div><div class="lines">${heroLine(favHero)}</div>` : ""}
          ${best.length ? `<div class="sec-head"><h3>Winning most with</h3></div><div class="lines">${best.map(heroLine).join("")}</div>` : ""}
          ${worst.length ? `<div class="sec-head"><h3>Struggling with</h3></div><div class="lines">${worst.map(heroLine).join("")}</div>` : ""}
          <div class="sec-head"><h3>By mode</h3></div>
          <div class="lines">${modeRows.map((m) => `<div class="line static"><span class="grow"><b>${esc(m.name)}</b><span class="muted"> ${m.played} games</span></span><span class="num ${toneOfRate(m.rate)}">${pct(m.rate)}</span></div>`).join("")}</div>
          ${p && p.peers.length ? `<div class="sec-head"><h3>Played with most</h3></div>
            <div class="lines">${p.peers.slice(0, 5).map((x) => `<div class="line static">${imgHtml(x.avatar, "avatar small")}<span class="grow"><b>${esc(x.personaname)}</b><span class="muted"> ${x.games} games together</span></span><span class="num ${toneOfRate(x.winRate)}">${pct(x.winRate)}</span></div>`).join("")}</div>` : ""}
        </section>
      </div>`;
  },
});

const resultClass = (m) => (m.abandoned ? "other" : m.won === true ? "win" : m.won === false ? "loss" : "other");
const resultText = (m) => (m.abandoned ? "Left" : m.won === true ? "Won" : m.won === false ? "Lost" : "–");

function insightHtml(i) {
  return `<div class="insight ${esc(i.tone)}"><b>${esc(i.title)}</b><p>${esc(i.detail)}</p></div>`;
}

// ---------- Live ----------

const GSI = { busy: false, error: null };

async function refreshGsi() {
  try {
    S.boot.gsi = await invoke("gsi_status");
  } catch (_) {
    /* keep the last status */
  }
  rerender();
}

function setupRow(state, title, detail) {
  return `<div class="check ${state === true ? "ok" : state === false ? "bad" : "idle"}"><span class="dot"></span><div><b>${title}</b><p>${detail}</p></div></div>`;
}

function liveSetupHtml() {
  const g = S.boot.gsi;
  const live = S.live || {};
  const rows = [];

  if (live.serverError) {
    rows.push(setupRow(false, "Live tracking couldn't start", esc(live.serverError)));
  }
  if (!g.cfgDirs.length) {
    rows.push(setupRow(false, "Dota 2 wasn't found", "TheTracker looked in every Steam library on this PC. Install Dota 2 through Steam, then press Check again."));
  } else if (g.installed) {
    rows.push(setupRow(true, "Dota is set up to send match data", `TheTracker added a small config file to Dota's folder. It listens on port ${g.port}.`));
  } else {
    rows.push(setupRow(false, "Dota isn't set up to send match data yet", "TheTracker needs to add one small config file to Dota's folder."));
  }

  if (g.launchOption === true) {
    rows.push(setupRow(true, "Launch option is set", "<code>-gamestateintegration</code> is in Dota's launch options."));
  } else if (g.cfgDirs.length) {
    rows.push(setupRow(g.launchOption === false ? false : null, "Launch option needed",
      `Dota only sends data when it starts with <code>-gamestateintegration</code>. Use <b>Play Dota</b> below, or add it once in Steam: right-click Dota 2, Properties, and paste it into Launch Options.
       <span class="copy"><code id="launchOpt">-gamestateintegration</code><button class="link" data-act="copy-launch" type="button">Copy</button></span>`));
  }

  if (dotaConnected()) rows.push(setupRow(true, "Dota is connected", "Receiving data right now. Your next match will appear here when it starts."));
  else if (known(live.gsiAgeSecs)) rows.push(setupRow(null, "Dota is closed", `Last heard from Dota ${agoSecs(live.gsiAgeSecs)}.`));
  else if (g.installed) rows.push(setupRow(null, "Nothing from Dota yet", "Expected while Dota is closed. If Dota is open, restart it: it only reads the config when it starts."));

  if (live.trackingEnabled === false) rows.push(setupRow(false, "Recording is paused", "Matches won't be saved until you press Paused in the top bar to resume."));

  const bg = S.boot.background;
  return `<div class="panel narrow">
    <h2>Waiting for a match</h2>
    <p class="muted">This page fills in by itself the moment a Dota match starts: your last hits at each mark, deaths, gold lost and key items, compared with your own averages.</p>
    <div class="checks">${rows.join("")}</div>
    ${GSI.error ? `<div class="note err">${esc(GSI.error)}</div>` : ""}
    <div class="row wrap">
      ${g.cfgDirs.length ? `<button class="btn" data-act="launch-dota" type="button">Play Dota</button>` : ""}
      ${g.cfgDirs.length && !g.installed ? `<button class="btn" data-act="gsi-install" type="button" ${GSI.busy ? "disabled" : ""}>Set up Dota</button>` : ""}
      <button class="btn ghost" data-act="gsi-check" type="button">Check again</button>
      <button class="btn ghost" data-act="sim-start" type="button" title="Plays a short fake match so you can see this page and the overlay working. Nothing is saved.">Try it with a test match</button>
    </div>
    ${bg && bg.trayAvailable && !bg.startWithWindows ? `<p class="hint">TheTracker can only record a match while it's running. <button class="link" data-act="autostart-yes" type="button">Start it with Windows</button> so it's always there.</p>` : ""}
    ${g.cfgDirs.length ? `<details class="more"><summary>What TheTracker changed on this PC</summary>
      <p class="muted">One file, in each Dota install. It tells Dota to send your own match state to this app, on this PC only. Nothing reads the game's memory.</p>
      ${g.cfgDirs.map((d) => `<code class="path">${esc(d)}\\gamestate_integration\\gamestate_integration_thetracker.cfg</code>`).join("")}
      ${g.installed ? `<button class="link" data-act="gsi-remove" type="button">Remove the file</button>` : ""}
    </details>` : ""}
  </div>`;
}

act("gsi-check", refreshGsi);
act("gsi-install", async () => {
  GSI.busy = true;
  GSI.error = null;
  rerender();
  try {
    S.boot.gsi = await invoke("gsi_install");
    toast("Dota is set up. Restart Dota if it's open.");
  } catch (e) {
    GSI.error = e.message;
  }
  GSI.busy = false;
  rerender();
});
act("gsi-remove", async () => {
  const s = await attempt(() => invoke("gsi_remove"), "Removed. Dota will stop sending data.");
  if (s) S.boot.gsi = s;
  rerender();
});
act("launch-dota", () => attempt(() => invoke("launch_dota"), "Starting Dota through Steam…"));
act("copy-launch", async () => {
  try {
    await navigator.clipboard.writeText("-gamestateintegration");
    toast("Copied.");
  } catch (_) {
    toast("Couldn't copy. Select the text and press Ctrl+C.", "err");
  }
});
act("sim-start", async () => {
  if (await attempt(() => invoke("sim_start", { seconds: 90 }), "Test match started. It runs for 90 seconds and isn't saved.")) pollLive();
});
act("sim-stop", async () => {
  await attempt(() => invoke("sim_stop"));
  pollLive();
});
act("live-type", async (el) => {
  const live = await attempt(() => invoke("set_live_game_type", { gameType: el.dataset.type }));
  if (live) applyLive(live);
});
act("roshan", async () => {
  const live = await attempt(() => invoke("mark_roshan_death"));
  if (live) applyLive(live);
});
act("toggle-log", async () => {
  D.showLog = !D.showLog;
  if (D.showLog) D.log = (await invoke("get_live", { log: true }).catch(() => ({}))).log || [];
  rerender();
});
act("live-dismiss", () => {
  D.dismissed = S.live && S.live.current && S.live.current.matchid;
  rerender();
});

/// Your own average last hits at a checkpoint, over recorded games of the
/// same type (or all of them when the type is not set yet).
function myCheckpointAvg(minute, gameType) {
  const pool = S.history.filter((h) => (gameType === "unspecified" || h.gameType === gameType) && h.checkpoints && h.checkpoints[minute]);
  if (pool.length < 2) return null;
  return pool.reduce((a, h) => a + h.checkpoints[minute].lastHits, 0) / pool.length;
}

function goalsHtml(goals) {
  if (!goals || !goals.length) return "";
  return `<div class="goals">${goals
    .map((g) => {
      const state = !known(g.value) ? "idle" : g.met ? "ok" : "bad";
      return `<div class="goal ${state}"><span class="dot"></span><span>${esc(g.label)} <b>${g.target}</b></span><span class="num">${dash(g.value)}</span></div>`;
    })
    .join("")}</div>`;
}

function comparisonHtml(s) {
  const c = s.comparison;
  if (!c || !s.gamesComparedAgainst) return `<p class="hint">Comparisons appear once you have other ${esc(gameTypeLabel(s.gameType))} games recorded.</p>`;
  const verdict = (m, unit = "") => {
    if (m.verdict === "no_data") return `<span class="muted">no data</span>`;
    const word = m.isBest ? "your best yet" : m.verdict === "better" ? "better than usual" : m.verdict === "worse" ? "worse than usual" : "about your usual";
    return `<span class="${m.verdict === "better" ? "win" : m.verdict === "worse" ? "loss" : "muted"}">${word}</span> <span class="muted">(you average ${m.avg}${unit})</span>`;
  };
  const cps = Object.keys(c.checkpoints).map(Number).sort((a, b) => a - b).filter((min) => known(c.checkpoints[min].value));
  return `<table class="mini">
    <tr><td>Deaths</td><td class="num">${s.totalDeaths}</td><td>${verdict(c.deaths)}</td></tr>
    <tr><td>Gold lost to deaths</td><td class="num">${fmtNum(s.totalGoldLost)}</td><td>${verdict(c.goldLost)}</td></tr>
    ${cps.map((min) => `<tr><td>Last hits at ${min}:00</td><td class="num">${c.checkpoints[min].value}</td><td>${verdict(c.checkpoints[min])}</td></tr>`).join("")}
  </table><p class="hint">Compared with your other ${s.gamesComparedAgainst} ${esc(gameTypeLabel(s.gameType))} games.</p>`;
}

function liveMatchHtml(m) {
  const live = S.live;
  const cps = [5, 10, 15, 20, 25];
  const typeChips = GAME_TYPES.map((g) => `<button class="chip ${m.gameType === g.id ? "on" : ""}" data-act="live-type" data-type="${g.id}" type="button">${g.label}</button>`).join("");

  if (m.ended && m.summary) {
    const s = m.summary;
    return `<div class="panel">
      ${m.simulated ? `<div class="note">This was a test match. It wasn't saved.</div>` : ""}
      <div class="live-head">
        ${heroImgHtml(m.heroName, "portrait big")}
        <div class="grow"><h2 class="display">${esc(heroName(m.heroName))} <span class="res ${s.won === true ? "win" : s.won === false ? "loss" : "other"}">${s.won === true ? "Won" : s.won === false ? "Lost" : "Finished"}</span></h2>
          <p class="muted">${esc(s.duration)} ${esc(gameTypeLabel(s.gameType))}${s.incomplete ? ", ended early" : ""}</p></div>
        <button class="btn ghost" data-act="live-dismiss" type="button">Done</button>
      </div>
      ${statRow([
        { label: "K / D / A", value: `${s.kills} / ${s.totalDeaths} / ${dash(s.assists)}` },
        { label: "Last hits", value: dash(s.lastHits), sub: `${dash(s.denies)} denies` },
        { label: "Gold per minute", value: dash(s.gpm), sub: `${dash(s.xpm)} XP per minute` },
        { label: "Gold lost to deaths", value: fmtNum(s.totalGoldLost) },
      ])}
      ${goalsHtml(live.goals)}
      ${m.simulated ? "" : comparisonHtml(s)}
    </div>`;
  }

  const state = !live.live && !m.inProgress ? (m.lastClockTime < 0 ? "Picking heroes" : "Waiting for the horn") : live.live ? "In progress" : "Dota stopped reporting";
  return `<div class="panel">
    ${m.simulated ? `<div class="note">Test match: nothing here is saved. <button class="link" data-act="sim-stop" type="button">Stop the test</button></div>` : ""}
    <div class="live-head">
      ${heroImgHtml(m.heroName, "portrait big")}
      <div class="grow"><h2 class="display">${esc(heroName(m.heroName))}</h2>
        <p class="muted"><span class="live-dot ${live.live ? "" : "off"}"></span> ${state}${m.team ? `, ${m.team === "radiant" ? "Radiant" : "Dire"}` : ""}${known(m.level) ? `, level ${m.level}` : ""}</p></div>
      <div class="clock display">${fmtClock(m.lastClockTime)}</div>
    </div>
    ${statRow([
      { label: "K / D / A", value: `${m.kills} / ${m.deaths.length} / ${dash(m.assists)}` },
      { label: "Last hits", value: m.lastHits, sub: `${m.denies} denies` },
      { label: "Gold per minute", value: dash(m.gpm), sub: `${dash(m.xpm)} XP per minute` },
      { label: "Gold lost to deaths", value: fmtNum(m.deaths.reduce((a, d) => a + (d.goldLost || 0), 0)), sub: known(m.gold) ? `${fmtNum(m.gold)} unspent now` : "" },
    ])}
    ${goalsHtml(live.goals)}

    <div class="cols">
      <section>
        <div class="sec-head"><h3>Last hits at each mark</h3></div>
        <table class="mini">${cps.map((min) => {
          const cp = m.checkpoints[min];
          const avg = myCheckpointAvg(min, m.gameType);
          const diff = cp && known(avg) ? cp.lastHits - avg : null;
          return `<tr><td>${min}:00</td><td class="num">${cp ? cp.lastHits : `<span class="muted">${m.lastClockTime > min * 60 ? "missed" : "–"}</span>`}</td>
            <td>${known(diff) ? `<span class="${diff >= 0 ? "win" : "loss"}">${diff >= 0 ? "+" : ""}${diff.toFixed(0)}</span> <span class="muted">vs your ${avg.toFixed(0)} average</span>` : known(avg) ? `<span class="muted">you average ${avg.toFixed(0)}</span>` : ""}</td></tr>`;
        }).join("")}</table>

        <div class="sec-head"><h3>Game type</h3></div>
        <div class="chips">${typeChips}</div>
        <p class="hint">Dota doesn't say which mode a match is. Pick one, or leave it: TheTracker fills it in from OpenDota afterwards. Turbo changes the overlay's lotus timing.</p>
      </section>
      <section>
        <div class="sec-head"><h3>Deaths</h3></div>
        ${m.deaths.length ? `<table class="mini">${m.deaths.map((d) => `<tr><td>${esc(d.clock)}</td><td class="num loss">${known(d.goldLost) ? `-${fmtNum(d.goldLost)} gold` : ""}</td></tr>`).join("")}</table>` : `<p class="muted">None so far.</p>`}
        <div class="sec-head"><h3>Key items</h3></div>
        ${m.keyItemLog.length ? `<div class="item-times">${m.keyItemLog.map((k) => `<span class="item-time">${itemImgHtml(k.item)}<span class="num">${esc(k.clock)}</span></span>`).join("")}</div>` : `<p class="muted">Boots, Blink, BKB and other big items are timed as you buy them.</p>`}
        <div class="sec-head"><h3>Roshan</h3></div>
        <div class="row"><span class="muted">${m.roshan.deaths ? `Killed ${m.roshan.deaths} time${m.roshan.deaths === 1 ? "" : "s"}, last at ${fmtClock(m.roshan.lastDeathClock)}` : "No kills noted"}</span>
          <button class="btn ghost small" data-act="roshan" type="button">Roshan just died</button></div>
      </section>
    </div>
    <button class="link" data-act="toggle-log" type="button">${D.showLog ? "Hide" : "Show"} the event log</button>
    ${D.showLog ? `<pre class="log">${esc((D.log || []).slice(-60).join("\n")) || "Nothing logged yet."}</pre>` : ""}
  </div>`;
}

view("live", {
  game: "dota", nav: true, icon: "live", title: "Live", live: true,
  sub: () => (S.live && S.live.live ? "Tracking your match" : "Fills in when a match starts"),
  load() {
    loadHistory();
    return refreshGsi();
  },
  render() {
    const m = S.live && S.live.current;
    if (!m || (m.ended && D.dismissed === m.matchid)) return liveSetupHtml();
    return liveMatchHtml(m);
  },
});

// ---------- Matches ----------

const MATCH_COLS = [
  { key: "heroName", label: "Hero" },
  { key: "result", label: "Result" },
  { key: "kda", label: "KDA", num: true },
  { key: "kills", label: "K", num: true },
  { key: "deaths", label: "D", num: true },
  { key: "assists", label: "A", num: true },
  { key: "lastHits", label: "LH", num: true },
  { key: "goldPerMin", label: "GPM", num: true },
  { key: "xpPerMin", label: "XPM", num: true },
  { key: "heroDamage", label: "Damage", num: true },
  { key: "durationSeconds", label: "Length", num: true },
  { key: "startTime", label: "Played", num: true },
];

function sortRows(list, key, dir) {
  const val = (m) => (key === "result" ? (m.abandoned ? 2 : m.won === true ? 0 : m.won === false ? 1 : null) : m[key]);
  const d = dir === "asc" ? 1 : -1;
  return [...list].sort((a, b) => {
    const av = val(a), bv = val(b);
    // A figure a match doesn't have sorts last in either direction.
    if (!known(av) || !known(bv)) return known(av) ? -1 : known(bv) ? 1 : 0;
    return typeof av === "string" ? av.localeCompare(bv) * d : (av - bv) * d;
  });
}

function scoreboardHtml(id) {
  const d = D.details.get(id);
  if (!d) return `<div class="muted">Loading the scoreboard…</div>`;
  if (d.error) return `<div class="note err">${esc(d.error)}</div>`;
  const side = (radiant) => {
    const players = d.players.filter((p) => p.radiant === radiant);
    return `<table class="board"><thead><tr><th>${radiant ? "Radiant" : "Dire"} ${radiant === d.radiantWin ? `<span class="res win">won</span>` : ""}</th><th class="num">K</th><th class="num">D</th><th class="num">A</th><th class="num">LH</th><th class="num">GPM</th><th class="num">Net worth</th><th class="num">Damage</th><th>Items</th></tr></thead>
      <tbody>${players.map((p) => `<tr class="${p.isMe ? "me" : ""}"><td><span class="cell">${heroImgHtml(p.heroSlug, "portrait small")}<span>${esc(p.name)}<span class="muted"> ${esc(p.heroName)}</span></span></span></td>
        <td class="num">${p.kills}</td><td class="num">${p.deaths}</td><td class="num">${p.assists}</td><td class="num">${p.lastHits}</td><td class="num">${p.goldPerMin}</td><td class="num">${fmtNum(p.netWorth)}</td><td class="num">${fmtNum(p.heroDamage)}</td>
        <td><span class="items">${p.items.map((k) => itemImgHtml(k, "item small")).join("")}</span></td></tr>`).join("")}</tbody></table>`;
  };
  return `<div class="row"><span class="muted">${d.radiantScore} to ${d.direScore}, ${fmtClock(d.durationSeconds)}, ${esc(d.modeName)} (${esc(d.lobbyName)})</span><span class="grow"></span>
    <button class="link" data-act="open-url" data-url="https://www.opendota.com/matches/${d.matchId}" type="button">Open on OpenDota</button></div>
    ${side(true)}${side(false)}`;
}

act("match-toggle", async (el) => {
  const id = Number(el.dataset.id);
  if (D.open.has(id)) {
    D.open.delete(id);
    return rerender();
  }
  D.open.add(id);
  rerender();
  if (!D.details.has(id) || D.details.get(id).error) {
    try {
      D.details.set(id, await invoke("dota_match_detail", { matchId: id }));
    } catch (e) {
      D.details.set(id, { error: e.message });
    }
    rerender();
  }
});
act("match-local", (el) => {
  D.openSessions.add(el.dataset.id);
  go("sessions", { focus: el.dataset.id });
});
act("match-sort", (el) => {
  const key = el.dataset.key;
  if (D.sortKey === key) D.sortDir = D.sortDir === "asc" ? "desc" : "asc";
  else [D.sortKey, D.sortDir] = [key, key === "heroName" ? "asc" : "desc"];
  rerender();
});
act("match-filter", (el) => {
  D[el.dataset.axis] = el.dataset.value;
  rerender();
});
onChange("match-hero", (el) => {
  D.heroQuery = el.value;
  rerender();
});
onChange("match-limit", (el) => {
  D.limit = Number(el.value);
  dHist.load();
  rerender();
});
act("od-rescan", () => attempt(() => invoke("dota_refresh"), "Asked OpenDota to rescan your account. New matches usually appear within a few minutes."));

view("matches", {
  game: "dota", nav: true, icon: "matches", title: "Matches",
  sub: () => (dHist.data ? `${timeline().length} matches from OpenDota and your recorded sessions` : ""),
  load(force) {
    if (!dotaLinked()) return loadSteamAccounts();
    ensureHeroList();
    return Promise.all([dHist.load(force), loadHistory()]);
  },
  render() {
    if (!dotaLinked()) return linkPanelHtml("dota");
    const wait = gate(dHist, "Loading your matches…");
    if (wait) return wait;
    const rows = timeline();
    if (!rows.length) return emptyState("No matches found for this account", "Matches appear here once OpenDota has them, and straight away for any game TheTracker records.");

    const q = D.heroQuery.trim().toLowerCase();
    const shown = rows.filter(
      (m) =>
        (D.mode === "all" || m.modeKey === D.mode) &&
        (D.queue === "all" || (known(m.ranked) && (D.queue === "ranked") === !!m.ranked)) &&
        (D.result === "all" || (D.result === "win" ? m.won === true : m.won === false)) &&
        (!q || m.heroName.toLowerCase().includes(q))
    );
    const modes = new Map();
    for (const m of rows) if (m.modeKey) modes.set(m.modeKey, m.modeName);
    const chip = (axis, value, label) => `<button class="chip ${D[axis] === value ? "on" : ""}" data-act="match-filter" data-axis="${axis}" data-value="${esc(value)}" type="button">${esc(label)}</button>`;
    const rec = recordOf(shown);
    const local = rows.filter((m) => m.local).length;

    return `${staleNote(dHist)}
      <div class="filters">
        <div class="chips">${chip("mode", "all", "All modes")}${[...modes].sort((a, b) => a[1].localeCompare(b[1])).map(([k, n]) => chip("mode", k, n)).join("")}</div>
        <div class="chips">${chip("queue", "all", "Any queue")}${chip("queue", "ranked", `Ranked (${rows.filter((m) => m.ranked === true).length})`)}${chip("queue", "unranked", `Unranked (${rows.filter((m) => m.ranked === false).length})`)}
          <span class="sep"></span>${chip("result", "all", "Any result")}${chip("result", "win", "Wins")}${chip("result", "loss", "Losses")}</div>
        <div class="row">
          <input class="input" id="matchHero" type="search" placeholder="Filter by hero" value="${esc(D.heroQuery)}" data-input="match-hero" />
          <label class="muted" for="matchLimit">Show last</label>
          <select class="input" id="matchLimit" data-change="match-limit">${[50, 100, 200, 500].map((n) => `<option value="${n}" ${D.limit === n ? "selected" : ""}>${n}</option>`).join("")}</select>
          <span class="grow"></span>
          <span class="muted">${shown.length} shown${rec.games ? `, ${rec.wins} won and ${rec.losses} lost (${pct(rec.rate)})` : ""}</span>
        </div>
      </div>
      ${shown.length ? `<div class="table-wrap"><table class="table">
        <thead><tr>${MATCH_COLS.map((c) => `<th class="${c.num ? "num" : ""} ${D.sortKey === c.key ? "sorted" : ""}"><button data-act="match-sort" data-key="${c.key}" type="button">${c.label}${D.sortKey === c.key ? (D.sortDir === "asc" ? " ▲" : " ▼") : ""}</button></th>`).join("")}</tr></thead>
        <tbody>${sortRows(shown, D.sortKey, D.sortDir).map((m) => {
          const open = !m.local && D.open.has(m.matchId);
          const sub = m.local
            ? `${esc(m.modeName || (m.tag === "ranked" ? "Ranked" : "Mode not known yet"))}, <span class="accent">recorded by TheTracker</span>${m.incomplete ? ", ended early" : ""}`
            : `${esc(m.modeName)}, ${esc(m.lobbyName)}${m.partySize > 1 ? `, party of ${m.partySize}` : ""}`;
          return `<tr class="click ${resultClass(m)}" data-act="${m.local ? "match-local" : "match-toggle"}" data-id="${esc(m.matchId)}" title="${m.local ? "Open this recorded match in Sessions" : "Show the scoreboard"}">
            <td><span class="cell">${heroImgHtml(m.heroSlug)}<span><b>${esc(m.heroName)}</b><span class="sub">${sub}</span></span></span></td>
            <td><span class="res ${resultClass(m)}">${resultText(m)}</span></td>
            <td class="num ${known(m.kda) ? (m.kda >= 4 ? "win" : m.kda < 1.5 ? "loss" : "") : ""}">${dash(m.kda, (v) => v.toFixed(2))}</td>
            <td class="num">${m.kills}</td><td class="num">${m.deaths}</td><td class="num">${dash(m.assists)}</td>
            <td class="num">${dash(m.lastHits)}</td><td class="num">${dash(m.goldPerMin)}</td><td class="num">${dash(m.xpPerMin)}</td>
            <td class="num">${dash(m.heroDamage, fmtNum)}</td><td class="num">${dash(m.durationSeconds, fmtClock)}</td><td class="num muted">${ago(m.startTime)}</td></tr>
            ${open ? `<tr class="detail"><td colspan="${MATCH_COLS.length}">${scoreboardHtml(m.matchId)}</td></tr>` : ""}`;
        }).join("")}</tbody></table></div>` : emptyState("Nothing matches those filters", "Try a different mode, queue or hero.")}
      <p class="hint">Results, modes and scoreboards come from OpenDota${local ? `; ${local} match${local === 1 ? "" : "es"} it doesn't have ${local === 1 ? "was" : "were"} recorded by TheTracker from Dota's live feed` : ""}.
        A game you just finished can take a few minutes to arrive. <button class="link" data-act="od-rescan" type="button">Ask OpenDota to rescan</button></p>`;
  },
});

// ---------- Heroes ----------

act("hero-sort", (el) => {
  D.heroSort = el.dataset.key;
  rerender();
});
act("hero-fav", async (el) => {
  const slug = el.dataset.slug;
  const current = S.boot.prefs.favorites.dota;
  const prefs = await attempt(() => invoke("set_favorite_hero", { game: "dota", hero: current === slug ? null : slug }));
  if (prefs) {
    S.boot.prefs = prefs;
    toast(current === slug ? "Removed from your overview." : `${heroName(slug)} is now on your overview.`);
    rerender();
  }
});

const HERO = { popular: new Map(), matchups: new Map() };

async function loadHeroExtras(slug) {
  await ensureHeroList();
  const h = S.heroBySlug && S.heroBySlug[slug];
  if (!h) return;
  for (const [store, cmd] of [[HERO.popular, "dota_popular_builds"], [HERO.matchups, "dota_matchups"]]) {
    if (store.has(h.id)) continue;
    store.set(h.id, { loading: true });
    invoke(cmd, { heroId: h.id })
      .then((data) => store.set(h.id, { data }))
      .catch((e) => store.set(h.id, { error: e.message }))
      .finally(rerender);
  }
}

function heroDetailHtml(slug) {
  const rows = timeline().filter((m) => m.heroSlug === slug);
  const life = dPlayer.data && dPlayer.data.heroes.find((h) => h.heroSlug === slug);
  const h = S.heroBySlug && S.heroBySlug[slug];
  const rec = recordOf(rows);
  const fav = S.boot.prefs.favorites.dota === slug;
  const builds = S.boot.prefs.builds.filter((b) => b.game === "dota" && b.hero === slug);
  const pop = h && HERO.popular.get(h.id);
  const mu = h && HERO.matchups.get(h.id);
  const chrono = [...rows].reverse();

  let matchupHtml = `<p class="muted">Loading matchups…</p>`;
  if (mu && mu.error) matchupHtml = `<p class="muted">${esc(mu.error)}</p>`;
  else if (mu && mu.data) {
    const solid = mu.data.filter((x) => x.games >= 15);
    const line = (x) => `<div class="line static">${heroImgHtml(x.heroSlug, "portrait small")}<span class="grow">${esc(x.heroName)}</span><span class="num ${toneOfRate(x.winRate)}">${pct(x.winRate)}</span><span class="muted when">${x.games} games</span></div>`;
    const good = [...solid].sort((a, b) => b.winRate - a.winRate).slice(0, 5);
    const bad = [...solid].sort((a, b) => a.winRate - b.winRate).slice(0, 5);
    matchupHtml = solid.length
      ? `<div class="cols"><section><h4>Does well against</h4><div class="lines">${good.map(line).join("")}</div></section><section><h4>Struggles against</h4><div class="lines">${bad.map(line).join("")}</div></section></div>
         <p class="hint">${esc(heroName(slug))}'s win rate against each hero in OpenDota's matchup data, for pairings with at least 15 games.</p>`
      : `<p class="muted">OpenDota has too few games for this hero to say.</p>`;
  }

  let popHtml = `<p class="muted">Loading popular items…</p>`;
  if (pop && pop.error) popHtml = `<p class="muted">${esc(pop.error)}</p>`;
  else if (pop && pop.data) {
    popHtml = pop.data.length
      ? pop.data.map((ph) => `<div class="phase"><span class="muted">${esc(ph.phase)}</span><span class="items">${ph.items.map((i) => itemImgHtml(i.key)).join("")}</span></div>`).join("") +
        `<p class="hint">What players buy most on this hero at each stage, from OpenDota.</p>`
      : `<p class="muted">OpenDota has no item data for this hero yet.</p>`;
  }

  return `<button class="link back" data-act="go" data-view="heroes" type="button">All heroes</button>
    <section class="hero-head">
      ${heroImgHtml(slug, "portrait huge")}
      <div class="grow"><h2 class="display">${esc(heroName(slug))}</h2>
        <p class="muted">${h ? esc(h.roles.join(", ")) : ""}</p>${formStrip(rows.map((m) => m.won), 30)}</div>
      <button class="btn ${fav ? "" : "ghost"}" data-act="hero-fav" data-slug="${esc(slug)}" type="button">${fav ? "On your overview" : "Put on my overview"}</button>
    </section>
    ${statRow([
      { label: `Last ${rows.length} games`, value: rows.length ? pct(rec.rate) : "–", tone: toneOfRate(rec.rate), sub: rows.length ? `${rec.wins} won, ${rec.losses} lost` : "Not played recently" },
      { label: "All-time", value: life ? pct(life.winRate) : "–", tone: life ? toneOfRate(life.winRate) : "", sub: life ? `${life.games} games, last ${ago(life.lastPlayed)}` : "No games on record" },
      { label: "KDA", value: dash(kdaOf(rows), (v) => v.toFixed(2)), extra: sparkline(chrono.map((m) => m.kda)) },
      { label: "Gold per minute", value: dash(avgOf(rows, "goldPerMin"), Math.round), extra: sparkline(chrono.map((m) => m.goldPerMin)) },
      { label: "Last hits", value: dash(avgOf(rows, "lastHits"), Math.round), extra: sparkline(chrono.map((m) => m.lastHits)) },
    ])}
    <div class="cols">
      <section>
        <div class="sec-head"><h3>Popular items</h3></div>${popHtml}
        <div class="sec-head"><h3>Your builds</h3><button class="link" data-act="build-new" data-hero="${esc(slug)}" type="button">New build</button></div>
        ${builds.length ? builds.map((b) => buildCardHtml(b, false)).join("") : `<p class="muted">You haven't saved a build for this hero.</p>`}
      </section>
      <section>
        <div class="sec-head"><h3>Recent games</h3></div>
        ${rows.length ? `<div class="lines">${rows.slice(0, 8).map((m) => `<div class="line static ${resultClass(m)}"><span class="res ${resultClass(m)}">${resultText(m)}</span><span class="grow muted">${esc(m.modeName || gameTypeLabel(m.tag))}</span><span class="num">${m.kills}/${m.deaths}/${dash(m.assists)}</span><span class="num muted">${dash(m.goldPerMin)} GPM</span><span class="muted when">${ago(m.startTime)}</span></div>`).join("")}</div>` : `<p class="muted">No games in your recent matches.</p>`}
      </section>
    </div>
    <div class="sec-head"><h3>Matchups</h3></div>${matchupHtml}`;
}

view("heroes", {
  game: "dota", nav: true, icon: "heroes", title: "Heroes",
  sub: () => (S.params.hero ? "Your record, builds and matchups" : "Every hero you've played, recent and all-time"),
  load(force) {
    if (!dotaLinked()) return loadSteamAccounts();
    if (S.params.hero) loadHeroExtras(S.params.hero);
    ensureHeroList();
    return Promise.all([dHist.load(force), dPlayer.load(force), loadHistory()]);
  },
  render() {
    if (!dotaLinked()) return linkPanelHtml("dota");
    const wait = gate(dHist, "Loading your heroes…");
    if (wait) return wait;
    if (S.params.hero) return heroDetailHtml(S.params.hero);

    const recent = new Map(heroAggregate(timeline()).map((h) => [h.slug, h]));
    const life = (dPlayer.data && dPlayer.data.heroes) || [];
    const slugs = new Set([...recent.keys(), ...life.map((h) => h.heroSlug).filter(Boolean)]);
    const lifeBy = new Map(life.map((h) => [h.heroSlug, h]));
    const fav = S.boot.prefs.favorites.dota;
    let list = [...slugs].map((slug) => {
      const r = recent.get(slug), l = lifeBy.get(slug);
      return { slug, name: (r && r.name) || (l && l.heroName) || heroName(slug), r, l,
        games: l ? l.games : r ? r.played : 0, recent: r ? r.played : 0, rate: l ? l.winRate : r ? r.rate : null, recentRate: r ? r.rate : null, last: l ? l.lastPlayed : r ? r.last : 0 };
    });
    const sorters = {
      games: (a, b) => b.games - a.games, recent: (a, b) => b.recent - a.recent || b.games - a.games,
      rate: (a, b) => (b.games >= 5) - (a.games >= 5) || (b.rate ?? -1) - (a.rate ?? -1), last: (a, b) => b.last - a.last, name: (a, b) => a.name.localeCompare(b.name),
    };
    list.sort(sorters[D.heroSort]);
    if (!list.length) return emptyState("No heroes yet", "Heroes appear here as soon as you have matches on record.");
    const th = (key, label, num = true) => `<th class="${num ? "num" : ""} ${D.heroSort === key ? "sorted" : ""}"><button data-act="hero-sort" data-key="${key}" type="button">${label}${D.heroSort === key ? " ▼" : ""}</button></th>`;

    return `${staleNote(dHist)}<div class="table-wrap"><table class="table">
      <thead><tr>${th("name", "Hero", false)}${th("recent", `Last ${timeline().length}`)}<th class="num">Recent win rate</th><th class="num">Recent KDA</th>${th("games", "All-time games")}${th("rate", "All-time win rate")}${th("last", "Last played")}<th>Form</th></tr></thead>
      <tbody>${list.map((h) => `<tr class="click" data-act="go" data-view="heroes" data-params='${JSON.stringify({ hero: h.slug })}'>
        <td><span class="cell">${heroImgHtml(h.slug)}<b>${esc(h.name)}</b>${fav === h.slug ? `<span class="star" title="On your overview">${icon("star")}</span>` : ""}</span></td>
        <td class="num">${h.recent || "–"}</td><td class="num ${toneOfRate(h.recentRate)}">${h.r && h.r.games ? pct(h.recentRate) : "–"}</td><td class="num">${h.r ? dash(h.r.kda, (v) => v.toFixed(2)) : "–"}</td>
        <td class="num">${h.l ? h.l.games : "–"}</td><td class="num ${h.l ? toneOfRate(h.l.winRate) : ""}">${h.l ? pct(h.l.winRate) : "–"}</td><td class="num muted">${ago(h.last)}</td>
        <td>${h.r ? formStrip(h.r.rows.map((m) => m.won), 10) : ""}</td></tr>`).join("")}</tbody></table></div>
      <p class="hint">All-time figures are OpenDota's record for your account across every mode. Recent figures cover your last ${timeline().length} matches.</p>`;
  },
});

// ---------- Sessions ----------

const SESSION = { goalsOpen: false, exported: null };

act("session-toggle", (el) => {
  const id = el.dataset.id;
  D.openSessions.has(id) ? D.openSessions.delete(id) : D.openSessions.add(id);
  D.typeMenu = null;
  rerender();
});
act("session-type-menu", (el, e) => {
  e.stopPropagation();
  D.typeMenu = D.typeMenu === el.dataset.id ? null : el.dataset.id;
  rerender();
});
act("session-type", async (el, e) => {
  e.stopPropagation();
  const h = await attempt(() => invoke("set_history_game_type", { matchid: el.dataset.id, gameType: el.dataset.type }));
  if (h) S.history = h;
  D.typeMenu = null;
  dInsights.clear();
  rerender();
});
act("session-notes", async (el) => {
  const id = el.dataset.id;
  const box = document.getElementById("notes-" + id);
  const h = await attempt(() => invoke("set_history_notes", { matchid: id, notes: box.value }), "Note saved.");
  if (h) S.history = h;
  rerender();
});
act("session-delete", async (el) => {
  if (el.dataset.confirm !== "yes") {
    el.dataset.confirm = "yes";
    el.textContent = "Press again to delete this session";
    return;
  }
  const h = await attempt(() => invoke("delete_history", { matchid: el.dataset.id }), "Session deleted.");
  if (h) S.history = h;
  dInsights.clear();
  dInsights.load();
  rerender();
});
act("goals-toggle", () => {
  SESSION.goalsOpen = !SESSION.goalsOpen;
  rerender();
});
act("goals-save", async () => {
  const num = (id) => Math.max(0, parseInt(($("#" + id) || {}).value, 10) || 0);
  const prefs = await attempt(() => invoke("save_goals", { lastHits10: num("goalLh"), maxDeaths: num("goalDeaths"), minGpm: num("goalGpm") }), "Goals saved.");
  if (prefs) {
    S.boot.prefs = prefs;
    SESSION.goalsOpen = false;
    dInsights.load(true);
  }
  rerender();
});
act("export", async (el) => {
  const out = await attempt(() => invoke("export_sessions", { format: el.dataset.format }));
  if (out) {
    SESSION.exported = out.path;
    toast("Exported.");
    rerender();
  }
});
act("reveal", (el) => attempt(() => invoke("reveal", { path: el.dataset.path })));

function sessionHtml(m) {
  const open = D.openSessions.has(m.matchid);
  const menu = D.typeMenu === m.matchid;
  const result = m.won === true ? "win" : m.won === false ? "loss" : "other";
  const g = S.boot.prefs.goals;
  const goals = [];
  if (g.lastHits10 && m.checkpoints[10]) goals.push(m.checkpoints[10].lastHits >= g.lastHits10);
  if (g.maxDeaths) goals.push(m.totalDeaths <= g.maxDeaths);
  if (g.minGpm && known(m.gpm)) goals.push(m.gpm >= g.minGpm);
  return `<div class="session ${open ? "open" : ""}" id="session-${esc(m.matchid)}">
    <div class="session-head click" data-act="session-toggle" data-id="${esc(m.matchid)}">
      ${heroImgHtml(m.heroName)}
      <span class="grow"><b>${esc(heroName(m.heroName))}</b> <span class="res ${result}">${m.won === true ? "Won" : m.won === false ? "Lost" : ""}</span>
        <span class="sub">${esc(fmtDate(m.date))}, ${esc(m.duration)}${m.incomplete ? ", ended early" : ""}${m.notes ? ", has a note" : ""}</span></span>
      <span class="num">${m.kills} / ${m.totalDeaths} / ${dash(m.assists)}</span>
      <span class="num muted">${dash(m.lastHits)} LH</span>
      ${goals.length ? `<span class="goal-marks" title="Goals met in this game">${goals.map((ok) => `<i class="${ok ? "w" : "l"}"></i>`).join("")}</span>` : ""}
      <span class="type-wrap"><button class="chip ${m.gameType === "unspecified" ? "" : "on"}" data-act="session-type-menu" data-id="${esc(m.matchid)}" type="button" title="Change the game type">${esc(gameTypeLabel(m.gameType))}</button>
        ${menu ? `<span class="menu">${GAME_TYPES.map((t) => `<button data-act="session-type" data-id="${esc(m.matchid)}" data-type="${t.id}" class="${t.id === m.gameType ? "on" : ""}" type="button">${t.label}</button>`).join("")}</span>` : ""}</span>
    </div>
    ${open ? `<div class="session-body">
      ${statRow([
        { label: "Last hits", value: dash(m.lastHits), sub: `${dash(m.denies)} denies` },
        { label: "Gold per minute", value: dash(m.gpm), sub: `${dash(m.xpm)} XP per minute` },
        { label: "Gold lost to deaths", value: fmtNum(m.totalGoldLost) },
        { label: "Roshan kills noted", value: m.roshanDeaths },
      ])}
      <div class="cols">
        <section><h4>Against your usual</h4>${comparisonHtml(m)}</section>
        <section>
          <h4>Key items</h4>${m.keyItems.length ? `<div class="item-times">${m.keyItems.map((k) => `<span class="item-time">${itemImgHtml(k.item)}<span class="num">${esc(k.clock)}</span></span>`).join("")}</div>` : `<p class="muted">None recorded.</p>`}
          <h4>Deaths</h4>${m.deaths.length ? `<div class="death-list">${m.deaths.map((d) => `<span>${esc(d.clock)}${known(d.goldLost) ? ` <span class="loss">-${fmtNum(d.goldLost)}</span>` : ""}</span>`).join("")}</div>` : `<p class="muted">You didn't die.</p>`}
        </section>
      </div>
      <h4>Your note</h4>
      <textarea class="input" id="notes-${esc(m.matchid)}" rows="2" maxlength="2000" placeholder="What went well, what to change next time">${esc(m.notes || "")}</textarea>
      <div class="row"><button class="btn small" data-act="session-notes" data-id="${esc(m.matchid)}" type="button">Save note</button><span class="grow"></span>
        <span class="muted">Match ${esc(m.matchid)}</span><button class="link danger" data-act="session-delete" data-id="${esc(m.matchid)}" type="button">Delete session</button></div>
    </div>` : ""}
  </div>`;
}

view("sessions", {
  game: "dota", nav: true, icon: "sessions", title: "Sessions",
  sub: () => "Matches TheTracker recorded live, with what Dota's own feed says about your play",
  load(force) {
    ensureHeroList();
    dInsights.load(force);
    return loadHistory().then(() => {
      rerender();
      if (S.params.focus) {
        const focus = S.params.focus;
        S.params = {};
        setTimeout(() => {
          const el = document.getElementById("session-" + focus);
          if (el) el.scrollIntoView({ block: "center" });
        }, 60);
      }
    });
  },
  render() {
    const items = [...S.history].reverse();
    const d = dInsights.data;
    const g = S.boot.prefs.goals;
    const anyGoal = g.lastHits10 || g.maxDeaths || g.minGpm;

    const goalsBlock = `<div class="sec-head"><h3>Your goals</h3><button class="link" data-act="goals-toggle" type="button">${SESSION.goalsOpen ? "Cancel" : anyGoal ? "Change" : "Set goals"}</button></div>
      ${SESSION.goalsOpen ? `<div class="goal-form">
          <label>Last hits by 10:00, at least <input class="input tiny" id="goalLh" type="number" min="0" max="200" value="${g.lastHits10 || ""}" placeholder="off" /></label>
          <label>Deaths per game, at most <input class="input tiny" id="goalDeaths" type="number" min="0" max="50" value="${g.maxDeaths || ""}" placeholder="off" /></label>
          <label>Gold per minute, at least <input class="input tiny" id="goalGpm" type="number" min="0" max="2000" value="${g.minGpm || ""}" placeholder="off" /></label>
          <button class="btn small" data-act="goals-save" type="button">Save goals</button>
          <p class="hint">Leave one empty to switch it off. Progress shows on the Live page during a match and against every session.</p></div>`
        : anyGoal && d ? `<div class="goals">${d.goals.map((p) => `<div class="goal ${!p.counted ? "idle" : p.met * 2 >= p.counted ? "ok" : "bad"}"><span class="dot"></span><span>${esc(p.label)} <b>${p.target}</b></span><span class="num">${p.counted ? `met in ${p.met} of ${p.counted}` : "no games yet"}</span></div>`).join("")}</div>`
        : `<p class="muted">Pick a target for last hits, deaths or gold per minute and TheTracker scores every game against it.</p>`}`;

    if (!items.length) {
      return `${emptyState("No sessions recorded yet", "A session is a match TheTracker watched live. Keep the app running while you play and each game lands here when it ends, with your last hits at every mark, each death and what it cost, and your item timings.",
        `<button class="btn" data-act="go" data-view="live" type="button">Check the live setup</button>`)}${goalsBlock}`;
    }
    return `
      ${d && d.insights.length ? `<div class="sec-head"><h3>What your games show</h3></div><div class="insights">${d.insights.map(insightHtml).join("")}</div>`
        : items.length < 3 ? `<p class="muted">Insights appear after three recorded games. You have ${items.length}.</p>` : ""}
      ${goalsBlock}
      <div class="sec-head"><h3>${items.length} recorded ${items.length === 1 ? "game" : "games"}</h3>
        <span class="row"><button class="link" data-act="export" data-format="csv" type="button">Export as spreadsheet</button><button class="link" data-act="export" data-format="json" type="button">Export as JSON</button></span></div>
      ${SESSION.exported ? `<div class="note">Saved to ${esc(SESSION.exported)} <button class="link" data-act="reveal" data-path="${esc(SESSION.exported)}" type="button">Show in folder</button></div>` : ""}
      <div class="sessions">${items.map(sessionHtml).join("")}</div>`;
  },
});

// ---------- Leaderboard ----------

const LB_METRICS = [
  { key: "last_hits_25", label: "Most last hits at 25:00", higher: true, get: (m) => (m.checkpoints[25] ? m.checkpoints[25].lastHits : null) },
  { key: "fewest_deaths", label: "Fewest deaths", higher: false, get: (m) => m.totalDeaths },
  { key: "least_gold_lost", label: "Least gold lost", higher: false, get: (m) => m.totalGoldLost },
  { key: "most_kills", label: "Most kills", higher: true, get: (m) => m.kills },
];

async function loadGlobalBoard(force) {
  const key = `${D.lbMetric}|${D.lbType}`;
  const g = D.lbGlobal;
  if (!force && g.key === key && (g.rows || g.loading)) return;
  Object.assign(g, { key, loading: true, error: null, rows: null });
  rerender();
  try {
    g.rows = await invoke("global_leaderboard", { metric: D.lbMetric, gameType: D.lbType, limit: 25 });
  } catch (e) {
    g.error = e.message;
  }
  g.loading = false;
  rerender();
}

act("lb-set", (el) => {
  D[el.dataset.axis] = el.dataset.value;
  if (D.lbScope === "global") loadGlobalBoard();
  rerender();
});

view("leaderboard", {
  game: "dota", nav: true, icon: "leaderboard", title: "Leaderboard",
  sub: () => (D.lbScope === "global" ? "Best recorded games from everyone who syncs" : "Your own best recorded games"),
  load(force) {
    if (D.lbScope === "global") loadGlobalBoard(force);
    return loadHistory();
  },
  render() {
    const metric = LB_METRICS.find((m) => m.key === D.lbMetric);
    const chip = (axis, value, label) => `<button class="chip ${D[axis] === value ? "on" : ""}" data-act="lb-set" data-axis="${axis}" data-value="${value}" type="button">${esc(label)}</button>`;
    const head = `<div class="filters">
      <div class="chips">${chip("lbScope", "personal", "Just me")}${chip("lbScope", "global", "Everyone")}</div>
      <div class="chips">${LB_METRICS.map((m) => chip("lbMetric", m.key, m.label)).join("")}</div>
      <div class="chips">${chip("lbType", "all", "All game types")}${GAME_TYPES.map((t) => chip("lbType", t.id, t.label)).join("")}</div></div>`;

    let rows;
    if (D.lbScope === "global") {
      const g = D.lbGlobal;
      if (g.error) return head + errorState(g.error);
      if (!g.rows) return head + loadingState("Loading the shared leaderboard…");
      const me = S.boot.auth.userId;
      rows = g.rows.map((r) => ({ hero: r.heroName, name: r.username || "Unnamed player", value: r.value, type: r.gameType, date: r.date, mine: me && r.userId === me }));
    } else {
      rows = S.history
        .filter((m) => D.lbType === "all" || m.gameType === D.lbType)
        .map((m) => ({ hero: m.heroName, name: null, value: metric.get(m), type: m.gameType, date: m.date, mine: false }))
        .filter((r) => known(r.value))
        .sort((a, b) => (metric.higher ? b.value - a.value : a.value - b.value))
        .slice(0, 25);
    }
    const auth = S.boot.auth;
    const foot = D.lbScope === "global" && !auth.signedIn
      ? `<p class="hint">You can read this without an account. To put your own games on it, <button class="link" data-act="go" data-view="settings" data-params='{"tab":"account"}' type="button">sign in</button>.</p>` : "";
    if (!rows.length) {
      return head + emptyState("Nothing here yet", D.lbScope === "global" ? "Nobody has synced a game of this type." : "Recorded games rank here once you have some. Last hits at 25:00 needs a game that ran that long.") + foot;
    }
    return `${head}<div class="table-wrap"><table class="table"><thead><tr><th class="num">#</th><th>${D.lbScope === "global" ? "Player" : "Hero"}</th><th class="num">${esc(metric.label)}</th><th>Game type</th><th class="num">When</th></tr></thead>
      <tbody>${rows.map((r, i) => `<tr class="${r.mine ? "me" : ""}"><td class="num">${i + 1}</td>
        <td><span class="cell">${heroImgHtml(r.hero)}<span><b>${esc(r.name || heroName(r.hero))}</b>${r.name ? `<span class="sub">${esc(heroName(r.hero))}${r.mine ? ", you" : ""}</span>` : ""}</span></span></td>
        <td class="num display">${fmtNum(r.value)}</td><td>${esc(gameTypeLabel(r.type))}</td><td class="num muted">${esc(fmtDate(r.date))}</td></tr>`).join("")}</tbody></table></div>${foot}`;
  },
});
