// "Popular builds" — what people actually buy on a hero.
//
// Sourced from the same public APIs the rest of the app reads: OpenDota's
// item-popularity data for Dota, deadlock-api's build-item stats for
// Deadlock. Both are aggregate behaviour across a large sample, not anyone's
// curated guide — the UI says as much rather than dressing it up.
//
// Deliberately not scraped from dota2protracker or Statlocker: their curated
// builds are their product, and copying them would breach their terms.

const POPULAR = {
  dota: new Map(), // heroId -> phases[]
  deadlock: new Map(), // heroId -> items[]
  loading: new Set(),
  error: null,
};

async function loadDotaPopular(heroId) {
  if (POPULAR.dota.has(heroId) || POPULAR.loading.has(`d${heroId}`)) return;
  POPULAR.loading.add(`d${heroId}`);
  try {
    POPULAR.dota.set(heroId, await invoke("dota_popular_builds", { heroId }));
    POPULAR.error = null;
  } catch (e) {
    POPULAR.error = String(e);
  }
  POPULAR.loading.delete(`d${heroId}`);
}

async function loadDeadlockPopular(heroId) {
  if (POPULAR.deadlock.has(heroId) || POPULAR.loading.has(`k${heroId}`)) return;
  POPULAR.loading.add(`k${heroId}`);
  try {
    POPULAR.deadlock.set(heroId, await invoke("deadlock_popular_items", { heroId }));
    POPULAR.error = null;
  } catch (e) {
    POPULAR.error = String(e);
  }
  POPULAR.loading.delete(`k${heroId}`);
}

/// A phase of a Dota build. The share bar is relative to the most-bought
/// item in that phase, which is what makes "everyone buys this" visible at
/// a glance versus "some people buy this".
function popularPhaseHtml(phase) {
  const max = Math.max(...phase.items.map((i) => i.count), 1);
  return `
    <div class="pop-phase">
      <div class="pop-phase-label">${escapeHtml(phase.phase)}</div>
      <div class="pop-items">
        ${phase.items
          .map(
            (i) => `
          <div class="pop-item" title="${escapeHtml(i.name)} — ${i.count} matches">
            <img src="${DOTA_ITEM_CDN}${i.key}.png" alt="" onerror="this.style.visibility='hidden'" />
            <div class="pop-bar"><div class="pop-bar-fill" style="width:${(i.count / max) * 100}%"></div></div>
            <span class="pop-name">${escapeHtml(i.name)}</span>
          </div>`
          )
          .join("")}
      </div>
    </div>`;
}

function dotaPopularHtml(heroSlug, heroId) {
  if (POPULAR.loading.has(`d${heroId}`)) {
    return `<div class="skel-row skeleton"></div><div class="skel-row skeleton"></div>`;
  }
  const phases = POPULAR.dota.get(heroId);
  if (!phases) return "";
  if (!phases.length) {
    return `<p class="hint">No aggregate build data for this hero yet.</p>`;
  }
  return `
    <div class="pop-grid">${phases.map(popularPhaseHtml).join("")}</div>
    <p class="field-hint" style="margin-top:10px">
      Aggregate pick rates from OpenDota across many recent matches &mdash; what
      most players build, not a curated guide.
    </p>`;
}

function deadlockPopularHtml(heroId) {
  if (POPULAR.loading.has(`k${heroId}`)) {
    return `<div class="skel-row skeleton"></div><div class="skel-row skeleton"></div>`;
  }
  const items = POPULAR.deadlock.get(heroId);
  if (!items) return "";
  if (!items.length) return `<p class="hint">No aggregate build data for this hero yet.</p>`;

  const max = Math.max(...items.map((i) => i.builds), 1);
  return `
    <div class="pop-list">
      ${items
        .map(
          (i) => `
        <div class="pop-row">
          <span class="pop-row-name">${escapeHtml(i.name)}</span>
          <div class="pop-bar"><div class="pop-bar-fill" style="width:${(i.builds / max) * 100}%"></div></div>
          <span class="pop-row-count">${i.builds}</span>
        </div>`
        )
        .join("")}
    </div>
    <p class="field-hint" style="margin-top:10px">
      How many published builds include each item, from the community Deadlock
      API &mdash; aggregate data, not a curated guide.
    </p>`;
}
