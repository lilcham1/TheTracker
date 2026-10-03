// Overwatch pages, from the career profile Blizzard publishes. There is no
// match list and nothing live: Blizzard exposes neither. What a profile does
// show — totals by mode, role and hero — is laid out here.

const OW = { mode: "all", query: "", results: [], searching: false, error: null, heroSort: "timePlayed", role: "all" };
const owOverview = resource("owOverview", "ow_overview", { args: () => ({ mode: OW.mode }), ttl: 10 * 60000 });
const owLinked = () => !!(S.boot && S.boot.overwatchLink && S.boot.overwatchLink.playerId);
const owHours = (secs) => (secs >= 3600 ? `${(secs / 3600).toFixed(secs >= 36000 ? 0 : 1)} h` : `${Math.round(secs / 60)} min`);

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
  toast(`Overwatch connected to ${link.name}.`);
  ACTIONS.refresh();
});
act("ow-mode", (el) => {
  OW.mode = el.dataset.value;
  owOverview.load();
  rerender();
});
act("ow-set", (el) => {
  OW[el.dataset.axis] = el.dataset.value;
  rerender();
});

function owLinkHtml() {
  return `<div class="panel narrow">
    <h2>Connect your Overwatch profile</h2>
    <p class="muted">Overwatch runs on Battle.net, so it's found by BattleTag rather than through Steam. Your career profile has to be public: in Overwatch, open Options, then Social, and set Career Profile Visibility to Public.</p>
    <div class="row"><input class="input grow" id="owQuery" type="text" placeholder="BattleTag, like Name#1234" data-enter="ow-search" />
      <button class="btn" data-act="ow-search" type="button" ${OW.searching ? "disabled" : ""}>${OW.searching ? "Searching…" : "Search"}</button></div>
    ${OW.error ? `<div class="note err">${esc(OW.error)}</div>` : ""}
    <div class="pick-list">${OW.results.map((r) => `<button class="pick" data-act="ow-pick" data-id="${esc(r.playerId)}" data-name="${esc(r.name)}" data-avatar="${esc(r.avatar || "")}" type="button">
      <span class="row">${imgHtml(r.avatar, "avatar small")}<span><b>${esc(r.name)}</b><span class="muted"> ${esc(r.title || "")}${r.public ? "" : " (private profile)"}</span></span></span>
      <span class="muted">Use this profile</span></button>`).join("")}</div>
  </div>`;
}

function owModeChips() {
  return `<div class="chips">${[["all", "All modes"], ["competitive", "Competitive"], ["quickplay", "Quick Play"]].map(([k, l]) => `<button class="chip ${OW.mode === k ? "on" : ""}" data-act="ow-mode" data-value="${k}" type="button">${l}</button>`).join("")}</div>`;
}

function owLoad(force) {
  if (!owLinked()) return Promise.resolve();
  return owOverview.load(force);
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
    return `${staleNote(owOverview)}
      <section class="hero-head">${imgHtml(d.avatar, "avatar large")}
        <div class="grow"><h2 class="display">${esc(d.name)}</h2>
          <p class="muted">${[d.title ? esc(d.title) : null, d.endorsement ? `Endorsement level ${d.endorsement}` : null].filter(Boolean).join(", ")}</p>
          <div class="ranks">${d.ranks.length ? d.ranks.map((r) => `<span class="rank">${imgHtml(r.icon, "rank-icon")}<span>${esc(r.role)} <b>${esc(r.division.charAt(0).toUpperCase() + r.division.slice(1))} ${r.tier}</b></span></span>`).join("") : `<span class="muted">No competitive rank this season</span>`}</div></div>
      </section>
      ${owModeChips()}
      ${g.gamesPlayed ? statRow([
        { label: "Win rate", value: pct(g.winRate), tone: toneOfRate(g.winRate), sub: `${fmtNum(g.gamesWon)} won, ${fmtNum(g.gamesLost)} lost` },
        { label: "KDA", value: g.kda.toFixed(2), sub: `${g.avgEliminations.toFixed(1)} eliminations per 10 min` },
        { label: "Damage per 10 min", value: fmtNum(Math.round(g.avgDamage)), sub: `${fmtNum(Math.round(g.avgHealing))} healing` },
        { label: "Time played", value: owHours(g.timePlayed), sub: `${fmtNum(g.gamesPlayed)} games` },
      ]) : emptyState("No games in this mode", "Pick another mode above.")}
      <div class="cols">
        <section><div class="sec-head"><h3>Most played heroes</h3><button class="link" data-act="go" data-view="ow-heroes" type="button">All heroes</button></div>
          <div class="lines">${top.map((h) => `<div class="line static">${imgHtml(h.portrait, "avatar small")}<span class="grow"><b>${esc(h.name)}</b><span class="muted"> ${owHours(h.timePlayed)}</span></span><span class="num muted">${h.kda.toFixed(2)} KDA</span><span class="num ${toneOfRate(h.winRate)}">${pct(h.winRate)}</span></div>`).join("") || `<p class="muted">Nothing played in this mode.</p>`}</div></section>
        <section><div class="sec-head"><h3>By role</h3></div>
          <div class="lines">${d.roles.map((r) => `<div class="line static"><span class="grow"><b>${esc(r.name)}</b><span class="muted"> ${fmtNum(r.gamesPlayed)} games, ${owHours(r.timePlayed)}</span></span><span class="num muted">${r.kda.toFixed(2)} KDA</span><span class="num ${toneOfRate(r.winRate)}">${pct(r.winRate)}</span></div>`).join("") || `<p class="muted">Nothing played in this mode.</p>`}</div></section>
      </div>
      <p class="hint">From the community OverFast API, which reads the career profile Blizzard publishes. Blizzard doesn't publish individual matches, so there is no match list, and totals update when Blizzard refreshes your profile.</p>`;
  },
});

view("ow-heroes", {
  game: "overwatch", nav: true, icon: "heroes", title: "Heroes",
  sub: () => "Every hero on your career profile",
  load: owLoad,
  render() {
    if (!owLinked()) return owLinkHtml();
    const wait = gate(owOverview, "Loading your heroes…");
    if (wait) return owModeChips() + wait;
    const d = owOverview.data;
    const sorters = { timePlayed: (a, b) => b.timePlayed - a.timePlayed, winRate: (a, b) => (b.gamesPlayed >= 5) - (a.gamesPlayed >= 5) || b.winRate - a.winRate, kda: (a, b) => b.kda - a.kda, name: (a, b) => a.name.localeCompare(b.name), gamesPlayed: (a, b) => b.gamesPlayed - a.gamesPlayed };
    const rows = d.heroes.filter((h) => OW.role === "all" || h.role === OW.role).sort(sorters[OW.heroSort]);
    const th = (key, label, num = true) => `<th class="${num ? "num" : ""} ${OW.heroSort === key ? "sorted" : ""}"><button data-act="ow-set" data-axis="heroSort" data-value="${key}" type="button">${label}${OW.heroSort === key ? " ▼" : ""}</button></th>`;
    const chip = (k, l) => `<button class="chip ${OW.role === k ? "on" : ""}" data-act="ow-set" data-axis="role" data-value="${k}" type="button">${l}</button>`;
    return `${staleNote(owOverview)}<div class="filters">${owModeChips()}<div class="chips">${chip("all", "All roles")}${chip("tank", "Tank")}${chip("damage", "Damage")}${chip("support", "Support")}</div></div>
      ${rows.length ? `<div class="table-wrap"><table class="table"><thead><tr>${th("name", "Hero", false)}${th("timePlayed", "Time played")}${th("gamesPlayed", "Games")}${th("winRate", "Win rate")}<th></th>${th("kda", "KDA")}<th class="num">Elims / 10 min</th><th class="num">Damage / 10 min</th><th class="num">Healing / 10 min</th></tr></thead>
        <tbody>${rows.map((h) => `<tr><td><span class="cell">${imgHtml(h.portrait, "avatar small")}<span><b>${esc(h.name)}</b><span class="sub">${esc(h.role ? h.role.charAt(0).toUpperCase() + h.role.slice(1) : "")}</span></span></span></td>
          <td class="num">${owHours(h.timePlayed)}</td><td class="num">${fmtNum(h.gamesPlayed)}</td><td class="num display ${toneOfRate(h.winRate)}">${pct(h.winRate)}</td><td>${rateBar(h.winRate, 35, 65)}</td>
          <td class="num">${h.kda.toFixed(2)}</td><td class="num">${h.avgEliminations.toFixed(1)}</td><td class="num">${fmtNum(Math.round(h.avgDamage))}</td><td class="num">${fmtNum(Math.round(h.avgHealing))}</td></tr>`).join("")}</tbody></table></div>` : emptyState("No heroes played in this mode and role")}`;
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
        ${card("cs2", "Counter-Strike 2", "Live tracking of your own matches: score, kills, deaths and headshots, saved as you play.")}
        ${card("overwatch", "Overwatch", "Your career stats by mode, role and hero, from your public profile.")}
      </div>
      <button class="btn" data-act="welcome-done" type="button" ${any ? "" : "disabled"}>Continue</button>
      ${any ? "" : `<p class="hint">Pick at least one game.</p>`}
    </div>`;
  },
});
