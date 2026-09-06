// Deadlock favourite hero and builds.
//
// Split out of deadlock.js to keep that file focused on fetching and the
// core views. Both reuse the shared pieces from heroes.js / builds.js
// (form strip, improvement trend, build cards) rather than duplicating them.

/// Same shape as the Dota hero aggregate, so the shared trend and form-strip
/// helpers work unchanged.
function dlHeroAggregate() {
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
      recent: [],
    };
    e.played++;
    if (m.outcome === "win") e.wins++;
    else if (m.outcome === "loss") e.losses++;
    e.k += m.kills;
    e.d += m.deaths;
    e.a += m.assists;
    e.souls += m.netWorth;
    // Normalised so the shared helpers can read `won` / `startTime`.
    e.recent.push({
      ...m,
      won: m.outcome === "win",
      goldPerMin: 0,
      lastHits: m.lastHits || 0,
    });
    byHero.set(m.heroName, e);
  }
  return [...byHero.values()].map((e) => {
    const scored = e.wins + e.losses;
    return {
      ...e,
      winRate: scored ? (e.wins / scored) * 100 : 0,
      kda: (e.k + e.a) / Math.max(1, e.d),
      avgSouls: Math.round(e.souls / e.played),
    };
  });
}

function dlRenderFavorite() {
  const root = document.getElementById("tab-dlfavorite");
  if (!root) return;

  if (!DL.link.accountId) {
    root.innerHTML = dlNotLinkedHtml();
    dlWireNotLinked(root);
    return;
  }

  const fav = (state.prefs && state.prefs.favorites && state.prefs.favorites.deadlock) || null;
  if (!fav) {
    root.innerHTML = `
      <div class="empty-state">
        No favourite Deadlock hero yet.<br />
        Open <b>Heroes</b> and press the star next to one to track it here.
      </div>`;
    return;
  }
  if (!DL.matches.length) {
    root.innerHTML = `<div class="empty-state">${DL.loading ? "Loading&hellip;" : "No matches loaded yet."}</div>`;
    return;
  }

  const h = dlHeroAggregate().find((x) => x.name === fav);
  if (!h) {
    root.innerHTML = `
      <div class="empty-state">
        No games on <b>${escapeHtml(fav)}</b> in the last ${DL.matches.length} matches.
      </div>`;
    return;
  }

  const builds = (state.prefs.builds || []).filter((b) => b.game === "deadlock" && b.hero === fav);

  root.innerHTML = `
    <div class="card live-hero">
      ${h.image ? `<img src="${h.image}" alt="" />` : ""}
      <div class="grow">
        <p class="live-hero-name">${escapeHtml(h.name)}</p>
        <span class="hint">${h.played} games &middot; ${h.wins}W ${h.losses}L</span>
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
        <div class="stat-label">Avg souls</div>
        <div class="stat-value brand">${dlFmtSouls(h.avgSouls)}</div>
        <div class="stat-sub">per game</div>
      </div>
    </div>

    ${improvementHtml(h.recent)}

    <section class="home-section">
      <div class="home-head"><h2 class="home-title">Popular items on ${escapeHtml(h.name)}</h2></div>
      <div id="dlPopularHost">${deadlockPopularHtml(h.recent[0] && h.recent[0].heroId)}</div>
    </section>

    <div class="card col">
      <div class="section-head">
        <h3 class="section-title">Your builds for ${escapeHtml(h.name)}</h3>
        <button class="chip" id="dlNewBuild" type="button">New build</button>
      </div>
      ${
        builds.length
          ? builds.map((b) => dlBuildCardHtml(b)).join("")
          : `<p class="hint">No saved builds for this hero yet.</p>`
      }
    </div>`;

  const heroId = h.recent[0] && h.recent[0].heroId;
  if (heroId && !POPULAR.deadlock.has(heroId)) {
    loadDeadlockPopular(heroId).then(() => {
      const host = document.getElementById("dlPopularHost");
      if (host) host.innerHTML = deadlockPopularHtml(heroId);
    });
  }

  const nb = root.querySelector("#dlNewBuild");
  if (nb) {
    nb.addEventListener("click", () => {
      BUILDS.draft = { id: "", game: "deadlock", hero: fav, name: "", items: [], notes: "" };
      setView("dlbuilds");
    });
  }
}

/// Deadlock items have no Valve icon CDN the way Dota does, so builds here
/// are an ordered text list rather than an icon grid — honest about what's
/// available instead of showing Dota icons for a different game.
function dlBuildCardHtml(b) {
  return `
    <div class="build-card">
      <div class="build-head">
        <div class="grow">
          <div class="build-name">${escapeHtml(b.name || "Untitled build")}</div>
          <div class="hint">${escapeHtml(b.hero)} &middot; ${b.items.length} items</div>
        </div>
        <button class="chip" data-dl-edit="${escapeHtml(b.id)}" type="button">Edit</button>
        <button class="chip" data-dl-del="${escapeHtml(b.id)}" type="button">Delete</button>
      </div>
      ${b.items.length ? `<div class="hint">${b.items.map(escapeHtml).join(" &rarr; ")}</div>` : ""}
      ${b.notes ? `<p class="hint">${escapeHtml(b.notes)}</p>` : ""}
    </div>`;
}

function dlRenderBuilds() {
  const root = document.getElementById("tab-dlbuilds");
  if (!root) return;

  const all = (state.prefs.builds || []).filter((b) => b.game === "deadlock");
  const heroes = DL.matches.length ? dlHeroAggregate().map((h) => ({ id: h.name, name: h.name })) : [];
  const fav = state.prefs.favorites && state.prefs.favorites.deadlock;
  if (fav && !heroes.some((h) => h.id === fav)) heroes.unshift({ id: fav, name: fav });
  if (!heroes.length) heroes.push({ id: "unknown", name: "(load matches first)" });

  const d = BUILDS.draft && BUILDS.draft.game === "deadlock" ? BUILDS.draft : null;

  if (d) {
    root.innerHTML = `
      <div class="card col">
        <div class="section-head">
          <h3 class="section-title">${d.id ? "Edit build" : "New build"}</h3>
          <button class="chip" id="dlBuildCancel" type="button">Cancel</button>
        </div>
        <div class="row">
          <div class="grow">
            <label class="form-label">Build name</label>
            <input class="text-input" id="dlBuildName" type="text" value="${escapeHtml(d.name)}" placeholder="e.g. Spirit into Cold Front" />
          </div>
          <div class="grow">
            <label class="form-label">Hero</label>
            <select class="text-input" id="dlBuildHero">
              ${heroes
                .map((h) => `<option value="${escapeHtml(h.id)}" ${h.id === d.hero ? "selected" : ""}>${escapeHtml(h.name)}</option>`)
                .join("")}
            </select>
          </div>
        </div>
        <div>
          <label class="form-label">Items, one per line, in build order</label>
          <textarea class="text-input" id="dlBuildItems" rows="7" placeholder="Extra Health&#10;Mystic Burst&#10;Cold Front">${escapeHtml(d.items.join("\n"))}</textarea>
        </div>
        <div>
          <label class="form-label">Notes</label>
          <textarea class="text-input" id="dlBuildNotes" rows="2" placeholder="When to go this, what it counters&hellip;">${escapeHtml(d.notes)}</textarea>
        </div>
        <div class="row"><button class="btn" id="dlBuildSave" type="button">Save build</button></div>
      </div>`;

    root.querySelector("#dlBuildName").addEventListener("input", (e) => (d.name = e.target.value));
    root.querySelector("#dlBuildNotes").addEventListener("input", (e) => (d.notes = e.target.value));
    root.querySelector("#dlBuildHero").addEventListener("change", (e) => (d.hero = e.target.value));
    root.querySelector("#dlBuildItems").addEventListener("input", (e) => {
      d.items = e.target.value.split("\n").map((s) => s.trim()).filter(Boolean);
    });
    root.querySelector("#dlBuildCancel").addEventListener("click", () => {
      BUILDS.draft = null;
      dlRenderBuilds();
    });
    root.querySelector("#dlBuildSave").addEventListener("click", async () => {
      if (!d.name.trim()) d.name = `${d.hero} build`;
      state.prefs = await invoke("save_build", { build: { ...d, updatedAt: "" } });
      BUILDS.draft = null;
      dlRenderBuilds();
      dlRenderFavorite();
    });
    return;
  }

  root.innerHTML = `
    <div class="section-head">
      <p class="section-title">${all.length} saved build${all.length === 1 ? "" : "s"}</p>
      <button class="btn" id="dlNewBuildBtn" type="button">New build</button>
    </div>
    ${
      all.length
        ? `<div class="col" style="gap:8px">${all.map((b) => dlBuildCardHtml(b)).join("")}</div>`
        : `<div class="empty-state">
             No Deadlock builds yet.<br />
             Builds are saved on this PC and shown beside the hero they belong to.
           </div>`
    }`;

  root.querySelector("#dlNewBuildBtn").addEventListener("click", () => {
    BUILDS.draft = { id: "", game: "deadlock", hero: heroes[0].id, name: "", items: [], notes: "" };
    dlRenderBuilds();
  });
  root.querySelectorAll("[data-dl-edit]").forEach((el) =>
    el.addEventListener("click", () => {
      const b = all.find((x) => x.id === el.dataset.dlEdit);
      if (b) BUILDS.draft = { ...b, items: [...b.items] };
      dlRenderBuilds();
    })
  );
  root.querySelectorAll("[data-dl-del]").forEach((el) =>
    el.addEventListener("click", async () => {
      state.prefs = await invoke("delete_build", { id: el.dataset.dlDel });
      dlRenderBuilds();
      dlRenderFavorite();
    })
  );
}
