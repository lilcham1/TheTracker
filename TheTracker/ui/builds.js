// Custom item builds, saved locally per hero.
//
// Items are stored by their internal Dota name ("mage_slayer"), which is
// both what Valve's icon CDN is keyed on and what the live key-item matcher
// compares against — so a build written here lines up with what the tracker
// reports you actually bought.

const BUILDS = {
  draft: null, // null = list view; an object = editing that build
  itemQuery: "",
};

// A working set of the items people actually build, grouped so the picker
// isn't a wall of 200 icons. Names are Valve's internal ones.
const ITEM_CATALOG = {
  Boots: [
    "boots", "power_treads", "phase_boots", "arcane_boots", "tranquil_boots",
    "travel_boots", "guardian_greaves", "boots_of_bearing",
  ],
  Core: [
    "blink", "black_king_bar", "ultimate_scepter", "aghanims_shard", "manta",
    "sange_and_yasha", "yasha_and_kaya", "echo_sabre", "harpoon", "diffusal_blade",
    "desolator", "mage_slayer", "maelstrom", "mjollnir", "orchid", "bloodthorn",
  ],
  Damage: [
    "daedalus", "butterfly", "satanic", "monkey_king_bar", "silver_edge",
    "abyssal_blade", "nullifier", "skadi", "radiance", "rapier",
  ],
  Defence: [
    "heart", "assault", "shivas_guard", "crimson_guard", "pipe", "lotus_orb",
    "linkens_sphere", "blade_mail", "heavens_halberd", "eternal_shroud",
  ],
  Support: [
    "force_staff", "glimmer_cape", "ghost", "solar_crest", "vladmir",
    "spirit_vessel", "aeon_disk", "sheepstick", "refresher", "octarine_core",
    "rod_of_atos", "dagon_5", "wind_waker", "aghanims_blessing",
  ],
};

function itemIcon(name) {
  return `${DOTA_ITEM_CDN}${name}.png`;
}

function prettyItem(name) {
  return name
    .split("_")
    .map((w) => (w ? w[0].toUpperCase() + w.slice(1) : w))
    .join(" ");
}

function buildCardHtml(b, showHero = true) {
  return `
    <div class="build-card" data-build-id="${escapeHtml(b.id)}">
      <div class="build-head">
        <div class="grow">
          <div class="build-name">${escapeHtml(b.name || "Untitled build")}</div>
          <div class="hint">${showHero ? escapeHtml(prettyItem(b.hero)) + " · " : ""}${b.items.length} items</div>
        </div>
        <button class="chip" data-edit-build="${escapeHtml(b.id)}" type="button">Edit</button>
        <button class="chip" data-delete-build="${escapeHtml(b.id)}" type="button">Delete</button>
      </div>
      <div class="build-items">
        ${b.items
          .map(
            (i) =>
              `<img src="${itemIcon(i)}" title="${escapeHtml(prettyItem(i))}" alt="" onerror="this.style.visibility='hidden'" />`
          )
          .join("")}
      </div>
      ${b.notes ? `<p class="hint">${escapeHtml(b.notes)}</p>` : ""}
    </div>`;
}

function buildEditorHtml(d, heroOptions) {
  return `
    <div class="card col">
      <div class="section-head">
        <h3 class="section-title">${d.id ? "Edit build" : "New build"}</h3>
        <button class="chip" id="buildCancel" type="button">Cancel</button>
      </div>

      <div class="row">
        <div class="grow">
          <label class="form-label">Build name</label>
          <input class="text-input" id="buildName" type="text" placeholder="e.g. Fast Blink into BKB" value="${escapeHtml(d.name)}" />
        </div>
        <div class="grow">
          <label class="form-label">Hero</label>
          <select class="text-input" id="buildHero">
            ${heroOptions
              .map(
                (h) =>
                  `<option value="${escapeHtml(h.slug)}" ${h.slug === d.hero ? "selected" : ""}>${escapeHtml(h.name)}</option>`
              )
              .join("")}
          </select>
        </div>
      </div>

      <div>
        <label class="form-label">Build order — click to remove</label>
        <div class="build-items editable">
          ${
            d.items.length
              ? d.items
                  .map(
                    (i, idx) =>
                      `<img src="${itemIcon(i)}" title="${escapeHtml(prettyItem(i))} — click to remove" data-remove-item="${idx}" alt="" onerror="this.style.visibility='hidden'" />`
                  )
                  .join("")
              : `<span class="hint">Nothing added yet — pick items below.</span>`
          }
        </div>
      </div>

      <div>
        <label class="form-label">Notes</label>
        <textarea class="text-input" id="buildNotes" rows="2" placeholder="When to go this, what it counters…">${escapeHtml(d.notes)}</textarea>
      </div>

      <div class="row">
        <button class="btn" id="buildSave" type="button">Save build</button>
        <span class="flash" id="buildFlash">Saved!</span>
      </div>
    </div>

    <div class="card col">
      <div class="section-head"><h3 class="section-title">Add items</h3></div>
      <input class="text-input" id="itemSearch" type="text" placeholder="Filter items…" value="${escapeHtml(BUILDS.itemQuery)}" />
      ${Object.entries(ITEM_CATALOG)
        .map(([group, items]) => {
          const q = BUILDS.itemQuery.toLowerCase().replace(/\s+/g, "_");
          const shown = q ? items.filter((i) => i.includes(q)) : items;
          if (!shown.length) return "";
          return `
            <div>
              <div class="col-label" style="margin-bottom:6px">${group}</div>
              <div class="item-picker">
                ${shown
                  .map(
                    (i) =>
                      `<img src="${itemIcon(i)}" title="${escapeHtml(prettyItem(i))}" data-add-item="${i}" alt="" onerror="this.parentElement.removeChild(this)" />`
                  )
                  .join("")}
              </div>
            </div>`;
        })
        .join("")}
    </div>`;
}

function renderBuilds() {
  const root = document.getElementById("tab-builds");
  if (!root) return;

  const all = (state.prefs.builds || []).filter((b) => b.game === "dota");

  // Hero list comes from played heroes, plus the favourite, so the dropdown
  // is short and relevant rather than all 120+ heroes.
  const played = DOTA.matches.length ? dtHeroAggregate(DOTA.matches) : [];
  const heroOptions = played.map((h) => ({ slug: h.slug, name: h.name }));
  const fav = state.prefs.favorites && state.prefs.favorites.dota;
  if (fav && !heroOptions.some((h) => h.slug === fav)) {
    heroOptions.unshift({ slug: fav, name: prettyItem(fav) });
  }
  if (!heroOptions.length) heroOptions.push({ slug: "unknown", name: "(load Match History first)" });

  if (BUILDS.draft) {
    root.innerHTML = buildEditorHtml(BUILDS.draft, heroOptions);
    wireBuildEditor(root);
    return;
  }

  root.innerHTML = `
    <div class="section-head">
      <p class="section-title">${all.length} saved build${all.length === 1 ? "" : "s"}</p>
      <button class="btn" id="newBuild" type="button">New build</button>
    </div>
    ${
      all.length
        ? `<div class="col" style="gap:8px">${all.map((b) => buildCardHtml(b)).join("")}</div>`
        : `<div class="empty-state">
             No builds yet.<br />
             Builds are saved on this PC and shown next to the hero they belong to.
           </div>`
    }`;

  const nb = root.querySelector("#newBuild");
  if (nb)
    nb.addEventListener("click", () => {
      BUILDS.draft = { id: "", game: "dota", hero: heroOptions[0].slug, name: "", items: [], notes: "" };
      renderBuilds();
    });

  root.querySelectorAll("[data-edit-build]").forEach((el) =>
    el.addEventListener("click", () => {
      const b = all.find((x) => x.id === el.dataset.editBuild);
      if (b) BUILDS.draft = { ...b, items: [...b.items] };
      renderBuilds();
    })
  );
  root.querySelectorAll("[data-delete-build]").forEach((el) =>
    el.addEventListener("click", async () => {
      state.prefs = await invoke("delete_build", { id: el.dataset.deleteBuild });
      renderBuilds();
      renderFavoriteHero();
    })
  );
}

function wireBuildEditor(root) {
  const d = BUILDS.draft;

  root.querySelector("#buildName").addEventListener("input", (e) => (d.name = e.target.value));
  root.querySelector("#buildNotes").addEventListener("input", (e) => (d.notes = e.target.value));
  root.querySelector("#buildHero").addEventListener("change", (e) => (d.hero = e.target.value));

  const search = root.querySelector("#itemSearch");
  search.addEventListener("input", (e) => {
    BUILDS.itemQuery = e.target.value;
    renderBuilds();
    const again = document.querySelector("#itemSearch");
    if (again) {
      again.focus();
      again.setSelectionRange(again.value.length, again.value.length);
    }
  });

  root.querySelectorAll("[data-add-item]").forEach((el) =>
    el.addEventListener("click", () => {
      d.items.push(el.dataset.addItem);
      renderBuilds();
    })
  );
  root.querySelectorAll("[data-remove-item]").forEach((el) =>
    el.addEventListener("click", () => {
      d.items.splice(Number(el.dataset.removeItem), 1);
      renderBuilds();
    })
  );

  root.querySelector("#buildCancel").addEventListener("click", () => {
    BUILDS.draft = null;
    renderBuilds();
  });

  root.querySelector("#buildSave").addEventListener("click", async () => {
    if (!d.name.trim()) d.name = `${prettyItem(d.hero)} build`;
    state.prefs = await invoke("save_build", { build: { ...d, updatedAt: "" } });
    BUILDS.draft = null;
    renderBuilds();
    renderFavoriteHero();
  });
}
