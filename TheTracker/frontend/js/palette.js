// Quick search (Ctrl+K) and keyboard shortcuts.
//
// The search lists every page of the games that are switched on and every
// hero the app knows for them; typing narrows it, Enter opens the top match.

const PAL = { open: false, q: "", sel: 0 };

function palEntries() {
  const out = [{ label: "Today", hint: "All games", run: () => go("today") }];
  for (const g of enabledGames()) {
    for (const id of NAV[g]) out.push({ label: VIEWS[id].title, hint: GAMES[g].label, run: () => go(id) });
  }
  for (const [tab, label] of SETTINGS_TABS) out.push({ label: `Settings: ${label}`, hint: "Settings", run: () => go("settings", { tab }) });
  const on = S.boot.prefs.games;
  if (on.dota && dHeroes.data) {
    for (const h of dHeroes.data) out.push({ label: h.name, hint: "Dota 2 hero", img: heroImgHtml(h.slug, "avatar tiny"), run: () => go("heroes", { hero: h.slug }) });
  }
  if (on.deadlock && dlHeroes.data) {
    for (const h of dlHeroes.data) out.push({ label: h.name, hint: "Deadlock hero", img: imgHtml(h.image, "avatar tiny"), run: () => go("dl-heroes", { hero: h.id }) });
  }
  if (on.overwatch && owOverview.data) {
    for (const h of owOverview.data.heroes) {
      out.push({ label: h.name, hint: "Overwatch hero", img: imgHtml(h.portrait, "avatar tiny"), run: () => { OW.hero = h.key; go("ow-heroes"); } });
    }
  }
  return out;
}

/// Lower is better; null means no match. Starts of words beat the middle.
function palScore(label, q) {
  if (!q) return 0;
  const l = label.toLowerCase();
  const i = l.indexOf(q);
  if (i === 0) return 0;
  if (i > 0) return l[i - 1] === " " || l[i - 1] === "-" ? 1 : 2 + i / 100;
  // Letters in order, like "ps" for "Phantom Assassin's ... settings".
  let at = 0;
  for (const ch of q) {
    at = l.indexOf(ch, at);
    if (at < 0) return null;
    at++;
  }
  return 5;
}

function palMatches() {
  const q = PAL.q.trim().toLowerCase();
  return palEntries()
    .map((e) => ({ e, s: palScore(e.label, q) }))
    .filter((x) => x.s !== null)
    .sort((a, b) => a.s - b.s)
    .slice(0, 9)
    .map((x) => x.e);
}

function palPaint() {
  const host = $("#palette");
  if (!PAL.open) {
    host.hidden = true;
    return;
  }
  const list = palMatches();
  PAL.sel = Math.min(PAL.sel, Math.max(0, list.length - 1));
  host.hidden = false;
  $("#palList").innerHTML = list.length
    ? list.map((e, i) => `<button class="pal-item ${i === PAL.sel ? "on" : ""}" data-act="pal-run" data-i="${i}" type="button">${e.img || ""}<span class="grow">${esc(e.label)}</span><span class="muted">${esc(e.hint)}</span></button>`).join("")
    : `<p class="muted pal-empty">Nothing matches.</p>`;
}

function palOpen() {
  PAL.open = true;
  PAL.q = "";
  PAL.sel = 0;
  // Hero names come with the hero lists: fetch any not loaded yet.
  if (S.boot.prefs.games.dota) ensureHeroList().then(palPaint);
  if (S.boot.prefs.games.deadlock) dlHeroes.load().then(palPaint);
  const input = $("#palInput");
  input.value = "";
  palPaint();
  input.focus();
}

function palClose() {
  PAL.open = false;
  palPaint();
}

act("pal-run", (el) => {
  const e = palMatches()[Number(el.dataset.i)];
  palClose();
  if (e) e.run();
});
act("pal-open", palOpen);
act("pal-close", palClose);

document.addEventListener("input", (e) => {
  if (e.target.id === "palInput") {
    PAL.q = e.target.value;
    PAL.sel = 0;
    palPaint();
  }
});

const typing = (el) => el && (el.tagName === "INPUT" || el.tagName === "TEXTAREA" || el.tagName === "SELECT" || el.isContentEditable);

document.addEventListener("keydown", (e) => {
  const key = e.key.toLowerCase();
  if (e.ctrlKey && key === "k") {
    e.preventDefault();
    PAL.open ? palClose() : palOpen();
    return;
  }
  if (PAL.open) {
    if (e.key === "Escape") palClose();
    else if (e.key === "ArrowDown" || e.key === "ArrowUp") {
      e.preventDefault();
      PAL.sel = Math.max(0, PAL.sel + (e.key === "ArrowDown" ? 1 : -1));
      palPaint();
    } else if (e.key === "Enter") {
      e.preventDefault();
      const it = palMatches()[PAL.sel];
      palClose();
      if (it) it.run();
    }
    return;
  }
  if (typing(e.target) || !S.boot || !S.boot.prefs.games.chosen) return;
  if (e.ctrlKey && /^[1-4]$/.test(e.key)) {
    const g = enabledGames()[Number(e.key) - 1];
    if (g) {
      e.preventDefault();
      go(LAST_VIEW[g]);
    }
  } else if (e.ctrlKey && key === "t") {
    e.preventDefault();
    go("today");
  } else if (e.ctrlKey && e.key === ",") {
    e.preventDefault();
    go("settings");
  }
});
