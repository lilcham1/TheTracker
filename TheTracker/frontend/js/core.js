// TheTracker UI core: the bridge to the backend, shared helpers, navigation
// and the live status that every page sits under. Plain JS, no build step.

// ---------- Backend bridge ----------

/// Every backend call. Rejects with an Error whose message is written for the
/// person using the app, so callers can show it as-is.
async function invoke(cmd, args) {
  let resp;
  try {
    resp = await fetch("/api/" + cmd, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(args || {}),
    });
  } catch (_) {
    throw new Error("TheTracker's backend isn't responding. Restart the app.");
  }
  let data = null;
  try {
    data = await resp.json();
  } catch (_) {
    /* an empty body is a valid reply */
  }
  if (!resp.ok) throw new Error((data && data.error) || "Something went wrong.");
  return data;
}

// ---------- Small helpers ----------

const HERO_CDN = "https://cdn.cloudflare.steamstatic.com/apps/dota2/images/dota_react/heroes/";
const ITEM_CDN = "https://cdn.cloudflare.steamstatic.com/apps/dota2/images/dota_react/items/";

const $ = (sel, root = document) => root.querySelector(sel);

function esc(s) {
  return String(s ?? "").replace(/[&<>"']/g, (c) => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" }[c]));
}

const known = (v) => v !== null && v !== undefined;
const dash = (v, fmt = String) => (known(v) ? fmt(v) : "–");

function heroSlug(raw) {
  if (!raw) return "";
  return raw.startsWith("npc_dota_hero_") ? raw.slice(14) : raw;
}

function heroImg(slug) {
  slug = heroSlug(slug);
  return slug ? `${HERO_CDN}${slug}.png` : "";
}

const HERO_NAMES = {
  antimage: "Anti-Mage", nevermore: "Shadow Fiend", windrunner: "Windranger", zuus: "Zeus",
  queenofpain: "Queen of Pain", skeleton_king: "Wraith King", rattletrap: "Clockwerk",
  furion: "Nature's Prophet", necrolyte: "Necrophos", obsidian_destroyer: "Outworld Destroyer",
  wisp: "Io", magnataur: "Magnus", centaur: "Centaur Warrunner", abyssal_underlord: "Underlord",
  keeper_of_the_light: "Keeper of the Light", doom_bringer: "Doom", shredder: "Timbersaw",
  treant: "Treant Protector", life_stealer: "Lifestealer", vengefulspirit: "Vengeful Spirit",
};

/// A readable hero name from Valve's internal one. The hero list from
/// OpenDota is preferred when it has loaded; this is the fallback.
function heroName(raw) {
  const slug = heroSlug(raw);
  if (!slug) return "Unknown hero";
  const h = S.heroBySlug && S.heroBySlug[slug];
  if (h) return h.name;
  return HERO_NAMES[slug] || slug.split("_").map((w) => (w ? w[0].toUpperCase() + w.slice(1) : w)).join(" ");
}

const ITEM_NAMES = {
  greater_crit: "Daedalus", sphere: "Linken's Sphere", ultimate_scepter: "Aghanim's Scepter",
  ultimate_scepter_2: "Aghanim's Blessing", aghanims_shard: "Aghanim's Shard", sheepstick: "Scythe of Vyse",
  skadi: "Eye of Skadi", heart: "Heart of Tarrasque", assault: "Assault Cuirass", blink: "Blink Dagger",
  boots: "Boots of Speed", travel_boots: "Boots of Travel", travel_boots_2: "Boots of Travel 2",
  refresher: "Refresher Orb", dagon_5: "Dagon", manta: "Manta Style", black_king_bar: "Black King Bar",
};

function itemName(key) {
  const cat = S.itemByKey && S.itemByKey[key];
  if (cat) return cat.name;
  return ITEM_NAMES[key] || key.split("_").map((w) => (w ? w[0].toUpperCase() + w.slice(1) : w)).join(" ");
}

function itemImgHtml(key, cls = "item") {
  return `<img class="${cls}" src="${ITEM_CDN}${esc(key)}.png" alt="${esc(itemName(key))}" title="${esc(itemName(key))}" loading="lazy" />`;
}

function heroImgHtml(slug, cls = "portrait") {
  const src = heroImg(slug);
  return src ? `<img class="${cls}" src="${esc(src)}" alt="" loading="lazy" />` : `<span class="${cls} blank"></span>`;
}

function imgHtml(url, cls) {
  return url ? `<img class="${cls}" src="${esc(url)}" alt="" loading="lazy" />` : `<span class="${cls} blank"></span>`;
}

function fmtClock(seconds) {
  if (!known(seconds)) return "–";
  const neg = seconds < 0;
  const abs = Math.floor(Math.abs(seconds));
  return `${neg ? "-" : ""}${Math.floor(abs / 60)}:${String(abs % 60).padStart(2, "0")}`;
}

function clockSeconds(text) {
  const m = /^(\d+):(\d{2})$/.exec(text || "");
  return m ? Number(m[1]) * 60 + Number(m[2]) : null;
}

function fmtNum(n) {
  if (!known(n)) return "–";
  if (n >= 1e6) return (n / 1e6).toFixed(1) + "M";
  if (n >= 10000) return (n / 1000).toFixed(1) + "k";
  return Number(n).toLocaleString();
}

const pct = (n, digits = 0) => (known(n) ? `${Number(n).toFixed(digits)}%` : "–");

function ago(unixSeconds) {
  if (!unixSeconds) return "";
  const diff = Math.floor(Date.now() / 1000 - unixSeconds);
  if (diff < 90) return "just now";
  if (diff < 3600) return `${Math.floor(diff / 60)} min ago`;
  if (diff < 86400) return `${Math.floor(diff / 3600)} h ago`;
  if (diff < 7 * 86400) return `${Math.floor(diff / 86400)} d ago`;
  return new Date(unixSeconds * 1000).toLocaleDateString(undefined, { month: "short", day: "numeric", year: diff > 300 * 86400 ? "numeric" : undefined });
}

function agoSecs(secs) {
  if (!known(secs)) return null;
  if (secs < 60) return "just now";
  if (secs < 3600) return `${Math.floor(secs / 60)} min ago`;
  return `${Math.floor(secs / 3600)} h ago`;
}

function fmtDate(iso) {
  const d = new Date(iso);
  if (isNaN(d)) return "";
  return d.toLocaleString(undefined, { month: "short", day: "numeric", hour: "2-digit", minute: "2-digit" });
}

const GAME_TYPES = [
  { id: "ranked", label: "Ranked" },
  { id: "all_pick", label: "All Pick" },
  { id: "turbo", label: "Turbo" },
  { id: "other", label: "Other" },
];

function gameTypeLabel(t) {
  if (t === "unranked") return "All Pick";
  const g = GAME_TYPES.find((g) => g.id === t);
  return g ? g.label : "Mode not known yet";
}

/// A trend chart for a small series, oldest first. Match-by-match numbers
/// are noisy, so longer series are drawn as a rolling average: the shape
/// shows where the figure is heading, not every spike. A dashed line marks
/// the overall average, and a caption says which way things have gone
/// lately. Returns "" under three points, where a line would say nothing.
function sparkline(values, { width = 240, height = 44, tone = "accent", trend = true, points = false } = {}) {
  values = values.filter(known);
  if (values.length < 3) return "";
  const n = values.length;
  const win = n >= 12 ? Math.max(3, Math.round(n / 8)) : 1;
  const series = values.map((_, i) => {
    const part = values.slice(Math.max(0, i - win + 1), i + 1);
    return part.reduce((a, v) => a + v, 0) / part.length;
  });
  const mean = values.reduce((a, v) => a + v, 0) / n;
  const min = Math.min(...series, mean), max = Math.max(...series, mean), span = max - min || 1;
  const step = (width - 2) / (n - 1);
  const y = (v) => height - 4 - ((v - min) / span) * (height - 9);
  const pts = series.map((v, i) => [1 + i * step, y(v)]);
  // A smooth curve: each point is a control point, the curve passes through
  // the midpoints between neighbours.
  let line = `M${pts[0][0].toFixed(1)},${pts[0][1].toFixed(1)}`;
  for (let i = 1; i < pts.length - 1; i++) {
    const mx = (pts[i][0] + pts[i + 1][0]) / 2, my = (pts[i][1] + pts[i + 1][1]) / 2;
    line += ` Q${pts[i][0].toFixed(1)},${pts[i][1].toFixed(1)} ${mx.toFixed(1)},${my.toFixed(1)}`;
  }
  const last = pts[pts.length - 1];
  line += ` L${last[0].toFixed(1)},${last[1].toFixed(1)}`;
  const area = `${line} L${last[0].toFixed(1)},${height} L${pts[0][0].toFixed(1)},${height} Z`;

  let caption = "";
  if (trend && n >= 8) {
    const q = Math.max(2, Math.round(n / 4));
    const avg = (xs) => xs.reduce((a, v) => a + v, 0) / xs.length;
    const recent = avg(values.slice(-q)), before = avg(values.slice(0, n - q));
    // A series that is already a percentage changes in points, not percent.
    const change = points ? recent - before : before ? ((recent - before) / Math.abs(before)) * 100 : 0;
    const dir = change >= 4 ? "up" : change <= -4 ? "down" : "flat";
    caption = `<span class="spark-trend ${dir}" title="Your last ${q} compared with the ${n - q} before them">${dir === "up" ? "▲" : dir === "down" ? "▼" : "•"} ${dir === "flat" ? "steady" : `${Math.abs(change).toFixed(0)}${points ? " points" : "%"} ${dir}`} <span class="muted">last ${q}</span></span>`;
  }
  return `<div class="spark-wrap"><svg class="spark ${tone}" viewBox="0 0 ${width} ${height}" aria-hidden="true" preserveAspectRatio="none">
    <path d="${area}" fill="currentColor" fill-opacity=".1" stroke="none" />
    <line x1="0" x2="${width}" y1="${y(mean).toFixed(1)}" y2="${y(mean).toFixed(1)}" stroke="currentColor" stroke-opacity=".35" stroke-width="1" stroke-dasharray="3 4" vector-effect="non-scaling-stroke" />
    <path d="${line}" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" vector-effect="non-scaling-stroke" /></svg>${caption}</div>`;
}

/// The run of results as a strip of marks, oldest on the left. `results` is
/// newest-first: true win, false loss, null unknown.
function formStrip(results, limit = 20) {
  const cells = results.slice(0, limit).reverse();
  if (!cells.length) return "";
  return `<div class="form" role="img" aria-label="Recent results, oldest first">${cells
    .map((w) => `<i class="${w === true ? "w" : w === false ? "l" : "u"}"></i>`)
    .join("")}</div>`;
}

function streakOf(results) {
  const decided = results.filter(known);
  if (!decided.length) return null;
  let n = 0;
  for (const r of decided) {
    if (r === decided[0]) n++;
    else break;
  }
  return { won: decided[0], n };
}

/// Figures in a row, separated by rules. Each entry: {label, value, sub, tone, extra}.
function statRow(entries) {
  return `<div class="stats">${entries
    .map(
      (e) => `<div class="stat">
        <div class="stat-label">${e.label}</div>
        <div class="stat-value ${e.tone || ""}">${e.value}</div>
        ${e.sub ? `<div class="stat-sub">${e.sub}</div>` : ""}
        ${e.extra || ""}
      </div>`
    )
    .join("")}</div>`;
}

function emptyState(title, body = "", action = "") {
  return `<div class="empty"><h3>${title}</h3>${body ? `<p>${body}</p>` : ""}${action}</div>`;
}

function loadingState(text = "Loading…") {
  return `<div class="empty"><div class="spinner" aria-hidden="true"></div><p>${esc(text)}</p></div>`;
}

function errorState(message, act = "refresh") {
  return `<div class="empty">
    <h3>That didn't load</h3><p>${esc(message)}</p>
    <button class="btn" data-act="${act}" type="button">Try again</button>
  </div>`;
}

/// Shown above data that came from the cache because the API is down.
function staleNote(r) {
  const d = r && r.data;
  if (!d || !d.stale) return "";
  return `<div class="note warn">Showing data from ${esc(ago(d.fetchedAt))}. ${esc(d.error || "The service couldn't be reached.")}</div>`;
}

let toastTimer = null;
function toast(message, tone = "") {
  const el = $("#toast");
  el.textContent = message;
  el.className = "toast " + tone;
  el.hidden = false;
  clearTimeout(toastTimer);
  toastTimer = setTimeout(() => (el.hidden = true), tone === "err" ? 6000 : 3200);
}

/// Runs an action, showing its error as a toast. Returns undefined on failure.
async function attempt(fn, okMessage) {
  try {
    const out = await fn();
    if (okMessage) toast(okMessage);
    return out === undefined ? true : out;
  } catch (e) {
    toast(e.message || String(e), "err");
    return undefined;
  }
}

// ---------- Icons ----------

const ICONS = {
  overview: '<path d="M3 13h7V3H3zM14 21h7V11h-7zM3 21h7v-5H3zM14 3v5h7V3z"/>',
  live: '<circle cx="12" cy="12" r="3.2"/><path d="M5.6 5.6a9 9 0 0 0 0 12.8M18.4 5.6a9 9 0 0 1 0 12.8"/>',
  matches: '<path d="M4 6h16M4 12h16M4 18h10"/>',
  heroes: '<circle cx="12" cy="8" r="4"/><path d="M4 21c0-4.4 3.6-7 8-7s8 2.6 8 7"/>',
  draft: '<path d="M4 4h7v7H4zM13 13h7v7h-7zM13 4h7v7h-7zM4 13h7v7H4z"/>',
  meta: '<path d="M4 20V10M10 20V4M16 20v-7M22 20H2"/>',
  builds: '<path d="M14.7 6.3a4 4 0 0 0-5.4 5.4L3 18l3 3 6.3-6.3a4 4 0 0 0 5.4-5.4l-2.6 2.6-2.4-2.4z"/>',
  sessions: '<circle cx="12" cy="12" r="9"/><path d="M12 7v5l3 2"/>',
  leaderboard: '<path d="M8 21V10M16 21V14M12 21V4M3 21h18"/>',
  settings: '<path d="M4 6h10M18 6h2M4 12h4M12 12h8M4 18h12"/><circle cx="16" cy="6" r="2"/><circle cx="10" cy="12" r="2"/><circle cx="18" cy="18" r="2"/>',
  overlay: '<rect x="3" y="4" width="18" height="14" rx="2"/><path d="M7 9h5M7 13h3"/>',
  refresh: '<path d="M20 11a8 8 0 1 0-2.3 5.7M20 5v6h-6"/>',
  today: '<rect x="3" y="5" width="18" height="16" rx="2"/><path d="M8 3v4M16 3v4M3 10h18"/>',
  star: '<path d="M12 3l2.8 5.7 6.2.9-4.5 4.4 1.1 6.2L12 17.3 6.4 20.2l1.1-6.2L3 9.6l6.2-.9z"/>',
};

function icon(name) {
  return `<svg viewBox="0 0 24 24" width="18" height="18" fill="none" stroke="currentColor" stroke-width="1.7" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">${ICONS[name] || ""}</svg>`;
}

function paintIcons(root = document) {
  root.querySelectorAll("[data-ico]").forEach((el) => {
    if (!el.firstChild) el.innerHTML = icon(el.dataset.ico);
  });
}

// ---------- State ----------

const S = {
  boot: null,
  view: null,
  params: {},
  game: "dota",
  live: null,
  history: [],
  heroBySlug: null,
  itemByKey: null,
  update: null,
};

// ---------- Resources ----------
//
// A resource is one piece of backend data a page depends on. It remembers
// what it loaded, so moving between pages does not refetch, and it never has
// two requests in flight for the same thing.

const RES = {};

function resource(name, cmd, { args = () => ({}), ttl = 120000 } = {}) {
  const r = {
    name, data: null, error: null, loading: false, at: 0, key: null, pending: null,
    /// Loads if missing, stale or forced. Resolves when settled; never throws.
    load(force = false, extra = {}) {
      const a = { ...args(), ...extra };
      const key = JSON.stringify(a);
      const fresh = r.data && r.key === key && Date.now() - r.at < ttl;
      if (!force && fresh) return Promise.resolve(r);
      if (r.pending && r.pendingKey === key && !force) return r.pending;
      if (r.key !== key) r.data = null; // different account or filter: old data would mislead
      r.loading = true;
      r.error = null;
      r.pendingKey = key;
      r.pending = invoke(cmd, force ? { ...a, force: true } : a)
        .then((data) => {
          r.data = data;
          r.key = key;
          r.at = Date.now();
        })
        .catch((e) => {
          r.error = e.message || String(e);
        })
        .finally(() => {
          r.loading = false;
          r.pending = null;
          rerender();
        });
      return r.pending;
    },
    clear() {
      r.data = null;
      r.error = null;
      r.at = 0;
      r.key = null;
    },
  };
  RES[name] = r;
  return r;
}

/// Standard page states for a resource: loading, failed, or null to go on.
function gate(r, loadingText) {
  if (r.data) return null;
  if (r.error) return errorState(r.error);
  return loadingState(loadingText);
}

// ---------- Views and navigation ----------

const VIEWS = {};
const NAV = { dota: [], deadlock: [], cs2: [], overwatch: [] };

/// Registers a page. `render` returns HTML for the current state; `load`
/// starts whatever it needs and is called on entry and on Refresh.
function view(id, def) {
  VIEWS[id] = { id, ...def };
  if (def.nav) NAV[def.game].push(id);
}

function renderNav() {
  $("#nav").innerHTML = NAV[S.game]
    .map((id) => {
      const v = VIEWS[id];
      const liveDot = id === "live" && S.live && S.live.live ? `<span class="live-dot" title="A match is running"></span>` : "";
      return `<button class="nav-item ${S.view === id ? "on" : ""}" data-act="go" data-view="${id}" type="button">
        <span class="ico">${icon(v.icon)}</span><span>${esc(v.title)}</span>${liveDot}</button>`;
    })
    .join("");
  // Only the games the player switched on get a tab, and with a single game
  // there is nothing to switch between.
  const games = enabledGames();
  const switcher = $("#games");
  switcher.hidden = games.length < 2;
  switcher.style.gridTemplateColumns = `repeat(${Math.min(games.length, 2)}, 1fr)`;
  switcher.innerHTML = games
    .map((g) => `<button class="game ${g === S.game && S.view !== "today" ? "on" : ""}" data-act="game" data-game="${g}" type="button">${GAMES[g].label}</button>`)
    .join("");
  const settingsBtn = $('.side-foot [data-view="settings"]');
  if (settingsBtn) settingsBtn.classList.toggle("on", S.view === "settings");
  const todayBtn = $("#todayBtn");
  if (todayBtn) todayBtn.classList.toggle("on", S.view === "today");
  // The Today page belongs to no game: no game's colour, no Dota controls.
  $("#shell").dataset.game = S.view === "today" ? "today" : S.game;
}

const GAMES = {
  dota: { label: "Dota 2", home: "overview" },
  deadlock: { label: "Deadlock", home: "dl-overview" },
  cs2: { label: "CS2", home: "cs-overview" },
  overwatch: { label: "Overwatch", home: "ow-overview" },
};
const LAST_VIEW = { dota: "overview", deadlock: "dl-overview", cs2: "cs-overview", overwatch: "ow-overview" };

function enabledGames() {
  const g = (S.boot && S.boot.prefs.games) || {};
  const on = Object.keys(GAMES).filter((k) => g[k]);
  return on.length ? on : ["dota"];
}

function go(id, params = {}) {
  if (!VIEWS[id]) id = "overview";
  // A page of a game that is switched off goes to one that is on.
  if (VIEWS[id].game && !enabledGames().includes(VIEWS[id].game)) {
    id = GAMES[enabledGames()[0]].home;
    params = {};
  }
  const v = VIEWS[id];
  $("#shell").classList.toggle("bare", !!v.bare);
  S.view = id;
  S.params = params;
  if (v.game) {
    S.game = v.game;
    LAST_VIEW[v.game] = id;
  }
  lastHtml = null;
  renderNav();
  $("#viewTitle").textContent = v.title;
  rerender();
  if (v.load) v.load(false);
  $("#page").scrollTop = 0;
  try {
    localStorage.setItem("tt.view", id);
  } catch (_) {
    /* storage is a convenience */
  }
}

let lastHtml = null;
let renderQueued = false;

/// Redraws the current page, coalescing bursts of requests, and only if its markup
/// changed — so a poll that changes nothing never disturbs a click, a
/// selection or an open menu.
function rerender() {
  if (renderQueued) return;
  renderQueued = true;
  // A timer, not an animation frame: frames stop while the window is hidden
  // in the tray, and a page must be current the moment it is shown again.
  setTimeout(() => {
    renderQueued = false;
    const v = VIEWS[S.view];
    if (!v) return;
    let html;
    try {
      html = v.render();
    } catch (e) {
      console.error(e);
      html = `<div class="empty"><h3>This page failed to draw</h3><pre class="trace">${esc((e && e.stack) || e)}</pre></div>`;
    }
    $("#viewSub").textContent = (typeof v.sub === "function" ? v.sub() : v.sub) || "";
    if (html === lastHtml) return;

    // Keep what has been typed, and where the caret is, across a redraw.
    const page = $("#page");
    const active = document.activeElement;
    const focus = active && page.contains(active) && active.id ? { id: active.id, start: active.selectionStart, end: active.selectionEnd } : null;
    const typed = new Map();
    page.querySelectorAll("input[id], textarea[id]").forEach((el) => {
      if (!["checkbox", "radio", "range"].includes(el.type)) typed.set(el.id, el.value);
    });
    const scroll = page.scrollTop;
    page.innerHTML = html;
    lastHtml = html;
    paintIcons(page);
    page.scrollTop = scroll;
    for (const [id, value] of typed) {
      const el = document.getElementById(id);
      if (el && el.value !== value) el.value = value;
    }
    if (focus) {
      const el = document.getElementById(focus.id);
      if (el) {
        el.focus();
        try {
          el.setSelectionRange(focus.start, focus.end);
        } catch (_) {
          /* not a text field */
        }
      }
    }
  }, 0);
}

// ---------- Actions ----------
//
// One click listener for the whole app. Buttons name what they do with
// data-act; handlers are registered here. Nothing is wired per render, so a
// redraw can never leave a dead button behind.

const ACTIONS = {};
const act = (name, fn) => (ACTIONS[name] = fn);

document.addEventListener("click", (e) => {
  const el = e.target.closest("[data-act]");
  if (!el || el.disabled) return;
  const fn = ACTIONS[el.dataset.act];
  if (fn) {
    e.preventDefault();
    fn(el, e);
  }
});

const CHANGES = {};
const onChange = (name, fn) => (CHANGES[name] = fn);

for (const type of ["change", "input"]) {
  document.addEventListener(type, (e) => {
    const el = e.target.closest(`[data-${type}]`);
    if (!el) return;
    const fn = CHANGES[el.dataset[type]];
    if (fn) fn(el, e);
  });
}

document.addEventListener("keydown", (e) => {
  if (e.key === "Enter") {
    const el = e.target.closest("[data-enter]");
    if (el && ACTIONS[el.dataset.enter]) {
      e.preventDefault();
      ACTIONS[el.dataset.enter](el, e);
    }
  }
  if (e.key === "F5" || (e.ctrlKey && e.key.toLowerCase() === "r")) {
    e.preventDefault();
    ACTIONS.refresh();
  }
});

// A portrait that fails to load is hidden rather than shown as a broken
// image. Capture phase, because error events do not bubble.
document.addEventListener(
  "error",
  (e) => {
    if (e.target.tagName === "IMG") e.target.classList.add("broken");
  },
  true
);

act("go", (el) => go(el.dataset.view, el.dataset.params ? JSON.parse(el.dataset.params) : {}));
act("game", (el) => go(LAST_VIEW[el.dataset.game]));
act("open-url", (el) => attempt(() => invoke("open_url", { url: el.dataset.url })));

act("refresh", async () => {
  const v = VIEWS[S.view];
  const btn = $("#refreshBtn");
  btn.classList.add("busy");
  try {
    if (v && v.load) await v.load(true);
    else rerender();
  } finally {
    btn.classList.remove("busy");
  }
});

act("toggle-tracking", async () => {
  const on = !(S.live && S.live.trackingEnabled);
  const live = await attempt(() => invoke("set_tracking", { enabled: on }));
  if (live) {
    applyLive(live);
    toast(on ? "Recording matches again." : "Recording paused. Matches won't be saved until you resume.");
  }
});

act("toggle-overlay", async () => {
  const visible = await invoke("overlay_visible").catch(() => false);
  const ok = await attempt(() => invoke(visible ? "overlay_hide" : "overlay_show"));
  if (ok) {
    S.overlayVisible = !visible;
    paintTop();
    if (!visible && !(S.live && S.live.live)) {
      toast("Overlay is on. It stays empty until a reminder is due in a match.");
    }
  }
});

// ---------- Live status ----------

function dotaConnected() {
  const age = S.live && S.live.gsiAgeSecs;
  return known(age) && age < 45;
}

function paintTop() {
  const live = S.live;
  const dota = $("#dotaPill");
  const connected = dotaConnected();
  dota.classList.toggle("ok", connected);
  dota.classList.toggle("bad", !!(live && live.serverError));
  $("#dotaPillText").textContent = live && live.serverError ? "Live feed down" : connected ? "Dota connected" : "Dota not running";
  const heard = live ? agoSecs(live.gsiAgeSecs) : null;
  dota.title = live && live.serverError
    ? live.serverError
    : connected
      ? "Dota is sending data to TheTracker"
      : heard
        ? `Dota isn't sending data. Last heard ${heard}.`
        : "Nothing from Dota since TheTracker started. Click for details.";

  const track = $("#trackPill");
  const on = !live || live.trackingEnabled;
  track.classList.toggle("ok", on);
  track.classList.toggle("warn", !on);
  $("#trackPillText").textContent = on ? "Recording" : "Paused";
  track.setAttribute("aria-pressed", String(on));

  $("#overlayPill").classList.toggle("ok", !!S.overlayVisible);

  const sync = S.sync;
  const line = $("#syncLine");
  const auth = S.boot && S.boot.auth;
  if (!auth || !auth.signedIn) {
    line.textContent = "Saved on this PC";
    line.title = auth && auth.steam ? auth.lastError || "The shared leaderboard isn't connected." : "Your matches are stored locally. Sign in with Steam under Settings to publish to the leaderboard.";
  } else if (sync && sync.pending) {
    line.textContent = `Syncing ${sync.pending}…`;
    line.title = "";
  } else if (sync && sync.lastError) {
    line.textContent = "Sync problem";
    line.title = sync.lastError;
  } else {
    line.textContent = "Synced to your account";
    line.title = sync && sync.lastSync ? `Last sync ${sync.lastSync}` : "";
  }
}

let liveSig = "";
function applyLive(live) {
  const wasEnded = S.live && S.live.current && S.live.current.ended;
  const wasLive = S.live && S.live.live;
  S.live = live;
  paintTop();
  if (!!wasLive !== !!live.live) renderNav();

  // A match just ended: the session list has a new entry.
  if (live.current && live.current.ended && !wasEnded) {
    loadHistory().then(rerender);
    RES.insights && RES.insights.clear();
  }
  // Only pages that show live data are redrawn on a tick, and only when
  // something they show has changed.
  const v = VIEWS[S.view];
  if (v && v.live) {
    const sig = JSON.stringify(live);
    if (sig !== liveSig) {
      liveSig = sig;
      rerender();
    }
  }
}

async function pollLive() {
  try {
    applyLive(await invoke("get_live"));
  } catch (_) {
    /* the next tick will try again */
  }
}

async function pollSlow() {
  try {
    S.sync = await invoke("sync_status");
    S.overlayVisible = await invoke("overlay_visible");
    paintTop();
  } catch (_) {
    /* cosmetic */
  }
}

async function loadHistory() {
  try {
    S.history = await invoke("get_history");
  } catch (_) {
    /* keep the last copy */
  }
  return S.history;
}

// ---------- Banners ----------

/// App-wide notices above the page: an update, the start-with-Windows
/// question, a live-feed problem. Each is dismissible or actionable.
function renderBanners() {
  const out = [];
  const b = S.boot;
  if (!b) return;

  const gsi = b.gsi;
  if (gsi && gsi.listenerNotice) {
    out.push(`<div class="banner warn"><span>${esc(gsi.listenerNotice)}</span></div>`);
  }
  if (S.update && S.update.available && !S.updateDismissed) {
    out.push(`<div class="banner">
      <span><b>Version ${esc(S.update.version)} is available.</b> ${esc((S.update.notes || "").split("\n")[0].slice(0, 160))}</span>
      <button class="btn small" data-act="install-update" type="button" ${S.updating ? "disabled" : ""}>${S.updating ? "Downloading…" : "Update and restart"}</button>
      <button class="link" data-act="dismiss-update" type="button">Later</button>
    </div>`);
  }
  const bg = b.background;
  // Only live tracking needs the app running, so only games with a live
  // feed raise the question.
  const liveGames = b.prefs.games.dota || b.prefs.games.cs2;
  if (bg && liveGames && b.prefs.games.chosen && bg.trayAvailable && !bg.startWithWindows && !bg.autostartAsked) {
    out.push(`<div class="banner">
      <span><b>TheTracker only records matches while it's running.</b> Start it with Windows and it's always there when you play. It opens in the tray, not on screen.</span>
      <button class="btn small" data-act="autostart-yes" type="button">Start with Windows</button>
      <button class="link" data-act="autostart-no" type="button">Not now</button>
    </div>`);
  }
  const html = out.join("");
  const host = $("#banners");
  if (host.dataset.html !== html) {
    host.innerHTML = html;
    host.dataset.html = html;
  }
}

act("autostart-yes", async () => {
  const bg = await attempt(() => invoke("set_start_with_windows", { enabled: true }), "TheTracker will start with Windows, in the tray.");
  if (bg) S.boot.background = bg;
  renderBanners();
  rerender();
});

act("autostart-no", async () => {
  const bg = await attempt(() => invoke("dismiss_autostart_prompt"));
  if (bg) S.boot.background = bg;
  renderBanners();
});

act("dismiss-update", () => {
  S.updateDismissed = true;
  renderBanners();
});

act("install-update", async () => {
  if (S.live && S.live.live && !S.live.simulating) {
    toast("A match is running. Update once it has finished, so it gets recorded.", "err");
    return;
  }
  S.updating = true;
  renderBanners();
  rerender();
  await attempt(() => invoke("install_update"));
  S.updating = false;
  renderBanners();
  rerender();
});

async function checkForUpdate(quiet = true) {
  try {
    S.update = await invoke("check_for_update");
    S.updateError = null;
  } catch (e) {
    S.updateError = e.message;
    if (!quiet) toast(e.message, "err");
  }
  S.updateChecked = Date.now();
  renderBanners();
  rerender();
}
