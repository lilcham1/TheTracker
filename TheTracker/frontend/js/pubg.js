// PUBG pages, from PUBG's official API through the cloud service (which
// holds the key PUBG requires to stay on a server). The API keeps 14 days of
// matches; season and ranked totals come per mode.

const PG = { mode: "", query: "", shard: "steam", results: [], searching: false, error: null, open: null, filter: "all" };
const pgOverview = resource("pgOverview", "pubg_overview", { ttl: 5 * 60000 });
const pgLinked = () => !!(S.boot && S.boot.pubgLink && S.boot.pubgLink.accountId);
const PG_SHARDS = [["steam", "Steam"], ["kakao", "Kakao"], ["psn", "PlayStation"], ["xbox", "Xbox"], ["console", "Console"]];
const pgMins = (s) => (s >= 3600 ? `${Math.floor(s / 3600)} h ${Math.round((s % 3600) / 60)} min` : `${Math.round(s / 60)} min`);
const pgM = (m) => (m >= 1000 ? `${(m / 1000).toFixed(1)} km` : `${Math.round(m)} m`);
const pgPlace = (m) => `<span class="pg-place ${m.place === 1 ? "win" : m.place <= 10 ? "top" : ""}">#${m.place}</span><span class="muted">/${m.teams}</span>`;

act("pg-search", async () => {
  const q = ($("#pgQuery") || {}).value || "";
  PG.searching = true;
  PG.error = null;
  PG.results = [];
  rerender();
  try {
    PG.results = await invoke("pubg_search", { name: q, shard: PG.shard });
    if (!PG.results.length) PG.error = "No PUBG player by that exact name on that platform. Names are case-sensitive.";
  } catch (e) {
    PG.error = e.message;
  }
  PG.searching = false;
  rerender();
});
act("pg-shard", (el) => {
  PG.shard = el.dataset.value;
  rerender();
});
act("pg-pick", async (el) => {
  const link = await attempt(() => invoke("pubg_link", { accountId: el.dataset.id, name: el.dataset.name, shard: el.dataset.shard }));
  if (!link) return;
  S.boot.pubgLink = link;
  pgOverview.clear();
  toast(`PUBG connected to ${link.name}.`);
  ACTIONS.refresh();
});
act("pg-unlink", async () => {
  const link = await attempt(() => invoke("pubg_unlink"));
  if (!link) return;
  S.boot.pubgLink = link;
  pgOverview.clear();
  rerender();
});
act("pg-mode", (el) => {
  PG.mode = el.dataset.value;
  rerender();
});
act("pg-filter", (el) => {
  PG.filter = el.dataset.value;
  rerender();
});
act("pg-open", (el) => {
  PG.open = PG.open === el.dataset.id ? null : el.dataset.id;
  rerender();
});

function pgLinkHtml() {
  return `<div class="panel narrow">
    <h2>Connect your PUBG account</h2>
    <p class="muted">Type your in-game name exactly as it shows in PUBG (it's case-sensitive), and pick where you play.</p>
    <div class="chips seg">${PG_SHARDS.map(([k, l]) => `<button class="chip ${PG.shard === k ? "on" : ""}" data-act="pg-shard" data-value="${k}" type="button">${l}</button>`).join("")}</div>
    <div class="row"><input class="input grow" id="pgQuery" type="text" placeholder="Your PUBG name" data-enter="pg-search" />
      <button class="btn" data-act="pg-search" type="button" ${PG.searching ? "disabled" : ""}>${PG.searching ? "Searching…" : "Search"}</button></div>
    ${PG.error ? `<div class="note err">${esc(PG.error)}</div>` : ""}
    <div class="pick-list">${PG.results.map((r) => `<button class="pick" data-act="pg-pick" data-id="${esc(r.accountId)}" data-name="${esc(r.name)}" data-shard="${esc(r.shard)}" type="button">
      <span><b>${esc(r.name)}</b><span class="muted"> ${esc((PG_SHARDS.find(([k]) => k === r.shard) || [, r.shard])[1])}</span></span>
      <span class="muted">Use this account</span></button>`).join("")}</div>
  </div>`;
}

function pgNotSetUp(err) {
  return /isn't set up on the cloud service/.test(err || "");
}

function pgSetupHtml() {
  return `<div class="panel narrow"><h2>PUBG needs a key on the cloud service</h2>
    <p class="muted">PUBG's API requires its key to stay on a server, so TheTracker reads PUBG through your cloud service. Once, the owner of the deployment:</p>
    <ol class="tips"><li>Gets a free key at <b>developer.pubg.com</b>.</li>
      <li>Runs <code>npx convex env set --prod PUBG_API_KEY &lt;key&gt;</code> in the TheTracker folder.</li>
      <li>Runs <code>npx convex deploy</code>.</li></ol></div>`;
}

function pgLoad(force) {
  if (!pgLinked()) return Promise.resolve();
  return pgOverview.load(force);
}

function pgGate() {
  if (!pgLinked()) return pgLinkHtml();
  if (pgOverview.error && pgNotSetUp(pgOverview.error)) return pgSetupHtml();
  return gate(pgOverview, "Reading your PUBG matches…");
}

/// One match row, with the squad when opened.
function pgMatchHtml(m) {
  const open = PG.open === m.id;
  return `<button class="line ${m.place === 1 ? "win" : ""}" data-act="pg-open" data-id="${esc(m.id)}" type="button">
      <span class="pg-rank">${pgPlace(m)}</span>
      <span class="grow"><b>${esc(m.map)}</b> <span class="muted">${esc(m.modeName)}${m.ranked ? " · Ranked" : ""}${m.custom ? " · Custom" : ""}</span>
        <span class="sub">${m.kills} kill${m.kills === 1 ? "" : "s"}, ${Math.round(m.damage)} damage, ${m.knocks} knock${m.knocks === 1 ? "" : "s"}, alive ${pgMins(m.survived)}</span></span>
      <span class="muted when">${ago(m.startTime)}</span>
    </button>
    ${open ? `<div class="pg-detail">
      <table class="mini"><tr class="muted"><td>Squad</td><td class="num">Kills</td><td class="num">Assists</td><td class="num">Knocks</td><td class="num">Damage</td><td class="num">Revives</td><td class="num">Alive</td></tr>
        ${m.team.map((p) => `<tr class="${p.me ? "me" : ""}"><td>${esc(p.name)}</td><td class="num">${p.kills}</td><td class="num">${p.assists}</td><td class="num">${p.knocks}</td><td class="num">${Math.round(p.damage)}</td><td class="num">${p.revives}</td><td class="num">${pgMins(p.survived)}</td></tr>`).join("")}</table>
      <p class="muted small">Headshots ${m.headshots} · longest kill ${pgM(m.longest)} · walked ${pgM(m.walked)}, drove ${pgM(m.rode)} · ${m.heals} heals, ${m.boosts} boosts · ${m.deathType === "alive" ? "survived to the end" : m.deathType ? esc(m.deathType.replace("byplayer", "killed by a player").replace("byzone", "died to the zone").replace("suicide", "self-inflicted")) : ""} · match ${pgMins(m.duration)}</p>
    </div>` : ""}`;
}

function pgModeChips(modes) {
  if (!modes.length) return "";
  if (!PG.mode || !modes.some((m) => m.mode === PG.mode)) PG.mode = modes.slice().sort((a, b) => b.rounds - a.rounds)[0].mode;
  return `<div class="chips seg">${modes.map((m) => `<button class="chip ${PG.mode === m.mode ? "on" : ""}" data-act="pg-mode" data-value="${m.mode}" type="button">${esc(m.modeName)}</button>`).join("")}</div>`;
}

view("pg-overview", {
  game: "pubg", nav: true, icon: "overview", title: "Overview",
  sub: () => (pgLinked() ? "This season and your last 14 days, from PUBG's official API" : "Connect your PUBG account"),
  load: pgLoad,
  render() {
    const wait = pgGate();
    if (wait) return wait;
    const d = pgOverview.data;
    const ms = d.matches;
    const recent = ms.filter((m) => !m.custom);
    const avg = (f) => (recent.length ? recent.reduce((a, m) => a + f(m), 0) / recent.length : null);
    const ranked = d.ranked.slice().sort((a, b) => b.rounds - a.rounds)[0];
    const mode = d.modes.find((m) => m.mode === PG.mode) || d.modes[0];
    return `<section class="hero-head">
        <div class="grow"><h2 class="display">${esc(d.link.name)}</h2>
          <p class="muted">${esc((PG_SHARDS.find(([k]) => k === d.link.shard) || [, d.link.shard])[1])}${d.season ? ` · Season ${esc(d.season.replace(/^.*-(\d+)$/, "$1"))}` : ""}</p>
          ${formStrip(recent.slice(0, 30).map((m) => (m.place === 1 ? true : m.place <= 10 ? null : false)), 30)}</div>
        ${ranked ? `<div class="today"><div class="muted">Ranked ${esc(ranked.modeName)}</div><div class="display">${esc(ranked.tier)} ${esc(ranked.subTier)}</div><div class="muted">${fmtNum(ranked.points)} RP · best ${esc(ranked.bestTier)} ${esc(ranked.bestSubTier)}</div></div>` : ""}
      </section>
      ${statRow([
        { label: "Last 14 days", value: `${recent.length} match${recent.length === 1 ? "" : "es"}`, sub: `${recent.filter((m) => m.place === 1).length} chicken dinner${recent.filter((m) => m.place === 1).length === 1 ? "" : "s"}, ${recent.filter((m) => m.place <= 10).length} top 10` },
        { label: "Average place", value: avg((m) => m.place) != null ? `#${avg((m) => m.place).toFixed(1)}` : "–", sub: recent.length ? `of about ${Math.round(avg((m) => m.teams))} teams` : "" },
        { label: "Kills a match", value: avg((m) => m.kills) != null ? avg((m) => m.kills).toFixed(2) : "–", sub: recent.length ? `${avg((m) => m.knocks).toFixed(1)} knocks` : "" },
        { label: "Damage a match", value: avg((m) => m.damage) != null ? fmtNum(Math.round(avg((m) => m.damage))) : "–", sub: recent.length ? `alive ${pgMins(avg((m) => m.survived))}` : "" },
      ])}
      ${d.modes.length ? `<div class="sec-head"><h3>This season</h3></div>${pgModeChips(d.modes)}
        ${statRow([
          { label: "Matches", value: fmtNum(mode.rounds), sub: `${mode.wins} won, ${mode.top10s} top 10` },
          { label: "Top 10 rate", value: pct((mode.top10s * 100) / mode.rounds) },
          { label: "K/D", value: (mode.kills / Math.max(1, mode.rounds - mode.wins)).toFixed(2), sub: `${fmtNum(mode.kills)} kills, ${mode.assists} assists` },
          { label: "Damage a match", value: fmtNum(Math.round(mode.damage / mode.rounds)), sub: `${pct((mode.headshots * 100) / Math.max(1, mode.kills))} headshot kills · longest ${pgM(mode.longest)}` },
        ])}` : `<p class="muted">No normal matches this season yet.</p>`}
      ${d.ranked.length ? `<div class="sec-head"><h3>Ranked this season</h3></div>
        <table class="mini">${d.ranked.map((r) => `<tr><td><b>${esc(r.modeName)}</b></td><td>${esc(r.tier)} ${esc(r.subTier)} <span class="muted">${fmtNum(r.points)} RP</span></td>
          <td class="num">${r.rounds} played</td><td class="num">${r.wins} won</td><td class="num">avg #${r.avgRank.toFixed(1)}</td><td class="num">KDA ${r.kda.toFixed(2)}</td></tr>`).join("")}</table>` : ""}
      <div class="sec-head"><h3>Recent matches</h3><button class="link" data-act="go" data-view="pg-matches" type="button">All matches</button></div>
      ${ms.length ? `<div class="lines">${ms.slice(0, 8).map(pgMatchHtml).join("")}</div>` : `<p class="muted">No matches in the last 14 days.</p>`}
      ${d.missing ? `<p class="hint">${d.missing} match${d.missing === 1 ? "" : "es"} couldn't be read yet.</p>` : ""}
      ${d.freshness && d.freshness.stale ? `<p class="hint">Showing the last copy: ${esc(d.freshness.error || "PUBG couldn't be reached")}.</p>` : ""}`;
  },
});

view("pg-matches", {
  game: "pubg", nav: true, icon: "matches", title: "Matches",
  sub: () => "The last 14 days, the longest PUBG keeps",
  load: pgLoad,
  render() {
    const wait = pgGate();
    if (wait) return wait;
    const ms = pgOverview.data.matches;
    const modes = [...new Set(ms.map((m) => m.modeName))];
    const shown = ms.filter((m) => PG.filter === "all" || (PG.filter === "top10" ? m.place <= 10 : PG.filter === "ranked" ? m.ranked : m.modeName === PG.filter));
    return `<div class="chips seg">${[["all", "All"], ["top10", "Top 10"], ["ranked", "Ranked"], ...modes.map((m) => [m, m])].map(([k, l]) => `<button class="chip ${PG.filter === k ? "on" : ""}" data-act="pg-filter" data-value="${esc(k)}" type="button">${esc(l)}</button>`).join("")}</div>
      ${shown.length ? `<div class="lines">${shown.map(pgMatchHtml).join("")}</div>` : `<p class="muted">No matches here.</p>`}`;
  },
});

function pgSettingsHtml() {
  const l = S.boot.pubgLink || {};
  return `<section class="set"><h3>Your PUBG account</h3>
      <p class="muted">Matches and stats come from PUBG's official API, through your cloud service, which holds the API key PUBG requires to stay on a server.</p>
      <div class="lines"><div class="line static"><span class="grow"><b>${l.accountId ? esc(l.name) : "Not connected"}</b>
        <span class="muted"> ${l.accountId ? esc((PG_SHARDS.find(([k]) => k === l.shard) || [, l.shard])[1]) : ""}</span></span>
        ${l.accountId ? `<button class="link" data-act="pg-unlink" type="button">Disconnect</button>` : `<button class="link" data-act="go" data-view="pg-overview" type="button">Connect</button>`}</div></div>
    </section>
    <section class="set"><h3>What works for PUBG</h3>
      <p class="muted">PUBG has no live feed apps can read, so there's no overlay or live tracking: your matches show up a few minutes after they end. The API keeps 14 days of matches. The connection check and Game mode work while PUBG is open (Connection tab).</p>
    </section>`;
}
