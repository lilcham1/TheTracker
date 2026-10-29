// Valorant pages, from HenrikDev's community API through the cloud service
// (which holds its key), with names, pictures, ranks and map callouts from
// valorant-api.com. Each match read is kept, so the history grows over time.

const VAL = { query: "", searching: false, error: null, found: null, open: null, filter: "all", region: "" };
const valLinked = () => !!(S.boot && S.boot.valLink && S.boot.valLink.puuid);
const valOverview = resource("valOverview", "val_overview", { ttl: 3 * 60000 });
const valGoals = resource("valGoals", "val_goals", { args: () => ({ ranked: VAL.ranked !== false }), ttl: 5 * 60000 });
const valBoard = resource("valBoard", "val_leaderboard", { args: () => ({ region: VAL.region || (S.boot.valLink && S.boot.valLink.region) || "eu" }), ttl: 30 * 60000 });
const valStreams = resource("valStreams", "val_streams", { ttl: 60000 });
const VAL_REGIONS = [["eu", "Europe"], ["na", "North America"], ["ap", "Asia Pacific"], ["kr", "Korea"], ["latam", "Latin America"], ["br", "Brazil"]];

act("val-find", async () => {
  const q = ($("#valQuery") || {}).value || "";
  VAL.searching = true;
  VAL.error = null;
  VAL.found = null;
  rerender();
  try {
    VAL.found = await invoke("val_find", { riotId: q });
  } catch (e) {
    VAL.error = e.message;
  }
  VAL.searching = false;
  rerender();
});
act("val-pick", async () => {
  const link = await attempt(() => invoke("val_link", VAL.found));
  if (!link) return;
  S.boot.valLink = link;
  valOverview.clear();
  toast(`Valorant connected to ${link.name}#${link.tag}.`);
  ACTIONS.refresh();
});
act("val-unlink", async () => {
  const link = await attempt(() => invoke("val_unlink"));
  if (!link) return;
  S.boot.valLink = link;
  valOverview.clear();
  rerender();
});
act("val-open", (el) => {
  VAL.open = VAL.open === el.dataset.id ? null : el.dataset.id;
  rerender();
});
act("val-filter", (el) => {
  VAL.filter = el.dataset.value;
  rerender();
});
act("val-region", (el) => {
  VAL.region = el.dataset.value;
  valBoard.load();
  rerender();
});

function valNotSetUp(err) {
  return /isn't set up on the cloud service/.test(err || "");
}

function valSetupHtml() {
  return `<div class="panel narrow"><h2>Valorant needs a key on the cloud service</h2>
    <p class="muted">Riot opens its match data only to approved apps, so TheTracker reads Valorant through HenrikDev's community API, whose key stays on your cloud service. Once, the owner of the deployment:</p>
    <ol class="tips"><li>Gets a free key from HenrikDev's Discord (<b>docs.henrikdev.xyz</b> explains how).</li>
      <li>Runs <code>npx convex env set --prod HENRIKDEV_API_KEY &lt;key&gt;</code> in the TheTracker folder.</li>
      <li>Runs <code>npx convex deploy</code>.</li></ol></div>`;
}

function valLinkHtml() {
  const f = VAL.found;
  return `<div class="panel narrow">
    <h2>Connect your Riot account</h2>
    <p class="muted">Type your Riot ID as it shows in Valorant, with the tag after the #.</p>
    <div class="row"><input class="input grow" id="valQuery" type="text" placeholder="Name#TAG" data-enter="val-find" />
      <button class="btn" data-act="val-find" type="button" ${VAL.searching ? "disabled" : ""}>${VAL.searching ? "Looking…" : "Find"}</button></div>
    ${VAL.error ? (valNotSetUp(VAL.error) ? valSetupHtml() : `<div class="note err">${esc(VAL.error)}</div>`) : ""}
    ${f ? `<div class="pick-list"><button class="pick" data-act="val-pick" type="button">
      <span><b>${esc(f.name)}#${esc(f.tag)}</b><span class="muted"> ${esc((VAL_REGIONS.find(([k]) => k === f.region) || [, f.region])[1])}${f.level ? ` · level ${f.level}` : ""}</span></span>
      <span class="muted">Use this account</span></button></div>` : ""}
  </div>`;
}

function valLoad(force) {
  if (!valLinked()) return Promise.resolve();
  return valOverview.load(force).then(() => valGoals.load(force));
}

function valGate() {
  if (!valLinked()) return valLinkHtml();
  if (valOverview.error && valNotSetUp(valOverview.error)) return valSetupHtml();
  return gate(valOverview, "Reading your Valorant matches…");
}

const valAgentImg = (d, name, cls = "") => {
  const a = d && d.agents && d.agents[(name || "").toLowerCase()];
  return a && a.icon ? `<img class="val-agent ${cls}" src="${esc(a.icon)}" alt="" loading="lazy" />` : "";
};
const valResult = (m) => (m.won === true ? `<span class="res win">Won</span>` : m.won === false ? `<span class="res loss">Lost</span>` : `<span class="res draw">Draw</span>`);

/// One match row, with both teams when opened.
function valMatchHtml(d, m) {
  const open = VAL.open === m.id;
  const board = (title, list) => `<table class="mini"><tr class="muted"><td>${title}</td><td class="num">ACS</td><td class="num">K / D / A</td><td class="num">ADR</td><td class="num">HS%</td><td>Rank</td></tr>
    ${list.map((p) => `<tr class="${p.me ? "me" : ""}"><td>${valAgentImg(d, p.agent, "small")} ${esc(p.name)} <span class="muted">${esc(p.agent)}</span></td><td class="num">${Math.round(p.acs)}</td>
      <td class="num">${p.kills} / ${p.deaths} / ${p.assists}</td><td class="num">${Math.round(p.adr)}</td><td class="num">${Math.round(p.hsPct)}%</td><td class="muted">${esc(p.tier || "")}</td></tr>`).join("")}</table>`;
  return `<button class="line ${m.won === true ? "win" : ""}" data-act="val-open" data-id="${esc(m.id)}" type="button">
      <span class="val-res">${valAgentImg(d, m.agent)}</span>
      <span class="grow"><b>${esc(m.map)}</b> <span class="muted">${esc(m.mode)} · ${esc(m.agent)}</span> ${valResult(m)} <span class="num">${m.teamRounds}–${m.enemyRounds}</span>
        <span class="sub">${m.kills} / ${m.deaths} / ${m.assists} · ACS ${Math.round(m.acs)} · ADR ${Math.round(m.adr)} · HS ${Math.round(m.hsPct)}% · KAST ${Math.round(m.kast)}%${m.firstBloods ? ` · ${m.firstBloods} first blood${m.firstBloods === 1 ? "" : "s"}` : ""}${m.clutches ? ` · ${m.clutches} clutch${m.clutches === 1 ? "" : "es"}` : ""}</span></span>
      <span class="muted when">${ago(m.startTime)}</span>
    </button>
    ${open ? `<div class="pg-detail">${board("Your team", m.team)}${board("Enemy team", m.enemies)}
      <p class="muted small">#${m.teamPlace} in your team, #${m.matchPlace} in the match · ${fmtClock(m.duration)}${m.plants ? ` · ${m.plants} plant${m.plants === 1 ? "" : "s"}` : ""}${m.defuses ? ` · ${m.defuses} defuse${m.defuses === 1 ? "" : "s"}` : ""} · <button class="link" data-act="val-review" data-id="${esc(m.id)}" type="button">Review this match</button></p></div>` : ""}`;
}
act("val-review", (el) => {
  REV.valorant = el.dataset.id;
  reviewRes.valorant.clear();
  if (S.boot.coach) coachRes.valorant.clear();
  go("val-review");
});

view("val-overview", {
  game: "valorant", nav: true, icon: "overview", title: "Overview",
  sub: () => (valLinked() ? "Your rank and recent matches, from HenrikDev's Valorant API" : "Connect your Riot account"),
  load: valLoad,
  render() {
    const wait = valGate();
    if (wait) return wait;
    const d = valOverview.data;
    const ms = d.matches.filter((m) => m.rounds >= 8);
    const avg = (f) => (ms.length ? ms.reduce((a, m) => a + f(m), 0) / ms.length : null);
    const rank = d.rank;
    const wins = ms.filter((m) => m.won === true).length, losses = ms.filter((m) => m.won === false).length;
    const agents = {};
    for (const m of ms) {
      const a = (agents[m.agent] = agents[m.agent] || { name: m.agent, n: 0, won: 0, acs: 0 });
      a.n++;
      a.won += m.won === true ? 1 : 0;
      a.acs += m.acs;
    }
    const topAgents = Object.values(agents).sort((a, b) => b.n - a.n).slice(0, 6);
    return `<section class="hero-head">
        <div class="grow"><h2 class="display">${esc(d.link.name)}<span class="muted">#${esc(d.link.tag)}</span></h2>
          <p class="muted">${esc((VAL_REGIONS.find(([k]) => k === d.link.region) || [, d.link.region])[1])}${d.link.level ? ` · level ${d.link.level}` : ""} · ${d.matches.length} match${d.matches.length === 1 ? "" : "es"} read</p>
          ${formStrip(ms.slice(0, 30).map((m) => m.won), 30)}</div>
        ${rank && rank.tier ? `<div class="today val-rank">${rank.icon ? `<img src="${esc(rank.icon)}" alt="" />` : ""}<div><div class="display">${esc(rank.tier)}</div>
          <div class="muted">${rank.rr} RR${rank.lastChange ? ` · last game ${rank.lastChange > 0 ? "+" : ""}${rank.lastChange}` : ""}${rank.leaderboard ? ` · #${rank.leaderboard} on the leaderboard` : ""}</div>
          ${rank.peak ? `<div class="muted small">Peak ${esc(rank.peak)}${rank.peakSeason ? ` (${esc(rank.peakSeason)})` : ""}</div>` : ""}</div></div>` : ""}
      </section>
      ${statRow([
        { label: "Won", value: pct(wins + losses ? (wins * 100) / (wins + losses) : null), tone: toneOfRate(wins + losses ? (wins * 100) / (wins + losses) : null), sub: `${wins} won, ${losses} lost` },
        { label: "ACS", value: avg((m) => m.acs) != null ? Math.round(avg((m) => m.acs)) : "–", sub: avg((m) => m.adr) != null ? `${Math.round(avg((m) => m.adr))} damage a round` : "" },
        { label: "K/D", value: ms.length ? (ms.reduce((a, m) => a + m.kills, 0) / Math.max(1, ms.reduce((a, m) => a + m.deaths, 0))).toFixed(2) : "–", sub: avg((m) => m.kast) != null ? `KAST ${Math.round(avg((m) => m.kast))}%` : "" },
        { label: "Headshots", value: avg((m) => m.hsPct) != null ? pct(avg((m) => m.hsPct)) : "–", sub: ms.length ? `${ms.reduce((a, m) => a + m.firstBloods, 0)} first bloods, ${ms.reduce((a, m) => a + m.clutches, 0)} clutches` : "" },
      ])}
      ${ms.length >= 3 ? `<div class="sec-head"><h3>ACS, match by match</h3><span class="muted">oldest to newest</span></div>${sparkline(ms.slice(0, 30).map((m) => m.acs).reverse(), { width: 520, height: 60, points: true })}` : ""}
      ${valGoals.data && valGoals.data.goals.length ? `<section class="rev-goals">${roleGoalsHtml(valGoals.data)}</section>` : ""}
      ${topAgents.length ? `<div class="sec-head"><h3>Your agents</h3><span class="muted">in the matches read</span></div>
        <table class="mini">${topAgents.map((a) => `<tr><td>${valAgentImg(d, a.name, "small")} <b>${esc(a.name)}</b></td><td class="num">${a.n} played</td><td class="num">${pct((a.won * 100) / a.n)} won</td><td class="num">ACS ${Math.round(a.acs / a.n)}</td></tr>`).join("")}</table>` : ""}
      ${rank && rank.seasons && rank.seasons.length ? `<div class="sec-head"><h3>Ranked by act</h3></div>
        <table class="mini">${rank.seasons.slice(0, 6).map((s) => `<tr><td><b>${esc(s.season)}</b></td><td>${esc(s.tier || "")}</td><td class="num">${s.games} played</td><td class="num">${s.wins} won</td></tr>`).join("")}</table>` : ""}
      <div class="sec-head"><h3>Recent matches</h3><button class="link" data-act="go" data-view="val-matches" type="button">All matches</button></div>
      ${d.matches.length ? `<div class="lines">${d.matches.slice(0, 6).map((m) => valMatchHtml(d, m)).join("")}</div>` : `<p class="muted">No matches yet. HenrikDev lists your last ten; each one read is kept here.</p>`}
      ${d.freshness && d.freshness.stale ? `<p class="hint">Showing the last copy: ${esc(d.freshness.error || "the Valorant API couldn't be reached")}.</p>` : ""}`;
  },
});

view("val-matches", {
  game: "valorant", nav: true, icon: "matches", title: "Matches",
  sub: () => "Every match read here; HenrikDev lists your last ten each time",
  load: valLoad,
  render() {
    const wait = valGate();
    if (wait) return wait;
    const d = valOverview.data;
    const modes = [...new Set(d.matches.map((m) => m.mode))];
    const shown = d.matches.filter((m) => VAL.filter === "all" || (VAL.filter === "won" ? m.won === true : m.mode === VAL.filter));
    return `<div class="chips seg">${[["all", "All"], ["won", "Won"], ...modes.map((m) => [m, m])].map(([k, l]) => `<button class="chip ${VAL.filter === k ? "on" : ""}" data-act="val-filter" data-value="${esc(k)}" type="button">${esc(l)}</button>`).join("")}</div>
      ${shown.length ? `<div class="lines">${shown.map((m) => valMatchHtml(d, m)).join("")}</div>` : `<p class="muted">No matches here.</p>`}`;
  },
});

view("val-leaderboard", {
  game: "valorant", nav: true, icon: "leaderboard", title: "Leaderboard",
  sub: () => "Riot's ranked leaderboard, top 200 by region",
  load(force) {
    return valBoard.load(force);
  },
  render() {
    const region = VAL.region || (S.boot.valLink && S.boot.valLink.region) || "eu";
    const chips = `<div class="chips seg">${VAL_REGIONS.map(([k, l]) => `<button class="chip ${region === k ? "on" : ""}" data-act="val-region" data-value="${k}" type="button">${l}</button>`).join("")}</div>`;
    if (valBoard.error && valNotSetUp(valBoard.error)) return valSetupHtml();
    const wait = gate(valBoard, "Reading the leaderboard…");
    if (wait) return `<div class="filters">${chips}</div>${wait}`;
    const b = valBoard.data;
    return `<div class="filters">${chips}</div>
      ${b.rows.length ? `<div class="table-wrap"><table class="table"><thead><tr><th class="num">#</th><th>Player</th><th>Rank</th><th class="num">RR</th><th class="num">Wins</th></tr></thead>
        <tbody>${b.rows.map((r) => `<tr class="${r.me ? "me" : ""}"><td class="num">${r.rank}</td><td>${r.hidden ? `<span class="muted">${esc(r.name)}</span>` : `<b>${esc(r.name)}</b>`}</td><td>${esc(r.tier || "")}</td><td class="num">${fmtNum(r.rr)}</td><td class="num">${r.wins}</td></tr>`).join("")}</tbody></table></div>`
        : `<p class="muted">No leaderboard for this region yet this act.</p>`}
      <p class="hint">From Riot's ranked leaderboard, through HenrikDev's API. Players who hide their name show as Secret Agent.</p>`;
  },
});

view("val-live", {
  game: "valorant", nav: true, icon: "live", title: "Live",
  sub: () => "Valorant on Twitch right now, most watched first",
  load(force) {
    return valStreams.load(force);
  },
  render() {
    const wait = gate(valStreams, "Finding live streams…");
    if (wait) return wait;
    const d = valStreams.data;
    if (d.reason === "not_set_up") return `<p class="muted">Live streams need the Twitch keys on the cloud service, the same ones the other streamer pages use.</p>`;
    if (d.reason === "unknown_game") return `<p class="muted">The cloud service doesn't know Valorant yet: run <code>npx convex deploy</code> in the TheTracker folder.</p>`;
    if (d.reason) return `<p class="muted">Twitch couldn't be reached right now. Try again in a minute.</p>`;
    if (!d.streams.length) return `<p class="muted">Nobody is streaming Valorant on Twitch right now.</p>`;
    return `<div class="stream-grid">${d.streams.slice(0, 60).map((s) => `<button class="stream-card" data-act="open-url" data-url="${esc(twitchUrl(s.login))}" type="button" title="Watch ${esc(s.name)} on Twitch">
        <span class="stream-thumb">${s.thumbnail ? `<img src="${esc(s.thumbnail)}" alt="" loading="lazy" />` : ""}<span class="live-tag">Live</span><span class="viewers">${fmtViewers(s.viewers)} watching</span></span>
        <span class="stream-body"><span class="stream-name"><b>${esc(s.name)}</b></span><span class="stream-title">${esc(s.title || "")}</span></span>
      </button>`).join("")}</div>`;
  },
});

function valSettingsHtml() {
  const l = S.boot.valLink || {};
  const c = S.boot.prefs.coach || {};
  const level = c.valLevel || "push";
  const chip = (v, lbl, dsc) => `<button class="chip ${level === v ? "on" : ""}" data-act="coach-set" data-key="valLevel" data-value="${v}" type="button">${lbl} <span class="muted">${dsc}</span></button>`;
  return `<section class="set"><h3>Your Riot account</h3>
      <p class="muted">Matches and rank come from HenrikDev's community Valorant API through your cloud service, which holds its key. Riot's own match API is open only to approved apps.</p>
      <div class="lines"><div class="line static"><span class="grow"><b>${l.puuid ? `${esc(l.name)}#${esc(l.tag)}` : "Not connected"}</b>
        <span class="muted"> ${l.puuid ? esc((VAL_REGIONS.find(([k]) => k === l.region) || [, l.region])[1]) : ""}</span></span>
        ${l.puuid ? `<button class="link" data-act="val-unlink" type="button">Disconnect</button>` : `<button class="link" data-act="go" data-view="val-overview" type="button">Connect</button>`}</div></div>
    </section>
    <section class="set"><h3>Goals</h3>
      <p class="muted">Goals for combat score, damage a round, headshots and deaths, drawn from your own matches read here, checked on Overview and in each match's Review.</p>
      <div class="field"><span>How high</span><div class="chips seg">${chip("steady", "Steady", "top 50%")}${chip("push", "Push", "top 30%")}${chip("pro", "Pro", "top 20%")}</div></div>
    </section>
    ${typeof presetsHtml === "function" ? presetsHtml("valorant") : ""}
    <section class="set"><h3>What works for Valorant</h3>
      <p class="muted">Valorant has no live feed apps can read, and Vanguard rightly blocks anything that hooks the game, so there's no overlay or live tracking. Matches, rank, reviews and the AI coach arrive after each match; HenrikDev lists your last ten, and every one read is kept here, so the history grows. The connection check and Game mode work while Valorant is open.</p>
    </section>`;
}
