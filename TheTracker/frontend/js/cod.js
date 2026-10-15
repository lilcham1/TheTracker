// Call of Duty pages. Activision publishes no stats API, so matches come from
// a quick log the player fills in after each one; the app adds when the game
// was open (play sessions) and asks for the matches when a session ends.
// Black Ops 7 and Modern Warfare 4 are win/loss; Warzone is placed.

const COD_GAMES = [["bo7", "Black Ops 7"], ["warzone", "Warzone"], ["mw4", "Modern Warfare 4"]];
const COD_NAME = Object.fromEntries(COD_GAMES);
const COD_MODES = {
  mp: ["Team Deathmatch", "Domination", "Hardpoint", "Search and Destroy", "Kill Confirmed", "Control", "Free-for-all", "Gunfight", "Ranked Play", "Other"],
  warzone: ["Battle Royale Quads", "Battle Royale Trios", "Battle Royale Duos", "Battle Royale Solos", "Resurgence Quads", "Resurgence Trios", "Resurgence Duos", "Plunder", "Ranked Battle Royale", "Other"],
};
const codModes = (game) => (game === "warzone" ? COD_MODES.warzone : COD_MODES.mp);
const codPlaced = (game) => game === "warzone";
const codKD = (k, d) => (d ? k / d : k);

const COD = { form: null, filter: "all", game: "" };
const codOverview = resource("codOverview", "cod_overview", { ttl: 30000 });
const codGoals = resource("codGoals", "cod_goals", { args: () => ({ game: COD.game }), ttl: 60000 });
const codStreams = resource("codStreams", "cod_streams", { ttl: 60000 });

const codMins = (m) => (m >= 60 ? `${Math.floor(m / 60)} h ${String(m % 60).padStart(2, "0")} min` : `${m} min`);

function codLoad(force) {
  return codOverview.load(force).then(() => codGoals.load(force));
}

/// A blank form, keeping the game and mode of the last one: matches in a
/// row are usually the same playlist.
function codNewForm(session = "") {
  const last = (codOverview.data && codOverview.data.matches[0]) || null;
  const game = (COD.form && COD.form.game) || (last && last.game) || "bo7";
  const mode = (COD.form && COD.form.mode) || (last && last.game === game && last.mode) || codModes(game)[0];
  return { id: "", game, mode, result: "", session };
}

function codFormValues() {
  const v = (id) => ($("#" + id) || {}).value || "";
  const n = (id) => Math.max(0, Math.round(Number(v(id)) || 0));
  const f = COD.form;
  return {
    id: f.id, game: f.game, mode: f.mode, result: f.result, session: f.session,
    place: n("codPlace"), kills: n("codKills"), deaths: n("codDeaths"), assists: n("codAssists"),
    score: codPlaced(f.game) ? 0 : n("codScore"), damage: codPlaced(f.game) ? n("codDamage") : 0,
    map: v("codMap").trim(), note: v("codNote").trim(),
  };
}

function codClearInputs() {
  for (const id of ["codPlace", "codKills", "codDeaths", "codAssists", "codScore", "codDamage", "codMap", "codNote"]) {
    const el = $("#" + id);
    if (el) el.value = "";
  }
}

act("cod-log", (el) => {
  if (!COD.form || COD.form.id) COD.form = codNewForm(el.dataset.session || "");
  if (el.dataset.session) COD.form.session = el.dataset.session;
  if (S.view !== "cod-overview" && S.view !== "cod-matches") go("cod-matches");
  else rerender();
  setTimeout(() => ($("#codKills") || {}).focus && $("#codKills").focus(), 60);
});
act("cod-more", () => {
  COD.form.full = true;
  rerender();
});
act("cod-form-close", () => {
  COD.form = null;
  codClearInputs();
  rerender();
});
act("cod-form-game", (el) => {
  COD.form.game = el.dataset.value;
  if (!codModes(COD.form.game).includes(COD.form.mode)) COD.form.mode = codModes(COD.form.game)[0];
  rerender();
});
onChange("cod-form-mode", (el) => {
  COD.form.mode = el.value;
});
act("cod-form-result", (el) => {
  COD.form.result = el.dataset.value;
  rerender();
});
act("cod-save", async (el) => {
  const m = codFormValues();
  const saved = await attempt(() => invoke("cod_save", m), m.id ? "Match updated." : "Match logged.");
  if (!saved) return;
  codOverview.clear();
  codGoals.clear();
  if (typeof workOnRes !== "undefined") workOnRes.cod.clear();
  // Another match from the same session: keep the form open and empty.
  if (el.dataset.again || !COD.form.full) {
    COD.form = { ...codNewForm(m.session), game: m.game, mode: m.mode };
    codClearInputs();
  } else {
    COD.form = null;
  }
  await codLoad(true);
  rerender();
});
act("cod-edit", (el) => {
  const m = (codOverview.data.matches || []).find((x) => x.id === el.dataset.id);
  if (!m) return;
  COD.form = { id: m.id, game: m.game, mode: m.mode, result: m.result || "", session: m.session || "", full: true };
  rerender();
  // The inputs exist after the redraw; fill them once.
  setTimeout(() => {
    const set = (id, v) => $("#" + id) && ($("#" + id).value = v || v === 0 ? v : "");
    set("codPlace", m.place || "");
    set("codKills", m.kills);
    set("codDeaths", m.deaths);
    set("codAssists", m.assists);
    set("codScore", m.score || "");
    set("codDamage", m.damage || "");
    set("codMap", m.map || "");
    set("codNote", m.note || "");
  }, 30);
});
act("cod-delete", (el) => {
  confirmBox({
    title: "Delete this match?",
    body: `<p>It comes out of your stats, goals and reviews. This can't be undone.</p>`,
    ok: "Delete",
    then: async () => {
      const ov = await attempt(() => invoke("cod_delete", { id: el.dataset.id }), "Match deleted.");
      if (!ov) return;
      codOverview.clear();
      codGoals.clear();
      if (COD.form && COD.form.id === el.dataset.id) COD.form = null;
      await codLoad(true);
      rerender();
    },
  });
});
act("cod-filter", (el) => {
  COD.filter = el.dataset.value;
  rerender();
});
act("cod-stats-game", (el) => {
  COD.game = el.dataset.value;
  codGoals.load();
  rerender();
});
act("cod-review", (el) => {
  REV.cod = el.dataset.id;
  reviewRes.cod.clear();
  if (S.boot.coach) coachRes.cod.clear();
  go("cod-review");
});

/// The logger on Overview and Matches: one line (the quick logger), or the
/// full form for more detail and for edits.
function codLoggerHtml(d) {
  if (!COD.form) COD.form = codNewForm(d.toLog ? d.toLog.id : "");
  if (d.toLog && !COD.form.session && !COD.form.id) COD.form.session = d.toLog.id;
  return COD.form.full ? codFormHtml() : codQuickHtml(d);
}

function codQuickHtml(d) {
  const f = COD.form;
  const placed = codPlaced(f.game);
  const num = (id, label, ph) => `<label class="cod-q-num"><span class="muted">${label}</span><input class="input" id="${id}" type="number" min="0" inputmode="numeric" placeholder="${ph}" data-enter="cod-save" /></label>`;
  const session = d.toLog && f.session === d.toLog.id ? `Logging for your ${codMins(Math.round((d.toLog.end - d.toLog.start) / 60))} session` : d.playing ? "Logging for the session running now" : "";
  return `<section class="cod-quick">
    <div class="chips seg">${COD_GAMES.map(([k, l]) => `<button class="chip ${f.game === k ? "on" : ""}" data-act="cod-form-game" data-value="${k}" type="button" title="${l}">${{ bo7: "BO7", warzone: "Warzone", mw4: "MW4" }[k]}</button>`).join("")}</div>
    <select class="input cod-q-mode" data-change="cod-form-mode" aria-label="Mode">${codModes(f.game).map((m) => `<option ${m === f.mode ? "selected" : ""}>${esc(m)}</option>`).join("")}</select>
    ${placed ? num("codPlace", "#", "place") : `<div class="chips seg">${[["win", "Win"], ["loss", "Loss"], ["draw", "Draw"]].map(([k, l]) => `<button class="chip ${f.result === k ? "on" : ""}" data-act="cod-form-result" data-value="${k}" type="button">${l}</button>`).join("")}</div>`}
    ${num("codKills", "K", "0")}${num("codDeaths", "D", "0")}${num("codAssists", "A", "0")}
    <button class="btn" data-act="cod-save" type="button">Save</button>
    <button class="link" data-act="cod-more" type="button">More…</button>
    ${session ? `<span class="cod-q-session muted small">${esc(session)}</span>` : ""}
  </section>`;
}

function codFormHtml() {
  const f = COD.form;
  if (!f) return "";
  const placed = codPlaced(f.game);
  const num = (id, label, ph = "0") => `<label class="field cod-num"><span>${label}</span><input class="input" id="${id}" type="number" min="0" inputmode="numeric" placeholder="${ph}" /></label>`;
  return `<section class="panel cod-form">
    <div class="sec-head"><h3>${f.id ? "Edit match" : "Log a match"}</h3><button class="link" data-act="cod-form-close" type="button">Close</button></div>
    <div class="chips seg">${COD_GAMES.map(([k, l]) => `<button class="chip ${f.game === k ? "on" : ""}" data-act="cod-form-game" data-value="${k}" type="button">${l}</button>`).join("")}</div>
    <div class="cod-grid">
      <label class="field"><span>Mode</span><select class="input" data-change="cod-form-mode">${codModes(f.game).map((m) => `<option ${m === f.mode ? "selected" : ""}>${esc(m)}</option>`).join("")}</select></label>
      ${placed ? num("codPlace", "Placed", "#") : `<div class="field"><span>Result</span><div class="chips seg">${[["win", "Win"], ["loss", "Loss"], ["draw", "Draw"]].map(([k, l]) => `<button class="chip ${f.result === k ? "on" : ""}" data-act="cod-form-result" data-value="${k}" type="button">${l}</button>`).join("")}</div></div>`}
    </div>
    <div class="cod-nums">
      ${num("codKills", "Kills")}${num("codDeaths", "Deaths")}${num("codAssists", "Assists")}
      ${placed ? num("codDamage", "Damage", "optional") : num("codScore", "Score", "optional")}
    </div>
    <div class="cod-grid">
      <label class="field"><span>Map <span class="muted">optional</span></span><input class="input" id="codMap" type="text" maxlength="40" /></label>
      <label class="field"><span>Note <span class="muted">optional, for you and the coach</span></span><input class="input" id="codNote" type="text" maxlength="500" placeholder="What decided it?" /></label>
    </div>
    <div class="row wrap">
      <button class="btn" data-act="cod-save" type="button">${f.id ? "Save changes" : "Save"}</button>
      ${f.id ? "" : `<button class="btn ghost" data-act="cod-save" data-again="1" type="button">Save and log another</button>`}
    </div>
  </section>`;
}

/// One logged match.
function codMatchHtml(m) {
  const placed = codPlaced(m.game);
  const res = placed
    ? `<span class="pg-place ${m.place === 1 ? "win" : m.place <= 10 ? "top" : ""}">#${m.place}</span>`
    : `<span class="res ${m.result}">${{ win: "Won", loss: "Lost", draw: "Draw" }[m.result]}</span>`;
  const extra = placed ? (m.damage ? `, ${fmtNum(m.damage)} damage` : "") : m.score ? `, score ${fmtNum(m.score)}` : "";
  return `<div class="line ${m.result === "win" || m.place === 1 ? "win" : ""}">
      <span class="cod-res">${res}</span>
      <span class="grow"><b>${esc(m.mode)}</b> <span class="muted">${esc(COD_NAME[m.game] || m.game)}${m.map ? ` · ${esc(m.map)}` : ""}</span>
        <span class="sub">${m.kills} / ${m.deaths} / ${m.assists} · K/D ${codKD(m.kills, m.deaths).toFixed(2)}${extra}${m.note ? ` · <i>${esc(m.note)}</i>` : ""}</span></span>
      <span class="muted when">${ago(m.date)}</span>
      <span class="cod-acts"><button class="link" data-act="cod-review" data-id="${esc(m.id)}" type="button">Review</button>
        <button class="link" data-act="cod-edit" data-id="${esc(m.id)}" type="button">Edit</button>
        <button class="link danger" data-act="cod-delete" data-id="${esc(m.id)}" type="button">Delete</button></span>
    </div>`;
}

view("cod-overview", {
  game: "cod", nav: true, icon: "overview", title: "Overview",
  sub: () => "From the matches you log, and when the game was open",
  load: codLoad,
  render() {
    const wait = gate(codOverview, "Reading your log…");
    if (wait) return wait;
    const d = codOverview.data;
    const playing = d.playing ? `<div class="note"><span class="live-dot"></span> Call of Duty is open: this session is being timed. Log each match when it ends, or all of them afterwards.</div>` : "";
    if (!d.matches.length) {
      return `${playing}${codLoggerHtml(d)}${emptyState("Log your first match",
        "Activision doesn't let apps read your matches, so TheTracker keeps your own log: after a match, fill in the line above (Enter saves). Stats, goals, reviews and the AI coach work from it.")}
        ${d.weekMinutes ? `<p class="hint">Played ${codMins(d.weekMinutes)} in the last 7 days.</p>` : ""}`;
    }
    if (!COD.game || !d.stats.some((s) => s.game === COD.game)) COD.game = d.stats.find((s) => s.game === d.matches[0].game) ? d.matches[0].game : d.stats[0].game;
    const s = d.stats.find((x) => x.game === COD.game);
    const placed = codPlaced(s.game);
    const form = d.matches.filter((m) => m.game === s.game).slice(0, 30).map((m) => (placed ? (m.place === 1 ? true : m.place <= 10 ? null : false) : m.result === "win" ? true : m.result === "loss" ? false : null));
    return `${playing}${codLoggerHtml(d)}
      <section class="hero-head">
        <div class="grow"><h2 class="display">${esc(s.label)}</h2>
          <p class="muted">${s.matches} logged match${s.matches === 1 ? "" : "es"}${s.topMode ? ` · mostly ${esc(s.topMode)}` : ""}</p>
          ${formStrip(form, 30)}</div>
      </section>
      ${d.stats.length > 1 ? `<div class="chips seg">${d.stats.map((x) => `<button class="chip ${x.game === s.game ? "on" : ""}" data-act="cod-stats-game" data-value="${x.game}" type="button">${esc(x.label)}</button>`).join("")}</div>` : ""}
      ${statRow([
        { label: "K/D", value: s.kd.toFixed(2), sub: `${fmtNum(s.kills)} kills, ${fmtNum(s.deaths)} deaths` },
        placed
          ? { label: "Average place", value: `#${(s.avgPlace || 0).toFixed(1)}`, sub: `${s.wins1 || 0} win${s.wins1 === 1 ? "" : "s"}, ${s.top10 || 0} top 10` }
          : { label: "Win rate", value: pct(s.winRate * 100), tone: toneOfRate(s.winRate * 100), sub: `${s.wins} won, ${s.losses} lost${s.draws ? `, ${s.draws} drawn` : ""}` },
        placed && s.avgDamage ? { label: "Damage a match", value: fmtNum(Math.round(s.avgDamage)) } : { label: "Kills a match", value: (s.kills / s.matches).toFixed(1), sub: `${(s.assists / s.matches).toFixed(1)} assists` },
        { label: "Last 7 days", value: codMins(d.weekMinutes), sub: `${d.weekMatches} match${d.weekMatches === 1 ? "" : "es"} logged` },
      ])}
      ${s.form.length >= 3 ? `<div class="sec-head"><h3>K/D, match by match</h3><span class="muted">oldest to newest</span></div>${sparkline(s.form, { width: 520, height: 60, points: true })}` : ""}
      ${codGoals.data && codGoals.data.goals.length ? `<section class="rev-goals">${roleGoalsHtml(codGoals.data)}</section>` : codGoals.data && codGoals.data.note ? `<p class="hint">${esc(codGoals.data.note)}</p>` : ""}
      <div class="sec-head"><h3>Recent matches</h3><button class="link" data-act="go" data-view="cod-matches" type="button">All matches</button></div>
      <div class="lines">${d.matches.slice(0, 6).map(codMatchHtml).join("")}</div>
      ${d.sessions.length ? `<div class="sec-head"><h3>Play sessions</h3><span class="muted">timed while Call of Duty was open</span></div>
        <table class="mini">${d.sessions.slice(0, 6).map((x) => `<tr><td>${new Date(x.start * 1000).toLocaleString(undefined, { weekday: "short", hour: "2-digit", minute: "2-digit" })}</td>
          <td class="num">${x.open ? "playing now" : codMins(Math.round((x.end - x.start) / 60))}</td>
          <td class="num">${d.matches.filter((m) => m.session === x.id).length} logged</td></tr>`).join("")}</table>` : ""}`;
  },
});

view("cod-matches", {
  game: "cod", nav: true, icon: "matches", title: "Matches",
  sub: () => "Your logged matches: add, fix or delete",
  load: codLoad,
  render() {
    const wait = gate(codOverview, "Reading your log…");
    if (wait) return wait;
    const d = codOverview.data;
    const games = [...new Set(d.matches.map((m) => m.game))];
    const shown = d.matches.filter((m) => COD.filter === "all" || m.game === COD.filter || m.mode === COD.filter);
    return `${codLoggerHtml(d)}
      ${d.matches.length ? `<div class="chips seg">${[["all", "All"], ...games.map((g) => [g, COD_NAME[g] || g])].map(([k, l]) => `<button class="chip ${COD.filter === k ? "on" : ""}" data-act="cod-filter" data-value="${esc(k)}" type="button">${esc(l)}</button>`).join("")}</div>
        <div class="lines">${shown.map(codMatchHtml).join("")}</div>`
        : `<p class="muted">Nothing logged yet. After a match, fill in the line above: it takes about ten seconds.</p>`}`;
  },
});

view("cod-live", {
  game: "cod", nav: true, icon: "live", title: "Live",
  sub: () => "Call of Duty on Twitch right now, most watched first",
  load(force) {
    return codStreams.load(force);
  },
  render() {
    const wait = gate(codStreams, "Finding live streams…");
    if (wait) return wait;
    const d = codStreams.data;
    if (d.reason === "not_set_up") return `<p class="muted">Live streams need the Twitch keys on the cloud service, the same ones the other streamer pages use.</p>`;
    if (d.reason === "unknown_game") return `<p class="muted">The cloud service doesn't know Call of Duty yet: run <code>npx convex deploy</code> in the TheTracker folder.</p>`;
    if (d.reason) return `<p class="muted">Twitch couldn't be reached right now. Try again in a minute.</p>`;
    const cats = [...new Set(d.streams.map((s) => s.game).filter(Boolean))];
    const pick = COD.liveCat && cats.includes(COD.liveCat) ? COD.liveCat : "";
    const shown = d.streams.filter((s) => !pick || s.game === pick);
    if (!d.streams.length) return `<p class="muted">Nobody is streaming Call of Duty on Twitch right now.</p>`;
    return `${cats.length > 1 ? `<div class="chips seg">${[["", "All"], ...cats.map((c) => [c, c.replace(/^Call of Duty: /, "")])].map(([k, l]) => `<button class="chip ${pick === k ? "on" : ""}" data-act="cod-live-cat" data-value="${esc(k)}" type="button">${esc(l)}</button>`).join("")}</div>` : ""}
      <div class="stream-grid">${shown.slice(0, 60).map((s) => `<button class="stream-card" data-act="open-url" data-url="${esc(twitchUrl(s.login))}" type="button" title="Watch ${esc(s.name)} on Twitch">
        <span class="stream-thumb">${s.thumbnail ? `<img src="${esc(s.thumbnail)}" alt="" loading="lazy" />` : ""}<span class="live-tag">Live</span><span class="viewers">${fmtViewers(s.viewers)} watching</span></span>
        <span class="stream-body"><span class="stream-name"><b>${esc(s.name)}</b>${s.game ? ` <span class="muted small">${esc(s.game.replace(/^Call of Duty: /, ""))}</span>` : ""}</span><span class="stream-title">${esc(s.title || "")}</span></span>
      </button>`).join("")}</div>`;
  },
});
act("cod-live-cat", (el) => {
  COD.liveCat = el.dataset.value;
  rerender();
});

function codSettingsHtml() {
  const c = S.boot.prefs.coach || {};
  const level = c.codLevel || "push";
  const chip = (v, l, d) => `<button class="chip ${level === v ? "on" : ""}" data-act="coach-set" data-key="codLevel" data-value="${v}" type="button">${l} <span class="muted">${d}</span></button>`;
  return `<section class="set"><h3>Goals</h3>
      <p class="muted">Goals for kills, deaths and score (or damage and placement in Warzone), drawn from your own logged matches of the last 60 days, checked on Overview and in each match's Review.</p>
      <div class="field"><span>How high</span><div class="chips seg">${chip("steady", "Steady", "top 50%")}${chip("push", "Push", "top 30%")}${chip("pro", "Pro", "top 20%")}</div></div>
    </section>
    ${typeof presetsHtml === "function" ? presetsHtml("cod") : ""}
    <section class="set"><h3>What works for Call of Duty</h3>
      <p class="muted">Activision doesn't let apps read Call of Duty matches or stats, so there's no automatic match history, leaderboard, meta or overlay. Instead you log each match in a few taps, and the stats, goals, reviews and AI coach work from that log. TheTracker times how long the game is open and, with notifications on, asks for your matches when you close it. Pro settings, Game mode and the connection check work as for the other games. Black Ops 7, Warzone and Modern Warfare 4 all run as Call of Duty HQ, so all three are covered.</p>
    </section>`;
}
