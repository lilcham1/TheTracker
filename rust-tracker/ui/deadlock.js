// Deadlock views: overview, match list, per-hero breakdown, account linking.
//
// Unlike the Dota side (which streams from Valve's official local GSI feed),
// everything here is fetched from the community Deadlock API through the
// Rust backend, and is post-match rather than live. The UI says so plainly
// rather than pretending an empty list means "no games".

const DL = {
  loading: false,
  error: null,
  loadedAt: 0,
  matches: [],
  summary: null,
  rank: null,
  link: { accountId: null, personaname: null, avatar: null },
  search: { query: "", results: [], busy: false, error: null },
  live: null,
  heroFilter: null,
};

const DL_CACHE_MS = 60000;

function dlFmtDuration(seconds) {
  const m = Math.floor(seconds / 60);
  return `${m}:${String(Math.floor(seconds % 60)).padStart(2, "0")}`;
}

function dlFmtSouls(n) {
  return n >= 1000 ? `${(n / 1000).toFixed(1)}k` : String(n);
}

function dlFmtWhen(unixSeconds) {
  if (!unixSeconds) return "";
  const d = new Date(unixSeconds * 1000);
  const diffDays = (Date.now() - d.getTime()) / 86400000;
  if (diffDays < 1) return d.toLocaleTimeString(undefined, { hour: "2-digit", minute: "2-digit" });
  if (diffDays < 7) return d.toLocaleDateString(undefined, { weekday: "short", hour: "2-digit", minute: "2-digit" });
  return d.toLocaleDateString(undefined, { month: "short", day: "numeric" });
}

async function dlLoad(force = false) {
  if (!DL.link.accountId) return;
  if (!force && Date.now() - DL.loadedAt < DL_CACHE_MS && DL.matches.length) return;

  DL.loading = true;
  DL.error = null;
  dlRender();
  try {
    const data = await invoke("deadlock_overview", { limit: 50 });
    DL.matches = data.matches || [];
    DL.summary = data.summary || null;
    DL.rank = data.rank || null;
    DL.loadedAt = Date.now();
  } catch (e) {
    DL.error = String(e);
  }
  DL.loading = false;
  dlRender();
}

async function dlRefreshLink() {
  try {
    DL.link = (await invoke("deadlock_link_status")) || DL.link;
  } catch (_) {
    /* leave as-is */
  }
}

// ---------- Overview ----------

function dlRenderOverview() {
  const root = document.getElementById("tab-dloverview");
  if (!DL.link.accountId) {
    root.innerHTML = dlNotLinkedHtml();
    dlWireNotLinked(root);
    return;
  }
  if (DL.loading && !DL.matches.length) {
    root.innerHTML = `<div class="empty-state">Loading your Deadlock matches…</div>`;
    return;
  }
  if (DL.error) {
    root.innerHTML = dlErrorHtml(DL.error);
    dlWireRetry(root);
    return;
  }

  const s = DL.summary;
  const rank = DL.rank;
  const recent = DL.matches.slice(0, 10);
  const form = recent
    .map((m) => {
      const cls = m.outcome === "win" ? "form-w" : m.outcome === "loss" ? "form-l" : "form-x";
      const ch = m.outcome === "win" ? "W" : m.outcome === "loss" ? "L" : "–";
      return `<span class="form-pip ${cls}" title="${escapeHtml(m.heroName)} · ${escapeHtml(m.outcome)}">${ch}</span>`;
    })
    .join("");

  root.innerHTML = `
    ${DL.live ? dlLiveCardHtml(DL.live) : ""}

    <div class="card dl-rank-card">
      <div class="dl-rank-main">
        <div>
          <p class="card-title" style="margin-bottom:4px">Current rank</p>
          <div class="dl-rank-label">${rank ? escapeHtml(rank.label) : "Unranked"}</div>
        </div>
        <div class="dl-rank-right">
          <div class="stat-label">Win rate</div>
          <div class="stat-value ${s && s.winRate >= 50 ? "good" : "danger"}">${s ? s.winRate.toFixed(0) : 0}%</div>
        </div>
      </div>
      <div class="dl-form">
        <span class="stat-label" style="margin-right:6px">Recent</span>${form || "<span class='ov-hint'>No games yet</span>"}
      </div>
    </div>

    <div class="stat-grid">
      <div class="stat-tile">
        <div class="stat-label">Record</div>
        <div class="stat-value">${s ? s.wins : 0}<span class="dl-dim">W</span> ${s ? s.losses : 0}<span class="dl-dim">L</span></div>
      </div>
      <div class="stat-tile">
        <div class="stat-label">KDA</div>
        <div class="stat-value">${s ? s.kda.toFixed(2) : "0.00"}</div>
      </div>
      <div class="stat-tile">
        <div class="stat-label">Avg souls</div>
        <div class="stat-value">${s ? dlFmtSouls(s.avgSouls) : 0}</div>
      </div>
      <div class="stat-tile">
        <div class="stat-label">Best hero</div>
        <div class="stat-value dl-small">${s && s.bestHero ? escapeHtml(s.bestHero) : "—"}</div>
      </div>
    </div>

    <div class="card">
      <p class="card-title">Totals across ${s ? s.matches : 0} matches</p>
      <div class="dl-kda-row">
        <div><span class="dl-k">${s ? s.kills : 0}</span><div class="stat-label">Kills</div></div>
        <div><span class="dl-d">${s ? s.deaths : 0}</span><div class="stat-label">Deaths</div></div>
        <div><span class="dl-a">${s ? s.assists : 0}</span><div class="stat-label">Assists</div></div>
      </div>
    </div>

    <p class="cloud-note">
      Deadlock has no live stats feed from Valve, so these come from the
      community Deadlock API after each match ends — a game can take a
      little while to appear, and some may be missing entirely.
      <button class="link-btn" data-dl-refresh>Refresh now</button>
    </p>
  `;
  dlWireRetry(root);
}

function dlLiveCardHtml(live) {
  const elapsed = live.startTime ? Math.floor(Date.now() / 1000 - live.startTime) : 0;
  return `
    <div class="card dl-live-card">
      <div class="dl-live-head">
        <span class="live-dot"></span>
        <span class="dl-live-title">In a match as ${escapeHtml(live.heroName)}</span>
        <span class="hero-clock">${dlFmtDuration(elapsed)}</span>
      </div>
      <div class="dl-live-teams">
        <div>
          <div class="stat-label">Your team</div>
          <div class="ov-heroes">${live.allyHeroes.map((h) => `<span class="ov-hero-chip">${escapeHtml(h)}</span>`).join("")}</div>
        </div>
        <div>
          <div class="stat-label">Opponents</div>
          <div class="ov-heroes">${live.enemyHeroes.map((h) => `<span class="ov-hero-chip">${escapeHtml(h)}</span>`).join("")}</div>
        </div>
      </div>
    </div>`;
}

// ---------- Matches ----------

function dlRenderMatches() {
  const root = document.getElementById("tab-dlmatches");
  if (!DL.link.accountId) {
    root.innerHTML = dlNotLinkedHtml();
    dlWireNotLinked(root);
    return;
  }
  if (DL.error) {
    root.innerHTML = dlErrorHtml(DL.error);
    dlWireRetry(root);
    return;
  }
  const list = DL.heroFilter ? DL.matches.filter((m) => m.heroName === DL.heroFilter) : DL.matches;
  if (!list.length) {
    root.innerHTML = `<div class="empty-state">${DL.loading ? "Loading…" : "No matches found for this account."}</div>`;
    return;
  }

  root.innerHTML = `
    ${DL.heroFilter ? `<div class="chip-row"><button class="chip selected" data-dl-clearfilter>${escapeHtml(DL.heroFilter)} ✕</button></div>` : ""}
    ${list
      .map((m) => {
        const cls = m.outcome === "win" ? "win" : m.outcome === "loss" ? "loss" : "neutral";
        const label = m.outcome === "win" ? "Win" : m.outcome === "loss" ? "Loss" : m.outcome === "abandoned" ? "Abandon" : "—";
        return `
        <div class="dl-match ${cls}">
          ${m.heroImage ? `<img class="dl-hero-img" src="${m.heroImage}" alt="" />` : `<div class="dl-hero-img"></div>`}
          <div class="dl-match-main">
            <div class="dl-match-top">
              <span class="dl-hero-name">${escapeHtml(m.heroName)}</span>
              <span class="dl-outcome ${cls}">${label}</span>
            </div>
            <div class="dl-match-sub">
              <span class="dl-kda">${m.kills} / <span class="dl-d">${m.deaths}</span> / ${m.assists}</span>
              <span>${dlFmtSouls(m.netWorth)} souls</span>
              <span>${dlFmtDuration(m.durationSeconds)}</span>
            </div>
            <div class="dl-match-meta">
              Lvl ${m.heroLevel} · ${m.lastHits} LH · ${m.denies} DN · ${dlFmtWhen(m.startTime)}
            </div>
          </div>
        </div>`;
      })
      .join("")}
  `;

  const clear = root.querySelector("[data-dl-clearfilter]");
  if (clear)
    clear.addEventListener("click", () => {
      DL.heroFilter = null;
      dlRender();
    });
}

// ---------- Heroes ----------

function dlRenderHeroes() {
  const root = document.getElementById("tab-dlheroes");
  if (!DL.link.accountId) {
    root.innerHTML = dlNotLinkedHtml();
    dlWireNotLinked(root);
    return;
  }
  if (!DL.matches.length) {
    root.innerHTML = `<div class="empty-state">${DL.loading ? "Loading…" : "No matches to break down yet."}</div>`;
    return;
  }

  const byHero = new Map();
  for (const m of DL.matches) {
    const e = byHero.get(m.heroName) || {
      name: m.heroName,
      image: m.heroImage,
      played: 0,
      wins: 0,
      losses: 0,
      k: 0,
      d: 0,
      a: 0,
      souls: 0,
    };
    e.played++;
    if (m.outcome === "win") e.wins++;
    else if (m.outcome === "loss") e.losses++;
    e.k += m.kills;
    e.d += m.deaths;
    e.a += m.assists;
    e.souls += m.netWorth;
    byHero.set(m.heroName, e);
  }

  const rows = [...byHero.values()].sort((x, y) => y.played - x.played);
  root.innerHTML = rows
    .map((h) => {
      const scored = h.wins + h.losses;
      const wr = scored ? (h.wins / scored) * 100 : 0;
      return `
      <div class="dl-hero-row" data-dl-hero="${escapeHtml(h.name)}">
        ${h.image ? `<img class="dl-hero-img sm" src="${h.image}" alt="" />` : `<div class="dl-hero-img sm"></div>`}
        <div class="dl-hero-main">
          <div class="dl-hero-top">
            <span class="dl-hero-name">${escapeHtml(h.name)}</span>
            <span class="dl-wr ${wr >= 50 ? "good" : "bad"}">${wr.toFixed(0)}%</span>
          </div>
          <div class="dl-winbar"><div class="dl-winbar-fill" style="width:${wr}%"></div></div>
          <div class="dl-match-meta">
            ${h.played} played · ${h.wins}W ${h.losses}L ·
            ${(h.k / h.played).toFixed(1)}/${(h.d / h.played).toFixed(1)}/${(h.a / h.played).toFixed(1)} avg
          </div>
        </div>
      </div>`;
    })
    .join("");

  root.querySelectorAll("[data-dl-hero]").forEach((el) =>
    el.addEventListener("click", () => {
      DL.heroFilter = el.dataset.dlHero;
      setDeadlockTab("dlmatches");
    })
  );
}

// ---------- Account ----------

function dlRenderAccount() {
  const root = document.getElementById("tab-dlaccount");
  if (DL.link.accountId) {
    root.innerHTML = `
      <div class="card">
        <p class="card-title">Linked Steam account</p>
        <div class="dl-linked">
          ${DL.link.avatar ? `<img class="dl-avatar" src="${DL.link.avatar}" alt="" />` : ""}
          <div>
            <div class="dl-hero-name">${escapeHtml(DL.link.personaname || "Unknown")}</div>
            <div class="dl-match-meta mono">${DL.link.accountId}</div>
          </div>
        </div>
        <p class="cloud-note">
          Match data is read from the public community Deadlock API for this
          account. Nothing is sent to it, and the app never reads the game.
        </p>
        <div style="display:flex;gap:8px;">
          <button class="roshan-btn" data-dl-refresh>Refresh matches</button>
          <button class="roshan-btn" id="dlUnlinkBtn">Unlink</button>
        </div>
      </div>`;
    root.querySelector("#dlUnlinkBtn").addEventListener("click", async () => {
      DL.link = await invoke("deadlock_unlink");
      DL.matches = [];
      DL.summary = null;
      DL.rank = null;
      DL.loadedAt = 0;
      dlRender();
    });
    dlWireRetry(root);
    return;
  }

  root.innerHTML = dlNotLinkedHtml();
  dlWireNotLinked(root);
}

function dlNotLinkedHtml() {
  const r = DL.search;
  return `
    <div class="card">
      <p class="card-title">Link your Deadlock account</p>
      <p class="cloud-note" style="margin-top:0">
        Deadlock has no local data feed, so stats are looked up by Steam
        account through the community Deadlock API. Search for your Steam
        profile name to link it.
      </p>
      <input class="text-input" id="dlSearchInput" type="text" placeholder="Steam profile name" value="${escapeHtml(r.query)}" />
      <div style="display:flex;gap:8px;align-items:center;margin-top:8px;">
        <button class="save-btn" id="dlSearchBtn" ${r.busy ? "disabled" : ""}>${r.busy ? "Searching…" : "Search"}</button>
      </div>
      ${r.error ? `<div class="auth-error" style="margin-top:8px">${escapeHtml(r.error)}</div>` : ""}
      ${
        r.results.length
          ? `<div class="dl-results">${r.results
              .map(
                (p) => `
          <div class="dl-result" data-dl-pick="${p.accountId}" data-dl-name="${escapeHtml(p.personaname)}" data-dl-avatar="${escapeHtml(p.avatar || "")}">
            ${p.avatar ? `<img class="dl-avatar sm" src="${p.avatar}" alt="" />` : `<div class="dl-avatar sm"></div>`}
            <div class="dl-result-main">
              <div class="dl-hero-name">${escapeHtml(p.personaname)}</div>
              <div class="dl-match-meta mono">${p.accountId}</div>
            </div>
            <span class="chip small">Link</span>
          </div>`
              )
              .join("")}</div>`
          : ""
      }
    </div>`;
}

function dlWireNotLinked(root) {
  const input = root.querySelector("#dlSearchInput");
  const btn = root.querySelector("#dlSearchBtn");
  if (!input || !btn) return;

  input.addEventListener("input", (e) => (DL.search.query = e.target.value));
  input.addEventListener("keydown", (e) => {
    if (e.key === "Enter") btn.click();
  });

  btn.addEventListener("click", async () => {
    DL.search.busy = true;
    DL.search.error = null;
    dlRender();
    try {
      DL.search.results = await invoke("deadlock_search", { query: DL.search.query });
      if (!DL.search.results.length) DL.search.error = "No Steam profiles matched that name.";
    } catch (e) {
      DL.search.error = String(e);
      DL.search.results = [];
    }
    DL.search.busy = false;
    dlRender();
  });

  root.querySelectorAll("[data-dl-pick]").forEach((el) =>
    el.addEventListener("click", async () => {
      DL.link = await invoke("deadlock_link", {
        accountId: Number(el.dataset.dlPick),
        personaname: el.dataset.dlName,
        avatar: el.dataset.dlAvatar || null,
      });
      DL.search = { query: "", results: [], busy: false, error: null };
      DL.loadedAt = 0;
      dlRender();
      dlLoad(true);
    })
  );
}

function dlErrorHtml(err) {
  return `
    <div class="card">
      <p class="card-title">Couldn't load Deadlock data</p>
      <div class="auth-error">${escapeHtml(err)}</div>
      <p class="cloud-note">
        The Deadlock API is community-run and Valve has been restricting how
        much match data it can pull, so this can fail or come back empty
        through no fault of yours.
      </p>
      <button class="roshan-btn" data-dl-refresh>Try again</button>
    </div>`;
}

function dlWireRetry(root) {
  root.querySelectorAll("[data-dl-refresh]").forEach((b) =>
    b.addEventListener("click", () => dlLoad(true))
  );
}

function dlRender() {
  if (!String(state.view || "").startsWith("dl")) return;
  switch (state.dlTab) {
    case "dlmatches":
      dlRenderMatches();
      break;
    case "dlheroes":
      dlRenderHeroes();
      break;
    case "dlaccount":
      dlRenderAccount();
      break;
    default:
      dlRenderOverview();
  }
}
