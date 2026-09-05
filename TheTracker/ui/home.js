// Account Overview — the landing page.
//
// One screen that answers "how am I doing?" for both games at once: rank,
// recent form, the headline rates, and what to fix. Deliberately built from
// data already loaded elsewhere so opening the app costs at most one request
// per linked game.
//
// Layout note: this page avoids the card-grid look on purpose. Numbers sit
// on a divided rail, trends are drawn as sparklines, and sections are
// separated by rules rather than boxed up — a stack of identical cards makes
// everything read as equally important, which is exactly wrong for a page
// whose job is to show what stands out.

/// Tiny inline sparkline. Values are plotted left-to-right, oldest first.
/// Returns "" for fewer than two points, since a single dot is not a trend.
function sparkline(values, opts = {}) {
  if (!values || values.length < 2) return "";
  const w = opts.width || 96;
  const h = opts.height || 26;
  const pad = 2;
  const min = Math.min(...values);
  const max = Math.max(...values);
  const span = max - min || 1;
  const step = (w - pad * 2) / (values.length - 1);

  const pts = values.map((v, i) => {
    const x = pad + i * step;
    const y = h - pad - ((v - min) / span) * (h - pad * 2);
    return [x, y];
  });

  const line = pts.map(([x, y], i) => `${i ? "L" : "M"}${x.toFixed(1)},${y.toFixed(1)}`).join("");
  const area = `${line}L${pts[pts.length - 1][0].toFixed(1)},${h}L${pts[0][0].toFixed(1)},${h}Z`;
  const rising = values[values.length - 1] >= values[0];
  const stroke = opts.color || (rising ? "var(--win)" : "var(--loss)");

  return `
    <svg class="spark" viewBox="0 0 ${w} ${h}" width="${w}" height="${h}" aria-hidden="true">
      <path d="${area}" fill="${stroke}" opacity="0.12" />
      <path d="${line}" fill="none" stroke="${stroke}" stroke-width="1.5"
            stroke-linecap="round" stroke-linejoin="round" />
    </svg>`;
}

/// A row of figures separated by rules instead of boxed into cards.
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

/// Games played since local midnight — the "how has today gone" figure every
/// tracker shows, and the one people actually check between games.
function todayRecord(matches, getTime, getWon) {
  const midnight = new Date();
  midnight.setHours(0, 0, 0, 0);
  const cutoff = midnight.getTime() / 1000;
  const today = matches.filter((m) => getTime(m) >= cutoff);
  const wins = today.filter(getWon).length;
  return { played: today.length, wins, losses: today.length - wins };
}

function dotaSectionHtml() {
  if (!DOTA.link.accountId) {
    return `
      <section class="home-section">
        <div class="home-head"><h2 class="home-title">Dota 2</h2></div>
        <p class="hint">
          No Steam account linked. Open <b>Dota 2 &rsaquo; Match History</b> and
          press <b>Detect from Steam</b> to pull in your games.
        </p>
      </section>`;
  }
  if (!DOTA.matches.length) {
    return `
      <section class="home-section">
        <div class="home-head"><h2 class="home-title">Dota 2</h2></div>
        <p class="hint">${DOTA.loading ? "Loading your matches&hellip;" : "No matches loaded yet."}</p>
      </section>`;
  }

  const s = DOTA.summary || {};
  const recent = DOTA.matches;
  const streak = streakOf(recent);
  const today = todayRecord(recent, (m) => m.startTime, (m) => m.won);

  // Oldest-first so the sparkline reads left to right like a timeline.
  const chrono = [...recent].reverse();
  const kdaSeries = chrono.map((m) => (m.kills + m.assists) / Math.max(1, m.deaths));
  const gpmSeries = chrono.map((m) => m.goldPerMin);

  // Rolling win rate: a raw W/L sequence is too noisy to read as a trend.
  const winSeries = chrono.map((_, i) => {
    const win = chrono.slice(Math.max(0, i - 9), i + 1);
    return (win.filter((m) => m.won).length / win.length) * 100;
  });

  const heroes = dtHeroAggregate(recent);
  const best = [...heroes].filter((h) => h.played >= 2).sort((a, b) => b.winRate - a.winRate)[0];
  const worst = [...heroes].filter((h) => h.played >= 2).sort((a, b) => a.winRate - b.winRate)[0];

  return `
    <section class="home-section">
      <div class="home-head">
        <h2 class="home-title">Dota 2</h2>
        <div class="home-meta">
          ${escapeHtml(DOTA.link.personaname || "Linked")} &middot; last ${recent.length} matches
        </div>
        <button class="link-btn" data-goto="dotamatches">All matches &rsaquo;</button>
      </div>

      ${railHtml([
        {
          label: "Win rate",
          value: `${(s.winRate ?? 0).toFixed(0)}%`,
          tone: (s.winRate ?? 0) >= 50 ? "win" : "loss",
          sub: `${s.wins ?? 0}W &ndash; ${s.losses ?? 0}L`,
          spark: sparkline(winSeries),
        },
        {
          label: "KDA",
          value: (s.kda ?? 0).toFixed(2),
          sub: "avg per match",
          spark: sparkline(kdaSeries),
        },
        {
          label: "GPM",
          value: s.avgGpm ?? 0,
          sub: `${s.avgXpm ?? 0} XPM`,
          spark: sparkline(gpmSeries),
        },
        {
          label: "Today",
          value: today.played ? `${today.wins}&ndash;${today.losses}` : "&mdash;",
          tone: today.played ? (today.wins >= today.losses ? "win" : "loss") : "",
          sub: today.played ? `${today.played} played` : "no games yet",
        },
        {
          label: "Streak",
          value: streak ? `${streak.n}` : "&mdash;",
          tone: streak ? (streak.won ? "win" : "loss") : "",
          sub: streak ? (streak.won ? "wins in a row" : "losses in a row") : "",
        },
      ])}

      <div class="form-row">
        <span class="form-caption">Recent form &middot; newest first</span>
        ${formStripHtml(recent, 20)}
      </div>

      ${
        best || worst
          ? `<div class="split">
              ${
                best
                  ? `<div class="split-item">
                      <div class="rail-label">Best hero</div>
                      <div class="hero-line">
                        <img src="${DOTA_HERO_CDN}${best.slug}.png" alt="" onerror="this.style.visibility='hidden'" />
                        <span class="hero-line-name">${escapeHtml(best.name)}</span>
                        <span class="dl-wr good">${best.winRate.toFixed(0)}%</span>
                      </div>
                      <div class="rail-sub">${best.wins}W &ndash; ${best.losses}L over ${best.played} games</div>
                    </div>`
                  : ""
              }
              ${
                worst && best && worst.slug !== best.slug
                  ? `<div class="split-item">
                      <div class="rail-label">Weakest hero</div>
                      <div class="hero-line">
                        <img src="${DOTA_HERO_CDN}${worst.slug}.png" alt="" onerror="this.style.visibility='hidden'" />
                        <span class="hero-line-name">${escapeHtml(worst.name)}</span>
                        <span class="dl-wr bad">${worst.winRate.toFixed(0)}%</span>
                      </div>
                      <div class="rail-sub">${worst.wins}W &ndash; ${worst.losses}L over ${worst.played} games</div>
                    </div>`
                  : ""
              }
            </div>`
          : ""
      }
    </section>`;
}

function deadlockSectionHtml() {
  if (!DL.link.accountId) {
    return `
      <section class="home-section">
        <div class="home-head"><h2 class="home-title">Deadlock</h2></div>
        <p class="hint">
          No Steam account linked. Open <b>Deadlock &rsaquo; Overview</b> and press
          <b>Detect from Steam</b>.
        </p>
      </section>`;
  }
  if (!DL.matches.length) {
    return `
      <section class="home-section">
        <div class="home-head"><h2 class="home-title">Deadlock</h2></div>
        <p class="hint">${DL.loading ? "Loading your matches&hellip;" : "No matches loaded yet."}</p>
      </section>`;
  }

  const s = DL.summary || {};
  const recent = DL.matches.map((m) => ({ ...m, won: m.outcome === "win" }));
  const streak = streakOf(recent.filter((m) => m.outcome === "win" || m.outcome === "loss"));
  const today = todayRecord(recent, (m) => m.startTime, (m) => m.won);

  const chrono = [...recent].reverse();
  const kdaSeries = chrono.map((m) => (m.kills + m.assists) / Math.max(1, m.deaths));
  const soulsSeries = chrono.map((m) => m.netWorth);

  return `
    <section class="home-section">
      <div class="home-head">
        <h2 class="home-title">Deadlock</h2>
        <div class="home-meta">
          ${escapeHtml(DL.link.personaname || "Linked")}${DL.rank ? ` &middot; ${escapeHtml(DL.rank.label)}` : ""} &middot; last ${recent.length} matches
        </div>
        <button class="link-btn" data-goto="dlmatches">All matches &rsaquo;</button>
      </div>

      ${railHtml([
        {
          label: "Win rate",
          value: `${(s.winRate ?? 0).toFixed(0)}%`,
          tone: (s.winRate ?? 0) >= 50 ? "win" : "loss",
          sub: `${s.wins ?? 0}W &ndash; ${s.losses ?? 0}L`,
        },
        { label: "KDA", value: (s.kda ?? 0).toFixed(2), sub: "avg per match", spark: sparkline(kdaSeries) },
        {
          label: "Souls",
          value: dlFmtSouls(s.avgSouls ?? 0),
          sub: "avg net worth",
          spark: sparkline(soulsSeries),
        },
        {
          label: "Today",
          value: today.played ? `${today.wins}&ndash;${today.losses}` : "&mdash;",
          tone: today.played ? (today.wins >= today.losses ? "win" : "loss") : "",
          sub: today.played ? `${today.played} played` : "no games yet",
        },
        {
          label: "Streak",
          value: streak ? `${streak.n}` : "&mdash;",
          tone: streak ? (streak.won ? "win" : "loss") : "",
          sub: streak ? (streak.won ? "wins in a row" : "losses in a row") : "",
        },
      ])}

      <div class="form-row">
        <span class="form-caption">Recent form &middot; newest first</span>
        ${formStripHtml(recent, 20)}
      </div>

      ${
        DL.live
          ? `<div class="live-banner">
              <span class="live-pulse"></span>
              In a match right now as <b>${escapeHtml(DL.live.heroName)}</b>
            </div>`
          : ""
      }
    </section>`;
}

function renderHome() {
  const root = document.getElementById("tab-home");
  if (!root) return;

  const p = state.profile || {};
  const name = p.username || (state.auth && state.auth.email) || "Player";

  root.innerHTML = `
    <header class="home-hero">
      <div>
        <h1 class="home-hero-name">${escapeHtml(name)}</h1>
        <div class="home-hero-sub">
          ${state.auth && state.auth.signedIn ? "Signed in &middot; syncing to cloud" : "Local only &middot; not signed in"}
        </div>
      </div>
      ${
        state.live && state.live.current && !state.live.current.ended
          ? `<button class="live-banner" data-goto="live">
              <span class="live-pulse"></span> Dota match in progress &rsaquo;
             </button>`
          : ""
      }
    </header>

    ${dotaSectionHtml()}
    ${deadlockSectionHtml()}`;

  root.querySelectorAll("[data-goto]").forEach((el) =>
    el.addEventListener("click", () => setView(el.dataset.goto))
  );
}

/// The home page reads from both games, so it pulls whichever links exist.
/// Each loader caches, so revisiting home doesn't re-hit either API.
async function loadHome() {
  await Promise.all([dtRefreshLink(), dlRefreshLink()]);
  renderHome();
  const jobs = [];
  if (DOTA.link.accountId) jobs.push(dtLoad().then(renderHome));
  if (DL.link.accountId) jobs.push(dlLoad().then(renderHome));
  await Promise.all(jobs);
  renderHome();
}
