// Review: what went wrong in a match, what went well, and what keeps going
// wrong across recent matches, for Dota 2 and Deadlock. Built from public
// match data and what TheTracker recorded live.

// The match picked on each game's Review tab (0: the latest).
const REV = { dota: 0, deadlock: 0 };

const reviewRes = {
  dota: resource("dotaReview", "review", { args: () => ({ game: "dota", matchId: REV.dota }), ttl: 10 * 60000 }),
  deadlock: resource("dlReview", "review", { args: () => ({ game: "deadlock", matchId: REV.deadlock }), ttl: 10 * 60000 }),
};
const workOnRes = {
  dota: resource("dotaWorkOn", "work_on", { args: () => ({ game: "dota" }), ttl: 30 * 60000 }),
  deadlock: resource("dlWorkOn", "work_on", { args: () => ({ game: "deadlock" }), ttl: 30 * 60000 }),
};

onChange("rev-match", (el) => {
  REV[el.dataset.game] = Number(el.value);
  reviewRes[el.dataset.game].load();
  rerender();
});

/// The matches to choose from: the game's own match list.
function reviewChoices(game) {
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
  const img = game === "dota" ? heroImgHtml(m.heroSlug, "portrait big") : imgHtml(m.heroImage, "portrait big");
  const result = m.won === true ? `<span class="res win">Won</span>` : m.won === false ? `<span class="res loss">Lost</span>` : "";
  return `<div class="row rev-top">${picker}<span class="grow"></span>
      ${game === "dota" ? `<button class="link" data-act="open-url" data-url="https://www.opendota.com/matches/${m.matchId}" type="button">Open on OpenDota</button>` : ""}</div>
    <section class="panel rev-head">
      <div class="live-head">${img}
        <div class="grow"><h2 class="display">${esc(m.hero)} ${result}</h2>
          <p class="muted">${m.kills} / ${m.deaths} / ${m.assists} · ${fmtClock(m.duration)} · ${ago(m.startTime)}</p></div>
      </div>
      <p class="rev-verdict">${esc(m.verdict)}</p>
    </section>
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
          ? `<table class="mini">${m.deathLog.map((d) => `<tr><td>${esc(d.clock)}</td><td>${d.killer ? `by ${esc(d.killer)}` : ""}${d.note ? ` <span class="muted">· ${esc(d.note)}</span>` : ""}</td>
              <td class="num loss">${d.goldLost ? `-${fmtNum(d.goldLost)}` : ""}</td></tr>${(d.reasons || []).length ? `<tr class="why"><td colspan="3">${d.reasons.map(esc).join("<br>")}</td></tr>` : ""}`).join("")}</table>`
          : `<p class="muted">${m.deaths ? "No death details for this match." : "No deaths."}</p>`}
      </section>
    </div>
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
    return reviewRes.dota.load(force);
  },
  render() {
    if (!dotaLinked()) return linkPanelHtml("dota");
    return reviewHtml("dota");
  },
});

view("dl-review", {
  game: "deadlock", nav: true, tabOf: "dl-matches", icon: "matches", title: "Review",
  sub: () => "What went wrong, what went well, and what to work on",
  load(force) {
    dlOverview.load();
    workOnRes.deadlock.load(force);
    return reviewRes.deadlock.load(force);
  },
  render() {
    if (!dlLinked()) return linkPanelHtml("deadlock");
    return `${reviewHtml("deadlock")}
      <p class="hint">Deadlock has no live feed for apps, so there's no in-game overlay for it: this review is built from the match data after the game.</p>`;
  },
});
