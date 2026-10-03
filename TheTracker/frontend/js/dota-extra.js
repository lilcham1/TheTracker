// Dota pages: Draft helper, Meta, Builds.

const dMeta = resource("dotaMeta", "dota_meta", { ttl: 30 * 60000 });
const dItems = resource("dotaItems", "dota_items", { ttl: 6 * 3600000 });

// ---------- Draft helper ----------

const DRAFT = { enemies: [], query: "", picks: null, loading: false, error: null, position: "all", mineOnly: false };

async function runDraft() {
  if (!DRAFT.enemies.length) {
    DRAFT.picks = null;
    return rerender();
  }
  DRAFT.loading = true;
  DRAFT.error = null;
  rerender();
  const asked = DRAFT.enemies.join(",");
  try {
    const picks = await invoke("dota_draft", { enemies: DRAFT.enemies });
    if (asked === DRAFT.enemies.join(",")) DRAFT.picks = picks;
  } catch (e) {
    DRAFT.error = e.message;
  }
  DRAFT.loading = false;
  rerender();
}

act("draft-add", (el) => {
  const id = Number(el.dataset.id);
  if (DRAFT.enemies.length >= 5 || DRAFT.enemies.includes(id)) return;
  DRAFT.enemies.push(id);
  DRAFT.query = "";
  const box = $("#draftQuery");
  if (box) box.value = "";
  runDraft();
});
act("draft-remove", (el) => {
  DRAFT.enemies = DRAFT.enemies.filter((id) => id !== Number(el.dataset.id));
  runDraft();
});
act("draft-clear", () => {
  DRAFT.enemies = [];
  runDraft();
});
act("draft-set", (el) => {
  DRAFT[el.dataset.axis] = el.dataset.value === "toggle" ? !DRAFT[el.dataset.axis] : el.dataset.value;
  rerender();
});
onChange("draft-query", (el) => {
  DRAFT.query = el.value;
  rerender();
});

const POSITIONS = [
  ["carry", "Carry"], ["mid", "Mid"], ["offlane", "Offlane"], ["support", "Support"], ["hard_support", "Hard support"],
];
const positionLabel = (p) => (POSITIONS.find((x) => x[0] === p) || [, ""])[1];

view("draft", {
  game: "dota", nav: true, icon: "draft", title: "Draft",
  sub: () => "Pick the heroes the other team has shown, and see what has done well against them",
  load(force) {
    dMeta.load(force);
    if (dotaLinked()) dPlayer.load();
    return ensureHeroList();
  },
  render() {
    const wait = gate(dHeroes, "Loading heroes…");
    if (wait) return wait;
    const heroes = dHeroes.data;
    const q = DRAFT.query.trim().toLowerCase();
    const matches = q ? heroes.filter((h) => h.name.toLowerCase().includes(q) && !DRAFT.enemies.includes(h.id)).slice(0, 12) : [];
    const posBy = new Map(((dMeta.data && dMeta.data.heroes) || []).map((h) => [h.id, h.position]));
    const mine = new Map(((dPlayer.data && dPlayer.data.heroes) || []).map((h) => [h.heroId, h]));

    let body = `<div class="empty"><h3>Add the enemy's heroes as they pick</h3><p>Suggestions update with every hero you add. Everything here is something you can already see on the pick screen.</p></div>`;
    if (DRAFT.error) body = errorState(DRAFT.error, "draft-retry");
    else if (DRAFT.enemies.length && !DRAFT.picks) body = loadingState("Checking matchups…");
    else if (DRAFT.picks) {
      let picks = DRAFT.picks;
      if (DRAFT.position !== "all") picks = picks.filter((p) => posBy.get(p.heroId) === DRAFT.position);
      if (DRAFT.mineOnly) picks = picks.filter((p) => mine.has(p.heroId) && mine.get(p.heroId).games >= 5);
      const top = picks.slice(0, 15);
      const avoid = [...picks].reverse().slice(0, 6);
      const enemyHeroes = DRAFT.enemies.map((id) => S.heroById[id]);
      const row = (p) => {
        const m = mine.get(p.heroId);
        return `<tr class="click" data-act="go" data-view="heroes" data-params='${JSON.stringify({ hero: p.heroSlug })}'>
          <td><span class="cell">${heroImgHtml(p.heroSlug)}<span><b>${esc(p.heroName)}</b><span class="sub">${esc(positionLabel(posBy.get(p.heroId)))}</span></span></span></td>
          <td class="num display ${toneOfRate(p.winRate)}">${pct(p.winRate, 1)}</td>
          ${p.versus.map((v) => `<td class="num ${toneOfRate(v)}">${known(v) ? pct(v) : "–"}</td>`).join("")}
          <td class="num muted">${fmtNum(p.games)}</td>
          <td class="num">${m ? `${pct(m.winRate)} <span class="muted">in ${m.games}</span>` : `<span class="muted">not played</span>`}</td></tr>`;
      };
      const table = (list) => `<div class="table-wrap"><table class="table"><thead><tr><th>Hero</th><th class="num">Against this lineup</th>
        ${enemyHeroes.map((h) => `<th class="num">vs ${esc(h ? h.name : "?")}</th>`).join("")}<th class="num">Games</th><th class="num">Your record</th></tr></thead><tbody>${list.map(row).join("")}</tbody></table></div>`;
      body = picks.length
        ? `<div class="sec-head"><h3>Has done well against them</h3></div>${table(top)}
           <div class="sec-head"><h3>Has struggled against them</h3></div>${table(avoid)}
           <p class="hint">Win rates from OpenDota's hero matchup data, weighted by games played. A matchup number says how a hero has fared, not how you will: your own record on a hero is in the last column.</p>`
        : emptyState("No hero fits those filters", "Try another role, or include heroes you haven't played.");
    }

    return `<div class="draft-top">
        <div class="enemy-slots">${[0, 1, 2, 3, 4].map((i) => {
          const h = S.heroById[DRAFT.enemies[i]];
          return h ? `<button class="slot filled" data-act="draft-remove" data-id="${h.id}" type="button" title="Remove ${esc(h.name)}">${heroImgHtml(h.slug, "portrait big")}<span>${esc(h.name)}</span></button>` : `<span class="slot"><span class="muted">Enemy ${i + 1}</span></span>`;
        }).join("")}</div>
        <div class="row">
          <input class="input grow" id="draftQuery" type="search" placeholder="${DRAFT.enemies.length >= 5 ? "All five picked" : "Type an enemy hero's name"}" value="${esc(DRAFT.query)}" data-input="draft-query" ${DRAFT.enemies.length >= 5 ? "disabled" : ""} autocomplete="off" />
          ${DRAFT.enemies.length ? `<button class="btn ghost" data-act="draft-clear" type="button">Clear</button>` : ""}
        </div>
        ${matches.length ? `<div class="suggest">${matches.map((h) => `<button class="chip hero-chip" data-act="draft-add" data-id="${h.id}" type="button">${heroImgHtml(h.slug, "portrait small")}${esc(h.name)}</button>`).join("")}</div>` : q ? `<p class="muted">No hero matches "${esc(DRAFT.query)}".</p>` : ""}
        <div class="chips"><button class="chip ${DRAFT.position === "all" ? "on" : ""}" data-act="draft-set" data-axis="position" data-value="all" type="button">Any role</button>
          ${POSITIONS.map(([k, l]) => `<button class="chip ${DRAFT.position === k ? "on" : ""}" data-act="draft-set" data-axis="position" data-value="${k}" type="button">${l}</button>`).join("")}
          ${dotaLinked() ? `<span class="sep"></span><button class="chip ${DRAFT.mineOnly ? "on" : ""}" data-act="draft-set" data-axis="mineOnly" data-value="toggle" type="button">Only heroes I play</button>` : ""}</div>
      </div>${body}`;
  },
});
act("draft-retry", runDraft);

// ---------- Meta ----------

const META = { position: "all", sort: "winRate", dir: "desc", query: "" };

act("meta-set", (el) => {
  META[el.dataset.axis] = el.dataset.value;
  rerender();
});
act("meta-sort", (el) => {
  const key = el.dataset.key;
  if (META.sort === key) META.dir = META.dir === "asc" ? "desc" : "asc";
  else [META.sort, META.dir] = [key, key === "name" ? "asc" : "desc"];
  rerender();
});
onChange("meta-query", (el) => {
  META.query = el.value;
  rerender();
});

function trendHtml(delta) {
  if (Math.abs(delta) < 0.15) return `<span class="muted">steady</span>`;
  return `<span class="${delta > 0 ? "win" : "loss"}">${delta > 0 ? "▲" : "▼"} ${Math.abs(delta).toFixed(1)}</span>`;
}

function rateBar(rate, lo = 40, hi = 60) {
  const w = Math.max(0, Math.min(100, ((rate - lo) / (hi - lo)) * 100));
  return `<span class="bar"><i class="${toneOfRate(rate)}" style="width:${w.toFixed(0)}%"></i></span>`;
}

view("meta", {
  game: "dota", nav: true, icon: "meta", title: "Meta",
  sub: () => (dMeta.data ? `${fmtNum(dMeta.data.matches)} public matches, updated ${ago(dMeta.data.fetchedAt)}` : "Which heroes are winning right now"),
  load: (force) => dMeta.load(force),
  render() {
    const wait = gate(dMeta, "Reading the meta…");
    if (wait) return wait;
    const d = dMeta.data;
    const solid = d.heroes.filter((h) => h.pickRate >= 1.5);
    const rising = [...solid].filter((h) => h.trend >= 0.35).sort((a, b) => b.trend - a.trend).slice(0, 4);
    const falling = [...solid].filter((h) => h.trend <= -0.35).sort((a, b) => a.trend - b.trend).slice(0, 4);
    const contested = [...d.heroes].sort((a, b) => b.pickRate - a.pickRate).slice(0, 4);

    const q = META.query.trim().toLowerCase();
    let rows = d.heroes.filter((h) => (META.position === "all" || h.position === META.position) && (!q || h.name.toLowerCase().includes(q)));
    const dir = META.dir === "asc" ? 1 : -1;
    rows = [...rows].sort((a, b) => {
      const av = a[META.sort], bv = b[META.sort];
      if (!known(av) || !known(bv)) return known(av) ? -1 : known(bv) ? 1 : 0;
      return typeof av === "string" ? av.localeCompare(bv) * dir : (av - bv) * dir;
    });
    const th = (key, label, num = true) => `<th class="${num ? "num" : ""} ${META.sort === key ? "sorted" : ""}"><button data-act="meta-sort" data-key="${key}" type="button">${label}${META.sort === key ? (META.dir === "asc" ? " ▲" : " ▼") : ""}</button></th>`;
    const mover = (h, extra) => `<button class="line" data-act="go" data-view="heroes" data-params='${JSON.stringify({ hero: h.slug })}' type="button">${heroImgHtml(h.slug)}<span class="grow"><b>${esc(h.name)}</b></span><span class="num">${pct(h.winRate, 1)}</span>${extra}</button>`;

    return `${staleNote(dMeta)}
      <div class="cols three">
        <section><div class="sec-head"><h3>Rising</h3></div><div class="lines">${rising.map((h) => mover(h, trendHtml(h.trend))).join("") || `<p class="muted">Nothing is moving much.</p>`}</div></section>
        <section><div class="sec-head"><h3>Falling</h3></div><div class="lines">${falling.map((h) => mover(h, trendHtml(h.trend))).join("") || `<p class="muted">Nothing is moving much.</p>`}</div></section>
        <section><div class="sec-head"><h3>Most picked</h3></div><div class="lines">${contested.map((h) => mover(h, `<span class="muted when">${pct(h.pickRate)} of games</span>`)).join("")}</div></section>
      </div>
      <div class="filters">
        <div class="chips"><button class="chip ${META.position === "all" ? "on" : ""}" data-act="meta-set" data-axis="position" data-value="all" type="button">All roles</button>
          ${POSITIONS.map(([k, l]) => `<button class="chip ${META.position === k ? "on" : ""}" data-act="meta-set" data-axis="position" data-value="${k}" type="button">${l} (${d.heroes.filter((h) => h.position === k).length})</button>`).join("")}</div>
        <div class="row"><input class="input" id="metaQuery" type="search" placeholder="Find a hero" value="${esc(META.query)}" data-input="meta-query" /><span class="grow"></span><span class="muted">${rows.length} heroes</span></div>
      </div>
      <div class="table-wrap"><table class="table">
        <thead><tr>${th("name", "Hero", false)}${th("winRate", "Win rate")}<th></th>${th("pickRate", "Picked in")}${th("trend", "Trend")}${th("highWinRate", "Divine and above")}${th("turboWinRate", "Turbo")}${th("proPicks", "Pro picks")}${th("proBans", "Pro bans")}</tr></thead>
        <tbody>${rows.map((h) => `<tr class="click" data-act="go" data-view="heroes" data-params='${JSON.stringify({ hero: h.slug })}'>
          <td><span class="cell">${heroImgHtml(h.slug)}<span><b>${esc(h.name)}</b><span class="sub">${esc(positionLabel(h.position))}</span></span></span></td>
          <td class="num display ${toneOfRate(h.winRate)}">${pct(h.winRate, 1)}</td><td>${rateBar(h.winRate)}</td>
          <td class="num">${pct(h.pickRate, 1)}</td><td class="num">${trendHtml(h.trend)}</td>
          <td class="num ${toneOfRate(h.highWinRate)}">${dash(h.highWinRate, (v) => pct(v, 1))}</td><td class="num ${toneOfRate(h.turboWinRate)}">${dash(h.turboWinRate, (v) => pct(v, 1))}</td>
          <td class="num">${h.proPicks || "–"}</td><td class="num">${h.proBans || "–"}</td></tr>`).join("")}</tbody></table></div>
      <p class="hint">Computed from OpenDota's public match statistics. Trend is the change in win rate between the earlier and the more recent half of its sample.
        OpenDota doesn't publish stats by position, so each hero's role here is an estimate from the lane it's usually played in and its role tags.</p>`;
  },
});

// ---------- Builds ----------

const BUILD = { draft: null, itemQuery: "" };

// A starting set when the full item list hasn't loaded: the items most
// builds are made of, by Valve's internal names.
const COMMON_ITEMS = [
  "power_treads", "phase_boots", "arcane_boots", "tranquil_boots", "travel_boots", "guardian_greaves",
  "blink", "black_king_bar", "ultimate_scepter", "aghanims_shard", "manta", "sange_and_yasha", "echo_sabre", "desolator",
  "greater_crit", "butterfly", "satanic", "monkey_king_bar", "silver_edge", "abyssal_blade", "skadi", "radiance",
  "heart", "assault", "shivas_guard", "crimson_guard", "pipe", "lotus_orb", "sphere", "blade_mail",
  "force_staff", "glimmer_cape", "ghost", "solar_crest", "spirit_vessel", "aeon_disk", "sheepstick", "refresher", "octarine_core",
];

function buildCardHtml(b, showHero = true) {
  const isDota = b.game !== "deadlock";
  return `<div class="build">
    <div class="row">${showHero && isDota ? heroImgHtml(b.hero) : ""}<span class="grow"><b>${esc(b.name)}</b>${showHero ? `<span class="sub">${esc(isDota ? heroName(b.hero) : b.hero)}</span>` : ""}</span>
      <button class="link" data-act="build-edit" data-id="${esc(b.id)}" type="button">Edit</button><button class="link danger" data-act="build-delete" data-id="${esc(b.id)}" type="button">Delete</button></div>
    ${b.items.length ? `<div class="items">${b.items.map((k) => (isDota ? itemImgHtml(k) : `<span class="chip static">${esc(k)}</span>`)).join("")}</div>` : ""}
    ${b.notes ? `<p class="muted pre">${esc(b.notes)}</p>` : ""}
  </div>`;
}

act("build-new", (el) => {
  const game = el.dataset.game || "dota";
  BUILD.draft = { id: "", game, hero: el.dataset.hero || "", name: "", items: [], notes: "" };
  BUILD.itemQuery = "";
  go(game === "deadlock" ? "dl-builds" : "builds");
});
act("build-edit", (el) => {
  const b = S.boot.prefs.builds.find((x) => x.id === el.dataset.id);
  if (!b) return;
  BUILD.draft = JSON.parse(JSON.stringify(b));
  BUILD.itemQuery = "";
  go(b.game === "deadlock" ? "dl-builds" : "builds");
});
act("build-cancel", () => {
  BUILD.draft = null;
  rerender();
});
act("build-delete", async (el) => {
  if (el.dataset.confirm !== "yes") {
    el.dataset.confirm = "yes";
    el.textContent = "Press again to delete";
    return;
  }
  const prefs = await attempt(() => invoke("delete_build", { id: el.dataset.id }), "Build deleted.");
  if (prefs) S.boot.prefs = prefs;
  rerender();
});
/// Copies what is typed in the editor into the draft, so a redraw (adding an
/// item, say) doesn't lose it.
function syncBuildDraft() {
  const d = BUILD.draft;
  if (!d) return;
  const v = (id) => (document.getElementById(id) ? document.getElementById(id).value : null);
  if (v("buildName") !== null) d.name = v("buildName");
  if (v("buildHero") !== null) d.hero = v("buildHero");
  if (v("buildNotes") !== null) d.notes = v("buildNotes");
}
act("build-add-item", (el) => {
  syncBuildDraft();
  if (BUILD.draft.items.length < 24) BUILD.draft.items.push(el.dataset.key);
  rerender();
});
act("build-remove-item", (el) => {
  syncBuildDraft();
  BUILD.draft.items.splice(Number(el.dataset.index), 1);
  rerender();
});
act("build-add-text", () => {
  syncBuildDraft();
  const box = $("#buildItemText");
  const text = box ? box.value.trim() : "";
  if (text && BUILD.draft.items.length < 24) BUILD.draft.items.push(text);
  if (box) box.value = "";
  rerender();
});
onChange("build-item-query", (el) => {
  syncBuildDraft();
  BUILD.itemQuery = el.value;
  rerender();
});
act("build-save", async () => {
  syncBuildDraft();
  const prefs = await attempt(() => invoke("save_build", BUILD.draft), "Build saved.");
  if (prefs) {
    S.boot.prefs = prefs;
    BUILD.draft = null;
  }
  rerender();
});

function buildEditorHtml(d, heroOptions) {
  const isDota = d.game !== "deadlock";
  let picker = "";
  if (isDota) {
    const q = BUILD.itemQuery.trim().toLowerCase();
    const catalog = dItems.data || COMMON_ITEMS.map((key) => ({ key, name: itemName(key) }));
    const pool = q ? catalog.filter((i) => i.name.toLowerCase().includes(q) || i.key.includes(q)) : catalog.filter((i) => COMMON_ITEMS.includes(i.key));
    picker = `<label class="field"><span>Add items</span>
        <input class="input" id="buildItemQuery" type="search" placeholder="Search every item, or pick from the common ones below" value="${esc(BUILD.itemQuery)}" data-input="build-item-query" autocomplete="off" /></label>
      <div class="item-picker">${pool.slice(0, 60).map((i) => `<button class="item-btn" data-act="build-add-item" data-key="${esc(i.key)}" type="button" title="${esc(i.name)}">${itemImgHtml(i.key)}</button>`).join("") || `<p class="muted">No item matches that.</p>`}</div>`;
  } else {
    picker = `<label class="field"><span>Add an item</span><span class="row"><input class="input grow" id="buildItemText" type="text" placeholder="Item name" data-enter="build-add-text" /><button class="btn ghost" data-act="build-add-text" type="button">Add</button></span></label>`;
  }
  return `<div class="panel">
    <h2>${d.id ? "Edit build" : "New build"}</h2>
    <div class="form-grid">
      <label class="field"><span>Hero</span><select class="input" id="buildHero">${heroOptions}</select></label>
      <label class="field"><span>Build name</span><input class="input" id="buildName" type="text" maxlength="60" placeholder="For example: fast Blink into BKB" value="${esc(d.name)}" /></label>
    </div>
    <div class="field"><span>Items, in the order you buy them</span>
      <div class="items editable">${d.items.length ? d.items.map((k, i) => `<button class="item-btn" data-act="build-remove-item" data-index="${i}" type="button" title="Remove ${esc(isDota ? itemName(k) : k)}">${isDota ? itemImgHtml(k) : `<span class="chip static">${esc(k)}</span>`}</button>`).join("") : `<span class="muted">Nothing yet. Add items below; click one here to take it out.</span>`}</div></div>
    ${picker}
    <label class="field"><span>Notes</span><textarea class="input" id="buildNotes" rows="3" maxlength="1000" placeholder="When to pick this build, skill order, what to watch for">${esc(d.notes)}</textarea></label>
    <div class="row"><button class="btn" data-act="build-save" type="button">Save build</button><button class="btn ghost" data-act="build-cancel" type="button">Cancel</button></div>
  </div>`;
}

view("builds", {
  game: "dota", nav: true, icon: "builds", title: "Builds",
  sub: () => "Item plans you've saved for your heroes",
  load() {
    dItems.load().then(() => {
      if (dItems.data && !S.itemByKey) {
        S.itemByKey = Object.fromEntries(dItems.data.map((i) => [i.key, i]));
        rerender();
      }
    });
    return ensureHeroList();
  },
  render() {
    const d = BUILD.draft;
    if (d && d.game !== "deadlock") {
      const heroes = dHeroes.data || [];
      const options = `<option value="">Choose a hero</option>` + heroes.map((h) => `<option value="${esc(h.slug)}" ${h.slug === d.hero ? "selected" : ""}>${esc(h.name)}</option>`).join("");
      return buildEditorHtml(d, options);
    }
    const builds = S.boot.prefs.builds.filter((b) => b.game !== "deadlock");
    const head = `<div class="sec-head"><h3>${builds.length ? `${builds.length} saved` : ""}</h3><button class="btn" data-act="build-new" data-game="dota" type="button">New build</button></div>`;
    if (!builds.length) {
      return emptyState("No builds saved yet", "Save the items you plan to buy on a hero, in order, with your own notes. Each hero's page also shows what other players buy most.",
        `<button class="btn" data-act="build-new" data-game="dota" type="button">New build</button>`);
    }
    const byHero = new Map();
    for (const b of builds) {
      if (!byHero.has(b.hero)) byHero.set(b.hero, []);
      byHero.get(b.hero).push(b);
    }
    return head + [...byHero.entries()].sort((a, b) => heroName(a[0]).localeCompare(heroName(b[0]))).map(([hero, list]) => `<div class="build-group"><div class="row">${heroImgHtml(hero)}<h3>${esc(heroName(hero))}</h3></div>${list.map((b) => buildCardHtml(b, false)).join("")}</div>`).join("");
  },
});
