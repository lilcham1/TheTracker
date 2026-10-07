// Counter-Strike 2 pages. Everything here is what the app recorded live from
// the game's own feed, round by round: Valve publishes no CS2 match history
// for apps to read.

const CS = { status: null, setup: null, history: [], busy: false, error: null, open: new Set() };

const WEAPON_NAMES = {
  ak47: "AK-47", m4a1: "M4A4", m4a1_silencer: "M4A1-S", awp: "AWP", deagle: "Desert Eagle", glock: "Glock-18",
  usp_silencer: "USP-S", hkp2000: "P2000", p250: "P250", fiveseven: "Five-SeveN", tec9: "Tec-9", cz75a: "CZ75-Auto",
  elite: "Dual Berettas", revolver: "R8 Revolver", famas: "FAMAS", galilar: "Galil AR", aug: "AUG", sg556: "SG 553",
  ssg08: "SSG 08", scar20: "SCAR-20", g3sg1: "G3SG1", mp9: "MP9", mac10: "MAC-10", mp7: "MP7", mp5sd: "MP5-SD",
  ump45: "UMP-45", p90: "P90", bizon: "PP-Bizon", nova: "Nova", xm1014: "XM1014", mag7: "MAG-7", sawedoff: "Sawed-Off",
  negev: "Negev", m249: "M249", hegrenade: "HE grenade", molotov: "Molotov", incgrenade: "Incendiary", inferno: "Fire",
  knife: "Knife", knife_t: "Knife", bayonet: "Knife", taser: "Zeus x27",
};
const BUY_NAMES = { pistol: "Pistol round", eco: "Eco", force: "Force buy", full: "Full buy" };
const END_NAMES = { elimination: "Elimination", bomb: "Bomb exploded", defuse: "Bomb defused", time: "Time ran out" };
const weaponLabel = (k) => WEAPON_NAMES[k] || (k.startsWith("knife") ? "Knife" : k.replace(/_/g, " ").replace(/\b\w/g, (c) => c.toUpperCase()));
const csMapName = (m) => (m || "").replace(/^(de|cs|ar|dm|gd)_/, "").replace(/_/g, " ").replace(/\b\w/g, (c) => c.toUpperCase()) || "Unknown map";
const csResClass = (m) => (m.result === "win" ? "win" : m.result === "loss" ? "loss" : "other");
const csResText = (m) => ({ win: "Won", loss: "Lost", draw: "Draw" }[m.result] || (m.incomplete ? "Left" : "–"));
const csKd = (m) => m.kills / Math.max(1, m.deaths);
const csHs = (m) => (m.kills ? (m.headshotKills * 100) / m.kills : null);
const csAdr = (m) => (m.rounds && m.rounds.length ? m.damage / m.rounds.length : null);
const csWon = (m) => (m.result === "win" ? true : m.result === "loss" ? false : null);

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
    if (S.view === "cs-live" || (now && !before && (S.view === "today" || VIEWS[S.view].game === "cs2"))) rerender();
    renderNav();
  } catch (_) {
    /* next tick */
  }
}
setInterval(csPoll, 1000);

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
act("cs-check", () => csLoad());
act("cs-sim", async () => {
  if (await attempt(() => invoke("cs2_sim_start"), "Test match started. It plays 20 quick rounds and isn't saved.")) {
    CS.dismissed = null;
    if (S.view !== "cs-live") go("cs-live");
    csPoll();
  }
});
act("cs-sim-stop", async () => {
  await attempt(() => invoke("cs2_sim_stop"));
  csPoll();
});
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
act("cs-toggle", (el) => {
  CS.open.has(el.dataset.id) ? CS.open.delete(el.dataset.id) : CS.open.add(el.dataset.id);
  rerender();
});

// ---------- Shared pieces ----------

/// The round-by-round strip: one cell per round, taller and green for a win,
/// with a pip per kill and a mark if the player died. Half time is a gap.
function roundStrip(rounds) {
  if (!rounds || !rounds.length) return `<p class="muted">No rounds recorded for this match.</p>`;
  return `<div class="rounds" role="img" aria-label="Round by round">${rounds
    .map((r, i) => {
      const swap = i > 0 && rounds[i - 1].side !== r.side ? `<span class="half" title="Sides swapped"></span>` : "";
      const buy = r.buy ? ` ${BUY_NAMES[r.buy]}, $${fmtNum(r.equip)} carried.` : "";
      const end = r.end ? ` ${END_NAMES[r.end]}.` : "";
      const tip = `Round ${r.n}: ${r.won ? "won" : "lost"} as ${r.side}.${end}${buy} ${r.kills} kill${r.kills === 1 ? "" : "s"}${r.hs ? ` (${r.hs} headshot${r.hs === 1 ? "" : "s"})` : ""}, ${r.damage} damage${r.died ? ", died" : ", survived"}.`;
      const bomb = r.bomb === "exploded" || r.bomb === "defused" ? `<em class="bomb ${r.bomb}"></em>` : "";
      return `${swap}<span class="round ${r.won ? "w" : "l"} ${r.died ? "died" : ""} ${r.buy ? "buy-" + r.buy : ""}" title="${esc(tip)}">
        ${bomb}<span class="pips">${"<i></i>".repeat(Math.min(r.kills, 5))}</span><b class="side-${r.side === "CT" ? "ct" : "t"}"></b></span>`;
    })
    .join("")}</div>`;
}

function barRow(label, value, max, right, tone = "") {
  const w = max > 0 ? Math.max(2, (value / max) * 100) : 0;
  return `<div class="hbar"><span class="hbar-label">${label}</span><span class="hbar-track"><i class="${tone}" style="width:${w.toFixed(1)}%"></i></span><span class="num">${right}</span></div>`;
}

const csRate = (rs) => (rs.length ? (rs.filter((r) => r.won).length * 100) / rs.length : null);

/// Figures summed over a set of recorded matches.
function csAggregate(list) {
  const sum = (f) => list.reduce((a, m) => a + f(m), 0);
  const rounds = list.flatMap((m) => m.rounds || []);
  const decided = list.filter((m) => m.result === "win" || m.result === "loss");
  const wins = decided.filter((m) => m.result === "win").length;
  const kills = sum((m) => m.kills), deaths = sum((m) => m.deaths), hs = sum((m) => m.headshotKills);
  const group = (rs) => ({ played: rs.length, rate: csRate(rs) });
  const weapons = {};
  for (const m of list) for (const [k, n] of Object.entries(m.weaponKills || {})) weapons[k] = (weapons[k] || 0) + n;
  const multi = [2, 3, 4, 5].map((n) => rounds.filter((r) => (n === 5 ? r.kills >= 5 : r.kills === n)).length);
  // The first half of a match is every round before the sides first swap.
  const first = [], second = [];
  for (const m of list) {
    const rs = m.rounds || [];
    const swap = rs.findIndex((r, i) => i > 0 && rs[i - 1].side !== r.side);
    rs.forEach((r, i) => (swap < 0 || i < swap ? first : second).push(r));
  }
  return {
    matches: list.length, wins, losses: decided.length - wins, rate: decided.length ? (wins * 100) / decided.length : null,
    kills, deaths, assists: sum((m) => m.assists), kd: kills / Math.max(1, deaths), hs: kills ? (hs * 100) / kills : null,
    adr: rounds.length ? rounds.reduce((a, r) => a + r.damage, 0) / rounds.length : null,
    kpr: rounds.length ? rounds.reduce((a, r) => a + r.kills, 0) / rounds.length : null,
    survival: rounds.length ? (rounds.filter((r) => !r.died).length * 100) / rounds.length : null,
    rounds: rounds.length, ct: group(rounds.filter((r) => r.side === "CT")), t: group(rounds.filter((r) => r.side === "T")),
    firstHalf: group(first), secondHalf: group(second), multi,
    buys: Object.keys(BUY_NAMES).map((k) => ({ key: k, ...group(rounds.filter((r) => r.buy === k)) })),
    ends: Object.keys(END_NAMES).map((k) => {
      const rs = rounds.filter((r) => r.end === k);
      return { key: k, won: rs.filter((r) => r.won).length, lost: rs.filter((r) => !r.won).length };
    }),
    weapons: Object.entries(weapons).sort((a, b) => b[1] - a[1]),
  };
}

const csRateBar = (label, g, tone) => barRow(label, g.rate || 0, 100, `<span class="${toneOfRate(g.rate)}">${pct(g.rate)}</span> <span class="muted">of ${g.played} rounds</span>`, tone || toneOfRate(g.rate));

// ---------- Overview ----------

view("cs-overview", {
  game: "cs2", nav: true, icon: "overview", title: "Overview",
  sub: () => "Your Counter-Strike 2 numbers, from the matches recorded here",
  load: csLoad,
  render() {
    const all = [...CS.history].reverse();
    if (!all.length) {
      return emptyState("No CS2 matches recorded yet", "Keep TheTracker running while you play and each match is saved when it ends, round by round: kills, damage, headshots, weapon, side, what you bought and how the round ended.",
        `<span class="row"><button class="btn" data-act="go" data-view="cs-live" type="button">Check the live setup</button><button class="btn ghost" data-act="cs-sim" type="button">Watch a test match</button></span>`);
    }
    const a = csAggregate(all);
    const chrono = [...all].reverse();
    const maxW = a.weapons.length ? a.weapons[0][1] : 1;
    const hasBuys = a.buys.some((b) => b.played), hasEnds = a.ends.some((e) => e.won + e.lost);

    return `${statRow([
        { label: `Win rate, ${a.matches} match${a.matches === 1 ? "" : "es"}`, value: pct(a.rate), tone: toneOfRate(a.rate), sub: `${a.wins} won, ${a.losses} lost`, extra: formStrip(all.map(csWon), 30) },
        { label: "K/D", value: a.kd.toFixed(2), tone: a.kd >= 1 ? "win" : "loss", sub: `${dash(a.kpr, (v) => v.toFixed(2))} kills a round`, extra: sparkline(chrono.map(csKd)) },
        { label: "Damage per round", value: dash(a.adr, (v) => v.toFixed(0)), sub: `${dash(a.survival, (v) => pct(v))} of rounds survived`, extra: sparkline(chrono.map(csAdr)) },
        { label: "Headshots", value: dash(a.hs, (v) => pct(v)), sub: "of your kills", extra: sparkline(chrono.map(csHs), { points: true }) },
      ])}
      <div class="cols">
        <section>
          <div class="sec-head"><h3>Recent matches</h3><button class="link" data-act="go" data-view="cs-matches" type="button">All matches</button></div>
          <div class="lines">${all.slice(0, 6).map((m) => `<button class="line ${csResClass(m)}" data-act="go" data-view="cs-matches" type="button">
            <span class="grow"><b>${esc(csMapName(m.map))}</b><span class="muted"> ${m.myScore} : ${m.theirScore}</span></span>
            <span class="num">${m.kills}/${m.deaths}/${m.assists}</span><span class="num muted">${dash(csAdr(m), (v) => v.toFixed(0))} ADR</span>
            <span class="res ${csResClass(m)}">${csResText(m)}</span><span class="muted when">${ago(Date.parse(m.date) / 1000)}</span></button>`).join("")}</div>
          <div class="sec-head"><h3>Rounds won</h3></div>
          <div class="hbars">
            ${csRateBar(`<span class="side-dot ct"></span>Counter-Terrorist`, a.ct, "ct")}
            ${csRateBar(`<span class="side-dot t"></span>Terrorist`, a.t, "t")}
            ${a.secondHalf.played ? csRateBar("First half", a.firstHalf) + csRateBar("Second half", a.secondHalf) : ""}
          </div>
        </section>
        <section>
          <div class="sec-head"><h3>Kills by weapon</h3></div>
          ${a.weapons.length ? `<div class="hbars">${a.weapons.slice(0, 7).map(([k, n]) => barRow(esc(weaponLabel(k)), n, maxW, `${n} <span class="muted">${pct((n * 100) / Math.max(1, a.kills))}</span>`)).join("")}</div>` : `<p class="muted">Recorded from your next match on.</p>`}
          <div class="sec-head"><h3>Multi-kill rounds</h3></div>
          <div class="multi">${["2 kills", "3 kills", "4 kills", "Ace"].map((label, i) => `<div><span class="display">${a.multi[i]}</span><span class="muted">${label}</span></div>`).join("")}</div>
        </section>
      </div>
      ${hasBuys || hasEnds ? `<div class="cols even">
        <section><div class="sec-head"><h3>Rounds won by what you bought</h3></div>
          ${hasBuys ? `<div class="hbars">${a.buys.filter((b) => b.played).map((b) => csRateBar(BUY_NAMES[b.key], b)).join("")}</div>
            <p class="hint">From the value of what you carried into the round: under $1,500 is an eco, under $3,500 a force buy.</p>` : `<p class="muted">Recorded from your next match on.</p>`}</section>
        <section><div class="sec-head"><h3>How rounds ended</h3></div>
          ${hasEnds ? `<div class="stat-list">${a.ends.filter((e) => e.won + e.lost).map((e) => `<div><span>${END_NAMES[e.key]}</span><span class="num"><b class="win">${e.won} won</b> <span class="muted">·</span> <b class="loss">${e.lost} lost</b></span></div>`).join("")}</div>` : `<p class="muted">Recorded from your next match on.</p>`}</section>
      </div>` : ""}`;
  },
});

// ---------- Live ----------

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
    <p class="muted">This page fills in by itself when a CS2 match starts: the score, every round as it ends, your kills, damage and headshots. When the match is over it's saved to Matches.</p>
    <div class="checks">${rows.join("")}</div>
    ${CS.error ? `<div class="note err">${esc(CS.error)}</div>` : ""}
    <div class="row wrap">
      ${s.cfgDirs.length && !s.installed ? `<button class="btn" data-act="cs-install" type="button" ${CS.busy ? "disabled" : ""}>Set up CS2</button>` : ""}
      <button class="btn ghost" data-act="cs-check" type="button">Check again</button>
      <button class="btn ghost" data-act="cs-sim" type="button" title="Plays a short fake match so you can see this page working. Nothing is saved.">Watch a test match</button>
    </div>
    <p class="hint">TheTracker only sees your own state, the same official feed tournament overlays use. It reads nothing from the game's memory. It can only record a match while it's running.</p>
    ${s.cfgDirs.length ? `<details class="more"><summary>What TheTracker changed on this PC</summary>
      ${s.cfgDirs.map((d) => `<code class="path">${esc(d)}\\gamestate_integration_thetracker.cfg</code>`).join("")}
      <p class="muted">Switching CS2 off under Settings, General removes the file.</p></details>` : ""}
  </div>`;
}

function csMatchHtml(m, live) {
  const hs = csHs(m), adr = csAdr(m);
  const phase = { warmup: "Warmup", live: "In progress", intermission: "Half time", gameover: "Match over" }[m.phase] || "In progress";
  const wk = Object.entries(m.weaponKills || {}).sort((a, b) => b[1] - a[1]);
  return `<div class="panel">
    ${m.simulated ? `<div class="note">Test match: nothing here is saved. ${m.ended ? "" : `<button class="link" data-act="cs-sim-stop" type="button">Stop the test</button>`}</div>` : ""}
    <div class="live-head">
      <div class="grow"><h2 class="display">${esc(csMapName(m.map))} ${m.ended ? `<span class="res ${csResClass(m)}">${csResText(m)}</span>` : ""}</h2>
        <p class="muted">${m.ended ? "Match over" : `<span class="live-dot ${live ? "" : "off"}"></span> ${phase}`}${m.mode ? `, ${esc(m.mode)}` : ""}${m.team && !m.ended ? `, playing <span class="side-dot ${m.team === "CT" ? "ct" : "t"}"></span>${m.team === "CT" ? "Counter-Terrorist" : "Terrorist"}` : ""}</p></div>
      <div class="clock display"><span class="${m.myScore > m.theirScore ? "win" : m.myScore < m.theirScore ? "loss" : ""}">${m.myScore}</span><span class="muted"> : </span>${m.theirScore}</div>
      ${m.ended ? `<button class="btn ghost" data-act="cs-dismiss" type="button">Done</button>` : ""}
    </div>
    ${roundStrip(m.rounds)}
    ${statRow([
      { label: "K / D / A", value: `${m.kills} / ${m.deaths} / ${m.assists}`, sub: `${csKd(m).toFixed(2)} K/D` },
      { label: "Damage per round", value: dash(adr, (v) => v.toFixed(0)), sub: `${fmtNum(m.damage)} damage dealt` },
      { label: "Headshots", value: known(hs) ? pct(hs) : "–", sub: `${m.headshotKills} of ${m.kills} kills` },
      { label: "MVPs", value: m.mvps, sub: `${m.score} score` },
      ...(m.ended ? [] : [{ label: "Right now", value: "$" + fmtNum(m.money), sub: `${m.health} HP, ${m.armor} armor${m.weapon ? `, ${esc(weaponLabel(m.weapon))}` : ""}` }]),
    ])}
    ${wk.length ? `<div class="sec-head"><h3>Kills by weapon</h3></div><div class="hbars narrow">${wk.slice(0, 6).map(([k, n]) => barRow(esc(weaponLabel(k)), n, wk[0][1], String(n))).join("")}</div>` : ""}
    ${m.ended && m.incomplete ? `<p class="hint">You left before the end, so there's no result for this match.</p>` : ""}
    ${m.ended ? `<p class="hint">${m.simulated ? "" : "Saved to Matches. "}<button class="link" data-act="cs-sim" type="button">${m.simulated ? "Run the test match again" : "Watch a test match"}</button></p>` : ""}
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

// ---------- Matches ----------

view("cs-matches", {
  game: "cs2", nav: true, icon: "matches", title: "Matches",
  sub: () => (CS.history.length ? `${CS.history.length} match${CS.history.length === 1 ? "" : "es"} recorded while you played` : "Matches TheTracker recorded while you played"),
  load: csLoad,
  render() {
    const all = [...CS.history].reverse();
    if (!all.length) {
      return emptyState("No CS2 matches recorded yet", "Keep TheTracker running while you play and each match is saved here when it ends. Valve doesn't publish CS2 match history, so only matches played while the app is open can be listed.",
        `<button class="btn" data-act="go" data-view="cs-live" type="button">Check the live setup</button>`);
    }
    const maps = new Map();
    for (const m of all) {
      if (!maps.has(m.map)) maps.set(m.map, []);
      maps.get(m.map).push(m);
    }
    const mapRows = [...maps.entries()].map(([name, list]) => ({ name, ...csAggregate(list) })).sort((a, b) => b.matches - a.matches);

    return `<div class="table-wrap"><table class="table">
        <thead><tr><th>Map</th><th>Result</th><th class="num">Score</th><th class="num">K</th><th class="num">D</th><th class="num">A</th><th class="num">K/D</th><th class="num">ADR</th><th class="num">HS</th><th class="num">MVPs</th><th class="num">Played</th></tr></thead>
        <tbody>${all.map((m) => {
          const open = CS.open.has(m.id);
          const wk = Object.entries(m.weaponKills || {}).sort((a, b) => b[1] - a[1]);
          return `<tr class="click ${csResClass(m)}" data-act="cs-toggle" data-id="${esc(m.id)}" title="Show the rounds">
            <td><b>${esc(csMapName(m.map))}</b><span class="sub">${esc(m.mode || "")}${m.incomplete ? ", left early" : ""}</span></td>
            <td><span class="res ${csResClass(m)}">${csResText(m)}</span></td><td class="num display">${m.myScore} : ${m.theirScore}</td>
            <td class="num">${m.kills}</td><td class="num">${m.deaths}</td><td class="num">${m.assists}</td>
            <td class="num ${csKd(m) >= 1 ? "win" : "loss"}">${csKd(m).toFixed(2)}</td><td class="num">${dash(csAdr(m), (v) => v.toFixed(0))}</td>
            <td class="num">${dash(csHs(m), (v) => pct(v))}</td><td class="num">${m.mvps}</td><td class="num muted">${esc(fmtDate(m.date))}</td></tr>
            ${open ? `<tr class="detail"><td colspan="11">
              ${roundStrip(m.rounds)}
              <div class="row wrap">${wk.slice(0, 6).map(([k, n]) => `<span class="chip static">${esc(weaponLabel(k))} <b>${n}</b></span>`).join("")}
                <span class="grow"></span><button class="btn ghost small" data-act="share" data-kind="cs" data-id="${esc(m.id)}" type="button">Share</button><button class="link danger" data-act="cs-delete" data-id="${esc(m.id)}" type="button">Delete this match</button></div>
              <p class="hint">Hover a round for what you bought and how it ended. A dot above a round marks the bomb going off or being defused.</p>
            </td></tr>` : ""}`;
        }).join("")}</tbody></table></div>
      <div class="sec-head"><h3>By map</h3></div>
      <div class="table-wrap"><table class="table"><thead><tr><th>Map</th><th class="num">Played</th><th class="num">Win rate</th><th></th><th class="num">K/D</th><th class="num">ADR</th><th class="num">CT rounds won</th><th class="num">T rounds won</th></tr></thead>
        <tbody>${mapRows.map((r) => `<tr><td><b>${esc(csMapName(r.name))}</b></td><td class="num">${r.matches}</td>
          <td class="num display ${toneOfRate(r.rate)}">${pct(r.rate)}</td><td>${known(r.rate) ? rateBar(r.rate, 0, 100) : ""}</td>
          <td class="num">${r.kd.toFixed(2)}</td><td class="num">${dash(r.adr, (v) => v.toFixed(0))}</td>
          <td class="num ${toneOfRate(r.ct.rate)}">${pct(r.ct.rate)}</td><td class="num ${toneOfRate(r.t.rate)}">${pct(r.t.rate)}</td></tr>`).join("")}</tbody></table></div>
      <details class="about"><summary>About this data</summary><p>Recorded live from CS2's own feed. Valve publishes no CS2 match history for apps to read, so matches played while TheTracker was closed can't be added.</p></details>`;
  },
});
