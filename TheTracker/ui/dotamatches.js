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
  sortKey: "startTime",
  sortDir: "desc",
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

  // Oldest-first so the sparklines read left to right like a timeline.
  const chrono = [...DOTA.matches].reverse();
  const kdaSeries = chrono.map((m) => (m.kills + m.assists) / Math.max(1, m.deaths));
  const gpmSeries = chrono.map((m) => m.goldPerMin);
  const lhSeries = chrono.map((m) => m.lastHits);
  const winSeries = chrono.map((_, i) => {
    const w = chrono.slice(Math.max(0, i - 9), i + 1);
    return (w.filter((m) => m.won).length / w.length) * 100;
  });

  return railHtml([
    {
      label: "Win rate",
      value: `${s.winRate.toFixed(0)}%`,
      tone: s.winRate >= 50 ? "win" : "loss",
      sub: `${s.wins}W – ${s.losses}L of ${s.matches}`,
      spark: sparkline(winSeries),
    },
    {
      label: "Avg KDA",
      value: s.kda.toFixed(2),
      sub: `${(s.kills / Math.max(1, s.matches)).toFixed(1)} / ${(s.deaths / Math.max(1, s.matches)).toFixed(1)} / ${(s.assists / Math.max(1, s.matches)).toFixed(1)}`,
      spark: sparkline(kdaSeries),
    },
    { label: "Avg GPM", value: s.avgGpm, sub: `${s.avgXpm} XPM`, spark: sparkline(gpmSeries) },
    { label: "Avg last hits", value: s.avgLastHits, sub: "per match", spark: sparkline(lhSeries) },
  ]);
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

// Columns the table can sort by. Keeping this declarative means the header
// and the comparator can never drift apart.
const DT_COLUMNS = [
  { key: "heroName", label: "Hero", type: "text" },
  { key: "result", label: "Result", type: "text" },
  { key: "kda", label: "KDA", type: "num" },
  { key: "kills", label: "K", type: "num" },
  { key: "deaths", label: "D", type: "num" },
  { key: "assists", label: "A", type: "num" },
  { key: "lastHits", label: "LH", type: "num" },
  { key: "goldPerMin", label: "GPM", type: "num" },
  { key: "xpPerMin", label: "XPM", type: "num" },
  { key: "heroDamage", label: "DMG", type: "num" },
  { key: "durationSeconds", label: "Length", type: "num" },
  { key: "startTime", label: "When", type: "num" },
];

function dtSortValue(m, key) {
  if (key === "result") return m.abandoned ? 2 : m.won ? 0 : 1;
  return m[key];
}

function dtSorted(list) {
  const { sortKey, sortDir } = DOTA;
  const dir = sortDir === "asc" ? 1 : -1;
  return [...list].sort((a, b) => {
    const av = dtSortValue(a, sortKey);
    const bv = dtSortValue(b, sortKey);
    if (typeof av === "string") return av.localeCompare(bv) * dir;
    return (av - bv) * dir;
  });
}

function dtRowHtml(m) {
  const img = dtHeroImg(m.heroSlug);
  const open = DOTA.open.has(m.matchId);
  const result = m.abandoned ? "other" : m.won ? "win" : "loss";
  const resultText = m.abandoned ? "Left" : m.won ? "Win" : "Loss";
  const kdaCls = m.kda >= 4 ? "kda-good" : m.kda < 1.5 ? "kda-bad" : "";

  return `
    <tr class="${result}" data-dt-toggle="${m.matchId}">
      <td>
        <div class="cell-hero">
          ${img ? `<img src="${img}" alt="" onerror="this.style.visibility='hidden'" />` : ""}
          <div style="min-width:0">
            <div class="cell-hero-name">${escapeHtml(m.heroName)}</div>
            <div class="cell-sub">${escapeHtml(m.modeName)}${m.partySize && m.partySize > 1 ? ` · party ${m.partySize}` : ""}</div>
          </div>
        </div>
      </td>
      <td><span class="res ${result}">${resultText}</span></td>
      <td class="num ${kdaCls}">${m.kda.toFixed(2)}</td>
      <td class="num">${m.kills}</td>
      <td class="num">${m.deaths}</td>
      <td class="num">${m.assists}</td>
      <td class="num">${m.lastHits}</td>
      <td class="num">${m.goldPerMin}</td>
      <td class="num">${m.xpPerMin}</td>
      <td class="num">${dtNum(m.heroDamage)}</td>
      <td class="num">${dtDuration(m.durationSeconds)}</td>
      <td class="num cell-sub">${dtAgo(m.startTime)}</td>
    </tr>
    ${open ? `<tr class="detail-row"><td colspan="${DT_COLUMNS.length}">${dtDetailHtml(m.matchId)}</td></tr>` : ""}`;
}

function dtTableHtml(list) {
  return `
    <table class="dtable">
      <thead>
        <tr>
          ${DT_COLUMNS.map(
            (c) =>
              `<th class="${DOTA.sortKey === c.key ? "sorted" : ""}${c.type === "num" ? " num" : ""}" data-dt-sort="${c.key}">
                 ${c.label}${DOTA.sortKey === c.key ? (DOTA.sortDir === "asc" ? " ▲" : " ▼") : ""}
               </th>`
          ).join("")}
        </tr>
      </thead>
      <tbody>${dtSorted(list).map(dtRowHtml).join("")}</tbody>
    </table>`;
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
    ${shown.length ? dtTableHtml(shown) : `<div class="empty-state">No ${DOTA.filter} matches in the last ${DOTA.matches.length}.</div>`}`;

  root.querySelectorAll("[data-dt-filter]").forEach((el) =>
    el.addEventListener("click", () => {
      DOTA.filter = el.dataset.dtFilter;
      dtRender();
    })
  );
  root.querySelectorAll("[data-dt-toggle]").forEach((el) =>
    el.addEventListener("click", () => dtToggleMatch(Number(el.dataset.dtToggle)))
  );
  root.querySelectorAll("[data-dt-sort]").forEach((el) =>
    el.addEventListener("click", () => {
      const key = el.dataset.dtSort;
      // Clicking the active column flips direction; a new column starts
      // descending, which is what you want for every metric here.
      if (DOTA.sortKey === key) DOTA.sortDir = DOTA.sortDir === "asc" ? "desc" : "asc";
      else {
        DOTA.sortKey = key;
        DOTA.sortDir = "desc";
      }
      dtRender();
    })
  );

  const refresh = root.querySelector("#dtRefresh");
  if (refresh) refresh.addEventListener("click", () => dtLoad(true));
}
