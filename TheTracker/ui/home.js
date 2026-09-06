// Account overviews — one per game, each the landing page for its tab.
//
// Design notes, since the first attempt got this wrong:
//
// The earlier version put five metrics in one rail at equal weight, each
// with a sparkline too small to actually read, under a heading that
// repeated the player's name and the game they were already looking at.
// Everything competed, nothing led, and the page dead-ended into blank
// space.
//
// This version commits to a hierarchy. Win rate is the one number people
// open a tracker for, so it gets the headline slot and a real meter.
// Three supporting figures sit below it in a calm row. Then recent form,
// then hero performance, then a short list of recent matches so the page
// has somewhere to go rather than stopping. Section rules do the
// separating; nothing is boxed in a card.

/// Tiny inline sparkline. Only used where it earns its space — a trend line
/// under a single headline number, not decoration on every figure.
function sparkline(values, opts = {}) {
  if (!values || values.length < 3) return "";
  const w = opts.width || 180;
  const h = opts.height || 34;
  const pad = 2;
  const min = Math.min(...values);
  const max = Math.max(...values);
  const span = max - min || 1;
  const step = (w - pad * 2) / (values.length - 1);

  const pts = values.map((v, i) => [pad + i * step, h - pad - ((v - min) / span) * (h - pad * 2)]);
  const line = pts.map(([x, y], i) => `${i ? "L" : "M"}${x.toFixed(1)},${y.toFixed(1)}`).join("");
  const area = `${line}L${pts[pts.length - 1][0].toFixed(1)},${h}L${pts[0][0].toFixed(1)},${h}Z`;
  const stroke = opts.color || "var(--brand)";

  return `
    <svg class="spark" viewBox="0 0 ${w} ${h}" width="${w}" height="${h}" aria-hidden="true" preserveAspectRatio="none">
      <path d="${area}" fill="${stroke}" opacity="0.1" />
      <path d="${line}" fill="none" stroke="${stroke}" stroke-width="1.5"
            stroke-linecap="round" stroke-linejoin="round" />
    </svg>`;
}

/// A row of figures separated by rules rather than boxed into cards.
///
/// The overview no longer uses this — it needed a stronger hierarchy than a
/// flat row of equals — but Profile, Heroes and the Deadlock views all still
/// want exactly this shape, so it stays here as a shared component.
function railHtml(entries) {
  return `<div class="rail">${entries
    .map(
      (e) => `
      <div class="rail-item">
        <div class="rail-label">${e.label}</div>
        <div class="rail-value ${e.tone || ""}">${e.value}</div>
        ${e.sub ? `<div class="rail-sub">${e.sub}</div>` : ""}
        ${e.spark || ""}
      </div>`
    )
    .join("")}</div>`;
}

function streakOf(matches) {
  if (!matches.length) return null;
  const won = matches[0].won;
  let n = 0;
  for (const m of matches) {
    if (m.won === won) n++;
    else break;
  }
  return { won, n };
}

/// Games since local midnight — the figure people check between matches.
function todayRecord(matches, getTime, getWon) {
  const midnight = new Date();
  midnight.setHours(0, 0, 0, 0);
  const cutoff = midnight.getTime() / 1000;
  const today = matches.filter((m) => getTime(m) >= cutoff);
  const wins = today.filter(getWon).length;
  return { played: today.length, wins, losses: today.length - wins };
}

/// The headline block: one big number, a meter, and an optional trend.
function headlineHtml({ value, tone, caption, meter, trend }) {
  return `
    <div class="headline">
      <div class="headline-main">
        <div class="headline-value ${tone}">${value}</div>
        <div class="headline-caption">${caption}</div>
        ${meter !== undefined ? `<div class="headline-meter"><div class="headline-meter-fill ${tone}" style="width:${Math.min(100, meter)}%"></div></div>` : ""}
      </div>
      ${trend ? `<div class="headline-trend"><span class="trend-label">Trend, oldest to newest</span>${trend}</div>` : ""}
    </div>`;
}

/// Supporting figures. Three at most — beyond that nothing stands out.
function statsRowHtml(entries) {
  return `<div class="figures">${entries
    .map(
      (e) => `
      <div class="figure">
        <div class="figure-value ${e.tone || ""}">${e.value}</div>
        <div class="figure-label">${e.label}</div>
        ${e.sub ? `<div class="figure-sub">${e.sub}</div>` : ""}
      </div>`
    )
    .join("")}</div>`;
}

/// Form strip with a legend, sized to actually be read.
function formSectionHtml(matches, limit = 15) {
  const shown = matches.slice(0, limit);
  if (!shown.length) return "";
  const wins = shown.filter((m) => m.won).length;
  return `
    <section class="ov-section">
      <div class="ov-section-head">
        <h2 class="ov-section-title">Recent form</h2>
        <span class="ov-section-note">${wins}W &ndash; ${shown.length - wins}L over the last ${shown.length}, newest first</span>
      </div>
      <div class="form-track">
        ${shown
          .map(
            (m) =>
              `<span class="form-cell ${m.won ? "win" : "loss"}" title="${escapeHtml(m.heroName)} — ${m.won ? "Win" : "Loss"}"></span>`
          )
          .join("")}
      </div>
    </section>`;
}

function heroCompareHtml(best, worst, portraitFor) {
  if (!best) return "";
  const card = (h, label, tone) => `
    <div class="hero-compare">
      <div class="hero-compare-label">${label}</div>
      <div class="hero-compare-row">
        ${portraitFor(h)}
        <div class="grow">
          <div class="hero-compare-name">${escapeHtml(h.name)}</div>
          <div class="hero-compare-meta">${h.wins}W &ndash; ${h.losses}L over ${h.played}</div>
        </div>
        <div class="hero-compare-rate ${tone}">${h.winRate.toFixed(0)}%</div>
      </div>
    </div>`;

  return `
    <section class="ov-section">
      <div class="ov-section-head"><h2 class="ov-section-title">Hero performance</h2></div>
      <div class="hero-compare-grid">
        ${card(best, "Strongest", "win")}
        ${worst && worst.name !== best.name ? card(worst, "Weakest", "loss") : ""}
      </div>
    </section>`;
}

// ---------- Dota ----------

function dotaOverviewBodyHtml() {
  if (!DOTA.link.accountId) {
    return `
      <div class="empty-state">
        <div class="empty-ico">${icon("accounts", 20)}</div>
        <div class="empty-title">No Steam account linked</div>
        <div class="empty-sub">
          Open <b>Match History</b> and press <b>Detect from Steam</b> to pull in
          your games. Everything on this page comes from that.
        </div>
      </div>`;
  }

  if (!DOTA.matches.length) {
    if (DOTA.loading) {
      return `
        <div class="skel-line lg skeleton"></div>
        <div class="skel-line sm skeleton"></div>
        <div class="skel-row skeleton"></div>
        <div class="skel-row skeleton"></div>`;
    }
    return `
      <div class="empty-state">
        <div class="empty-ico">${icon("matches", 20)}</div>
        <div class="empty-title">No matches found</div>
        <div class="empty-sub">
          Your Dota profile may be private. In Dota 2: Settings &rsaquo; Options
          &rsaquo; Advanced Options &rsaquo; <b>Expose Public Match Data</b>.
        </div>
      </div>`;
  }

  const s = DOTA.summary || {};
  const recent = DOTA.matches;
  const streak = streakOf(recent);
  const today = todayRecord(recent, (m) => m.startTime, (m) => m.won);
  const wr = s.winRate ?? 0;

  // Rolling win rate reads as a trend; a raw W/L sequence is just noise.
  const chrono = [...recent].reverse();
  const winTrend = chrono.map((_, i) => {
    const window = chrono.slice(Math.max(0, i - 9), i + 1);
    return (window.filter((m) => m.won).length / window.length) * 100;
  });

  const heroes = dtHeroAggregate(recent);
  const ranked = heroes.filter((h) => h.played >= 2);
  const best = [...ranked].sort((a, b) => b.winRate - a.winRate)[0];
  const worst = [...ranked].sort((a, b) => a.winRate - b.winRate)[0];

  const portrait = (h) =>
    `<img class="hero-compare-img" src="${DOTA_HERO_CDN}${h.slug}.png" alt="" />`;

  return `
    ${headlineHtml({
      value: `${wr.toFixed(0)}%`,
      tone: wr >= 50 ? "win" : "loss",
      caption: `Win rate &middot; ${s.wins ?? 0}W &ndash; ${s.losses ?? 0}L across ${recent.length} matches`,
      meter: wr,
      trend: sparkline(winTrend, { color: wr >= 50 ? "var(--win)" : "var(--loss)" }),
    })}

    ${statsRowHtml([
      { label: "Average KDA", value: (s.kda ?? 0).toFixed(2), sub: `${(s.kills / Math.max(1, s.matches)).toFixed(1)} / ${(s.deaths / Math.max(1, s.matches)).toFixed(1)} / ${(s.assists / Math.max(1, s.matches)).toFixed(1)}` },
      { label: "Gold per minute", value: s.avgGpm ?? 0, sub: `${s.avgXpm ?? 0} XPM` },
      {
        label: today.played ? "Today" : "No games today",
        value: today.played ? `${today.wins}&ndash;${today.losses}` : "&mdash;",
        tone: today.played ? (today.wins >= today.losses ? "win" : "loss") : "",
        sub: streak ? `${streak.n} ${streak.won ? "win" : "loss"}${streak.n === 1 ? "" : "es"} in a row` : "",
      },
    ])}

    ${formSectionHtml(recent)}
    ${heroCompareHtml(best, worst, portrait)}

    <section class="ov-section">
      <div class="ov-section-head">
        <h2 class="ov-section-title">Latest matches</h2>
        <button class="link-btn" data-goto="dotamatches">View all ${recent.length} &rsaquo;</button>
      </div>
      <div class="mini-matches">
        ${recent
          .slice(0, 5)
          .map(
            (m) => `
          <div class="mini-match ${m.won ? "win" : "loss"}" data-goto="dotamatches">
            <img src="${DOTA_HERO_CDN}${m.heroSlug}.png" alt="" />
            <div class="grow">
              <div class="mini-hero">${escapeHtml(m.heroName)}</div>
              <div class="mini-meta">${escapeHtml(m.modeName)} &middot; ${dtDuration(m.durationSeconds)}</div>
            </div>
            <div class="mini-kda">${m.kills} / ${m.deaths} / ${m.assists}</div>
            <div class="mini-result ${m.won ? "win" : "loss"}">${m.won ? "Win" : "Loss"}</div>
            <div class="mini-when">${dtAgo(m.startTime)}</div>
          </div>`
          )
          .join("")}
      </div>
    </section>`;
}

function renderDotaOverview() {
  const root = document.getElementById("tab-dotaoverview");
  if (!root) return;

  const name = DOTA.link.personaname || (state.profile && state.profile.username) || "Dota 2";
  const inMatch = state.live && state.live.current && !state.live.current.ended;

  root.innerHTML = `
    <header class="ov-hero">
      <div>
        <div class="ov-kicker"><span class="ov-game-dot"></span>Dota 2 profile <span class="ov-kicker-sep">/</span> Local account data</div>
        <h1 class="ov-name">${escapeHtml(name)}</h1>
        <div class="ov-sub">
          ${DOTA.link.accountId ? `Steam ${DOTA.link.accountId}` : "No account linked"}
        </div>
      </div>
      ${inMatch ? `<button class="live-banner" data-goto="live"><span class="live-pulse"></span> Match in progress</button>` : ""}
    </header>

    ${dotaOverviewBodyHtml()}`;

  root.querySelectorAll("[data-goto]").forEach((el) =>
    el.addEventListener("click", () => setView(el.dataset.goto))
  );
  const link = root.querySelector("[data-detect]");
  if (link) link.addEventListener("click", () => setView("dotamatches"));
}

// ---------- Deadlock ----------

function deadlockOverviewBodyHtml() {
  if (!DL.matches.length) {
    if (DL.loading) {
      return `
        <div class="skel-line lg skeleton"></div>
        <div class="skel-line sm skeleton"></div>
        <div class="skel-row skeleton"></div>`;
    }
    return `
      <div class="empty-state">
        <div class="empty-ico">${icon("matches", 20)}</div>
        <div class="empty-title">No matches found</div>
        <div class="empty-sub">
          Deadlock data comes from a community API that Valve has been rate
          limiting, so recent games can take a while to appear.
        </div>
      </div>`;
  }

  const s = DL.summary || {};
  const recent = DL.matches.map((m) => ({ ...m, won: m.outcome === "win" }));
  const scored = recent.filter((m) => m.outcome === "win" || m.outcome === "loss");
  const streak = streakOf(scored);
  const today = todayRecord(recent, (m) => m.startTime, (m) => m.won);
  const wr = s.winRate ?? 0;

  const chrono = [...scored].reverse();
  const winTrend = chrono.map((_, i) => {
    const window = chrono.slice(Math.max(0, i - 9), i + 1);
    return (window.filter((m) => m.won).length / window.length) * 100;
  });

  const heroes = dlHeroAggregate().filter((h) => h.played >= 2);
  const best = [...heroes].sort((a, b) => b.winRate - a.winRate)[0];
  const worst = [...heroes].sort((a, b) => a.winRate - b.winRate)[0];

  const portrait = (h) =>
    h.image
      ? `<img class="hero-compare-img square" src="${h.image}" alt="" />`
      : `<span class="hero-compare-img square"></span>`;

  return `
    ${headlineHtml({
      value: `${wr.toFixed(0)}%`,
      tone: wr >= 50 ? "win" : "loss",
      caption: `Win rate &middot; ${s.wins ?? 0}W &ndash; ${s.losses ?? 0}L across ${scored.length} scored matches`,
      meter: wr,
      trend: sparkline(winTrend, { color: wr >= 50 ? "var(--win)" : "var(--loss)" }),
    })}

    ${statsRowHtml([
      { label: "Average KDA", value: (s.kda ?? 0).toFixed(2), sub: "per match" },
      { label: "Average souls", value: dlFmtSouls(s.avgSouls ?? 0), sub: "net worth" },
      {
        label: today.played ? "Today" : "No games today",
        value: today.played ? `${today.wins}&ndash;${today.losses}` : "&mdash;",
        tone: today.played ? (today.wins >= today.losses ? "win" : "loss") : "",
        sub: streak ? `${streak.n} ${streak.won ? "win" : "loss"}${streak.n === 1 ? "" : "es"} in a row` : "",
      },
    ])}

    ${formSectionHtml(scored)}
    ${heroCompareHtml(best, worst, portrait)}

    <section class="ov-section">
      <div class="ov-section-head">
        <h2 class="ov-section-title">Latest matches</h2>
        <button class="link-btn" data-goto="dlmatches">View all ${recent.length} &rsaquo;</button>
      </div>
      <div class="mini-matches">
        ${recent
          .slice(0, 5)
          .map((m) => {
            const cls = m.outcome === "win" ? "win" : m.outcome === "loss" ? "loss" : "other";
            const label = m.outcome === "win" ? "Win" : m.outcome === "loss" ? "Loss" : "—";
            return `
          <div class="mini-match ${cls}" data-goto="dlmatches">
            ${m.heroImage ? `<img src="${m.heroImage}" alt="" />` : `<span class="mini-img"></span>`}
            <div class="grow">
              <div class="mini-hero">${escapeHtml(m.heroName)}</div>
              <div class="mini-meta">${dlFmtDuration(m.durationSeconds)}</div>
            </div>
            <div class="mini-kda">${m.kills} / ${m.deaths} / ${m.assists}</div>
            <div class="mini-result ${cls}">${label}</div>
            <div class="mini-when">${dlFmtWhen(m.startTime)}</div>
          </div>`;
          })
          .join("")}
      </div>
    </section>`;
}

function renderDeadlockOverview() {
  const root = document.getElementById("tab-dloverview");
  if (!root) return;

  // Linking lives on this page when there is no account yet.
  if (!DL.link.accountId) {
    root.innerHTML = dlNotLinkedHtml();
    dlWireNotLinked(root);
    return;
  }

  const name = DL.link.personaname || "Deadlock";

  root.innerHTML = `
    <header class="ov-hero">
      <div>
        <div class="ov-kicker"><span class="ov-game-dot"></span>Deadlock profile <span class="ov-kicker-sep">/</span> Local account data</div>
        <h1 class="ov-name">${escapeHtml(name)}</h1>
        <div class="ov-sub">
          ${DL.rank ? `${escapeHtml(DL.rank.label)} &middot; ` : ""}Steam ${DL.link.accountId}
        </div>
      </div>
      ${DL.live ? `<div class="live-banner"><span class="live-pulse"></span> In a match as <b>${escapeHtml(DL.live.heroName)}</b></div>` : ""}
    </header>

    ${deadlockOverviewBodyHtml()}`;

  root.querySelectorAll("[data-goto]").forEach((el) =>
    el.addEventListener("click", () => setView(el.dataset.goto))
  );
}

// ---------- Loaders ----------
//
// The Deadlock overview is loaded by dlRender, which every Deadlock view
// shares; Dota's is its own since its views load independently.

async function loadDotaOverview() {
  await dtRefreshLink();
  renderDotaOverview();
  if (DOTA.link.accountId) await dtLoad();
  renderDotaOverview();
}
