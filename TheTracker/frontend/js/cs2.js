// Counter-Strike 2 pages. Everything here is what the app recorded live from
// the game's own feed: Valve publishes no match history or leaderboard that
// an app can read without each player's private codes.

const CS = { status: null, setup: null, history: [], busy: false, error: null, sort: "date" };

const csMapName = (m) => (m || "").replace(/^(de|cs|ar|dm)_/, "").replace(/_/g, " ").replace(/\b\w/g, (c) => c.toUpperCase()) || "Unknown map";
const csResClass = (m) => (m.result === "win" ? "win" : m.result === "loss" ? "loss" : "other");
const csResText = (m) => ({ win: "Won", loss: "Lost", draw: "Draw" }[m.result] || (m.incomplete ? "Left" : "–"));
const csKd = (m) => m.kills / Math.max(1, m.deaths);
const csHs = (m) => (m.kills ? (m.headshotKills * 100) / m.kills : null);

async function csLoad() {
  try {
    [CS.setup, CS.history] = await Promise.all([invoke("cs2_setup"), invoke("cs2_history")]);
  } catch (_) {
    /* keep what was loaded */
  }
  rerender();
}

async function csPoll() {
  if (!S.boot || !S.boot.prefs.games.cs2) return;
  try {
    const before = CS.status && CS.status.current && CS.status.current.ended;
    CS.status = await invoke("cs2_status");
    const now = CS.status.current && CS.status.current.ended;
    if (now && !before) CS.history = await invoke("cs2_history");
    if (VIEWS[S.view] && VIEWS[S.view].game === "cs2") rerender();
  } catch (_) {
    /* next tick */
  }
}
setInterval(csPoll, 1500);

act("cs-install", async () => {
  CS.busy = true;
  CS.error = null;
  rerender();
  try {
    CS.setup = await invoke("cs2_install");
    toast("CS2 is set up. Restart CS2 if it's open.");
  } catch (e) {
    CS.error = e.message;
  }
  CS.busy = false;
  rerender();
});
act("cs-check", csLoad);
act("cs-delete", async (el) => {
  if (el.dataset.confirm !== "yes") {
    el.dataset.confirm = "yes";
    el.textContent = "Press again to delete";
    return;
  }
  const h = await attempt(() => invoke("cs2_delete", { id: el.dataset.id }), "Match deleted.");
  if (h) CS.history = h;
  rerender();
});
act("cs-dismiss", () => {
  CS.dismissed = CS.status && CS.status.current && CS.status.current.id;
  rerender();
});

function csSetupHtml() {
  const s = CS.setup, st = CS.status || {};
  if (!s) return loadingState("Checking your CS2 setup…");
  const rows = [];
  if (S.live && S.live.serverError) rows.push(setupRow(false, "Live tracking couldn't start", esc(S.live.serverError)));
  if (!s.cfgDirs.length) rows.push(setupRow(false, "Counter-Strike 2 wasn't found", "TheTracker looked in every Steam library on this PC. Install CS2 through Steam, then press Check again."));
  else if (s.installed) rows.push(setupRow(true, "CS2 is set up to send match data", `TheTracker added a small config file to CS2's folder. It listens on port ${s.port}. No launch option is needed.`));
  else rows.push(setupRow(false, "CS2 isn't set up to send match data yet", "TheTracker needs to add one small config file to CS2's folder."));

  const age = st.gsiAgeSecs;
  if (known(age) && age < 45) rows.push(setupRow(true, "CS2 is connected", "Receiving data right now. Your next match will appear here when it starts."));
  else if (known(age)) rows.push(setupRow(null, "CS2 is closed", `Last heard from CS2 ${agoSecs(age)}.`));
  else if (s.installed) rows.push(setupRow(null, "Nothing from CS2 yet", "Expected while CS2 is closed. If it's open, restart it: CS2 only reads the config when it starts."));

  return `<div class="panel narrow">
    <h2>Waiting for a match</h2>
    <p class="muted">This page fills in by itself when a CS2 match starts: the score, your kills, deaths and headshots, round by round. When the match ends it's saved to Matches.</p>
    <div class="checks">${rows.join("")}</div>
    ${CS.error ? `<div class="note err">${esc(CS.error)}</div>` : ""}
    <div class="row wrap">
      ${s.cfgDirs.length && !s.installed ? `<button class="btn" data-act="cs-install" type="button" ${CS.busy ? "disabled" : ""}>Set up CS2</button>` : ""}
      <button class="btn ghost" data-act="cs-check" type="button">Check again</button>
    </div>
    <p class="hint">TheTracker only sees your own state, the same data tournament overlays use. It reads nothing from the game's memory and is not something VAC acts on. It can only record a match while it's running.</p>
    ${s.cfgDirs.length ? `<details class="more"><summary>What TheTracker changed on this PC</summary>
      ${s.cfgDirs.map((d) => `<code class="path">${esc(d)}\\gamestate_integration_thetracker.cfg</code>`).join("")}
      <p class="muted">Switching CS2 off under Settings, General removes the file.</p></details>` : ""}
  </div>`;
}

function csMatchHtml(m, live) {
  const hs = csHs(m);
  return `<div class="panel">
    <div class="live-head">
      <div class="grow"><h2 class="display">${esc(csMapName(m.map))} ${m.ended ? `<span class="res ${csResClass(m)}">${csResText(m)}</span>` : ""}</h2>
        <p class="muted">${m.ended ? "Match over" : `<span class="live-dot ${live ? "" : "off"}"></span> ${{ warmup: "Warmup", live: "In progress", intermission: "Half time", gameover: "Match over" }[m.phase] || "In progress"}`}${m.mode ? `, ${esc(m.mode)}` : ""}${m.team && !m.ended ? `, playing ${m.team === "CT" ? "Counter-Terrorist" : "Terrorist"}` : ""}</p></div>
      <div class="clock display"><span class="${m.myScore > m.theirScore ? "win" : m.myScore < m.theirScore ? "loss" : ""}">${m.myScore}</span> : ${m.theirScore}</div>
      ${m.ended ? `<button class="btn ghost" data-act="cs-dismiss" type="button">Done</button>` : ""}
    </div>
    ${statRow([
      { label: "K / D / A", value: `${m.kills} / ${m.deaths} / ${m.assists}` },
      { label: "K/D", value: csKd(m).toFixed(2), tone: csKd(m) >= 1 ? "win" : "loss" },
      { label: "Headshots", value: known(hs) ? pct(hs) : "–", sub: `${m.headshotKills} of ${m.kills} kills` },
      { label: "MVPs", value: m.mvps, sub: `${m.score} score` },
      ...(m.ended ? [] : [{ label: "Money", value: "$" + fmtNum(m.money), sub: `${m.health} HP, ${m.armor} armor` }]),
    ])}
    ${m.ended && m.incomplete ? `<p class="hint">You left before the end, so there's no result for this match.</p>` : ""}
  </div>`;
}

view("cs-live", {
  game: "cs2", nav: true, icon: "live", title: "Live",
  sub: () => (CS.status && CS.status.live ? "Tracking your match" : "Fills in when a match starts"),
  load: csLoad,
  render() {
    const st = CS.status;
    const m = st && st.current;
    if (!m || (m.ended && CS.dismissed === m.id)) return csSetupHtml();
    return csMatchHtml(m, st.live);
  },
});

act("cs-sort", (el) => {
  CS.sort = el.dataset.key;
  rerender();
});

view("cs-matches", {
  game: "cs2", nav: true, icon: "matches", title: "Matches",
  sub: () => "Matches TheTracker recorded while you played",
  load: csLoad,
  render() {
    const all = [...CS.history].reverse();
    if (!all.length) {
      return emptyState("No CS2 matches recorded yet", "Keep TheTracker running while you play and each match is saved here when it ends. Valve doesn't publish CS2 match history, so only matches played while the app is open can be shown.",
        `<button class="btn" data-act="go" data-view="cs-live" type="button">Check the live setup</button>`);
    }
    const decided = all.filter((m) => m.result === "win" || m.result === "loss");
    const wins = decided.filter((m) => m.result === "win").length;
    const kills = all.reduce((a, m) => a + m.kills, 0), deaths = all.reduce((a, m) => a + m.deaths, 0), hsk = all.reduce((a, m) => a + m.headshotKills, 0);
    const maps = new Map();
    for (const m of all) {
      if (!maps.has(m.map)) maps.set(m.map, []);
      maps.get(m.map).push(m);
    }
    const mapRows = [...maps.entries()].map(([name, list]) => {
      const d = list.filter((m) => m.result === "win" || m.result === "loss");
      const w = d.filter((m) => m.result === "win").length;
      return { name, played: list.length, rate: d.length ? (w * 100) / d.length : null, kd: list.reduce((a, m) => a + m.kills, 0) / Math.max(1, list.reduce((a, m) => a + m.deaths, 0)) };
    }).sort((a, b) => b.played - a.played);
    const chrono = [...all].reverse();

    return `${statRow([
        { label: `Win rate, ${all.length} matches`, value: decided.length ? pct((wins * 100) / decided.length) : "–", tone: decided.length ? toneOfRate((wins * 100) / decided.length) : "", sub: `${wins} won, ${decided.length - wins} lost`,
          extra: formStrip(all.map((m) => (m.result === "win" ? true : m.result === "loss" ? false : null)), 30) },
        { label: "K/D", value: (kills / Math.max(1, deaths)).toFixed(2), sub: `${(kills / all.length).toFixed(1)} kills a match`, extra: sparkline(chrono.map(csKd)) },
        { label: "Headshots", value: kills ? pct((hsk * 100) / kills) : "–", sub: "of your kills", extra: sparkline(chrono.map(csHs)) },
      ])}
      <div class="cols">
        <section><div class="sec-head"><h3>Matches</h3></div>
          <div class="table-wrap"><table class="table"><thead><tr><th>Map</th><th>Result</th><th class="num">Score</th><th class="num">K</th><th class="num">D</th><th class="num">A</th><th class="num">HS</th><th class="num">MVPs</th><th class="num">Played</th><th></th></tr></thead>
          <tbody>${all.map((m) => `<tr class="${csResClass(m)}"><td><b>${esc(csMapName(m.map))}</b><span class="sub">${esc(m.mode || "")}</span></td>
            <td><span class="res ${csResClass(m)}">${csResText(m)}</span></td><td class="num">${m.myScore} : ${m.theirScore}</td>
            <td class="num">${m.kills}</td><td class="num">${m.deaths}</td><td class="num">${m.assists}</td><td class="num">${dash(csHs(m), (v) => pct(v))}</td><td class="num">${m.mvps}</td>
            <td class="num muted">${esc(fmtDate(m.date))}</td><td><button class="link danger" data-act="cs-delete" data-id="${esc(m.id)}" type="button">Delete</button></td></tr>`).join("")}</tbody></table></div></section>
        <section><div class="sec-head"><h3>By map</h3></div>
          <div class="lines">${mapRows.map((r) => `<div class="line static"><span class="grow"><b>${esc(csMapName(r.name))}</b><span class="muted"> ${r.played} played</span></span><span class="num muted">${r.kd.toFixed(2)} K/D</span><span class="num ${toneOfRate(r.rate)}">${pct(r.rate)}</span></div>`).join("")}</div></section>
      </div>
      <p class="hint">Recorded live from CS2's own feed. Valve publishes no CS2 match history or leaderboard for apps to read, so matches played while TheTracker was closed can't be added.</p>`;
  },
});
