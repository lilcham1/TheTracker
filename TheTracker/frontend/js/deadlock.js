// Deadlock pages. Deadlock has no live feed for your own games, so they are
// read after the fact from the community Deadlock API. The Live page shows
// the game's top live matches and who is streaming them.

const dlOverview = resource("dlOverview", "deadlock_overview", { args: () => ({ limit: DL.limit }) });
const dlMeta = resource("dlMeta", "deadlock_meta", { ttl: 30 * 60000 });
const dlHeroes = resource("dlHeroes", "deadlock_heroes", { ttl: 6 * 3600000 });

const DL = {
  limit: 100, result: "all", heroQuery: "", sortKey: "startTime", sortDir: "desc",
  open: new Set(), details: new Map(), live: undefined, liveAt: 0,
  metaTab: "heroes", metaSort: "winRate", metaDir: "desc", metaQuery: "",
  popular: new Map(),
  dlRegion: "Europe", lbQuery: "",
};

const dlLinked = () => !!(S.boot && S.boot.deadlockLink && S.boot.deadlockLink.accountId);
const dlResults = (rows) => rows.map((m) => (m.outcome === "win" ? true : m.outcome === "loss" ? false : null));
const dlResClass = (m) => (m.outcome === "win" ? "win" : m.outcome === "loss" ? "loss" : "other");
const dlResText = (m) => ({ win: "Won", loss: "Lost", abandoned: "Left", unscored: "–" }[m.outcome] || "–");
const souls = (n) => (n >= 1000 ? (n / 1000).toFixed(1) + "k" : String(n));

function dlRecord(rows) {
  const wins = rows.filter((m) => m.outcome === "win").length;
  const losses = rows.filter((m) => m.outcome === "loss").length;
  return { wins, losses, games: wins + losses, rate: wins + losses ? (wins * 100) / (wins + losses) : null };
}

function dlHeroAggregate(rows) {
  const by = new Map();
  for (const m of rows) {
    let h = by.get(m.heroId);
    if (!h) by.set(m.heroId, (h = { id: m.heroId, name: m.heroName, image: m.heroImage, rows: [] }));
    h.rows.push(m);
  }
  return [...by.values()].map((h) => {
    const k = h.rows.reduce((a, m) => a + m.kills, 0), d = h.rows.reduce((a, m) => a + m.deaths, 0), as = h.rows.reduce((a, m) => a + m.assists, 0);
    return { ...h, ...dlRecord(h.rows), played: h.rows.length, kda: (k + as) / Math.max(1, d), souls: avgOf(h.rows, "netWorth"), last: h.rows[0].startTime };
  });
}

async function checkDlLive() {
  if (!dlLinked() || Date.now() - DL.liveAt < 60000) return;
  DL.liveAt = Date.now();
  try {
    DL.live = await invoke("deadlock_live");
  } catch (_) {
    DL.live = null;
  }
  rerender();
}

function dlLoad(force) {
  if (!dlLinked()) return loadSteamAccounts();
  checkDlLive();
  return dlOverview.load(force);
}

// ---------- Overview ----------

view("dl-overview", {
  game: "deadlock", nav: true, icon: "overview", title: "Overview",
  sub: () => (dlLinked() ? "Your recent Deadlock form" : "Connect Steam to see your matches"),
  load: dlLoad,
  render() {
    if (!dlLinked()) return linkPanelHtml("deadlock");
    const wait = gate(dlOverview, "Loading your matches…");
    if (wait) return wait;
    const d = dlOverview.data, rows = d.matches, s = d.summary, link = S.boot.deadlockLink;
    if (!rows.length) return emptyState("No Deadlock matches found for this account", "The Deadlock API only has matches it has been able to collect, and Valve limits how fast it can. Matches can take a while to appear.");

    const heroes = dlHeroAggregate(rows).sort((a, b) => b.played - a.played);
    const solid = heroes.filter((h) => h.games >= 3);
    const best = solid.filter((h) => h.rate >= 50).sort((a, b) => b.rate - a.rate).slice(0, 3);
    const chrono = [...rows].reverse();
    const midnight = new Date();
    midnight.setHours(0, 0, 0, 0);
    const today = dlRecord(rows.filter((m) => m.startTime >= midnight.getTime() / 1000));
    const streak = streakOf(dlResults(rows));
    const fav = S.boot.prefs.favorites.deadlock;
    const favHero = fav ? heroes.find((h) => h.name === fav) : null;
    const line = (h) => `<button class="line" data-act="go" data-view="dl-heroes" data-params='${esc(JSON.stringify({ hero: h.id }))}' type="button">${imgHtml(h.image, "portrait")}<span class="grow"><b>${esc(h.name)}</b><span class="muted"> ${h.played} games</span></span><span class="num ${toneOfRate(h.rate)}">${pct(h.rate)}</span></button>`;

    return `${staleNote(dlOverview)}
      ${DL.live ? `<div class="note"><span class="live-dot"></span> You're in a match right now as <b>${esc(DL.live.heroName)}</b>. It will show up here after it ends.</div>` : ""}
      <section class="hero-head">
        ${imgHtml(link.avatar, "avatar large")}
        <div class="grow"><h2 class="display">${esc(link.personaname || "Your account")}</h2>
          <p class="muted">${d.rank ? esc(d.rank.label) : "Not ranked yet"}</p>${formStrip(dlResults(rows), 30)}</div>
        <div class="today"><div class="stat-label">Today</div>
          <div class="display ${today.games ? toneOfRate(today.rate) : ""}">${today.games ? `${today.wins} – ${today.losses}` : "No games yet"}</div>
          ${streak && streak.n >= 2 ? `<div class="muted">${streak.n} ${streak.won ? "wins" : "losses"} in a row</div>` : ""}
          <button class="btn ghost small share-btn" data-act="share" data-kind="deadlock" type="button">Share</button></div>
      </section>
      ${statRow([
        { label: `Win rate, last ${rows.length}`, value: s.wins + s.losses ? pct(s.winRate) : "–", tone: toneOfRate(s.wins + s.losses ? s.winRate : null), sub: `${s.wins} won, ${s.losses} lost` },
        { label: "KDA", value: s.kda.toFixed(2), extra: sparkline(chrono.map((m) => (m.kills + m.assists) / Math.max(1, m.deaths))) },
        { label: "Souls per match", value: souls(s.avgSouls), extra: sparkline(chrono.map((m) => m.netWorth)) },
        { label: "Last hits", value: dash(avgOf(rows, "lastHits"), Math.round), sub: "per match" },
      ])}
      <div class="cols">
        <section><div class="sec-head"><h3>Recent matches</h3><button class="link" data-act="go" data-view="dl-matches" type="button">All matches</button></div>
          <div class="lines">${rows.slice(0, 7).map((m) => `<button class="line ${dlResClass(m)}" data-act="go" data-view="dl-matches" type="button">${imgHtml(m.heroImage, "portrait")}<span class="grow"><b>${esc(m.heroName)}</b></span><span class="num">${m.kills}/${m.deaths}/${m.assists}</span><span class="res ${dlResClass(m)}">${dlResText(m)}</span><span class="muted when">${ago(m.startTime)}</span></button>`).join("")}</div></section>
        <section>
          ${favHero ? `<div class="sec-head"><h3>Your hero</h3></div><div class="lines">${line(favHero)}</div>` : ""}
          ${best.length ? `<div class="sec-head"><h3>Winning most with</h3></div><div class="lines">${best.map(line).join("")}</div>` : ""}
          <div class="sec-head"><h3>Most played</h3></div><div class="lines">${heroes.slice(0, 5).map(line).join("")}</div>
        </section>
      </div>
      <details class="about"><summary>About this data</summary><p>Deadlock has no live feed, so matches appear after they end, from the community-run Deadlock API.</p></details>`;
  },
});

// ---------- Matches ----------

const DL_COLS = [
  { key: "heroName", label: "Hero" }, { key: "outcome", label: "Result" }, { key: "kills", label: "K", num: true }, { key: "deaths", label: "D", num: true },
  { key: "assists", label: "A", num: true }, { key: "netWorth", label: "Souls", num: true }, { key: "lastHits", label: "Last hits", num: true },
  { key: "heroLevel", label: "Level", num: true }, { key: "durationSeconds", label: "Length", num: true }, { key: "startTime", label: "Played", num: true },
];

act("dl-toggle", async (el) => {
  const id = Number(el.dataset.id);
  if (DL.open.has(id)) {
    DL.open.delete(id);
    return rerender();
  }
  DL.open.add(id);
  rerender();
  if (!DL.details.has(id) || DL.details.get(id).error) {
    try {
      DL.details.set(id, await invoke("deadlock_match_detail", { matchId: id }));
    } catch (e) {
      DL.details.set(id, { error: e.message });
    }
    rerender();
  }
});
act("dl-sort", (el) => {
  const key = el.dataset.key;
  if (DL.sortKey === key) DL.sortDir = DL.sortDir === "asc" ? "desc" : "asc";
  else [DL.sortKey, DL.sortDir] = [key, key === "heroName" ? "asc" : "desc"];
  rerender();
});
act("dl-set", (el) => {
  DL[el.dataset.axis] = el.dataset.value;
  rerender();
});
onChange("dl-hero", (el) => {
  DL.heroQuery = el.value;
  rerender();
});
onChange("dl-limit", (el) => {
  DL.limit = Number(el.value);
  dlOverview.load();
  rerender();
});

function dlBoardHtml(id) {
  const d = DL.details.get(id);
  if (!d) return `<div class="muted">Loading the scoreboard…</div>`;
  if (d.error) return `<div class="note err">${esc(d.error)}</div>`;
  const teams = [...new Set(d.players.map((p) => p.team))];
  return teams.map((team) => `<table class="board"><thead><tr><th>${team === d.winningTeam ? `Winning team` : "Losing team"}</th><th class="num">K</th><th class="num">D</th><th class="num">A</th><th class="num">Souls</th><th class="num">Last hits</th><th class="num">Level</th></tr></thead>
    <tbody>${d.players.filter((p) => p.team === team).map((p) => `<tr class="${p.isMe ? "me" : ""}"><td><span class="cell">${imgHtml(p.heroImage, "portrait small")}<span>${esc(p.heroName)}${p.isMe ? `<span class="muted"> you</span>` : ""}</span></span></td>
      <td class="num">${p.kills}</td><td class="num">${p.deaths}</td><td class="num">${p.assists}</td><td class="num">${souls(p.netWorth)}</td><td class="num">${p.lastHits}</td><td class="num">${p.level}</td></tr>`).join("")}</tbody></table>`).join("");
}

// ---------- Live: who is streaming each hero right now ----------
//
// Only streamers who are in a live standard match on the hero (ranked first);
// no Street Brawl, and no stream whose hero isn't known.

const dlBoard = resource("dlBoard", "deadlock_live_board", { ttl: 20000 });
// hero: "all", a hero id, or null for "the hero I'm playing, if any".
const DLL = { hero: null, query: "" };
const fmtViewers = (n) => (n >= 1000 ? (n / 1000).toFixed(n >= 10000 ? 0 : 1) + "k" : String(n));
const minsIn = (start) => Math.max(0, Math.round((Date.now() / 1000 - start) / 60));
const twitchUrl = (login) => "https://www.twitch.tv/" + encodeURIComponent(login);

act("dll-hero", (el) => {
  DLL.hero = el.dataset.hero;
  rerender();
});
document.addEventListener("input", (e) => {
  if (e.target.id === "dllQuery") {
    DLL.query = e.target.value;
    rerender();
  }
});

// Keep the board current while it is on screen.
setInterval(() => {
  if (S.view === "dl-live" && document.visibilityState === "visible") dlBoard.load();
}, 25000);

function streamCard(p) {
  const s = p.stream;
  return `<button class="stream-card" data-act="open-url" data-url="${esc(twitchUrl(s.login))}" type="button" title="Watch ${esc(s.name)} on Twitch">
    <span class="stream-thumb">${s.thumbnail ? `<img src="${esc(s.thumbnail)}" alt="" loading="lazy" />` : ""}<span class="live-tag">Live</span><span class="mode-tag ${p.mode === "Ranked" ? "ranked" : ""}">${esc(p.mode)}</span><span class="viewers">${fmtViewers(s.viewers)} watching</span></span>
    <span class="stream-body">
      <span class="stream-name"><b>${esc(s.name)}</b></span>
      <span class="stream-title">${esc(s.title || "")}</span>
      <span class="muted stream-meta">${p.startTime ? (minsIn(p.startTime) > 45 ? "may have just ended" : `${minsIn(p.startTime)} min into the match`) : ""}${p.linked ? " · linked by you" : p.name && liveSame(p.name, s.name) ? "" : ` · in game as ${esc(p.name || "?")}`}</span>
    </span></button>`;
}
const liveSame = (a, b) => a.toLowerCase().replace(/[^a-z0-9]/g, "") === b.toLowerCase().replace(/[^a-z0-9]/g, "");

function dlLiveSetupNote(reason) {
  if (reason === "bad_credentials") return `<div class="note warn">Twitch refused the app's credentials. Check the Twitch client ID and secret set on the cloud service.</div>`;
  if (reason === "unreachable") return `<div class="note">Twitch couldn't be reached just now. Try Refresh in a moment.</div>`;
  return `<div class="note">Streams appear here once Twitch is connected to TheTracker's cloud service (a one-time setup by whoever runs it).</div>`;
}

// ---------- Linked streamers ----------
//
// A Twitch channel tied by hand to the Steam account its streamer plays on,
// for streamers whose in-game name differs from their channel's.

const LNK = { open: false, twitch: "", query: "", results: [], searching: false, error: null };

act("lnk-open", () => {
  LNK.open = !LNK.open;
  LNK.error = null;
  rerender();
});
act("lnk-search", async () => {
  LNK.twitch = ($("#lnkTwitch") || {}).value || LNK.twitch;
  LNK.query = ($("#lnkSteam") || {}).value || "";
  LNK.searching = true;
  LNK.error = null;
  LNK.results = [];
  rerender();
  try {
    LNK.results = await invoke("friend_search", { game: "deadlock", query: LNK.query });
    if (!LNK.results.length) LNK.error = "No Steam account matches that. Try their exact Steam name, or paste their Steam profile link.";
  } catch (e) {
    LNK.error = e.message;
  }
  LNK.searching = false;
  rerender();
});
act("lnk-pick", async (el) => {
  const f = JSON.parse(el.dataset.friend);
  const twitch = ($("#lnkTwitch") || {}).value || LNK.twitch;
  try {
    await invoke("stream_link_add", { twitch, accountId: f.id, steamName: f.name, avatar: f.avatar || null });
  } catch (e) {
    LNK.error = e.message;
    return rerender();
  }
  Object.assign(LNK, { open: false, twitch: "", query: "", results: [], error: null });
  toast(`Linked ${twitch.replace(/^.*twitch\.tv\//i, "")} to ${f.name}.`);
  dlBoard.load(true);
});
act("lnk-remove", async (el) => {
  await attempt(() => invoke("stream_link_remove", { twitch: el.dataset.twitch }), "Link removed.");
  dlBoard.load(true);
});

function linkedHtml(b) {
  const rows = (b.linked || []).map((l) => {
    const s = l.stream;
    let status;
    if (s && l.heroName) status = `<b class="win">Live on ${esc(l.heroName)}</b> <span class="muted">· ${esc(l.mode)} · ${fmtViewers(s.viewers)} watching</span>`;
    else if (s) status = `<b>Live on Twitch</b> <span class="muted">· ${fmtViewers(s.viewers)} watching · this match isn't in the game's Watch tab, so the hero isn't known</span>`;
    else if (l.heroName) status = `In a ${esc(l.mode.toLowerCase())} match on <b>${esc(l.heroName)}</b> <span class="muted">· not streaming Deadlock right now</span>`;
    else status = `<span class="muted">Offline, or in a match that isn't in the Watch tab</span>`;
    return `<div class="line static">${imgHtml(l.avatar, "avatar tiny")}<span class="grow"><b>${esc(s ? s.name : l.twitch)}</b> <span class="muted">as ${esc(l.steamName || "Steam " + l.accountId)}</span><br />${status}</span>
      ${s ? `<button class="btn ghost small" data-act="open-url" data-url="${esc(twitchUrl(l.twitch))}" type="button">Watch</button>` : ""}
      <button class="link danger" data-act="lnk-remove" data-twitch="${esc(l.twitch)}" type="button" title="Remove this link">Remove</button></div>`;
  });
  const form = LNK.open ? `<div class="panel lnk-form">
      <p class="muted">Use this when a streamer's Steam name isn't the same as their Twitch channel. It's kept on this PC only.</p>
      <div class="row wrap"><input class="input grow" id="lnkTwitch" type="text" placeholder="Twitch channel, e.g. twitch.tv/name" value="${esc(LNK.twitch)}" />
        <input class="input grow" id="lnkSteam" type="text" placeholder="Their Steam name, profile link or ID" value="${esc(LNK.query)}" data-enter="lnk-search" />
        <button class="btn" data-act="lnk-search" type="button" ${LNK.searching ? "disabled" : ""}>${LNK.searching ? "Searching…" : "Find account"}</button></div>
      ${LNK.error ? `<div class="note err">${esc(LNK.error)}</div>` : ""}
      ${LNK.results.length ? `<div class="pick-list">${LNK.results.map((f) => `<button class="pick" data-act="lnk-pick" data-friend="${esc(JSON.stringify(f))}" type="button">
        <span class="row">${imgHtml(f.avatar, "avatar small")}<b>${esc(f.name)}</b><span class="muted">Steam ${esc(f.id)}</span></span><span class="muted">Link to this account</span></button>`).join("")}</div>` : ""}
    </div>` : "";
  return `<section class="linked">
      <div class="sec-head"><h3>Your linked streamers</h3><button class="link" data-act="lnk-open" type="button">${LNK.open ? "Cancel" : "Link a streamer"}</button></div>
      ${form}
      ${rows.length ? `<div class="lines">${rows.join("")}</div>` : LNK.open ? "" : `<p class="muted">Link a streamer whose Steam name isn't their Twitch name, and they'll be found in live matches and listed here.</p>`}
    </section>`;
}

view("dl-live", {
  game: "deadlock", nav: true, icon: "live", title: "Live",
  sub: () => "Streamers playing each hero right now",
  load(force) {
    if (dlLinked()) checkDlLive();
    return dlBoard.load(force);
  },
  render() {
    const wait = gate(dlBoard, "Reading the live matches…");
    if (wait) return wait;
    const b = dlBoard.data;
    // Until a hero is picked, follow the hero being played right now.
    const mine = DL.live && DL.live.heroId ? String(DL.live.heroId) : null;
    const pick = DLL.hero || mine || "all";
    const q = DLL.query.trim().toLowerCase();
    const streamed = b.heroes.filter((h) => h.streams);
    const total = streamed.reduce((a, h) => a + h.streams, 0), ranked = streamed.reduce((a, h) => a + h.ranked, 0);

    const shown = b.heroes
      .filter((h) => (pick === "all" ? h.streams : String(h.heroId) === pick))
      .map((h) => ({ ...h, players: h.players.filter((p) => p.stream && (!q || p.stream.name.toLowerCase().includes(q) || (p.name || "").toLowerCase().includes(q))) }))
      .filter((h) => h.players.length || pick !== "all");
    const pickedHero = pick !== "all" ? b.heroes.find((h) => String(h.heroId) === pick) : null;
    const chip = (id, label, n) => `<button class="chip ${pick === id ? "on" : ""}" data-act="dll-hero" data-hero="${id}" type="button">${label}${n ? ` <b>${n}</b>` : ""}</button>`;
    const chips = streamed.map((h) => chip(String(h.heroId), esc(h.name), h.streams));
    if (pickedHero && !pickedHero.streams) chips.unshift(chip(pick, esc(pickedHero.name)));
    if (mine && mine !== pick && !streamed.some((h) => String(h.heroId) === mine)) {
      const h = b.heroes.find((x) => String(x.heroId) === mine);
      if (h) chips.unshift(chip(mine, esc(h.name) + " (you)"));
    }

    const block = (h) => `<section class="live-hero">
        <div class="sec-head"><h3 class="cell">${imgHtml(h.image, "avatar small")}${esc(h.name)}${mine === String(h.heroId) ? ` <span class="muted">· your hero</span>` : ""}</h3>
          <span class="muted">${h.streams ? `${h.ranked} ranked · ${h.streams - h.ranked} standard` : ""}</span></div>
        ${h.players.length ? `<div class="stream-grid">${h.players.map(streamCard).join("")}</div>`
          : `<p class="muted">Nobody is streaming ${esc(h.name)} in a top live match right now.</p>`}
      </section>`;

    return `${staleNote(dlBoard)}
      ${DL.live ? `<div class="note"><span class="live-dot"></span> You're in a match as <b>${esc(DL.live.heroName)}</b>${pick === mine ? ", so this shows streamers on that hero." : "."}</div>` : ""}
      ${b.streamsAvailable ? "" : dlLiveSetupNote(b.reason)}
      ${linkedHtml(b)}
      ${statRow([
        { label: "Streamers in live matches", value: b.streamsAvailable ? fmtNum(total) : "–", sub: b.streamsAvailable ? `${ranked} ranked, ${total - ranked} standard` : "Twitch not connected" },
        { label: "Heroes being streamed", value: streamed.length },
        { label: "Matches checked", value: fmtNum(b.matches), sub: "standard games in the Watch tab" },
      ])}
      <div class="filters">
        <div class="row"><input class="input grow" id="dllQuery" type="text" placeholder="Find a streamer" value="${esc(DLL.query)}" /></div>
        <div class="chips">${chip("all", "All heroes")}${chips.join("")}</div>
      </div>
      ${shown.length ? shown.map(block).join("") : emptyState(q ? "No streamer matches that" : "No one is streaming a top live match right now", q ? "Try another name." : "Check back in a few minutes; the list refreshes every minute.")}
      ${aboutData(`Only standard matches are shown, ranked first; Street Brawl is left out. Matches come from the community Deadlock API's copy of the game's Watch tab, which lists the top live games only. Streams come from Twitch. A stream appears under a hero when the streamer's Twitch name matches the in-game Steam name or custom Steam profile address of someone playing that hero right now, or when you have linked the channel to that Steam account. The list of live matches is a few minutes behind the game, so right after a match ends a streamer can still show under the hero they just played.`)}`;
  },
});

view("dl-matches", {
  game: "deadlock", nav: true, icon: "matches", title: "Matches",
  sub: () => (dlOverview.data ? `Your last ${dlOverview.data.matches.length} matches` : ""),
  load: dlLoad,
  render() {
    if (!dlLinked()) return linkPanelHtml("deadlock");
    const wait = gate(dlOverview, "Loading your matches…");
    if (wait) return wait;
    const rows = dlOverview.data.matches;
    if (!rows.length) return emptyState("No Deadlock matches found for this account", "Matches appear once the Deadlock API has collected them.");
    const q = DL.heroQuery.trim().toLowerCase();
    const shown = rows.filter((m) => (DL.result === "all" || m.outcome === DL.result) && (!q || m.heroName.toLowerCase().includes(q)));
    const rec = dlRecord(shown);
    const chip = (value, label) => `<button class="chip ${DL.result === value ? "on" : ""}" data-act="dl-set" data-axis="result" data-value="${value}" type="button">${label}</button>`;
    const order = (key) => (m) => (key === "outcome" ? { win: 0, loss: 1, abandoned: 2, unscored: 3 }[m.outcome] : m[key]);
    const val = order(DL.sortKey), dir = DL.sortDir === "asc" ? 1 : -1;
    const sorted = [...shown].sort((a, b) => (typeof val(a) === "string" ? val(a).localeCompare(val(b)) : val(a) - val(b)) * dir);

    return `${staleNote(dlOverview)}
      <div class="filters"><div class="row">
        <div class="chips">${chip("all", "Any result")}${chip("win", "Wins")}${chip("loss", "Losses")}</div>
        <input class="input" id="dlHero" type="search" placeholder="Filter by hero" value="${esc(DL.heroQuery)}" data-input="dl-hero" />
        <label class="muted" for="dlLimit">Show last</label>
        <select class="input" id="dlLimit" data-change="dl-limit">${[50, 100, 200, 500].map((n) => `<option value="${n}" ${DL.limit === n ? "selected" : ""}>${n}</option>`).join("")}</select>
        <span class="grow"></span><span class="muted">${shown.length} shown${rec.games ? `, ${rec.wins} won and ${rec.losses} lost (${pct(rec.rate)})` : ""}</span></div></div>
      ${sorted.length ? `<div class="table-wrap"><table class="table">
        <thead><tr>${DL_COLS.map((c) => `<th class="${c.num ? "num" : ""} ${DL.sortKey === c.key ? "sorted" : ""}"><button data-act="dl-sort" data-key="${c.key}" type="button">${c.label}${DL.sortKey === c.key ? (DL.sortDir === "asc" ? " ▲" : " ▼") : ""}</button></th>`).join("")}</tr></thead>
        <tbody>${sorted.map((m) => `<tr class="click ${dlResClass(m)}" data-act="dl-toggle" data-id="${m.matchId}" title="Show the scoreboard">
          <td><span class="cell">${imgHtml(m.heroImage, "portrait")}<b>${esc(m.heroName)}</b></span></td><td><span class="res ${dlResClass(m)}">${dlResText(m)}</span></td>
          <td class="num">${m.kills}</td><td class="num">${m.deaths}</td><td class="num">${m.assists}</td><td class="num">${souls(m.netWorth)}</td><td class="num">${m.lastHits}</td><td class="num">${m.heroLevel}</td>
          <td class="num">${fmtClock(m.durationSeconds)}</td><td class="num muted">${ago(m.startTime)}</td></tr>
          ${DL.open.has(m.matchId) ? `<tr class="detail"><td colspan="${DL_COLS.length}">${dlBoardHtml(m.matchId)}</td></tr>` : ""}`).join("")}</tbody></table></div>` : emptyState("Nothing matches those filters")}`;
  },
});

// ---------- Heroes ----------

act("dl-fav", async (el) => {
  const name = el.dataset.name;
  const current = S.boot.prefs.favorites.deadlock;
  const prefs = await attempt(() => invoke("set_favorite_hero", { game: "deadlock", hero: current === name ? null : name }));
  if (prefs) {
    S.boot.prefs = prefs;
    toast(current === name ? "Removed from your overview." : `${name} is now on your overview.`);
    rerender();
  }
});

function loadDlPopular(heroId) {
  if (DL.popular.has(heroId)) return;
  DL.popular.set(heroId, { loading: true });
  invoke("deadlock_popular_items", { heroId })
    .then((data) => DL.popular.set(heroId, { data }))
    .catch((e) => DL.popular.set(heroId, { error: e.message }))
    .finally(rerender);
}

view("dl-heroes", {
  tab: "Your heroes",
  game: "deadlock", nav: true, icon: "heroes", title: "Heroes",
  sub: () => (S.params.hero ? "Your record and popular items" : "Every hero in your recent matches"),
  load(force) {
    if (S.params.hero) loadDlPopular(Number(S.params.hero));
    return dlLoad(force);
  },
  render() {
    if (!dlLinked()) return linkPanelHtml("deadlock");
    const wait = gate(dlOverview, "Loading your heroes…");
    if (wait) return wait;
    const heroes = dlHeroAggregate(dlOverview.data.matches).sort((a, b) => b.played - a.played);
    const fav = S.boot.prefs.favorites.deadlock;

    if (S.params.hero) {
      const h = heroes.find((x) => x.id === Number(S.params.hero));
      if (!h) return emptyState("No recent games on that hero", "", `<button class="btn" data-act="go" data-view="dl-heroes" type="button">All heroes</button>`);
      const pop = DL.popular.get(h.id);
      const builds = S.boot.prefs.builds.filter((b) => b.game === "deadlock" && b.hero === h.name);
      const chrono = [...h.rows].reverse();
      return `<button class="link back" data-act="go" data-view="dl-heroes" type="button">All heroes</button>
        <section class="hero-head">${imgHtml(h.image, "portrait huge")}<div class="grow"><h2 class="display">${esc(h.name)}</h2>${formStrip(dlResults(h.rows), 30)}</div>
          <button class="btn ${fav === h.name ? "" : "ghost"}" data-act="dl-fav" data-name="${esc(h.name)}" type="button">${fav === h.name ? "On your overview" : "Put on my overview"}</button></section>
        ${statRow([
          { label: `Last ${h.played} games`, value: h.games ? pct(h.rate) : "–", tone: toneOfRate(h.rate), sub: `${h.wins} won, ${h.losses} lost` },
          { label: "KDA", value: h.kda.toFixed(2), extra: sparkline(chrono.map((m) => (m.kills + m.assists) / Math.max(1, m.deaths))) },
          { label: "Souls per match", value: souls(Math.round(h.souls)), extra: sparkline(chrono.map((m) => m.netWorth)) },
        ])}
        <div class="cols"><section>
          <div class="sec-head"><h3>Items in the most builds</h3></div>
          ${!pop || pop.loading ? `<p class="muted">Loading…</p>` : pop.error ? `<p class="muted">${esc(pop.error)}</p>` : pop.data.length ? `<div class="lines">${pop.data.map((i) => `<div class="line static"><span class="grow">${esc(i.name)}</span><span class="num muted">${fmtNum(i.builds)} builds</span></div>`).join("")}</div><p class="hint">How many published community builds for this hero include each item.</p>` : `<p class="muted">The Deadlock API has no build data for this hero.</p>`}
          <div class="sec-head"><h3>Your builds</h3><button class="link" data-act="build-new" data-game="deadlock" data-hero="${esc(h.name)}" type="button">New build</button></div>
          ${builds.length ? builds.map((b) => buildCardHtml(b, false)).join("") : `<p class="muted">You haven't saved a build for this hero.</p>`}
        </section><section>
          <div class="sec-head"><h3>Recent games</h3></div>
          <div class="lines">${h.rows.slice(0, 10).map((m) => `<div class="line static ${dlResClass(m)}"><span class="res ${dlResClass(m)}">${dlResText(m)}</span><span class="grow"></span><span class="num">${m.kills}/${m.deaths}/${m.assists}</span><span class="num muted">${souls(m.netWorth)} souls</span><span class="muted when">${ago(m.startTime)}</span></div>`).join("")}</div>
        </section></div>`;
    }

    if (!heroes.length) return emptyState("No heroes yet", "Heroes appear here once you have matches on record.");
    return `${staleNote(dlOverview)}<div class="table-wrap"><table class="table">
      <thead><tr><th>Hero</th><th class="num">Games</th><th class="num">Win rate</th><th class="num">KDA</th><th class="num">Souls</th><th class="num">Last played</th><th>Form</th></tr></thead>
      <tbody>${heroes.map((h) => `<tr class="click" data-act="go" data-view="dl-heroes" data-params='${esc(JSON.stringify({ hero: h.id }))}'>
        <td><span class="cell">${imgHtml(h.image, "portrait")}<b>${esc(h.name)}</b>${fav === h.name ? `<span class="star" title="On your overview">${icon("star")}</span>` : ""}</span></td>
        <td class="num">${h.played}</td><td class="num ${toneOfRate(h.rate)}">${h.games ? pct(h.rate) : "–"}</td><td class="num">${h.kda.toFixed(2)}</td><td class="num">${souls(Math.round(h.souls))}</td><td class="num muted">${ago(h.last)}</td>
        <td>${formStrip(dlResults(h.rows), 10)}</td></tr>`).join("")}</tbody></table></div>`;
  },
});

// ---------- Meta ----------

act("dlmeta-set", (el) => {
  DL[el.dataset.axis] = el.dataset.value;
  if (el.dataset.axis === "metaTab") [DL.metaSort, DL.metaDir] = ["winRate", "desc"];
  rerender();
});
act("dlmeta-sort", (el) => {
  const key = el.dataset.key;
  if (DL.metaSort === key) DL.metaDir = DL.metaDir === "asc" ? "desc" : "asc";
  else [DL.metaSort, DL.metaDir] = [key, key === "name" ? "asc" : "desc"];
  rerender();
});
onChange("dlmeta-query", (el) => {
  DL.metaQuery = el.value;
  rerender();
});

view("dl-meta", {
  tabOf: "dl-heroes",
  game: "deadlock", nav: true, icon: "meta", title: "Meta",
  sub: () => (dlMeta.data ? `${fmtNum(dlMeta.data.matches)} matches, updated ${ago(dlMeta.data.fetchedAt)}` : "Which heroes and items are winning"),
  load: (force) => dlMeta.load(force),
  render() {
    const wait = gate(dlMeta, "Reading the meta…");
    if (wait) return wait;
    const d = dlMeta.data;
    const items = DL.metaTab === "items";
    const q = DL.metaQuery.trim().toLowerCase();
    const dir = DL.metaDir === "asc" ? 1 : -1;
    const rows = [...(items ? d.items : d.heroes)].filter((r) => !q || r.name.toLowerCase().includes(q)).sort((a, b) => {
      const av = a[DL.metaSort], bv = b[DL.metaSort];
      if (!known(av) || !known(bv)) return known(av) ? -1 : known(bv) ? 1 : 0;
      return typeof av === "string" ? av.localeCompare(bv) * dir : (av - bv) * dir;
    });
    const th = (key, label, num = true) => `<th class="${num ? "num" : ""} ${DL.metaSort === key ? "sorted" : ""}"><button data-act="dlmeta-sort" data-key="${key}" type="button">${label}${DL.metaSort === key ? (DL.metaDir === "asc" ? " ▲" : " ▼") : ""}</button></th>`;
    const tab = (value, label) => `<button class="chip ${DL.metaTab === value ? "on" : ""}" data-act="dlmeta-set" data-axis="metaTab" data-value="${value}" type="button">${label}</button>`;

    const table = items
      ? `<thead><tr>${th("name", "Item", false)}${th("winRate", "Win rate")}<th></th>${th("matches", "Bought in")}${th("share", "Popularity")}${th("buyMinute", "Bought around")}</tr></thead>
         <tbody>${rows.map((i) => `<tr><td><b>${esc(i.name)}</b></td><td class="num display ${toneOfRate(i.winRate)}">${pct(i.winRate, 1)}</td><td>${rateBar(i.winRate, 42, 58)}</td>
           <td class="num">${fmtNum(i.matches)} games</td><td class="num">${pct(i.share)}</td><td class="num">${known(i.buyMinute) ? `${i.buyMinute.toFixed(0)} min` : "–"}</td></tr>`).join("")}</tbody>`
      : `<thead><tr>${th("name", "Hero", false)}${th("winRate", "Win rate")}<th></th>${th("pickRate", "Picked in")}${th("kda", "KDA")}${th("avgSouls", "Souls")}${th("avgDamage", "Damage")}</tr></thead>
         <tbody>${rows.map((h) => `<tr><td><span class="cell">${imgHtml(h.image, "portrait")}<b>${esc(h.name)}</b></span></td><td class="num display ${toneOfRate(h.winRate)}">${pct(h.winRate, 1)}</td><td>${rateBar(h.winRate, 42, 58)}</td>
           <td class="num">${pct(h.pickRate, 1)}</td><td class="num">${h.kda.toFixed(2)}</td><td class="num">${souls(h.avgSouls)}</td><td class="num">${fmtNum(h.avgDamage)}</td></tr>`).join("")}</tbody>`;

    return `${staleNote(dlMeta)}
      <div class="filters"><div class="row"><div class="chips">${tab("heroes", `Heroes (${d.heroes.length})`)}${tab("items", `Items (${d.items.length})`)}</div>
        <input class="input" id="dlMetaQuery" type="search" placeholder="${items ? "Find an item" : "Find a hero"}" value="${esc(DL.metaQuery)}" data-input="dlmeta-query" /></div></div>
      ${rows.length ? `<div class="table-wrap"><table class="table">${table}</table></div>` : emptyState(items ? "No item data right now" : "Nothing matches that", items ? "The Deadlock API didn't return item statistics. Try Refresh in a minute." : "")}
      <details class="about"><summary>About this data</summary><p>Computed from the community Deadlock API's public statistics. ${items ? "Popularity is how often an item is bought compared with the most-bought item." : "Picked in is the share of matches the hero appears in."}</p></details>`;
  },
});

// ---------- Builds ----------

view("dl-builds", {
  tabOf: "dl-heroes",
  game: "deadlock", nav: true, icon: "builds", title: "Builds",
  sub: () => "Item plans you've saved for your heroes",
  load: () => dlHeroes.load(),
  render() {
    const d = BUILD.draft;
    if (d && d.game === "deadlock") {
      const names = (dlHeroes.data || []).map((h) => h.name);
      if (d.hero && !names.includes(d.hero)) names.unshift(d.hero);
      const options = `<option value="">Choose a hero</option>` + names.map((n) => `<option value="${esc(n)}" ${n === d.hero ? "selected" : ""}>${esc(n)}</option>`).join("");
      return buildEditorHtml(d, options);
    }
    const builds = S.boot.prefs.builds.filter((b) => b.game === "deadlock");
    if (!builds.length) {
      return emptyState("No builds saved yet", "Write down the items you plan to buy on a hero, in order, with your own notes.", `<button class="btn" data-act="build-new" data-game="deadlock" type="button">New build</button>`);
    }
    return `<div class="sec-head"><h3>${builds.length} saved</h3><button class="btn" data-act="build-new" data-game="deadlock" type="button">New build</button></div>
      ${builds.sort((a, b) => a.hero.localeCompare(b.hero)).map((b) => buildCardHtml(b, true)).join("")}`;
  },
});

// ---------- Leaderboard ----------

const dlLeaders = resource("dlLeaders", "deadlock_leaderboard", { args: () => ({ region: DL.dlRegion }), ttl: 30 * 60000 });

act("dl-region", (el) => {
  DL.dlRegion = el.dataset.value;
  dlLeaders.load();
  rerender();
});

view("dl-leaderboard", {
  tabOf: "dl-compare",
  game: "deadlock", nav: true, icon: "leaderboard", title: "Leaderboard",
  sub: () => "Deadlock's ranked leaderboard, by region",
  load: (force) => dlLeaders.load(force),
  render() {
    return topBoardHtml(dlLeaders, "", "dl-region", "dlRegion", DL,
      (p) => `<td><b>${esc(p.name)}</b>${p.isMe ? ` <span class="accent">you</span>` : ""}</td>
        <td><span class="cell">${p.images.slice(0, 4).map((src) => imgHtml(src, "portrait small")).join("")}<span class="muted">${esc(p.heroes.slice(0, 3).join(", "))}</span></span></td>`,
      "<th>Player</th><th>Most played</th>", "From the community Deadlock API. The game publishes names and ranks; a row is marked as yours only when its name could belong to no other account.");
  },
});
