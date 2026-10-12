// The Today page: every game the player has switched on, at a glance. It
// asks the backend for nothing new; it reads what each game's own pages
// already load.

const owProgressAll = resource("owProgressAll", "ow_progress", { args: () => ({ mode: "all" }), ttl: 60000 });

const GAME_NAMES = { dota: "Dota 2", deadlock: "Deadlock", cs2: "Counter-Strike 2", overwatch: "Overwatch", pubg: "PUBG" };

function dayStart(daysAgo = 0) {
  const d = new Date();
  d.setHours(0, 0, 0, 0);
  return d.getTime() / 1000 - daysAgo * 86400;
}

/// A game's matches as {t, won, title, detail}, newest first. Returns null
/// while loading and a string when there is something to set up first.
function todayFeed(game) {
  if (game === "dota") {
    if (dotaLinked()) {
      if (!dHist.data) return dHist.error ? [] : null;
      return dHist.data.matches.map((m) => ({ t: m.startTime + m.durationSeconds, won: m.won, title: m.heroName, detail: `${m.kills}/${m.deaths}/${m.assists}` }));
    }
    if (!S.history.length) return "Connect Steam to see your matches, or play one with TheTracker running.";
    return S.history
      .map((m) => ({ t: Date.parse(m.date) / 1000, won: m.won === true ? true : m.won === false ? false : null, title: heroName(m.heroName), detail: `${m.kills} kills, ${m.totalDeaths} deaths` }))
      .sort((a, b) => b.t - a.t);
  }
  if (game === "deadlock") {
    if (!dlLinked()) return "Connect Steam to see your Deadlock matches.";
    if (!dlOverview.data) return dlOverview.error ? [] : null;
    return dlOverview.data.matches.map((m) => ({ t: m.startTime + m.durationSeconds, won: m.outcome === "win" ? true : m.outcome === "loss" ? false : null, title: m.heroName, detail: `${m.kills}/${m.deaths}/${m.assists}` }));
  }
  if (game === "pubg") {
    if (!pgLinked()) return "Connect your PUBG account to see your matches.";
    if (!pgOverview.data) return pgOverview.error ? [] : null;
    return pgOverview.data.matches.filter((m) => !m.custom).map((m) => ({ t: m.startTime + m.duration, won: null, place: m.place, top10: m.place <= 10, title: `${m.map} #${m.place}/${m.teams}`, detail: `${m.kills} kills, ${Math.round(m.damage)} damage` }));
  }
  if (game === "cs2") {
    return [...CS.history].reverse().map((m) => ({ t: Date.parse(m.date) / 1000, won: csWon(m), title: `${csMapName(m.map)} ${m.myScore} : ${m.theirScore}`, detail: `${m.kills}/${m.deaths}/${m.assists}` }));
  }
  return [];
}

function todayRecord(rows, since) {
  const rs = rows.filter((r) => r.t >= since);
  const won = rs.filter((r) => r.won === true).length, lost = rs.filter((r) => r.won === false).length;
  return { games: rs.length, won, lost, rate: won + lost ? (won * 100) / (won + lost) : null };
}

const todayWL = (r) => (r.games ? `<span class="win">${r.won}W</span> <span class="loss">${r.lost}L</span>` : `<span class="muted">–</span>`);

/// Today's and this week's totals for one game, or null when unknown.
function todayTotals(game) {
  if (game === "overwatch") {
    const p = owLinked() && owProgressAll.data;
    if (!p || !p.since) return null;
    const sum = (since) => p.sessions.filter((s) => s.to >= since).reduce((a, s) => ({ games: a.games + s.games, won: a.won + s.won, lost: a.lost + s.lost }), { games: 0, won: 0, lost: 0 });
    const rate = (r) => ({ ...r, rate: r.won + r.lost ? (r.won * 100) / (r.won + r.lost) : null });
    return { today: rate(sum(dayStart())), week: rate(sum(dayStart(6))) };
  }
  const rows = todayFeed(game);
  if (!Array.isArray(rows)) return null;
  return { today: todayRecord(rows, dayStart()), week: todayRecord(rows, dayStart(6)) };
}

function todayCardBody(game) {
  if (game === "pubg") {
    const rows = todayFeed(game);
    if (typeof rows === "string") return `<p class="muted">${rows}</p>`;
    if (rows === null) return `<p class="muted">Loading…</p>`;
    if (!rows.length) return `<p class="muted">No matches in the last 14 days.</p>`;
    const span = (since) => {
      const rs = rows.filter((r) => r.t >= since);
      return { n: rs.length, dinners: rs.filter((r) => r.place === 1).length, top10: rs.filter((r) => r.top10).length, best: rs.length ? Math.min(...rs.map((r) => r.place)) : null };
    };
    const day = span(dayStart()), week = span(dayStart(6)), last = rows[0];
    const line = (s) => (s.n ? `${s.n} match${s.n === 1 ? "" : "es"}` : `<span class="muted">–</span>`);
    return `<div class="today-nums">
        <div><span class="muted">Today</span><b class="display">${line(day)}</b><span class="muted">${day.n ? `best #${day.best}` : ""}</span></div>
        <div><span class="muted">Last 7 days</span><b class="display">${line(week)}</b><span class="muted">${week.n ? `${week.dinners} dinner${week.dinners === 1 ? "" : "s"}, ${week.top10} top 10` : ""}</span></div>
      </div>
      <p class="today-last">Last: <b>${esc(last.title)}</b> ${esc(last.detail)}, ${ago(last.t)}</p>`;
  }
  if (game === "overwatch") {
    if (!owLinked()) return `<p class="muted">Connect your Battle.net profile to follow your Overwatch career.</p>`;
    const p = owProgressAll.data;
    if (!p) return `<p class="muted">Loading…</p>`;
    if (!p.since) return `<p class="muted">Progress tracking starts the first time your profile is read. Open Overwatch once to begin.</p>`;
    const t = todayTotals(game), last = p.sessions[0];
    return `<div class="today-nums">
        <div><span class="muted">Today</span><b class="display">${todayWL(t.today)}</b></div>
        <div><span class="muted">Last 7 days</span><b class="display">${todayWL(t.week)}</b><span class="muted">${t.week.games ? pct(t.week.rate) + " won" : ""}</span></div>
      </div>
      <p class="today-last">${last ? `Last session ${ago(last.to)}: ${last.games} game${last.games === 1 ? "" : "s"}, ${last.won} won${last.heroes.length ? `, mostly ${esc(last.heroes[0].name)}` : ""}` : `<span class="muted">No games since tracking began ${ago(p.since)}.</span>`}</p>`;
  }
  const rows = todayFeed(game);
  if (rows === null) return `<p class="muted">Loading…</p>`;
  if (typeof rows === "string") return `<p class="muted">${rows}</p>`;
  if (!rows.length) return `<p class="muted">${game === "cs2" ? "No matches recorded yet. Keep TheTracker running while you play." : "No matches found."}</p>`;
  const t = todayTotals(game), streak = streakOf(rows.map((r) => r.won)), last = rows[0];
  let goals = "";
  if (game === "dota") {
    const g = S.boot.prefs.goals, mine = S.history.filter((m) => Date.parse(m.date) / 1000 >= dayStart());
    const parts = [];
    if (g.maxDeaths && mine.length) parts.push(`${mine.filter((m) => m.totalDeaths <= g.maxDeaths).length} of ${mine.length} within ${g.maxDeaths} deaths`);
    if (g.minGpm && mine.some((m) => known(m.gpm))) parts.push(`${mine.filter((m) => known(m.gpm) && m.gpm >= g.minGpm).length} of ${mine.filter((m) => known(m.gpm)).length} at ${g.minGpm}+ GPM`);
    if (parts.length) goals = `<p class="today-last muted">Your goals today: ${parts.join(", ")}.</p>`;
  }
  return `<div class="today-nums">
      <div><span class="muted">Today</span><b class="display">${todayWL(t.today)}</b></div>
      <div><span class="muted">Last 7 days</span><b class="display">${todayWL(t.week)}</b><span class="muted">${t.week.won + t.week.lost ? pct(t.week.rate) + " won" : ""}</span></div>
      <div><span class="muted">Streak</span><b class="display ${streak ? (streak.won ? "win" : "loss") : ""}">${streak ? `${streak.n} ${streak.won ? "win" : "loss"}${streak.n === 1 ? "" : streak.won ? "s" : "es"}` : "–"}</b></div>
    </div>
    ${formStrip(rows.map((r) => r.won), 24)}
    <p class="today-last">Last: <b>${esc(last.title || "Match")}</b> ${esc(last.detail)}, ${last.won === true ? `<span class="win">won</span>` : last.won === false ? `<span class="loss">lost</span>` : "no result"}, ${ago(last.t)}</p>${goals}`;
}

view("today", {
  game: null, title: "Today", icon: "today",
  sub: () => new Date().toLocaleDateString(undefined, { weekday: "long", month: "long", day: "numeric" }),
  load(force) {
    const g = S.boot.prefs.games;
    if (g.dota && dotaLinked()) dHist.load(force);
    if (g.deadlock && dlLinked()) dlOverview.load(force);
    if (g.cs2) csLoad();
    if (g.overwatch && owLinked()) owOverview.load(force).then(() => owProgressAll.load(true));
    if (g.pubg && pgLinked()) pgOverview.load(force);
    // What to work on: built from the last ten matches, cached for a while.
    if (g.dota && dotaLinked()) workOnRes.dota.load();
    if (g.deadlock && dlLinked()) workOnRes.deadlock.load();
  },
  render() {
    const games = enabledGames();
    const totals = games.map(todayTotals).filter(Boolean);
    const sum = (k) => totals.reduce((a, t) => ({ games: a.games + t[k].games, won: a.won + t[k].won, lost: a.lost + t[k].lost }), { games: 0, won: 0, lost: 0 });
    const day = sum("today"), week = sum("week");
    const rate = (r) => (r.won + r.lost ? (r.won * 100) / (r.won + r.lost) : null);
    const liveNow = S.live && S.live.live && games.includes("dota") ? ["Dota 2", "overview"] : CS.status && CS.status.live && games.includes("cs2") ? ["Counter-Strike 2", "cs-live"] : null;

    return `${liveNow ? `<button class="since" data-act="go" data-view="${liveNow[1]}" type="button"><span class="live-dot"></span><span class="since-main display">A ${liveNow[0]} match is running</span><span class="grow"></span><span class="link">Watch it live</span></button>` : ""}
      <div id="netSlot" class="net-slot">${typeof netBlockHtml === "function" ? netBlockHtml() : ""}</div>
      <div class="row"><span class="grow"></span><button class="btn ghost small share-btn" data-act="share" data-kind="today" type="button">Share my week</button></div>
      ${statRow([
        { label: "Today", value: day.games ? `${day.games} game${day.games === 1 ? "" : "s"}` : "No games yet", sub: day.games ? `${day.won} won, ${day.lost} lost` : "Across every game you track" },
        { label: "Won today", value: pct(rate(day)), tone: toneOfRate(rate(day)) },
        { label: "Last 7 days", value: `${week.games} game${week.games === 1 ? "" : "s"}`, sub: `${week.won} won, ${week.lost} lost` },
        { label: "Won this week", value: pct(rate(week)), tone: toneOfRate(rate(week)) },
      ])}
      ${["dota", "deadlock"].filter((g) => games.includes(g) && workOnRes[g].data && workOnRes[g].data.items.length).map((g) => `<section class="today-workon">
        <div class="sec-head"><h3>To work on in ${GAME_NAMES[g]}</h3><button class="link" data-act="go" data-view="${g === "dota" ? "dota-review" : "dl-review"}" type="button">Review</button></div>
        ${workOnHtml(g, { compact: true })}</section>`).join("")}
      <div class="today-grid">${games.map((g) => `<section class="today-card" data-theme="${g}">
        <div class="sec-head"><h3>${GAME_NAMES[g]}</h3><button class="link" data-act="game" data-game="${g}" type="button">Open</button></div>
        ${todayCardBody(g)}</section>`).join("")}</div>
      <details class="about"><summary>About this data</summary><p>Dota 2 and Deadlock come from your match history, CS2 from the matches recorded here, and Overwatch from the changes seen on your profile, so its games can show up a little late.</p></details>`;
  },
});
