// Dota match history: OpenDota's record of the account, with the matches
// this app recorded itself merged in.
//
// OpenDota supplies the scoreboard, the mode and the queue, but its
// per-player history is not complete — on some accounts it holds no Turbo
// games at all. Matches the live feed recorded fill those gaps, so the list
// is one timeline of what was actually played rather than two partial ones.

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
  // Two independent axes, because a match is both at once. `filter` is the
  // mode key (all | all_pick | turbo | random_draft | …) and `queue` is
  // whether it came off the ranked ladder.
  filter: "all",
  queue: "all", // all | ranked | unranked
  sortKey: "startTime",
  sortDir: "desc",
  results: [],
  searching: false,
  searchError: null,
};

const DOTA_CACHE_MS = 120000;
// How many recent matches OpenDota is asked for.
const DT_LIMIT = 50;
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
    const data = await invoke("dota_api_history", { limit: DT_LIMIT });
    DOTA.matches = data.matches || [];
    DOTA.summary = data.summary || null;
    DOTA.loadedAt = Date.now();
  } catch (e) {
    DOTA.error = String(e);
  }
  DOTA.loading = false;
  dtRender();
}

// ---------- The merged timeline ----------

function dtKnown(v) {
  return v !== null && v !== undefined;
}

/// "mm:ss" from a recorded match, in seconds. "??:??" — a match that never
/// reached the clock — and the negative pre-horn clock are unknown.
function dtClockSeconds(text) {
  const m = /^(\d+):(\d{2})$/.exec(text || "");
  return m ? Number(m[1]) * 60 + Number(m[2]) : null;
}

/// What a recorded match's tag says about its mode and its queue.
///
/// The tag is coarser than OpenDota's two fields. "Ranked" says nothing about
/// the mode and an untagged match says nothing about either, so those stay
/// unknown — they show under "All modes" and "All queues" only, rather than
/// being filed under a guess.
function dtLocalKind(gameType) {
  switch (gameType) {
    case "turbo":
      return { modeKey: "turbo", modeName: "Turbo", ranked: false };
    case "all_pick":
    case "unranked":
      return { modeKey: "all_pick", modeName: "All Pick", ranked: false };
    case "ranked":
      return { modeKey: null, modeName: null, ranked: true };
    case "other":
      return { modeKey: null, modeName: null, ranked: false };
    default:
      return { modeKey: null, modeName: null, ranked: null };
  }
}

/// Matches this app recorded that OpenDota's list does not have, in the
/// shape of an OpenDota row so one table holds both. What the live feed never
/// reports — hero damage, and assists or GPM in records older than those
/// fields — is null and shows as a dash.
///
/// Only matches inside the span OpenDota's list covers are added. A recorded
/// match older than that is most likely on OpenDota too, just past the last
/// one it returned, and listing it here would say otherwise.
function dtLocalOnly() {
  const listed = new Set(DOTA.matches.map((m) => String(m.matchId)));
  const since = DOTA.matches.length >= DT_LIMIT ? Math.min(...DOTA.matches.map((m) => m.startTime)) : 0;
  const rows = [];
  for (const h of state.history || []) {
    if (listed.has(String(h.matchid))) continue;
    const ended = Date.parse(h.date) / 1000;
    if (!Number.isFinite(ended)) continue;
    const duration = dtClockSeconds(h.duration);
    // History is dated when a match was saved; OpenDota dates a match by when
    // it began. Backing off the game clock puts both on the same timeline.
    const startTime = Math.round(ended - (duration || 0));
    if (startTime < since) continue;

    const assists = dtKnown(h.assists) ? h.assists : null;
    rows.push({
      matchId: String(h.matchid),
      local: true,
      tag: h.gameType,
      heroName: heroDisplayName(h.heroName),
      heroSlug: heroCleanName(h.heroName),
      startTime,
      durationSeconds: duration,
      won: dtKnown(h.won) ? h.won : null,
      abandoned: false,
      incomplete: !!h.incomplete,
      kills: h.kills,
      deaths: h.totalDeaths,
      assists,
      kda: assists === null ? null : (h.kills + assists) / Math.max(1, h.totalDeaths),
      lastHits: dtKnown(h.lastHits) ? h.lastHits : null,
      goldPerMin: dtKnown(h.gpm) ? h.gpm : null,
      xpPerMin: dtKnown(h.xpm) ? h.xpm : null,
      heroDamage: null,
      ...dtLocalKind(h.gameType),
    });
  }
  return rows;
}

function dtTimeline() {
  return [...DOTA.matches, ...dtLocalOnly()];
}

// ---------- Rendering ----------

function dtAvg(rows, key) {
  const vals = rows.map((m) => m[key]).filter(dtKnown);
  return vals.length ? Math.round(vals.reduce((a, b) => a + b, 0) / vals.length) : null;
}

/// The figures above the table, over every match in the timeline. Each one
/// averages only the matches that have that figure, so a recorded match
/// without GPM lowers nothing.
function dtStatsHtml(rows) {
  if (!rows.length) return "";

  // Oldest-first so the sparklines read left to right like a timeline.
  const chrono = [...rows].sort((a, b) => a.startTime - b.startTime);
  const decided = chrono.filter((m) => m.won === true || m.won === false);
  const wins = decided.filter((m) => m.won).length;
  const losses = decided.length - wins;
  const winRate = decided.length ? (wins / decided.length) * 100 : 0;
  const winSeries = decided.map((_, i) => {
    const w = decided.slice(Math.max(0, i - 9), i + 1);
    return (w.filter((m) => m.won).length / w.length) * 100;
  });

  const withKda = chrono.filter((m) => dtKnown(m.kda));
  const n = Math.max(1, withKda.length);
  const sum = (key) => withKda.reduce((a, m) => a + m[key], 0);
  const kda = (sum("kills") + sum("assists")) / Math.max(1, sum("deaths"));

  const gpm = dtAvg(chrono, "goldPerMin");
  const xpm = dtAvg(chrono, "xpPerMin");
  const lh = dtAvg(chrono, "lastHits");
  const local = rows.filter((m) => m.local).length;

  return railHtml([
    {
      label: "Win rate",
      value: decided.length ? `${winRate.toFixed(0)}%` : "—",
      tone: decided.length ? (winRate >= 50 ? "win" : "loss") : "",
      // Says where the matches came from. OpenDota's history alone is not
      // always complete, so a bare "of 50" read as "of everything you
      // played" when it was only "of what OpenDota holds".
      sub: local
        ? `${wins}W – ${losses}L · ${rows.length - local} OpenDota + ${local} tracked here`
        : `${wins}W – ${losses}L of ${rows.length} on OpenDota`,
      spark: sparkline(winSeries),
    },
    {
      label: "Avg KDA",
      value: withKda.length ? kda.toFixed(2) : "—",
      sub: withKda.length
        ? `${(sum("kills") / n).toFixed(1)} / ${(sum("deaths") / n).toFixed(1)} / ${(sum("assists") / n).toFixed(1)}`
        : "",
      spark: sparkline(withKda.map((m) => m.kda)),
    },
    {
      label: "Avg GPM",
      value: dtKnown(gpm) ? gpm : "—",
      sub: dtKnown(xpm) ? `${xpm} XPM` : "",
      spark: sparkline(chrono.map((m) => m.goldPerMin).filter(dtKnown)),
    },
    {
      label: "Avg last hits",
      value: dtKnown(lh) ? lh : "—",
      sub: "per match",
      spark: sparkline(chrono.map((m) => m.lastHits).filter(dtKnown)),
    },
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
            .map((id) => `<img src="${DOTA_ITEM_CDN}${id}.png" alt="" />`)
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
  if (key === "result") return m.abandoned ? 2 : m.won === true ? 0 : m.won === false ? 1 : null;
  return m[key];
}

function dtSorted(list) {
  const { sortKey, sortDir } = DOTA;
  const dir = sortDir === "asc" ? 1 : -1;
  return [...list].sort((a, b) => {
    const av = dtSortValue(a, sortKey);
    const bv = dtSortValue(b, sortKey);
    // A figure a match does not have sorts after every one it does, in
    // either direction — otherwise "highest GPM" opens on a row of dashes.
    if (!dtKnown(av) || !dtKnown(bv)) return dtKnown(av) ? -1 : dtKnown(bv) ? 1 : 0;
    if (typeof av === "string") return av.localeCompare(bv) * dir;
    return (av - bv) * dir;
  });
}

/// A figure, or a dash where the source never had it.
function dtCell(v, fmt = String) {
  return dtKnown(v) ? fmt(v) : "—";
}

/// The line under the hero name. A recorded match carries the tag it was
/// given in Sessions instead of OpenDota's mode and lobby.
function dtSubHtml(m) {
  if (!m.local) {
    return `${escapeHtml(m.modeName)} · ${escapeHtml(m.lobbyName)}${m.partySize && m.partySize > 1 ? ` · party ${m.partySize}` : ""}`;
  }
  const tag = gameTypeLabel(m.tag);
  return `${escapeHtml(tag === "Unspecified" ? "Mode unknown" : tag)} · <span class="src-local">tracked by TheTracker</span>${m.incomplete ? " · ended early" : ""}`;
}

function dtRowHtml(m) {
  const img = dtHeroImg(m.heroSlug);
  const open = !m.local && DOTA.open.has(m.matchId);
  const result = m.abandoned ? "other" : m.won === true ? "win" : m.won === false ? "loss" : "other";
  const resultText = m.abandoned ? "Left" : m.won === true ? "Win" : m.won === false ? "Loss" : "—";
  const kdaCls = !dtKnown(m.kda) ? "" : m.kda >= 4 ? "kda-good" : m.kda < 1.5 ? "kda-bad" : "";
  // OpenDota rows expand into the scoreboard; a recorded match has no
  // scoreboard, so it opens its full record in Sessions instead.
  const hook = m.local
    ? `class="${result} local" data-dt-local="${escapeHtml(m.matchId)}" title="Recorded by TheTracker. Click to open it in Sessions."`
    : `class="${result}" data-dt-toggle="${m.matchId}"`;

  return `
    <tr ${hook}>
      <td>
        <div class="cell-hero">
          ${img ? `<img src="${img}" alt="" />` : ""}
          <div style="min-width:0">
            <div class="cell-hero-name">${escapeHtml(m.heroName)}</div>
            <div class="cell-sub">${dtSubHtml(m)}</div>
          </div>
        </div>
      </td>
      <td><span class="res ${result}">${resultText}</span></td>
      <td class="num ${kdaCls}">${dtCell(m.kda, (v) => v.toFixed(2))}</td>
      <td class="num">${m.kills}</td>
      <td class="num">${m.deaths}</td>
      <td class="num">${dtCell(m.assists)}</td>
      <td class="num">${dtCell(m.lastHits)}</td>
      <td class="num">${dtCell(m.goldPerMin)}</td>
      <td class="num">${dtCell(m.xpPerMin)}</td>
      <td class="num">${dtCell(m.heroDamage, dtNum)}</td>
      <td class="num">${dtCell(m.durationSeconds, dtDuration)}</td>
      <td class="num cell-sub">${dtAgo(m.startTime)}</td>
    </tr>
    ${open ? `<tr class="detail-row"><td colspan="${DT_COLUMNS.length}">${dtDetailHtml(m.matchId)}</td></tr>` : ""}`;
}

function dtTableHtml(list) {
  return `
    <div class="table-scroll" tabindex="0" aria-label="Match history"><table class="dtable">
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
    </table></div>`;
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
  showToast(`Dota 2 connected to ${personaname || `account ${accountId}`}`);
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

  // Nothing fetched yet reads as loading, not as an empty account: dtRender
  // runs once before dtLoad is called, so `loading` is still false on the
  // first paint of every launch. Recorded matches wait for OpenDota too —
  // which of them it lacks is unknown until its list arrives.
  if (!DOTA.matches.length && (DOTA.loading || (!DOTA.loadedAt && !DOTA.error))) {
    root.innerHTML = `<div class="empty-state">Loading matches…</div>`;
    return;
  }

  const rows = dtTimeline();
  if (!rows.length) {
    root.innerHTML = `<div class="empty-state">No matches found for this account.</div>`;
    return;
  }
  const localCount = rows.filter((m) => m.local).length;

  // Mode and queue are separate facts about a match, so they get separate
  // filters. The old single row mixed them — picking "Ranked" hid every
  // ranked Turbo game, and picking "Turbo" hid whether it was ranked — which
  // is why the counts never matched what you actually played. A recorded
  // match whose tag does not say (ranked: null) is only under "All queues".
  const shown = rows.filter(
    (m) =>
      (DOTA.filter === "all" || m.modeKey === DOTA.filter) &&
      (DOTA.queue === "all" || (dtKnown(m.ranked) && (DOTA.queue === "ranked") === !!m.ranked))
  );

  // Only offer modes that appear in the loaded matches, so the row does not
  // list Ability Draft to someone who has never played it.
  const present = new Map();
  for (const m of rows) if (m.modeKey) present.set(m.modeKey, m.modeName);
  const modes = [["all", "All modes"], ...[...present].sort((a, b) => a[1].localeCompare(b[1]))];

  const queues = [
    ["all", `All queues (${rows.length})`],
    ["ranked", `Ranked (${rows.filter((m) => m.ranked === true).length})`],
    ["unranked", `Unranked (${rows.filter((m) => m.ranked === false).length})`],
  ];

  root.innerHTML = `
    ${dtStatsHtml(rows)}
    <div class="section-head">
      <div class="chip-row">
        ${modes
          .map(
            ([id, label]) =>
              `<button class="chip ${DOTA.filter === id ? "selected" : ""}" data-dt-filter="${escapeHtml(id)}">${escapeHtml(label)}</button>`
          )
          .join("")}
      </div>
      <button class="chip" id="dtRefresh" type="button">${DOTA.loading ? "Refreshing…" : "Refresh"}</button>
    </div>
    <div class="chip-row" style="margin-bottom:12px">
      ${queues
        .map(
          ([id, label]) =>
            `<button class="chip ${DOTA.queue === id ? "selected" : ""}" data-dt-queue="${id}">${label}</button>`
        )
        .join("")}
    </div>
    ${
      shown.length
        ? dtTableHtml(shown)
        : `<div class="empty-state">Nothing matches that combination in the last ${rows.length} games.</div>`
    }
    <p class="hint" style="margin-top:14px;max-width:76ch">
      Scoreboards, modes and queues come from OpenDota, whose record of an
      account is not always complete &mdash; Turbo games in particular can be
      missing from it entirely.
      ${
        localCount
          ? `Rows marked <span class="src-local">tracked by TheTracker</span> are matches it lacks, recorded by this app from Dota's own live feed. Click one to open its full record in <b>Sessions</b>.`
          : `Matches played while TheTracker is running are recorded from Dota's own live feed, and any that OpenDota lacks are added here.`
      }
    </p>`;

  root.querySelectorAll("[data-dt-filter]").forEach((el) =>
    el.addEventListener("click", () => {
      DOTA.filter = el.dataset.dtFilter;
      dtRender();
    })
  );
  root.querySelectorAll("[data-dt-queue]").forEach((el) =>
    el.addEventListener("click", () => {
      DOTA.queue = el.dataset.dtQueue;
      dtRender();
    })
  );
  root.querySelectorAll("[data-dt-toggle]").forEach((el) =>
    el.addEventListener("click", () => dtToggleMatch(Number(el.dataset.dtToggle)))
  );
  root.querySelectorAll("[data-dt-local]").forEach((el) =>
    el.addEventListener("click", () => {
      const id = el.dataset.dtLocal;
      state.openHistory.add(id);
      state.focusHistory = id;
      setView("history");
    })
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
