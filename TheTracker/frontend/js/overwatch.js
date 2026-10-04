// Overwatch pages, from the career profile Blizzard publishes. There is no
// match list and nothing live: Blizzard exposes neither. What a profile does
// show — totals by mode, role and hero, and every hero's detailed record — is
// laid out here, next to the hero meta Blizzard publishes for everyone.

const OW = {
  mode: "all", query: "", results: [], searching: false, error: null, heroSort: "timePlayed", role: "all", hero: null,
  meta: { mode: "competitive", region: "europe", division: "all", map: "all", role: "all", sort: "winRate" },
};
const owOverview = resource("owOverview", "ow_overview", { args: () => ({ mode: OW.mode }), ttl: 10 * 60000 });
const owHero = resource("owHero", "ow_hero", { args: () => ({ hero: OW.hero, mode: OW.mode }), ttl: 10 * 60000 });
const owMeta = resource("owMeta", "ow_meta", { args: () => ({ mode: OW.meta.mode, region: OW.meta.region, division: OW.meta.division, map: OW.meta.map }), ttl: 30 * 60000 });
const owProgress = resource("owProgress", "ow_progress", { args: () => ({ mode: OW.mode }), ttl: 60000 });
const owLinked = () => !!(S.boot && S.boot.overwatchLink && S.boot.overwatchLink.playerId);
const owHours = (secs) => (secs >= 3600 ? `${(secs / 3600).toFixed(secs >= 36000 ? 0 : 1)} h` : secs >= 60 ? `${Math.round(secs / 60)} min` : `${Math.round(secs)} s`);
const owCap = (s) => (s ? s.charAt(0).toUpperCase() + s.slice(1) : "");
const owVal = (s) => (s.kind === "time" ? owHours(s.value) : s.kind === "percent" ? pct(s.value) : Number.isInteger(s.value) ? fmtNum(s.value) : s.value.toFixed(2));
const OW_ROLES = [["all", "All roles"], ["tank", "Tank"], ["damage", "Damage"], ["support", "Support"]];

act("ow-search", async () => {
  const q = ($("#owQuery") || {}).value || "";
  OW.searching = true;
  OW.error = null;
  OW.results = [];
  rerender();
  try {
    OW.results = await invoke("ow_search", { query: q });
    if (!OW.results.length) OW.error = "No Overwatch profile matches that. Try your full BattleTag, like Name#1234.";
  } catch (e) {
    OW.error = e.message;
  }
  OW.searching = false;
  rerender();
});
act("ow-pick", async (el) => {
  const link = await attempt(() => invoke("ow_link", { playerId: el.dataset.id, name: el.dataset.name, avatar: el.dataset.avatar || null }));
  if (!link) return;
  S.boot.overwatchLink = link;
  owOverview.clear();
  owHero.clear();
  owProgress.clear();
  OW.hero = null;
  toast(`Overwatch connected to ${link.name}.`);
  ACTIONS.refresh();
});
act("ow-mode", (el) => {
  OW.mode = el.dataset.value;
  owOverview.load().then(() => owProgress.load(true));
  if (OW.hero) owHero.load();
  rerender();
});
act("ow-set", (el) => {
  OW[el.dataset.axis] = el.dataset.value;
  rerender();
});
act("ow-hero", (el) => {
  OW.hero = el.dataset.hero;
  go("ow-heroes");
});
act("ow-hero-back", () => {
  OW.hero = null;
  rerender();
});
act("ow-meta-set", (el) => {
  OW.meta[el.dataset.axis] = el.dataset.value;
  owMeta.load();
  rerender();
});
onChange("ow-meta-pick", (el) => {
  OW.meta[el.dataset.axis] = el.value;
  owMeta.load();
  rerender();
});

function owLinkHtml() {
  return `<div class="panel narrow">
    <h2>Connect your Overwatch profile</h2>
    <p class="muted">Overwatch keeps its career stats on Battle.net, even when you play through Steam, so your profile is found by BattleTag. It has to be public: in Overwatch, open Options, then Social, and set Career Profile Visibility to Public.</p>
    <div class="row"><input class="input grow" id="owQuery" type="text" placeholder="BattleTag, like Name#1234" data-enter="ow-search" />
      <button class="btn" data-act="ow-search" type="button" ${OW.searching ? "disabled" : ""}>${OW.searching ? "Searching…" : "Search"}</button></div>
    ${OW.error ? `<div class="note err">${esc(OW.error)}</div>` : ""}
    <div class="pick-list">${OW.results.map((r) => `<button class="pick" data-act="ow-pick" data-id="${esc(r.playerId)}" data-name="${esc(r.name)}" data-avatar="${esc(r.avatar || "")}" type="button">
      <span class="row">${imgHtml(r.avatar, "avatar small")}<span><b>${esc(r.name)}</b><span class="muted"> ${esc(r.title || "")}${r.public ? "" : " (private profile)"}</span></span></span>
      <span class="muted">Use this profile</span></button>`).join("")}</div>
    <p class="hint">The hero meta doesn't need a profile. <button class="link" data-act="go" data-view="ow-meta" type="button">See the meta</button></p>
  </div>`;
}

function owModeChips() {
  return `<div class="chips">${[["all", "All modes"], ["competitive", "Competitive"], ["quickplay", "Quick Play"]].map(([k, l]) => `<button class="chip ${OW.mode === k ? "on" : ""}" data-act="ow-mode" data-value="${k}" type="button">${l}</button>`).join("")}</div>`;
}

function owLoad(force) {
  if (!owLinked()) return Promise.resolve();
  if (OW.hero && S.view === "ow-heroes") owHero.load(force);
  // Progress is read after the overview: fetching the overview is what
  // records a new snapshot.
  return owOverview.load(force).then(() => owProgress.load(true));
}

function owStatList(group, limit) {
  const stats = limit ? group.stats.slice(0, limit) : group.stats;
  return `<div class="stat-list">${stats.map((s) => `<div><span class="muted">${esc(s.label)}</span><b class="num">${owVal(s)}</b></div>`).join("")}</div>`;
}

/// How the time on a profile divides between the three roles.
function owRoleSplit(roles) {
  const total = roles.reduce((a, r) => a + r.timePlayed, 0);
  if (!total) return "";
  return `<div class="split" role="img" aria-label="Time by role">${roles.map((r) => `<i class="role-${r.key}" style="width:${((r.timePlayed * 100) / total).toFixed(1)}%" title="${esc(r.name)}: ${owHours(r.timePlayed)}"></i>`).join("")}</div>`;
}

view("ow-overview", {
  game: "overwatch", nav: true, icon: "overview", title: "Overview",
  sub: () => (owLinked() ? "Your career, from your public profile" : "Connect your profile to see your stats"),
  load: owLoad,
  render() {
    if (!owLinked()) return owLinkHtml();
    const wait = gate(owOverview, "Loading your career…");
    if (wait) return owModeChips() + wait;
    const d = owOverview.data, g = d.general;
    const top = d.heroes.slice(0, 6);
    const total = d.roles.reduce((a, r) => a + r.timePlayed, 0);
    const best = d.records.find((x) => x.key === "best"), avg = d.records.find((x) => x.key === "average");
    const banner = d.namecard ? ` style="background-image:linear-gradient(90deg, rgba(12,14,19,.94) 30%, rgba(12,14,19,.55)), url('${esc(d.namecard)}')"` : "";
    return `${staleNote(owOverview)}
      <section class="hero-head ow-banner"${banner}>${imgHtml(d.avatar, "avatar large")}
        <div class="grow"><h2 class="display">${esc(d.name)}</h2>
          <p class="muted">${[d.title ? esc(d.title) : null, d.endorsement ? `Endorsement level ${d.endorsement}` : null].filter(Boolean).join(", ")}</p>
          <div class="ranks">${d.ranks.length ? d.ranks.map((r) => `<span class="rank">${imgHtml(r.icon, "rank-icon")}<span>${esc(r.role)} <b>${esc(owCap(r.division))} ${r.tier}</b></span></span>`).join("") : `<span class="muted">No competitive rank this season</span>`}</div></div>
      </section>
      ${owModeChips()}
      ${g.gamesPlayed ? statRow([
        { label: "Win rate", value: pct(g.winRate), tone: toneOfRate(g.winRate), sub: `${fmtNum(g.gamesWon)} won, ${fmtNum(g.gamesLost)} lost` },
        { label: "KDA", value: g.kda.toFixed(2), sub: `${g.avgEliminations.toFixed(1)} eliminations, ${g.avgDeaths.toFixed(1)} deaths per 10 min` },
        { label: "Damage per 10 min", value: fmtNum(Math.round(g.avgDamage)), sub: `${fmtNum(Math.round(g.avgHealing))} healing` },
        { label: "Time played", value: owHours(g.timePlayed), sub: `${fmtNum(g.gamesPlayed)} games` },
      ]) : emptyState("No games in this mode", "Pick another mode above.")}
      ${owSinceHtml()}
      <div class="cols">
        <section><div class="sec-head"><h3>Most played heroes</h3><button class="link" data-act="go" data-view="ow-heroes" type="button">All heroes</button></div>
          <div class="lines">${top.map((h) => `<button class="line" data-act="ow-hero" data-hero="${esc(h.key)}" type="button" title="Everything recorded for ${esc(h.name)}">${imgHtml(h.portrait, "avatar small")}<span class="grow"><b>${esc(h.name)}</b><span class="muted"> ${owHours(h.timePlayed)}</span></span><span class="num muted">${h.kda.toFixed(2)} KDA</span><span class="num ${toneOfRate(h.winRate)}">${pct(h.winRate)}</span></button>`).join("") || `<p class="muted">Nothing played in this mode.</p>`}</div></section>
        <section><div class="sec-head"><h3>By role</h3></div>
          ${owRoleSplit(d.roles)}
          <div class="lines">${d.roles.map((r) => `<div class="line static"><span class="side-dot role-${r.key}"></span><span class="grow"><b>${esc(r.name)}</b><span class="muted"> ${fmtNum(r.gamesPlayed)} games, ${total ? pct((r.timePlayed * 100) / total) : "0%"} of your time</span></span><span class="num muted">${r.kda.toFixed(2)} KDA</span><span class="num ${toneOfRate(r.winRate)}">${pct(r.winRate)}</span></div>`).join("") || `<p class="muted">Nothing played in this mode.</p>`}</div></section>
      </div>
      ${best || avg ? `<div class="cols even">
        ${best ? `<section><div class="sec-head"><h3>Your best in one game</h3></div>${owStatList(best, 10)}</section>` : ""}
        ${avg ? `<section><div class="sec-head"><h3>Your average per 10 minutes</h3></div>${owStatList(avg, 10)}</section>` : ""}
      </div>${OW.mode === "all" ? `<p class="hint">Bests and averages are from Quick Play. Pick Competitive above for those.</p>` : ""}` : ""}
      <p class="hint">From the community OverFast API, which reads the career profile Blizzard publishes. Blizzard doesn't publish individual matches, so there is no match list, and totals update when Blizzard refreshes your profile.</p>`;
  },
});

function owHeroDetailHtml() {
  const d = owOverview.data;
  const sum = d.heroes.find((h) => h.key === OW.hero);
  const c = owHero.data;
  const name = (sum && sum.name) || (c && c.name) || OW.hero;
  const role = (sum && sum.role) || (c && c.role) || "";
  const body = gate(owHero, `Loading ${name}…`) || (c.groups.length
    ? `<div class="stat-groups">${c.groups.map((g) => `<section><div class="sec-head"><h3>${esc(g.label)}</h3></div>${owStatList(g)}</section>`).join("")}</div>`
    : emptyState(`Nothing recorded for ${esc(name)} in ${c.mode === "competitive" ? "Competitive" : "Quick Play"}`, "Pick another mode above."));
  return `<button class="link" data-act="ow-hero-back" type="button">← All heroes</button>
    <section class="hero-head">${imgHtml((sum && sum.portrait) || (c && c.portrait), "avatar large")}
      <div class="grow"><h2 class="display">${esc(name)}</h2><p class="muted"><span class="side-dot role-${esc(role)}"></span>${esc(owCap(role))}</p></div></section>
    ${owModeChips()}
    ${sum && sum.gamesPlayed ? statRow([
      { label: "Win rate", value: pct(sum.winRate), tone: toneOfRate(sum.winRate), sub: `${fmtNum(sum.gamesWon)} won, ${fmtNum(sum.gamesLost)} lost` },
      { label: "KDA", value: sum.kda.toFixed(2), sub: `${sum.avgEliminations.toFixed(1)} eliminations per 10 min` },
      { label: "Damage per 10 min", value: fmtNum(Math.round(sum.avgDamage)), sub: `${fmtNum(Math.round(sum.avgHealing))} healing` },
      { label: "Time played", value: owHours(sum.timePlayed), sub: `${fmtNum(sum.gamesPlayed)} games` },
    ]) : ""}
    ${body}
    ${OW.mode === "all" ? `<p class="hint">The detailed record below the totals is from Quick Play. Pick Competitive above for that record.</p>` : ""}`;
}

view("ow-heroes", {
  game: "overwatch", nav: true, icon: "heroes", title: "Heroes",
  sub: () => "Every hero on your career profile",
  load: owLoad,
  render() {
    if (!owLinked()) return owLinkHtml();
    const wait = gate(owOverview, "Loading your heroes…");
    if (wait) return owModeChips() + wait;
    if (OW.hero) return owHeroDetailHtml();
    const d = owOverview.data;
    const sorters = { timePlayed: (a, b) => b.timePlayed - a.timePlayed, winRate: (a, b) => (b.gamesPlayed >= 5) - (a.gamesPlayed >= 5) || b.winRate - a.winRate, kda: (a, b) => b.kda - a.kda, name: (a, b) => a.name.localeCompare(b.name), gamesPlayed: (a, b) => b.gamesPlayed - a.gamesPlayed };
    const rows = d.heroes.filter((h) => OW.role === "all" || h.role === OW.role).sort(sorters[OW.heroSort]);
    const th = (key, label, num = true) => `<th class="${num ? "num" : ""} ${OW.heroSort === key ? "sorted" : ""}"><button data-act="ow-set" data-axis="heroSort" data-value="${key}" type="button">${label}${OW.heroSort === key ? " ▼" : ""}</button></th>`;
    const chip = ([k, l]) => `<button class="chip ${OW.role === k ? "on" : ""}" data-act="ow-set" data-axis="role" data-value="${k}" type="button">${l}</button>`;
    return `${staleNote(owOverview)}<div class="filters">${owModeChips()}<div class="chips">${OW_ROLES.map(chip).join("")}</div></div>
      ${rows.length ? `<div class="table-wrap"><table class="table"><thead><tr>${th("name", "Hero", false)}${th("timePlayed", "Time played")}${th("gamesPlayed", "Games")}${th("winRate", "Win rate")}<th></th>${th("kda", "KDA")}<th class="num">Elims / 10 min</th><th class="num">Damage / 10 min</th><th class="num">Healing / 10 min</th></tr></thead>
        <tbody>${rows.map((h) => `<tr class="click" data-act="ow-hero" data-hero="${esc(h.key)}" title="Everything recorded for ${esc(h.name)}"><td><span class="cell">${imgHtml(h.portrait, "avatar small")}<span><b>${esc(h.name)}</b><span class="sub">${esc(owCap(h.role))}</span></span></span></td>
          <td class="num">${owHours(h.timePlayed)}</td><td class="num">${fmtNum(h.gamesPlayed)}</td><td class="num display ${toneOfRate(h.winRate)}">${pct(h.winRate)}</td><td>${rateBar(h.winRate, 35, 65)}</td>
          <td class="num">${h.kda.toFixed(2)}</td><td class="num">${h.avgEliminations.toFixed(1)}</td><td class="num">${fmtNum(Math.round(h.avgDamage))}</td><td class="num">${fmtNum(Math.round(h.avgHealing))}</td></tr>`).join("")}</tbody></table></div>
        <p class="hint">Select a hero for its full record: bests, averages and ability stats.</p>` : emptyState("No heroes played in this mode and role")}`;
  },
});

// ---------- Progress ----------
//
// Blizzard publishes running totals, not matches. The backend remembers the
// totals each time it reads the profile; these pages show the differences.

const owDay = (unix) => new Date(unix * 1000).toLocaleDateString(undefined, { weekday: "short", month: "short", day: "numeric" });
const owSessionRate = (s) => (s.won + s.lost ? (s.won * 100) / (s.won + s.lost) : null);

/// The latest sitting, as a strip on the Overview.
function owSinceHtml() {
  const p = owProgress.data;
  if (!p || !p.since) return "";
  const s = p.sessions[0];
  if (!s) return `<div class="note">Progress tracking started ${ago(p.since)}. After your next games, what changed will show here. <button class="link" data-act="go" data-view="ow-progress" type="button">How it works</button></div>`;
  return `<button class="since" data-act="go" data-view="ow-progress" type="button" title="Every session since tracking began">
    <span class="since-label">Last session <span class="muted">${ago(s.to)}</span></span>
    <span class="since-main display">${s.games} game${s.games === 1 ? "" : "s"} <span class="win">${s.won}W</span> <span class="loss">${s.lost}L</span></span>
    <span class="since-heroes">${s.heroes.slice(0, 5).map((h) => imgHtml(h.portrait, "avatar small")).join("")}</span>
    <span class="muted">${owHours(s.time)} played</span><span class="grow"></span><span class="link">All sessions</span></button>`;
}

view("ow-progress", {
  game: "overwatch", nav: true, icon: "sessions", title: "Progress",
  sub: () => "What changed on your profile, session by session",
  load: owLoad,
  render() {
    if (!owLinked()) return owLinkHtml();
    const wait = gate(owProgress, "Reading your progress…");
    if (wait) return owModeChips() + wait;
    const p = owProgress.data, t = p.total;
    const how = `<p class="hint">Blizzard publishes running totals, not matches. TheTracker remembers the totals each time it reads your profile (about every half hour while it's running) and shows the difference. So games appear here grouped into sessions, a little after you play them, and only from the day tracking began.</p>`;
    const ranks = p.ranks.length ? `<div class="sec-head"><h3>Rank history</h3></div>
      <div class="lines">${p.ranks.map((c) => `<div class="line static"><span class="when muted" style="text-align:left;min-width:110px">${owDay(c.at)}</span>
        <span class="ranks grow">${c.ranks.map((r) => `<span class="rank">${imgHtml(r.icon, "rank-icon")}<span>${esc(r.role)} <b>${esc(owCap(r.division))} ${r.tier}</b></span></span>`).join("")}</span></div>`).join("")}</div>` : "";
    if (!p.since) return owModeChips() + emptyState("Nothing recorded for this mode yet", "Your profile is read shortly after the app starts. Come back after your next games.") + how;
    if (!p.sessions.length) return owModeChips() + emptyState("No games since tracking began", `TheTracker started watching this profile ${ago(p.since)}. Your next games will show here.`) + ranks + how;

    const top = t.heroes[0];
    return `${owModeChips()}
      ${statRow([
        { label: `Since ${owDay(p.since)}`, value: `${fmtNum(t.games)} game${t.games === 1 ? "" : "s"}`, sub: `${t.won} won, ${t.lost} lost` },
        { label: "Win rate", value: pct(owSessionRate(t)), tone: toneOfRate(owSessionRate(t)), sub: "since tracking began", extra: sparkline([...p.sessions].reverse().map(owSessionRate), { points: true }) },
        { label: "Time played", value: owHours(t.time), sub: `${p.sessions.length} session${p.sessions.length === 1 ? "" : "s"}` },
        { label: "Most played", value: top ? `<span class="cell">${imgHtml(top.portrait, "avatar small")}${esc(top.name)}</span>` : "–", sub: top ? `${top.games} games, ${top.won} won` : "" },
      ])}
      <div class="sec-head"><h3>Sessions</h3></div>
      <div class="table-wrap"><table class="table"><thead><tr><th>Seen</th><th class="num">Games</th><th class="num">Won</th><th class="num">Lost</th><th class="num">Win rate</th><th class="num">Time</th><th>Heroes</th></tr></thead>
        <tbody>${p.sessions.map((s) => `<tr><td><b>${owDay(s.to)}</b><span class="sub">${ago(s.to)}</span></td>
          <td class="num display">${s.games}</td><td class="num win">${s.won}</td><td class="num loss">${s.lost}</td>
          <td class="num ${toneOfRate(owSessionRate(s))}">${pct(owSessionRate(s))}</td><td class="num">${owHours(s.time)}</td>
          <td><span class="chips">${s.heroes.slice(0, 6).map((h) => `<span class="chip static" title="${h.won} won of ${h.games}, ${owHours(h.time)}">${imgHtml(h.portrait, "avatar tiny")}${esc(h.name)} <b>${h.games}</b></span>`).join("")}</span></td></tr>`).join("")}</tbody></table></div>
      ${ranks}${how}`;
  },
});

// ---------- Meta ----------

view("ow-meta", {
  game: "overwatch", nav: true, icon: "meta", title: "Meta",
  sub: () => "How often each hero is picked, wins and is banned",
  load(force) {
    if (owLinked()) owOverview.load();
    return owMeta.load(force);
  },
  render() {
    const M = OW.meta, d = owMeta.data;
    const chips = (axis, options) => `<div class="chips">${options.map(([k, l]) => `<button class="chip ${M[axis] === k ? "on" : ""}" data-act="ow-meta-set" data-axis="${axis}" data-value="${k}" type="button">${l}</button>`).join("")}</div>`;
    const select = (axis, label, options, off) => `<label class="field inline"><span>${label}</span><select class="input" id="owMeta_${axis}" data-change="ow-meta-pick" data-axis="${axis}" ${off ? "disabled" : ""}>${options.map((o) => `<option value="${esc(o.id)}" ${M[axis] === o.id ? "selected" : ""}>${esc(o.label)}</option>`).join("")}</select></label>`;
    const filters = `<div class="filters">
      <div class="row wrap">${chips("mode", [["competitive", "Competitive"], ["quickplay", "Quick Play"]])}<span class="grow"></span>
        ${d ? select("region", "Region", d.regions) + select("division", "Rank", d.divisions, M.mode === "quickplay") + select("map", "Map", d.maps) : ""}</div>
      ${chips("role", OW_ROLES)}</div>`;
    const wait = gate(owMeta, "Loading the hero meta…");
    if (wait) return filters + wait;

    const mine = new Map(((owLinked() && owOverview.data && owOverview.data.heroes) || []).filter((h) => h.gamesPlayed >= 3).map((h) => [h.key, h]));
    const sorters = { winRate: (a, b) => b.winRate - a.winRate, pickRate: (a, b) => b.pickRate - a.pickRate, banRate: (a, b) => b.banRate - a.banRate, name: (a, b) => a.name.localeCompare(b.name) };
    const all = d.heroes;
    const rows = all.filter((h) => M.role === "all" || h.role === M.role).sort(sorters[M.sort]);
    const lead = (f) => all.reduce((a, h) => (f(h) > f(a) ? h : a), all[0]);
    const maxPick = Math.max(...all.map((h) => h.pickRate), 1), maxBan = Math.max(...all.map((h) => h.banRate), 1);
    const hasBans = all.some((h) => h.banRate > 0);
    const th = (key, label, num = true) => `<th class="${num ? "num" : ""} ${M.sort === key ? "sorted" : ""}"><button data-act="ow-meta-set" data-axis="sort" data-value="${key}" type="button">${label}${M.sort === key ? " ▼" : ""}</button></th>`;
    const tile = (label, h, value) => ({ label, value: `<span class="cell">${imgHtml(h.portrait, "avatar small")}${esc(h.name)}</span>`, sub: value });
    const modeName = { all: "all modes", competitive: "Competitive", quickplay: "Quick Play" }[OW.mode];

    return `${staleNote(owMeta)}${filters}
      ${statRow([
        tile("Highest win rate", lead((h) => h.winRate), `${pct(lead((h) => h.winRate).winRate, 1)} of games won`),
        tile("Most picked", lead((h) => h.pickRate), `in ${pct(lead((h) => h.pickRate).pickRate, 1)} of games`),
        ...(hasBans ? [tile("Most banned", lead((h) => h.banRate), `in ${pct(lead((h) => h.banRate).banRate, 1)} of games`)] : []),
      ])}
      ${rows.length ? `<div class="table-wrap"><table class="table"><thead><tr><th class="num">#</th>${th("name", "Hero", false)}${th("winRate", "Win rate")}<th></th>${th("pickRate", "Pick rate")}<th></th>${hasBans ? th("banRate", "Ban rate") + "<th></th>" : ""}${mine.size ? `<th class="num" title="Your win rate on this hero in ${modeName}, against everyone's">You</th>` : ""}</tr></thead>
        <tbody>${rows.map((h, i) => {
          const me = mine.get(h.key);
          const diff = me ? me.winRate - h.winRate : null;
          return `<tr><td class="num muted">${i + 1}</td><td><span class="cell">${imgHtml(h.portrait, "avatar small")}<span><b>${esc(h.name)}</b><span class="sub"><span class="side-dot role-${esc(h.role)}"></span>${esc(owCap(h.role))}</span></span></span></td>
            <td class="num display ${toneOfRate(h.winRate)}">${pct(h.winRate, 1)}</td><td>${rateBar(h.winRate, 40, 60)}</td>
            <td class="num">${pct(h.pickRate, 1)}</td><td><span class="bar"><i style="width:${((h.pickRate * 100) / maxPick).toFixed(0)}%"></i></span></td>
            ${hasBans ? `<td class="num">${pct(h.banRate, 1)}</td><td><span class="bar"><i class="loss" style="width:${((h.banRate * 100) / maxBan).toFixed(0)}%"></i></span></td>` : ""}
            ${mine.size ? `<td class="num">${me ? `${pct(me.winRate)} <span class="${diff >= 0 ? "win" : "loss"}">${diff >= 0 ? "+" : "−"}${Math.abs(diff).toFixed(0)}</span><span class="sub">${fmtNum(me.gamesPlayed)} games</span>` : `<span class="muted">–</span>`}</td>` : ""}</tr>`;
        }).join("")}</tbody></table></div>` : emptyState("No heroes in this role")}
      <p class="hint">Rates as Blizzard publishes them for PC, through the community OverFast API.${mine.size ? ` "You" is your own win rate on heroes with at least 3 games in ${modeName}, and how far it sits from everyone's.` : ""}</p>`;
  },
});

// ---------- First run: which games? ----------

const WELCOME = { pick: null };

onChange("welcome-game", (el) => {
  WELCOME.pick[el.dataset.game] = el.checked;
  rerender();
});
act("welcome-done", async () => {
  try {
    S.boot.prefs = await invoke("save_games", WELCOME.pick);
    S.boot.gsi = await invoke("gsi_status");
  } catch (e) {
    return toast(e.message, "err");
  }
  renderBanners();
  go(GAMES[enabledGames()[0]].home);
});

view("welcome", {
  game: null, title: "Welcome", bare: true,
  sub: () => "",
  render() {
    if (!WELCOME.pick) {
      const g = S.boot.prefs.games;
      WELCOME.pick = { dota: g.dota, deadlock: g.deadlock, cs2: g.cs2, overwatch: g.overwatch };
    }
    const p = WELCOME.pick;
    const card = (key, name, what) => `<label class="game-card ${p[key] ? "on" : ""}" data-theme="${key}">
      <input type="checkbox" data-change="welcome-game" data-game="${key}" ${p[key] ? "checked" : ""} />
      <span class="game-card-name display">${name}</span><span class="muted">${what}</span></label>`;
    const any = p.dota || p.deadlock || p.cs2 || p.overwatch;
    return `<div class="welcome">
      <img src="brand.png" alt="" width="56" height="56" />
      <h1 class="display">Which games do you play?</h1>
      <p class="muted">TheTracker only sets up and shows the ones you pick. You can change this any time under Settings.</p>
      <div class="game-cards">
        ${card("dota", "Dota 2", "Live match tracking, an in-game overlay for runes and stacks, match history, draft help and the meta.")}
        ${card("deadlock", "Deadlock", "Match history with scoreboards, your heroes, the meta and the ranked leaderboard.")}
        ${card("cs2", "Counter-Strike 2", "Live tracking of your own matches, round by round: kills, damage, headshots, weapons, buys and sides, saved as you play.")}
        ${card("overwatch", "Overwatch", "Your career by mode, role and hero from your public profile, what changed session by session, and the hero meta by rank and map.")}
      </div>
      <button class="btn" data-act="welcome-done" type="button" ${any ? "" : "disabled"}>Continue</button>
      ${any ? "" : `<p class="hint">Pick at least one game.</p>`}
    </div>`;
  },
});
