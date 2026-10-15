// Review: what went wrong in a match, what went well, and what keeps going
// wrong across recent matches, for Dota 2, Deadlock, PUBG and Call of Duty. Built from public
// match data and what TheTracker recorded live.

// The match picked on each game's Review tab (0: the latest).
const REV = { dota: 0, deadlock: 0, pubg: "", cod: "" };

const reviewRes = {
  dota: resource("dotaReview", "review", { args: () => ({ game: "dota", matchId: REV.dota }), ttl: 10 * 60000 }),
  deadlock: resource("dlReview", "review", { args: () => ({ game: "deadlock", matchId: REV.deadlock }), ttl: 10 * 60000 }),
  pubg: resource("pgReview", "review", { args: () => ({ game: "pubg", matchKey: REV.pubg }), ttl: 10 * 60000 }),
  cod: resource("codReview", "review", { args: () => ({ game: "cod", matchKey: REV.cod }), ttl: 60000 }),
};
const coachRes = {
  dota: resource("dotaCoach", "coach", { args: () => ({ game: "dota", matchId: REV.dota }), ttl: 6 * 3600000 }),
  deadlock: resource("dlCoach", "coach", { args: () => ({ game: "deadlock", matchId: REV.deadlock }), ttl: 6 * 3600000 }),
  pubg: resource("pgCoach", "coach", { args: () => ({ game: "pubg", matchKey: REV.pubg }), ttl: 6 * 3600000 }),
  cod: resource("codCoach", "coach", { args: () => ({ game: "cod", matchKey: REV.cod }), ttl: 6 * 3600000 }),
};
const workOnRes = {
  dota: resource("dotaWorkOn", "work_on", { args: () => ({ game: "dota" }), ttl: 30 * 60000 }),
  deadlock: resource("dlWorkOn", "work_on", { args: () => ({ game: "deadlock" }), ttl: 30 * 60000 }),
  pubg: resource("pgWorkOn", "work_on", { args: () => ({ game: "pubg" }), ttl: 30 * 60000 }),
  cod: resource("codWorkOn", "work_on", { args: () => ({ game: "cod" }), ttl: 60000 }),
};

onChange("rev-match", (el) => {
  REV[el.dataset.game] = el.dataset.game === "pubg" || el.dataset.game === "cod" ? (el.value === "0" ? "" : el.value) : Number(el.value);
  reviewRes[el.dataset.game].load();
  if (S.boot.coach) coachRes[el.dataset.game].load();
  rerender();
});
act("coach-again", (el) => coachRes[el.dataset.game].load(true));

/// The AI coach's write-up: its verdict, the habits to work on with a
/// drill each, and what to keep. Death notes go into the death table.
function coachHtml(game) {
  if (!S.boot.coach) return "";
  const c = coachRes[game];
  if (c.error) return `<section class="panel coach"><div class="sec-head"><h3>AI coach</h3></div><div class="note err">${esc(c.error)}</div></section>`;
  if (!c.data) return `<section class="panel coach"><div class="sec-head"><h3>AI coach</h3></div>${loadingState("Reading the match…")}</section>`;
  const d = c.data;
  if (d.status === "off" || d.status === "locked") return "";
  if (d.status === "not_set_up") return `<section class="panel coach"><div class="sec-head"><h3>AI coach</h3></div>
      <p class="muted">${d.error ? esc(d.error) + " " : ""}The coach writes through Claude Code on this PC, with your Claude plan. Open a terminal, run <code>claude</code>, sign in with <code>/login</code>, then try again.</p>
      <div class="row"><button class="btn ghost small" data-act="coach-again" data-game="${game}" type="button">Try again</button></div></section>`;
  if (d.status !== "ready" || !d.note) return `<section class="panel coach"><div class="sec-head"><h3>AI coach</h3></div><div class="note err">${esc(d.error || "No answer.")}</div>
      <div class="row"><button class="btn ghost small" data-act="coach-again" data-game="${game}" type="button">Ask again</button></div></section>`;
  const n = d.note;
  return `<section class="panel coach">
    <div class="sec-head"><h3>AI coach</h3><span class="muted small">${esc(n.model || "")}</span></div>
    <p class="coach-verdict">${esc(n.verdict)}</p>
    ${n.focus && n.focus.length ? `<div class="work-on">${n.focus.map((f) => `<div class="work-item"><b>${esc(f.title)}</b><p class="muted">${esc(f.why)}</p><div class="rev-habit"><span class="tag">Drill</span> ${esc(f.drill)}</div></div>`).join("")}</div>` : ""}
    ${n.praise ? `<p class="coach-praise"><span class="check-dot ok"></span> ${esc(n.praise)}</p>` : ""}
  </section>`;
}

function coachDeathNote(game, clock) {
  const c = coachRes[game];
  const n = c.data && c.data.status === "ready" && c.data.note;
  if (!n || !n.deaths) return "";
  const hit = n.deaths.find((d) => d.clock === clock);
  return hit ? hit.note : "";
}

/// The matches to choose from: the game's own match list.
function reviewChoices(game) {
  if (game === "cod") {
    return ((codOverview.data && codOverview.data.matches) || []).slice(0, 40)
      .map((m) => ({ id: m.id, label: `${m.mode} · ${m.place ? "#" + m.place : m.result} · ${m.kills}/${m.deaths} · ${ago(m.date)}` }));
  }
  if (game === "pubg") {
    return ((pgOverview.data && pgOverview.data.matches) || []).filter((m) => !m.custom).slice(0, 25)
      .map((m) => ({ id: m.id, label: `${m.map} · ${m.modeName} · #${m.place} · ${ago(m.startTime)}` }));
  }
  if (game === "dota") {
    return ((dHist.data && dHist.data.matches) || []).filter((m) => !m.abandoned).slice(0, 25)
      .map((m) => ({ id: m.matchId, label: `${m.heroName} · ${m.won ? "won" : "lost"} · ${ago(m.startTime)}` }));
  }
  return ((dlOverview.data && dlOverview.data.matches) || []).filter((m) => !m.abandoned).slice(0, 25)
    .map((m) => ({ id: m.matchId, label: `${m.heroName} · ${m.outcome === "win" ? "won" : "lost"} · ${ago(m.startTime)}` }));
}

function pointHtml(p, tone) {
  return `<div class="rev-point ${tone}">
    <div class="rev-title"><span class="check-dot ${tone === "bad" ? "bad" : "ok"}"></span><b>${esc(p.title)}</b></div>
    <div class="muted">${esc(p.detail)}</div>
    ${p.habit ? `<div class="rev-habit"><span class="tag">Work on</span> ${esc(p.habit)}</div>` : ""}
  </div>`;
}

function reviewHtml(game) {
  const r = reviewRes[game];
  const choices = reviewChoices(game);
  const picker = choices.length
    ? `<select class="input rev-pick" data-change="rev-match" data-game="${game}">
        <option value="0" ${REV[game] === 0 ? "selected" : ""}>Latest match</option>
        ${choices.map((c) => `<option value="${c.id}" ${REV[game] === c.id ? "selected" : ""}>${esc(c.label)}</option>`).join("")}
      </select>`
    : "";
  const wait = gate(r, "Reviewing the match…");
  if (wait) return `<div class="row">${picker}</div>${wait}`;
  const m = r.data;
  const img = game === "dota" ? heroImgHtml(m.heroSlug, "portrait big") : game === "pubg" || game === "cod" ? "" : imgHtml(m.heroImage, "portrait big");
  const result = game === "pubg" ? (m.won ? `<span class="res win">Dinner</span>` : "") : m.won === true ? `<span class="res win">Won</span>` : m.won === false ? `<span class="res loss">Lost</span>` : "";
  return `<div class="row rev-top">${picker}<span class="grow"></span>
      ${game === "dota" ? `<button class="link" data-act="open-url" data-url="https://www.opendota.com/matches/${m.matchId}" type="button">Open on OpenDota</button>` : ""}</div>
    <section class="panel rev-head">
      <div class="live-head">${img}
        <div class="grow"><h2 class="display">${esc(m.hero)} ${result}</h2>
          <p class="muted">${game === "pubg" ? `${m.kills} kill${m.kills === 1 ? "" : "s"}, ${m.assists} assist${m.assists === 1 ? "" : "s"}` : `${m.kills} / ${m.deaths} / ${m.assists}`}${m.duration ? ` · ${fmtClock(m.duration)}` : ""} · ${ago(m.startTime)}</p></div>
      </div>
      <p class="rev-verdict">${esc(m.verdict)}</p>
      ${m.facts && m.facts.length ? `<p class="muted small rev-facts">${m.facts.map(esc).join(" · ")}</p>` : ""}
    </section>
    ${coachHtml(game)}
    <div class="cols">
      <section>
        <div class="sec-head"><h3>What went wrong</h3></div>
        ${m.wrong.length ? m.wrong.map((p) => pointHtml(p, "bad")).join("") : `<p class="muted">Nothing stood out as a mistake in this match.</p>`}
      </section>
      <section>
        <div class="sec-head"><h3>What went well</h3></div>
        ${m.good.length ? m.good.map((p) => pointHtml(p, "good")).join("") : `<p class="muted">Nothing stood out above your usual.</p>`}
        <div class="sec-head"><h3>Deaths</h3></div>
        ${m.deathLog.length
          ? `<table class="mini">${m.deathLog.map((d) => {
              const coach = coachDeathNote(game, d.clock);
              const where = [d.fight, d.region ? `in ${d.region}` : "", d.note].filter(Boolean).join(", ");
              return `<tr><td class="num">${esc(d.clock)}</td><td>${d.killer ? `<b>${esc(d.killer)}</b>` : ""}${where ? ` <span class="muted">${esc(where)}</span>` : ""}</td>
              <td class="num loss">${d.goldLost ? `-${fmtNum(d.goldLost)}` : ""}</td></tr>${coach ? `<tr class="why"><td colspan="3"><span class="coach-note">${esc(coach)}</span></td></tr>` : ""}`;
            }).join("")}</table>`
          : `<p class="muted">${game === "cod" ? "Call of Duty doesn't share death details with apps." : m.deaths ? "No death details for this match." : "No deaths."}</p>`}
      </section>
    </div>
    ${m.goals ? `<section class="rev-goals">${roleGoalsHtml(m.goals)}</section>` : ""}
    ${m.notes.length ? `<div class="note">${m.notes.map(esc).join("<br>")}</div>` : ""}
    ${workOnHtml(game)}`;
}

function workOnHtml(game, { compact = false } = {}) {
  const w = workOnRes[game];
  if (!w.data) {
    if (compact) return "";
    return `<div class="sec-head"><h3>What to work on</h3></div>${w.error ? errorState(w.error) : loadingState("Looking through your last 10 matches…")}`;
  }
  const d = w.data;
  if (compact) {
    if (!d.items.length) return "";
    return `<div class="lines">${d.items.slice(0, 3).map((it) => `<div class="line static"><span class="grow"><b>${esc(it.title)}</b><span class="muted"> ${it.count} of ${it.of} matches</span><br><span class="muted small">${esc(it.habit)}</span></span></div>`).join("")}</div>`;
  }
  return `<div class="sec-head"><h3>What to work on</h3><span class="muted">over your last ${d.matches} matches</span></div>
    ${d.items.length
      ? `<div class="work-on">${d.items.map((it) => `<div class="work-item">
          <div class="row"><b class="grow">${esc(it.title)}</b><span class="num">${it.count} of ${it.of}</span></div>
          <div class="rev-habit"><span class="tag">Practise</span> ${esc(it.habit)}</div>
          ${it.series && it.series.length >= 3 ? sparkline(it.series, { height: 32, width: 200 }) : ""}
        </div>`).join("")}</div>`
      : `<p class="muted">No mistake keeps coming back across your recent matches.</p>`}
    ${d.strengths.length ? `<p class="hint">Keep doing: ${d.strengths.map((s) => `<b>${esc(s.title.toLowerCase())}</b> (${s.count} of ${s.of})`).join(", ")}.</p>` : ""}`;
}

view("dota-review", {
  game: "dota", nav: true, tabOf: "matches", icon: "matches", title: "Review",
  sub: () => "What went wrong, what went well, and what to work on",
  load(force) {
    if (!dotaLinked()) return loadSteamAccounts();
    ensureHeroList();
    dHist.load();
    workOnRes.dota.load(force);
    if (S.boot.coach) coachRes.dota.load(force);
    return reviewRes.dota.load(force);
  },
  render() {
    if (!dotaLinked()) return linkPanelHtml("dota");
    return reviewHtml("dota");
  },
});

view("pg-review", {
  game: "pubg", nav: true, tabOf: "pg-matches", icon: "matches", title: "Review",
  sub: () => "What went wrong, what went well, and what to work on",
  load(force) {
    if (!pgLinked()) return Promise.resolve();
    workOnRes.pubg.load(force);
    if (S.boot.coach) coachRes.pubg.load(force);
    return pgOverview.load().then(() => reviewRes.pubg.load(force));
  },
  render() {
    const wait = pgGate();
    if (wait) return wait;
    return `${reviewHtml("pubg")}
      <p class="hint">PUBG has no live feed for apps: this review is built from the match's telemetry, PUBG's event log of every match, which arrives a few minutes after it ends.</p>`;
  },
});

view("cod-review", {
  game: "cod", nav: true, tabOf: "cod-matches", icon: "matches", title: "Review",
  sub: () => "What went wrong, what went well, and what to work on",
  load(force) {
    workOnRes.cod.load(force);
    if (S.boot.coach) coachRes.cod.load(force);
    return codOverview.load().then(() => reviewRes.cod.load(force));
  },
  render() {
    const ov = codOverview.data;
    if (ov && !ov.matches.length) return emptyState("Nothing to review yet", "Log a match first: the review compares it with your other logged matches.", `<button class="btn" data-act="cod-log" type="button">Log a match</button>`);
    return `${reviewHtml("cod")}
      <p class="hint">Call of Duty has no match data for apps: this review compares the match you logged with your own log and play sessions.</p>`;
  },
});

view("dl-review", {
  game: "deadlock", nav: true, tabOf: "dl-matches", icon: "matches", title: "Review",
  sub: () => "What went wrong, what went well, and what to work on",
  load(force) {
    dlOverview.load();
    workOnRes.deadlock.load(force);
    if (S.boot.coach) coachRes.deadlock.load(force);
    return reviewRes.deadlock.load(force);
  },
  render() {
    if (!dlLinked()) return linkPanelHtml("deadlock");
    return `${reviewHtml("deadlock")}
      <p class="hint">Deadlock has no live feed for apps, so there's no in-game overlay for it: this review is built from the match data after the game.</p>`;
  },
});
