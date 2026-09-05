// Dota match history backed by OpenDota.
//
// This is separate from "Tracked Sessions", which is what the live GSI feed
// recorded locally. GSI never reports who won, so this view exists to give
// real results, real game modes, and the full scoreboard — the things a
// tracker is actually expected to show.

const DOTA = {
  link: { accountId: null, personaname: null, avatar: null },
  matches: [],
  summary: null,
  loading: false,
  error: null,
  loadedAt: 0,
  open: new Set(),
  details: new Map(), // matchId -> scoreboard, fetched on first expand
  detailLoading: new Set(),
  filter: "all", // all | ranked | all_pick | turbo | other
  results: [],
  searching: false,
  searchError: null,
};

const DOTA_CACHE_MS = 120000;
const DOTA_HERO_CDN = "https://cdn.cloudflare.steamstatic.com/apps/dota2/images/dota_react/heroes/";
const DOTA_ITEM_CDN = "https://cdn.cloudflare.steamstatic.com/apps/dota2/images/dota_react/items/";

function dtHeroImg(slug) {
  return slug ? `${DOTA_HERO_CDN}${slug}.png` : null;
}

function dtDuration(sec) {
  const m = Math.floor(sec / 60);
  return `${m}:${String(sec % 60).padStart(2, "0")}`;
}

function dtAgo(unixSeconds) {
  if (!unixSeconds) return "";
  const diff = Math.floor(Date.now() / 1000 - unixSeconds);
  if (diff < 3600) return `${Math.max(1, Math.floor(diff / 60))}m ago`;
  if (diff < 86400) return `${Math.floor(diff / 3600)}h ago`;
  if (diff < 604800) return `${Math.floor(diff / 86400)}d ago`;
  return new Date(unixSeconds * 1000).toLocaleDateString(undefined, { month: "short", day: "numeric" });
}

function dtNum(n) {
  return n >= 1000 ? `${(n / 1000).toFixed(1)}k` : String(n);
}

async function dtRefreshLink() {
  try {
    DOTA.link = (await invoke("dota_link_status")) || DOTA.link;
  } catch (_) {
    /* leave as-is */
  }
}

async function dtLoad(force = false) {
  if (!DOTA.link.accountId) return;
  if (!force && Date.now() - DOTA.loadedAt < DOTA_CACHE_MS && DOTA.matches.length) return;

  DOTA.loading = true;
  DOTA.error = null;
  dtRender();
  try {
    const data = await invoke("dota_api_history", { limit: 50 });
    DOTA.matches = data.matches || [];
    DOTA.summary = data.summary || null;
    DOTA.loadedAt = Date.now();
  } catch (e) {
    DOTA.error = String(e);
  }
  DOTA.loading = false;
  dtRender();
}

// ---------- Rendering ----------

function dtStatsHtml() {
  const s = DOTA.summary;
  if (!s) return "";
  const wr = s.winRate;
  return `
    <div class="stat-row">
      <div class="stat">
        <div class="stat-label">Win rate</div>
        <div class="stat-value ${wr >= 50 ? "win" : "loss"}">${wr.toFixed(0)}%</div>
        <div class="stat-sub">${s.wins}W · ${s.losses}L of ${s.matches}</div>
        <div class="meter"><div class="meter-fill ${wr >= 50 ? "" : "low"}" style="width:${Math.min(100, wr)}%"></div></div>
      </div>
      <div class="stat">
        <div class="stat-label">Avg KDA</div>
        <div class="stat-value">${s.kda.toFixed(2)}</div>
        <div class="stat-sub">${(s.kills / Math.max(1, s.matches)).toFixed(1)} / ${(s.deaths / Math.max(1, s.matches)).toFixed(1)} / ${(s.assists / Math.max(1, s.matches)).toFixed(1)} per game</div>
      </div>
      <div class="stat">
        <div class="stat-label">Avg GPM</div>
        <div class="stat-value brand">${s.avgGpm}</div>
        <div class="stat-sub">${s.avgXpm} XPM</div>
      </div>
      <div class="stat">
        <div class="stat-label">Avg last hits</div>
        <div class="stat-value">${s.avgLastHits}</div>
        <div class="stat-sub">per match</div>
      </div>
    </div>`;
}

function dtBoardSideHtml(players, radiant, label) {
  const side = players.filter((p) => p.radiant === radiant);
  if (!side.length) return "";
  return `
    <div class="board">
      <div class="board-team ${radiant ? "radiant" : "dire"}">${label}</div>
      <div class="board-row head">
        <span></span><span>Player</span><span>K / D / A</span>
        <span class="board-num">LH</span><span class="board-num">GPM</span>
        <span class="board-num">XPM</span><span class="board-num">Dmg</span><span>Items</span>
      </div>
      ${side
        .map((p) => {
          const img = dtHeroImg(p.heroSlug);
          return `
        <div class="board-row ${p.isMe ? "me" : ""}">
          ${img ? `<img class="board-hero" src="${img}" alt="" />` : `<span class="board-hero"></span>`}
          <span class="board-name">${escapeHtml(p.name)}</span>
          <span>${p.kills} / ${p.deaths} / ${p.assists}</span>
          <span class="board-num">${p.lastHits}</span>
          <span class="board-num">${p.goldPerMin}</span>
          <span class="board-num">${p.xpPerMin}</span>
          <span class="board-num">${dtNum(p.heroDamage)}</span>
          <span class="board-items">${p.items
            .map((id) => `<img src="${DOTA_ITEM_CDN}${id}.png" alt="" onerror="this.style.visibility='hidden'" />`)
            .join("")}</span>
        </div>`;
        })
        .join("")}
    </div>`;
}

function dtDetailHtml(matchId) {
  if (DOTA.detailLoading.has(matchId)) {
    return `<div class="hint">Loading scoreboard…</div>`;
  }
  const d = DOTA.details.get(matchId);
  if (!d) return `<div class="hint">Scoreboard unavailable.</div>`;
  if (d.error) return `<div class="note err">${escapeHtml(d.error)}</div>`;

  return `
    <div class="row" style="gap:14px">
      <span class="badge ${d.radiantWin ? "badge-win" : "badge-loss"}">${d.radiantWin ? "Radiant win" : "Dire win"}</span>
      <span class="hint">${d.radiantScore} – ${d.direScore} · ${dtDuration(d.durationSeconds)} · ${escapeHtml(d.modeName)} · ${escapeHtml(d.lobbyName)}</span>
      <span class="grow"></span>
      <span class="hint">Match ${d.matchId}</span>
    </div>
    ${dtBoardSideHtml(d.players, true, "Radiant")}
    ${dtBoardSideHtml(d.players, false, "Dire")}`;
}

function dtMatchRowHtml(m) {
  const img = dtHeroImg(m.heroSlug);
  const open = DOTA.open.has(m.matchId);
  const kdaCls = m.kda >= 4 ? "kda-good" : m.kda < 1.5 ? "kda-bad" : "";
  const result = m.abandoned ? "other" : m.won ? "win" : "loss";
  const resultText = m.abandoned ? "Left" : m.won ? "Win" : "Loss";

  return `
    <div class="match ${result} ${open ? "open" : ""}" data-dt-match="${m.matchId}">
      <div class="match-head" data-dt-toggle="${m.matchId}">
        ${img ? `<img class="hero-portrait" src="${img}" alt="" />` : `<span class="hero-portrait"></span>`}
        <div class="match-hero">
          <span class="match-hero-name">${escapeHtml(m.heroName)}</span>
          <span class="match-mode">${escapeHtml(m.modeName)} · ${escapeHtml(m.lobbyName)}${m.partySize && m.partySize > 1 ? ` · party of ${m.partySize}` : ""}</span>
        </div>
        <span class="match-result ${result}">${resultText}</span>
        <div class="match-col">
          <span class="col-value ${kdaCls}">${m.kills} / ${m.deaths} / ${m.assists}</span>
          <span class="col-label">KDA ${m.kda.toFixed(2)}</span>
        </div>
        <div class="match-col col-hide">
          <span class="col-value">${m.goldPerMin}</span>
          <span class="col-label">GPM</span>
        </div>
        <div class="match-col col-hide">
          <span class="col-value">${m.lastHits}</span>
          <span class="col-label">Last hits</span>
        </div>
        <div class="match-when">${dtDuration(m.durationSeconds)}<br />${dtAgo(m.startTime)}</div>
        <span class="chev">▸</span>
      </div>
      <div class="match-body">${open ? dtDetailHtml(m.matchId) : ""}</div>
    </div>`;
}

function dtNotLinkedHtml() {
  return `
    <div class="card col">
      <div>
        <h3 style="margin:0 0 6px;font-size:15px">Link your Steam account</h3>
        <p class="hint" style="margin:0">
          The live tracker reads Valve's GSI feed, which only ever reports your own
          state — it never says who won. Linking your account pulls full match
          history from OpenDota: results, game modes, GPM/XPM and the whole
          scoreboard. Public data, no API key, nothing read from your PC.
        </p>
      </div>
      ${steamDetectHtml()}
      <div class="hint" style="text-align:center">— or search by name —</div>
      <div class="row">
        <input class="text-input grow" id="dtSearchInput" type="text" placeholder="Steam display name…" value="" />
        <button class="btn" id="dtSearchBtn" type="button">Search</button>
      </div>
      ${DOTA.searchError ? `<div class="note err">${escapeHtml(DOTA.searchError)}</div>` : ""}
      ${DOTA.searching ? `<div class="hint">Searching…</div>` : ""}
      <div class="col" id="dtResults">
        ${DOTA.results
          .map(
            (r) => `
          <div class="result-row" data-dt-pick="${r.accountId}" data-dt-name="${escapeHtml(r.personaname)}" data-dt-avatar="${escapeHtml(r.avatar || "")}">
            ${r.avatar ? `<img src="${escapeHtml(r.avatar)}" alt="" />` : `<span class="result-row-img"></span>`}
            <div class="grow">
              <div class="result-name">${escapeHtml(r.personaname)}</div>
              <div class="result-id">Account ${r.accountId}</div>
            </div>
          </div>`
          )
          .join("")}
      </div>
      <p class="hint">
        Can't find yourself? Your Dota profile has to be public — in Dota 2:
        Settings → Options → Advanced Options → Expose Public Match Data.
      </p>
    </div>`;
}

async function dtLinkAccount(accountId, personaname, avatar) {
  DOTA.link = await invoke("dota_link", { accountId, personaname, avatar: avatar || null });
  DOTA.results = [];
  DOTA.loadedAt = 0;
  renderUserChip();
  await dtLoad(true);
}

function dtWireNotLinked(root) {
  wireSteamDetect(root, dtRender, (id, name) => dtLinkAccount(id, name, null));

  const input = root.querySelector("#dtSearchInput");
  const btn = root.querySelector("#dtSearchBtn");
  if (!input || !btn) return;

  const run = async () => {
    const q = input.value.trim();
    if (q.length < 2) {
      DOTA.searchError = "Type at least two characters.";
      dtRender();
      return;
    }
    DOTA.searching = true;
    DOTA.searchError = null;
    DOTA.results = [];
    dtRender();
    try {
      DOTA.results = await invoke("dota_search", { query: q });
      if (!DOTA.results.length) DOTA.searchError = "No matching Steam profiles.";
    } catch (e) {
      DOTA.searchError = String(e);
    }
    DOTA.searching = false;
    dtRender();
    // Keep what was typed after the re-render.
    const again = document.querySelector("#dtSearchInput");
    if (again) {
      again.value = q;
      again.focus();
    }
  };

  btn.addEventListener("click", run);
  input.addEventListener("keydown", (e) => {
    if (e.key === "Enter") run();
  });

  root.querySelectorAll("[data-dt-pick]").forEach((el) =>
    el.addEventListener("click", () =>
      dtLinkAccount(Number(el.dataset.dtPick), el.dataset.dtName, el.dataset.dtAvatar || null)
    )
  );
}

async function dtToggleMatch(matchId) {
  if (DOTA.open.has(matchId)) {
    DOTA.open.delete(matchId);
    dtRender();
    return;
  }
  DOTA.open.add(matchId);

  // Scoreboards are a separate request each, so fetch once and keep it.
  if (!DOTA.details.has(matchId)) {
    DOTA.detailLoading.add(matchId);
    dtRender();
    try {
      DOTA.details.set(matchId, await invoke("dota_match_detail", { matchId }));
    } catch (e) {
      DOTA.details.set(matchId, { error: String(e) });
    }
    DOTA.detailLoading.delete(matchId);
  }
  dtRender();
}

function dtRender() {
  const root = document.getElementById("tab-dotamatches");
  if (!root) return;

  if (!DOTA.link.accountId) {
    root.innerHTML = dtNotLinkedHtml();
    dtWireNotLinked(root);
    return;
  }

  if (DOTA.error) {
    root.innerHTML = `
      <div class="note err">${escapeHtml(DOTA.error)}</div>
      <div class="row"><button class="btn btn-secondary" id="dtRetry" type="button">Try again</button></div>`;
    const b = root.querySelector("#dtRetry");
    if (b) b.addEventListener("click", () => dtLoad(true));
    return;
  }

  if (!DOTA.matches.length) {
    root.innerHTML = `<div class="empty-state">${DOTA.loading ? "Loading matches…" : "No matches found for this account."}</div>`;
    return;
  }

  const shown =
    DOTA.filter === "all" ? DOTA.matches : DOTA.matches.filter((m) => m.gameType === DOTA.filter);

  const filters = [
    ["all", "All"],
    ["ranked", "Ranked"],
    ["all_pick", "All Pick"],
    ["turbo", "Turbo"],
    ["other", "Other"],
  ];

  root.innerHTML = `
    ${dtStatsHtml()}
    <div class="section-head">
      <div class="chip-row">
        ${filters
          .map(
            ([id, label]) =>
              `<button class="chip ${DOTA.filter === id ? "selected" : ""}" data-dt-filter="${id}">${label}</button>`
          )
          .join("")}
      </div>
      <button class="chip" id="dtRefresh" type="button">${DOTA.loading ? "Refreshing…" : "Refresh"}</button>
    </div>
    <div class="match-list">
      ${shown.length ? shown.map(dtMatchRowHtml).join("") : `<div class="empty-state">No ${DOTA.filter} matches in the last ${DOTA.matches.length}.</div>`}
    </div>`;

  root.querySelectorAll("[data-dt-filter]").forEach((el) =>
    el.addEventListener("click", () => {
      DOTA.filter = el.dataset.dtFilter;
      dtRender();
    })
  );
  root.querySelectorAll("[data-dt-toggle]").forEach((el) =>
    el.addEventListener("click", () => dtToggleMatch(Number(el.dataset.dtToggle)))
  );
  const refresh = root.querySelector("#dtRefresh");
  if (refresh) refresh.addEventListener("click", () => dtLoad(true));
}
