// Per-hero performance, and the favourite-hero view.
//
// Everything here is derived from match history already fetched for the
// Match History view — no extra API calls. That keeps it instant and keeps
// load off OpenDota, which is a free community service.

const HEROES_VIEW = { sort: "played" };

/// Rolls a Dota match list into per-hero rows.
function dtHeroAggregate(matches) {
  const map = new Map();
  for (const m of matches) {
    const e = map.get(m.heroSlug) || {
      slug: m.heroSlug,
      name: m.heroName,
      played: 0,
      wins: 0,
      k: 0,
      d: 0,
      a: 0,
      gpm: 0,
      xpm: 0,
      lastHits: 0,
      recent: [], // newest first, for the form strip
    };
    e.played++;
    if (m.won) e.wins++;
    e.k += m.kills;
    e.d += m.deaths;
    e.a += m.assists;
    e.gpm += m.goldPerMin;
    e.xpm += m.xpPerMin;
    e.lastHits += m.lastHits;
    e.recent.push(m);
    map.set(m.heroSlug, e);
  }
  return [...map.values()].map((e) => ({
    ...e,
    losses: e.played - e.wins,
    winRate: (e.wins / e.played) * 100,
    // Deathless games would divide by zero; treat them as one death.
    kda: (e.k + e.a) / Math.max(1, e.d),
    avgGpm: Math.round(e.gpm / e.played),
    avgXpm: Math.round(e.xpm / e.played),
    avgLastHits: Math.round(e.lastHits / e.played),
  }));
}

function formStripHtml(matches, limit = 10) {
  return `<div class="form-strip">${matches
    .slice(0, limit)
    .map((m) => `<span class="form-dot ${m.won ? "win" : "loss"}" title="${escapeHtml(m.heroName)} — ${m.won ? "Win" : "Loss"}"></span>`)
    .join("")}</div>`;
}

function heroRowHtml(h, favSlug) {
  const isFav = h.slug === favSlug;
  return `
    <div class="hero-row ${isFav ? "fav" : ""}" data-hero-slug="${escapeHtml(h.slug)}">
      <img class="hero-portrait" src="${DOTA_HERO_CDN}${h.slug}.png" alt="" onerror="this.style.visibility='hidden'" />
      <div class="hero-main">
        <div class="hero-top">
          <span class="hero-name">${escapeHtml(h.name)}${isFav ? ' <span class="badge badge-brand">Favorite</span>' : ""}</span>
          <span class="dl-wr ${h.winRate >= 50 ? "good" : "bad"}">${h.winRate.toFixed(0)}%</span>
        </div>
        <div class="dl-winbar"><div class="dl-winbar-fill" style="width:${h.winRate}%"></div></div>
        <div class="dl-match-meta">
          ${h.played} played · ${h.wins}W ${h.losses}L ·
          ${(h.k / h.played).toFixed(1)}/${(h.d / h.played).toFixed(1)}/${(h.a / h.played).toFixed(1)} ·
          ${h.avgGpm} GPM · ${h.avgLastHits} LH
        </div>
      </div>
      ${formStripHtml(h.recent, 8)}
      <button class="chip fav-btn${isFav ? " is-fav" : ""}" data-fav-slug="${escapeHtml(h.slug)}" title="${isFav ? "Your favourite hero" : "Set as favourite hero"}">${isFav ? "★" : "☆"}</button>
    </div>`;
}

function renderDotaHeroes() {
  const root = document.getElementById("tab-dotaheroes");
  if (!root) return;

  if (!DOTA.link.accountId) {
    root.innerHTML = dtNotLinkedHtml();
    dtWireNotLinked(root);
    return;
  }
  if (!DOTA.matches.length) {
    root.innerHTML = `<div class="empty-state">${DOTA.loading ? "Loading…" : "No matches loaded yet. Open Match History first."}</div>`;
    return;
  }

  const fav = (state.prefs && state.prefs.favorites && state.prefs.favorites.dota) || null;
  let rows = dtHeroAggregate(DOTA.matches);

  const sorters = {
    played: (a, b) => b.played - a.played,
    winrate: (a, b) => b.winRate - a.winRate || b.played - a.played,
    kda: (a, b) => b.kda - a.kda,
    gpm: (a, b) => b.avgGpm - a.avgGpm,
  };
  rows.sort(sorters[HEROES_VIEW.sort] || sorters.played);

  const best = [...rows].filter((h) => h.played >= 2).sort((a, b) => b.winRate - a.winRate)[0];
  const worst = [...rows].filter((h) => h.played >= 2).sort((a, b) => a.winRate - b.winRate)[0];

  root.innerHTML = `
    ${railHtml([
      { label: "Heroes played", value: rows.length, sub: `across ${DOTA.matches.length} matches` },
      ...(best
        ? [{ label: "Best hero", value: escapeHtml(best.name), tone: "win", sub: `${best.winRate.toFixed(0)}% over ${best.played} games` }]
        : []),
      ...(worst && best && worst.slug !== best.slug
        ? [{ label: "Needs work", value: escapeHtml(worst.name), tone: "loss", sub: `${worst.winRate.toFixed(0)}% over ${worst.played} games` }]
        : []),
    ])}

    <div class="chip-row">
      ${[
        ["played", "Most played"],
        ["winrate", "Win rate"],
        ["kda", "KDA"],
        ["gpm", "GPM"],
      ]
        .map(([id, label]) => `<button class="chip ${HEROES_VIEW.sort === id ? "selected" : ""}" data-hero-sort="${id}">${label}</button>`)
        .join("")}
    </div>

    <div class="col" style="gap:7px">${rows.map((h) => heroRowHtml(h, fav)).join("")}</div>`;

  root.querySelectorAll("[data-hero-sort]").forEach((el) =>
    el.addEventListener("click", () => {
      HEROES_VIEW.sort = el.dataset.heroSort;
      renderDotaHeroes();
    })
  );
  root.querySelectorAll("[data-fav-slug]").forEach((el) =>
    el.addEventListener("click", async (e) => {
      e.stopPropagation();
      const slug = el.dataset.favSlug;
      const next = (state.prefs.favorites.dota === slug) ? null : slug;
      state.prefs = await invoke("set_favorite_hero", { game: "dota", hero: next });
      renderDotaHeroes();
      renderFavoriteHero();
    })
  );
}

// ---------- Favourite hero ----------

/// Splits a hero's games into an older and a newer half and reports the
/// delta. With very few games this is noise, so it says so rather than
/// dressing up a two-match sample as a trend.
function improvementHtml(recent) {
  if (recent.length < 4) {
    return `<div class="note">Play at least 4 games on this hero to see a trend — anything less is noise, not improvement.</div>`;
  }
  const ordered = [...recent].sort((a, b) => a.startTime - b.startTime);
  const half = Math.floor(ordered.length / 2);
  const older = ordered.slice(0, half);
  const newer = ordered.slice(half);

  const avg = (list, pick) => list.reduce((s, m) => s + pick(m), 0) / list.length;
  const metrics = [
    ["Win rate", (m) => (m.won ? 100 : 0), "%", 0],
    ["KDA", (m) => (m.kills + m.assists) / Math.max(1, m.deaths), "", 2],
    ["GPM", (m) => m.goldPerMin, "", 0],
    ["Last hits", (m) => m.lastHits, "", 0],
    ["Deaths", (m) => m.deaths, "", 1],
  ];

  return `
    <div class="card">
      <div class="section-head"><h3 class="section-title">Improvement — first ${older.length} vs last ${newer.length}</h3></div>
      ${metrics
        .map(([label, pick, suffix, dp]) => {
          const before = avg(older, pick);
          const after = avg(newer, pick);
          const diff = after - before;
          // Fewer deaths is better; everything else here is higher-is-better.
          const better = label === "Deaths" ? diff < 0 : diff > 0;
          const flat = Math.abs(diff) < (label === "KDA" ? 0.15 : 0.5);
          return `
          <div class="cmp-row">
            <span class="cmp-label">${label}</span>
            <span class="cmp-value">${before.toFixed(dp)}${suffix} → ${after.toFixed(dp)}${suffix}</span>
            <span class="badge ${flat ? "badge-neutral" : better ? "badge-win" : "badge-loss"}">
              ${flat ? "Steady" : `${diff > 0 ? "+" : ""}${diff.toFixed(dp)}${suffix}`}
            </span>
          </div>`;
        })
        .join("")}
    </div>`;
}

function renderFavoriteHero() {
  const root = document.getElementById("tab-favorite");
  if (!root) return;

  const fav = (state.prefs && state.prefs.favorites && state.prefs.favorites.dota) || null;

  if (!fav) {
    root.innerHTML = `
      <div class="empty-state">
        No favourite hero picked yet.<br />
        Open <b>Heroes</b> and press the ☆ next to one to track it here.
      </div>`;
    return;
  }
  if (!DOTA.matches.length) {
    root.innerHTML = `<div class="empty-state">${DOTA.loading ? "Loading…" : "No matches loaded yet. Open Match History first."}</div>`;
    return;
  }

  const rows = dtHeroAggregate(DOTA.matches);
  const h = rows.find((r) => r.slug === fav);
  if (!h) {
    root.innerHTML = `
      <div class="empty-state">
        No games on <b>${escapeHtml(fav)}</b> in the last ${DOTA.matches.length} matches.<br />
        Pick a different favourite in <b>Heroes</b>, or play a game on it.
      </div>`;
    return;
  }

  const builds = (state.prefs.builds || []).filter((b) => b.game === "dota" && b.hero === fav);

  root.innerHTML = `
    <div class="card live-hero">
      <img src="${DOTA_HERO_CDN}${h.slug}.png" alt="" onerror="this.style.visibility='hidden'" />
      <div class="grow">
        <p class="live-hero-name">${escapeHtml(h.name)}</p>
        <span class="hint">${h.played} games · ${h.wins}W ${h.losses}L · last ${DOTA.matches.length} matches</span>
      </div>
      ${formStripHtml(h.recent, 12)}
    </div>

    <div class="stat-row">
      <div class="stat">
        <div class="stat-label">Win rate</div>
        <div class="stat-value ${h.winRate >= 50 ? "win" : "loss"}">${h.winRate.toFixed(0)}%</div>
        <div class="meter"><div class="meter-fill ${h.winRate >= 50 ? "" : "low"}" style="width:${h.winRate}%"></div></div>
      </div>
      <div class="stat">
        <div class="stat-label">KDA</div>
        <div class="stat-value">${h.kda.toFixed(2)}</div>
        <div class="stat-sub">${(h.k / h.played).toFixed(1)} / ${(h.d / h.played).toFixed(1)} / ${(h.a / h.played).toFixed(1)}</div>
      </div>
      <div class="stat">
        <div class="stat-label">Avg GPM</div>
        <div class="stat-value brand">${h.avgGpm}</div>
        <div class="stat-sub">${h.avgXpm} XPM</div>
      </div>
      <div class="stat">
        <div class="stat-label">Avg last hits</div>
        <div class="stat-value">${h.avgLastHits}</div>
        <div class="stat-sub">per game</div>
      </div>
    </div>

    ${improvementHtml(h.recent)}

    <section class="home-section">
      <div class="home-head"><h2 class="home-title">Popular items on ${escapeHtml(h.name)}</h2></div>
      <div id="popularHost">${dotaPopularHtml(h.slug, h.recent[0] && h.recent[0].heroId)}</div>
    </section>

    <div class="card col">
      <div class="section-head">
        <h3 class="section-title">Your builds for ${escapeHtml(h.name)}</h3>
        <button class="chip" id="favNewBuild" type="button">New build</button>
      </div>
      ${
        builds.length
          ? builds.map((b) => buildCardHtml(b, false)).join("")
          : `<p class="hint">No saved builds for this hero yet.</p>`
      }
    </div>

    <div class="card col">
      <div class="section-head"><h3 class="section-title">Recent games on ${escapeHtml(h.name)}</h3></div>
      ${dtTableHtml(h.recent.slice(0, 10))}
    </div>`;

  root.querySelectorAll("[data-dt-toggle]").forEach((el) =>
    el.addEventListener("click", () => dtToggleMatch(Number(el.dataset.dtToggle)))
  );
  // Pulled after first paint so the page appears immediately.
  const heroId = h.recent[0] && h.recent[0].heroId;
  if (heroId && !POPULAR.dota.has(heroId)) {
    loadDotaPopular(heroId).then(() => {
      const host = document.getElementById("popularHost");
      if (host) host.innerHTML = dotaPopularHtml(h.slug, heroId);
    });
  }

  const nb = root.querySelector("#favNewBuild");
  if (nb)
    nb.addEventListener("click", () => {
      BUILDS.draft = { id: "", game: "dota", hero: fav, name: "", items: [], notes: "" };
      setView("builds");
    });
}
