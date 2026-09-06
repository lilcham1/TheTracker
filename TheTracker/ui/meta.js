// Meta pages: which heroes are strong right now, and which way they moved.
//
// The shape of these pages is borrowed from dota2protracker and Statlocker —
// heroes grouped and ranked, a trend you can read at a glance, items that
// are rising. None of the numbers are theirs: everything here is computed
// in meta.rs from OpenDota and the community Deadlock API. See that file
// for why the line is drawn where it is.

const META = {
  dota: { data: null, loadedAt: 0, loading: false, error: null, role: "all", sort: "winRate" },
  dl: { data: null, loadedAt: 0, loading: false, error: null, tab: "heroes", sort: "winRate" },
};

// The backend caches the expensive aggregate calls for thirty minutes. Keep
// the browser cache on the same schedule; before this, opening Meta once
// meant it silently stayed stale for the rest of the app session.
const META_CACHE_MS = 30 * 60 * 1000;

function metaUpdatedAt(timestamp) {
  if (!timestamp) return "Updated now";
  const minutes = Math.max(0, Math.floor((Date.now() - timestamp) / 60000));
  return minutes ? `Updated ${minutes}m ago` : "Updated just now";
}

const META_SORTS = [
  ["winRate", "Win rate"],
  ["pickRate", "Picked"],
  ["trend", "Trending"],
];

function metaPct(n, digits = 1) {
  return `${n.toFixed(digits)}%`;
}

function metaCount(n) {
  if (n >= 1000000) return `${(n / 1000000).toFixed(1)}M`;
  if (n >= 1000) return `${(n / 1000).toFixed(n >= 10000 ? 0 : 1)}K`;
  return String(n);
}

/// A signed win-rate move, shown only when it is big enough to mean
/// anything. Under a third of a point is noise in a sample this size, and
/// an arrow on every row would make the ones that matter invisible.
function metaTrend(delta) {
  if (delta === null || delta === undefined || Math.abs(delta) < 0.35) {
    return `<span class="meta-trend flat">—</span>`;
  }
  const up = delta > 0;
  return `<span class="meta-trend ${up ? "up" : "down"}">${up ? "▲" : "▼"} ${Math.abs(delta).toFixed(1)}</span>`;
}

/// Win rate drives the bar, but a bar from 0–100% would put every hero in
/// the middle: real win rates live between about 42% and 58%. The scale is
/// clamped to that band so the differences that matter are visible.
function metaBar(winRate) {
  const pos = Math.max(0, Math.min(1, (winRate - 42) / 16)) * 100;
  const tone = winRate >= 52 ? "good" : winRate >= 48 ? "even" : "bad";
  return `<div class="meta-bar"><div class="meta-bar-fill ${tone}" style="width:${pos}%"></div></div>`;
}

// ---------- Dota ----------

async function loadDotaMeta(force = false) {
  if (META.dota.loading) return;
  if (META.dota.data && !force && Date.now() - META.dota.loadedAt < META_CACHE_MS) return;
  META.dota.loading = true;
  META.dota.error = null;
  renderDotaMeta();
  try {
    META.dota.data = await invoke("dota_meta");
    META.dota.loadedAt = Date.now();
  } catch (e) {
    META.dota.error = String(e);
  }
  META.dota.loading = false;
  renderDotaMeta();
}

/// Descending on every column here — win rate, pick rate and trend all read
/// "most first". A missing value sorts to the bottom rather than the top.
function metaSorted(list, key) {
  return [...list].sort((a, b) => (b[key] ?? 0) - (a[key] ?? 0));
}

function renderDotaMeta() {
  const root = document.getElementById("tab-dotameta");
  if (!root) return;

  if (META.dota.error) {
    root.innerHTML = `
      <section class="home-section" style="padding-top:0">
        <div class="home-head"><h2 class="home-title">Couldn't load the meta</h2></div>
        <p class="hint">${escapeHtml(META.dota.error)}</p>
        <button class="btn btn-secondary" style="margin-top:12px" data-meta-retry type="button">Try again</button>
      </section>`;
    root.querySelector("[data-meta-retry]").addEventListener("click", () => loadDotaMeta(true));
    return;
  }

  const d = META.dota.data;
  if (!d) {
    root.innerHTML = `<div class="empty-state">${META.dota.loading ? "Reading the meta…" : ""}</div>`;
    return;
  }

  const role = META.dota.role;
  const pool = role === "all" ? d.heroes : d.heroes.filter((h) => h.roles.includes(role));
  const rows = metaSorted(pool, META.dota.sort).slice(0, 40);

  // Movers are taken from heroes with a real sample, so a hero picked twice
  // last week cannot top the list on a fluke.
  const solid = d.heroes.filter((h) => h.pickRate >= 1.5);
  const rising = metaSorted(solid, "trend").slice(0, 5);
  const falling = [...solid].sort((a, b) => a.trend - b.trend).slice(0, 5);
  const contested = [...d.heroes].sort((a, b) => b.pickRate - a.pickRate).slice(0, 5);

  const moverRow = (h) => `
    <div class="meta-mover static">
      <img class="hero-portrait" src="${DOTA_HERO_CDN}${escapeHtml(h.slug)}.png" alt="" loading="lazy" decoding="async" />
      <span class="meta-mover-name">${escapeHtml(h.name)}</span>
      <span class="meta-mover-wr">${metaPct(h.winRate)}</span>
      ${metaTrend(h.trend)}
    </div>`;

  root.innerHTML = `
    <section class="home-section" style="padding-top:0">
      <div class="home-head">
        <h2 class="home-title">The meta right now</h2>
        <div class="home-meta">public matches · ${metaUpdatedAt(META.dota.loadedAt)}</div>
        <button class="link-btn" data-meta-retry type="button">${META.dota.loading ? "Refreshing…" : "Refresh"}</button>
      </div>
      ${railHtml([
        { label: "Sample", value: metaCount(d.matches), sub: "matches analysed" },
        { label: "Heroes", value: String(d.heroes.length), sub: "with a pick sample" },
        {
          label: "Most contested",
          value: escapeHtml(contested[0] ? contested[0].name : "—"),
          sub: contested[0] ? `${metaPct(contested[0].pickRate, 0)} of matches` : "",
        },
      ])}
      <p class="hint" style="margin-top:12px;max-width:74ch">
        Picks and wins across public matches, from OpenDota. Trend compares
        the recent half of the sample window against the earlier half, so a
        hero has to move for several days before it shows.
      </p>
    </section>

    <section class="home-section">
      <div class="home-head"><h2 class="home-title">On the way up</h2><div class="home-meta">win rate, last few days</div></div>
      <div class="meta-movers">${rising.map(moverRow).join("")}</div>
    </section>

    <section class="home-section">
      <div class="home-head"><h2 class="home-title">On the way down</h2><div class="home-meta">win rate, last few days</div></div>
      <div class="meta-movers">${falling.map(moverRow).join("")}</div>
    </section>

    <section class="home-section">
      <div class="home-head">
        <h2 class="home-title">Every hero</h2>
        <div class="home-meta">${pool.length} shown</div>
      </div>

      <div class="chip-row" style="margin-bottom:10px">
        <button class="chip ${role === "all" ? "selected" : ""}" data-meta-role="all" type="button">All roles</button>
        ${d.roles
          .map(
            (r) =>
              `<button class="chip ${role === r ? "selected" : ""}" data-meta-role="${escapeHtml(r)}" type="button">${escapeHtml(r)}</button>`
          )
          .join("")}
      </div>
      <div class="chip-row" style="margin-bottom:14px">
        ${META_SORTS.map(
          ([k, label]) =>
            `<button class="chip ${META.dota.sort === k ? "selected" : ""}" data-meta-sort="${k}" type="button">${label}</button>`
        ).join("")}
      </div>

      <div class="meta-head">
        <span></span><span>Hero</span><span class="num">Win rate</span><span></span>
        <span class="num">Picked</span><span class="num">Divine+</span><span class="num">Pro</span><span class="num">Trend</span>
      </div>
      ${rows
        .map(
          (h) => `
        <div class="meta-row static">
          <img class="hero-portrait" src="${DOTA_HERO_CDN}${escapeHtml(h.slug)}.png" alt="" loading="lazy" decoding="async" />
          <div class="meta-name">
            ${escapeHtml(h.name)}
            <span class="meta-roles">${h.roles.slice(0, 2).map(escapeHtml).join(" · ")}</span>
          </div>
          <span class="num meta-wr ${h.winRate >= 50 ? "good" : "bad"}">${metaPct(h.winRate)}</span>
          ${metaBar(h.winRate)}
          <span class="num">${metaPct(h.pickRate, 1)}</span>
          <span class="num">${h.highWinRate === null || h.highWinRate === undefined ? "—" : metaPct(h.highWinRate)}</span>
          <span class="num">${h.proPicks ? `${h.proPicks}p / ${h.proBans}b` : "—"}</span>
          <span class="num">${metaTrend(h.trend)}</span>
        </div>`
        )
        .join("")}
      <p class="hint" style="margin-top:12px;max-width:74ch">
        Roles are Valve's own tags, not positions one through five — OpenDota
        does not publish position data, and guessing it would be worse than
        saying so. <b>Divine+</b> is the highest rank bracket with a sample.
      </p>
    </section>`;

  root.querySelectorAll("[data-meta-role]").forEach((el) =>
    el.addEventListener("click", () => {
      META.dota.role = el.dataset.metaRole;
      renderDotaMeta();
    })
  );
  root.querySelectorAll("[data-meta-sort]").forEach((el) =>
    el.addEventListener("click", () => {
      META.dota.sort = el.dataset.metaSort;
      renderDotaMeta();
    })
  );
  root.querySelectorAll("[data-meta-retry]").forEach((el) =>
    el.addEventListener("click", () => loadDotaMeta(true))
  );
}

// ---------- Deadlock ----------

async function loadDeadlockMeta(force = false) {
  if (META.dl.loading) return;
  if (META.dl.data && !force && Date.now() - META.dl.loadedAt < META_CACHE_MS) return;
  META.dl.loading = true;
  META.dl.error = null;
  renderDeadlockMeta();
  try {
    META.dl.data = await invoke("deadlock_meta");
    META.dl.loadedAt = Date.now();
  } catch (e) {
    META.dl.error = String(e);
  }
  META.dl.loading = false;
  renderDeadlockMeta();
}

function renderDeadlockMeta() {
  const root = document.getElementById("tab-dlmeta");
  if (!root) return;

  if (META.dl.error) {
    root.innerHTML = `
      <section class="home-section" style="padding-top:0">
        <div class="home-head"><h2 class="home-title">Couldn't load the meta</h2></div>
        <p class="hint">${escapeHtml(META.dl.error)}</p>
        <button class="btn btn-secondary" style="margin-top:12px" data-meta-retry type="button">Try again</button>
      </section>`;
    root.querySelector("[data-meta-retry]").addEventListener("click", () => loadDeadlockMeta(true));
    return;
  }

  const d = META.dl.data;
  if (!d) {
    root.innerHTML = `<div class="empty-state">${META.dl.loading ? "Reading the meta…" : ""}</div>`;
    return;
  }

  const heroes = [...d.heroes].sort((a, b) =>
    META.dl.sort === "pickRate" ? b.pickRate - a.pickRate : b.winRate - a.winRate
  );
  const top = heroes[0];
  const contested = [...d.heroes].sort((a, b) => b.pickRate - a.pickRate)[0];

  // Ranked by win rate, but only once an item is common enough to mean
  // something — a niche pick in a handful of games would otherwise top the
  // table. Share is measured against the most-bought item, because the item
  // endpoint samples a wider window than the hero one and there is no honest
  // match count to divide by.
  const items = d.items.filter((i) => i.share >= 12).slice(0, 24);

  const showingItems = META.dl.tab === "items";

  root.innerHTML = `
    <section class="home-section" style="padding-top:0">
      <div class="home-head">
        <h2 class="home-title">The meta right now</h2>
        <div class="home-meta">ranked matches · ${metaUpdatedAt(META.dl.loadedAt)}</div>
        <button class="link-btn" data-meta-retry type="button">${META.dl.loading ? "Refreshing…" : "Refresh"}</button>
      </div>
      ${railHtml([
        { label: "Sample", value: metaCount(d.matches), sub: "matches analysed" },
        { label: "Strongest", value: escapeHtml(top ? top.name : "—"), sub: top ? metaPct(top.winRate) + " win rate" : "" },
        {
          label: "Most picked",
          value: escapeHtml(contested ? contested.name : "—"),
          sub: contested ? `${metaPct(contested.pickRate, 0)} of matches` : "",
        },
      ])}
      <p class="hint" style="margin-top:12px;max-width:74ch">
        From the community Deadlock API, which is where every Deadlock number
        in this app comes from — Valve publishes no feed of its own.
      </p>
    </section>

    <section class="home-section">
      <div class="home-head">
        <h2 class="home-title">${showingItems ? "Items" : "Heroes"}</h2>
        <div class="home-meta">${showingItems ? `${items.length} shown` : `${heroes.length} heroes`}</div>
      </div>

      <div class="chip-row" style="margin-bottom:14px">
        <button class="chip ${!showingItems ? "selected" : ""}" data-dlmeta-tab="heroes" type="button">Heroes</button>
        <button class="chip ${showingItems ? "selected" : ""}" data-dlmeta-tab="items" type="button">Items</button>
        ${
          showingItems
            ? ""
            : `<span style="flex:1"></span>
               <button class="chip ${META.dl.sort === "winRate" ? "selected" : ""}" data-dlmeta-sort="winRate" type="button">Win rate</button>
               <button class="chip ${META.dl.sort === "pickRate" ? "selected" : ""}" data-dlmeta-sort="pickRate" type="button">Picked</button>`
        }
      </div>

      ${
        showingItems
          ? `<div class="meta-head items">
               <span>Item</span><span class="num">Win rate</span><span></span>
               <span class="num">Matches</span><span class="num">Bought</span>
             </div>
             ${items
               .map(
                 (i) => `
               <div class="meta-row items">
                 <div class="meta-name">${escapeHtml(i.name)}</div>
                 <span class="num meta-wr ${i.winRate >= 50 ? "good" : "bad"}">${metaPct(i.winRate)}</span>
                 ${metaBar(i.winRate)}
                 <span class="num">${metaCount(i.matches)}</span>
                 <span class="num">${i.buyMinute ? `${i.buyMinute.toFixed(0)} min` : "—"}</span>
               </div>`
               )
               .join("")}
             <p class="hint" style="margin-top:12px;max-width:74ch">
               <b>Bought</b> is the average minute the item goes down, which the
               Deadlock API publishes and the Dota side has no equivalent for.
               <b>Matches</b> is a raw count, not a percentage: the item
               endpoint samples a wider window than the hero one, so there is
               no honest figure to divide it by. Only the common items are
               listed &mdash; anything under an eighth as popular as the
               busiest item is left out.
             </p>`
          : `<div class="meta-head dl">
               <span></span><span>Hero</span><span class="num">Win rate</span><span></span>
               <span class="num">Picked</span><span class="num">KDA</span><span class="num">Souls</span>
             </div>
             ${heroes
               .map(
                 (h) => `
               <div class="meta-row dl" data-dlmeta-hero="${escapeHtml(h.name)}">
                 ${h.image ? `<img class="dl-hero-img sm" src="${escapeHtml(h.image)}" alt="" loading="lazy" decoding="async" />` : `<div class="dl-hero-img sm"></div>`}
                 <div class="meta-name">${escapeHtml(h.name)}</div>
                 <span class="num meta-wr ${h.winRate >= 50 ? "good" : "bad"}">${metaPct(h.winRate)}</span>
                 ${metaBar(h.winRate)}
                 <span class="num">${metaPct(h.pickRate, 0)}</span>
                 <span class="num">${h.kda.toFixed(2)}</span>
                 <span class="num">${dlFmtSouls(h.avgSouls)}</span>
               </div>`
               )
               .join("")}`
      }
    </section>`;

  root.querySelectorAll("[data-dlmeta-tab]").forEach((el) =>
    el.addEventListener("click", () => {
      META.dl.tab = el.dataset.dlmetaTab;
      renderDeadlockMeta();
    })
  );
  root.querySelectorAll("[data-dlmeta-sort]").forEach((el) =>
    el.addEventListener("click", () => {
      META.dl.sort = el.dataset.dlmetaSort;
      renderDeadlockMeta();
    })
  );
  root.querySelectorAll("[data-meta-retry]").forEach((el) =>
    el.addEventListener("click", () => loadDeadlockMeta(true))
  );
  root.querySelectorAll("[data-dlmeta-hero]").forEach((el) =>
    el.addEventListener("click", () => {
      DL.heroFilter = el.dataset.dlmetaHero;
      setView("dlmatches");
    })
  );
}
