// Deadlock pages. Deadlock has no live feed, so everything here is read
// after the fact from the community Deadlock API.

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
          ${streak && streak.n >= 2 ? `<div class="muted">${streak.n} ${streak.won ? "wins" : "losses"} in a row</div>` : ""}</div>
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
      <p class="hint">Deadlock has no live feed, so matches appear after they end, from the community-run Deadlock API.</p>`;
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
      <p class="hint">Computed from the community Deadlock API's public statistics. ${items ? "Popularity is how often an item is bought compared with the most-bought item." : "Picked in is the share of matches the hero appears in."}</p>`;
  },
});

// ---------- Builds ----------

view("dl-builds", {
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
