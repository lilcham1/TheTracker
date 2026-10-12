// Pro settings: a sidebar page per game that previews the researched
// preset (video, audio, gameplay, visibility and a named hotkey layout)
// against the player's own files, line by line, and applies the ticked
// lines with a backup the Restore button puts back. Settings only links
// here.

const PRESETS = {};

function presetState(game) {
  if (!PRESETS[game]) PRESETS[game] = { data: null, error: null, loading: false, busy: false, skip: new Set(), showSame: false, layout: "" };
  return PRESETS[game];
}

async function loadPreset(game, force = false) {
  const p = presetState(game);
  if (p.loading) return;
  if (force) p.data = null;
  p.loading = true;
  p.error = null;
  try {
    p.data = await invoke("presets_preview", { game, layout: p.layout });
    p.layout = p.data.layout;
  } catch (e) {
    p.error = e.message || String(e);
  }
  p.loading = false;
  rerender();
}

const PRESET_GROUPS = ["Video", "Audio", "Gameplay", "Visibility"];
const PRESET_GROUP_LABEL = { Video: "Video", Audio: "Audio", Gameplay: "Gameplay", Visibility: "Visibility & accessibility" };
const PRESET_GAME = { dota: "Dota 2", deadlock: "Deadlock", cs2: "Counter-Strike 2", pubg: "PUBG" };
const PRESET_VIEW = { dota: "hotkeys", deadlock: "dl-hotkeys", cs2: "cs-hotkeys", pubg: "pg-pro" };
const PRESET_INTRO = {
  pubg: "Fullscreen at render scale 100, FOV 95 (the most common pro FOV), every quality setting very low except textures (medium) and view distance (low), sharpen on, V-sync, motion blur and smoothing off, legacy gun sounds with HRTF, scope on hold, recommendations off. Pros keep PUBG's default movement keys.",
  dota: "Borderless at native resolution, effects off, textures high, camera 6000, zoom and shake off, Stop-to-cancel teleports, music off, Yatoro's minimap. Pros don't share one keyboard layout: pick one below.",
  deadlock: "Fullscreen, effects off, fog low, shadows medium, FOV 90, hold to zoom and crouch, reduced shake. Pros agree on abilities 1–4, parry F, cancel Space, self-cast Mouse 3; pick a player's items and melee below.",
  cs2: "Fullscreen, shadows high, detail low, HDR quality, the shared 68 / 2.5 / 0 / -1.5 viewmodel, donk's crosshair, the centred rotating radar every pro keeps, stingers off. Binds follow the prosettings.net guide.",
};
const PRESET_SOURCES = {
  pubg: "Sources: prosettings.net PUBG guide (68 pros) and player pages (TGLTN, Kickstart); vortexgaming.io pro settings round-up.",
  dota: "Sources: prosettings.net, gamingcfg.com (Yatoro, miCKe, Puppey, Topson, Miracle-), hawk.live, winio.ai, cyberscore.live, esports.gg.",
  deadlock: "Sources: prosettings.net (MikaelS, Lefaa, Hydration, Surefour, Recrent), Dexerto, BLAST, deadlocklabs.gg.",
  cs2: "Sources: prosettings.net (ZywOo, donk, NiKo, m0NESY, ropz), white.market, favade.org.",
};

const isHotkey = (l) => l.group === "Hotkeys";

/// Lines an apply would write: ticked and changed.
function presetPicked(game) {
  const p = presetState(game);
  if (!p.data) return [];
  return p.data.lines.filter((l) => l.changed && !p.skip.has(l.id));
}

/// Who the value comes from, as a small chip.
function srcChip(source) {
  if (!source) return "";
  const mine = source === "my pick";
  return `<span class="ptag src ${mine ? "mine" : ""}" title="${mine ? "No pro data found for this line; this app's own judgement" : "Who uses this value"}">${esc(source)}</span>`;
}

function presetStatusHtml(d, name) {
  return !d.installed
    ? `<span class="check-dot idle"></span><b>${esc(name)} isn't installed</b> <span class="muted">The preset is shown from the files Steam kept; it applies once the game is installed.</span>`
    : d.running ? `<span class="check-dot bad"></span><b>${esc(name)} is open</b> <span class="muted">Close it to apply: it rewrites its settings when it exits.</span>`
    : !d.canApply ? `<span class="check-dot bad"></span><b>Can't apply yet</b> <span class="muted">${esc(d.reasons.join(" "))}</span>`
    : d.changes === 0 ? `<span class="check-dot ok"></span><b>You're on the preset</b>`
    : `<span class="check-dot ok"></span><b>Ready</b> <span class="muted">${d.changes} line${d.changes === 1 ? "" : "s"} differ from the preset.</span>`;
}

function presetBackupText(d) {
  return d.lastBackup ? `Last backup ${esc(d.lastBackup.replace("_", " ").replace(/-(\d\d)-(\d\d)$/, ":$1:$2"))}.` : "No backup yet.";
}

// ---------- Rows ----------

const valText = (v) => (v === "" ? "—" : v);

/// A video / audio / gameplay / visibility line.
function settingRowHtml(game, l, p) {
  const on = !p.skip.has(l.id);
  const current = l.present ? `<code>${esc(valText(l.current))}</code>` : `<span class="muted">not set</span>`;
  const target = l.remove ? `<span class="muted">removed</span>` : `<code>${esc(valText(l.value))}</code>`;
  return `<label class="switch hk-row ${l.changed ? "" : "same"}">
    <input type="checkbox" data-change="preset-action" data-game="${game}" data-ids="${esc(l.id)}" ${on ? "checked" : ""} ${p.busy || !l.changed ? "disabled" : ""} />
    <span class="hk-label">${esc(l.label)}${srcChip(l.source)}${l.present ? "" : `<span class="ptag">new</span>`}${l.note ? `<span class="muted small"> ${esc(l.note)}</span>` : ""}</span>
    <span class="hk-keys">${l.changed ? `${current} <span class="muted">→</span> ${target}` : target}</span>
  </label>`;
}

function settingGroupHtml(game, group, lines, p) {
  const changing = lines.filter((l) => l.changed);
  const same = lines.length - changing.length;
  const show = p.showSame;
  return `<section class="set hk-section">
    <h3>${PRESET_GROUP_LABEL[group]} <span class="muted small">${changing.length ? `${changing.length} change${changing.length === 1 ? "" : "s"}` : "all set"}${same && !show ? `, ${same} already set` : ""}</span></h3>
    <div class="hk-rows">${(show ? lines : changing).map((l) => settingRowHtml(game, l, p)).join("") || `<p class="muted small">Already on the preset.</p>`}</div>
  </section>`;
}

const KEY_NAMES = {
  MOUSE1: "Mouse 1", MOUSE2: "Mouse 2", MOUSE3: "Mouse 3", MOUSE4: "Mouse 4", MOUSE5: "Mouse 5",
  MWHEELUP: "Wheel up", MWHEELDOWN: "Wheel down", SPACE: "Space", CTRL: "Ctrl", SHIFT: "Shift", ALT: "Alt",
  TAB: "Tab", ENTER: "Enter", CAPSLOCK: "Caps Lock", ESCAPE: "Esc", NONE: "", "<unbound>": "",
};

function kbdHtml(mod, key) {
  if (!key && !mod) return `<span class="muted">unbound</span>`;
  const name = (k) => (KEY_NAMES[k.toUpperCase()] !== undefined ? KEY_NAMES[k.toUpperCase()] : k.length === 1 ? k.toUpperCase() : k);
  const parts = [];
  if (mod) parts.push(`<kbd>${esc(name(mod))}</kbd>`);
  if (key) parts.push(`<kbd>${esc(name(key))}</kbd>`);
  return parts.length ? parts.join("+") : `<span class="muted">unbound</span>`;
}

const modeWord = (v) => (v === "1" ? "quick cast" : v === "0" ? "normal cast" : v === "-1" ? "default" : v);

/// One row per in-game action, built from its Key / Modifier / Mode lines.
function hotkeyRowHtml(game, action, lines, p) {
  if (lines[0].info) {
    const l = lines[0];
    return `<div class="hk-row info"><span class="hk-label">${esc(l.label)}${srcChip(l.source)}<span class="muted small"> ${esc(l.note || "")}</span></span><span class="hk-keys">${esc(l.value)}</span></div>`;
  }
  const changed = lines.some((l) => l.changed);
  const on = lines.every((l) => !p.skip.has(l.id));
  const byLast = {};
  for (const l of lines) byLast[l.remove ? "Modifier-" : l.id.split("/").pop()] = l;
  const key = byLast.Key;
  const label = (key || lines[0]).label.replace(/ (modifier|quick cast|second key cleared)( removed)?$/, "");
  let current, target, extra = "";
  if (game === "cs2") {
    // Key to command: the action is the key.
    current = key.present ? `<code>${esc(key.current)}</code>` : `<span class="muted">unbound</span>`;
    target = `<code>${esc(key.value)}</code>`;
  } else if (byLast.Mode && !key) {
    // Dota's per-item modes hold a 0/1.
    const m = byLast.Mode;
    current = m.present ? modeWord(m.current) : `<span class="muted">default</span>`;
    target = modeWord(m.value);
  } else if (/QuickCastMode|AltCastMode/.test(action)) {
    current = key.present ? modeWord(key.current) : `<span class="muted">default</span>`;
    target = modeWord(key.value);
  } else {
    const setMod = byLast.Modifier;
    const dropMod = byLast["Modifier-"];
    const curMod = setMod && setMod.present ? setMod.current : dropMod ? dropMod.current : "";
    current = key.present ? kbdHtml(curMod, key.current) : `<span class="muted">unbound</span>`;
    target = kbdHtml(setMod ? setMod.value : "", key.value);
    if (byLast.Mode) {
      const m = byLast.Mode;
      extra = `<span class="ptag ${m.changed ? "" : "dim"}">${modeWord(m.value)}</span>`;
    }
    if (byLast.Key2 && byLast.Key2.present && byLast.Key2.current !== "NONE") extra += `<span class="muted small"> second key ${esc(byLast.Key2.current)} cleared</span>`;
  }
  const same = !changed;
  return `<label class="switch hk-row ${same ? "same" : ""}">
    <input type="checkbox" data-change="preset-action" data-game="${game}" data-ids="${esc(lines.map((l) => l.id).join("|"))}" ${on ? "checked" : ""} ${p.busy || same ? "disabled" : ""} />
    <span class="hk-label">${esc(label)}${srcChip((key || lines[0]).source)}${key && !key.present ? `<span class="ptag">new</span>` : ""}</span>
    <span class="hk-keys">${same ? target : `${current} <span class="muted">→</span> ${target}`}${extra}</span>
  </label>`;
}

function hotkeySectionHtml(game, section, actions, p) {
  const rows = actions.map(([action, lines]) => ({ action, lines, changed: lines.some((l) => l.changed) || lines[0].info }));
  const changing = rows.filter((r) => r.changed);
  const same = rows.length - changing.length;
  const show = p.showSame;
  const real = changing.filter((r) => !r.lines[0].info).length;
  return `<section class="set hk-section">
    <h3>${esc(section)} <span class="muted small">${real ? `${real} change${real === 1 ? "" : "s"}` : changing.length ? "set in the game" : "all set"}${same && !show ? `, ${same} already set` : ""}</span></h3>
    <div class="hk-rows">${(show ? rows : changing).map((r) => hotkeyRowHtml(game, r.action, r.lines, p)).join("") || `<p class="muted small">Already on the layout.</p>`}</div>
  </section>`;
}

// ---------- The page ----------

function proSettingsHtml(game) {
  const p = presetState(game);
  const name = PRESET_GAME[game] || game;
  const intro = `<p class="muted">${PRESET_INTRO[game]} Each line says who uses it; <span class="ptag src mine">my pick</span> means no pro data. Untick what you'd rather keep. Files are backed up first.</p>`;
  if (!p.data) {
    if (!p.error && !p.loading) setTimeout(() => loadPreset(game), 0);
    return `<section class="set">${intro}${p.error ? `<div class="note err">${esc(p.error)}</div><button class="btn ghost" data-act="preset-reload" data-game="${game}" type="button">Try again</button>` : loadingState(`Reading your ${name} settings…`)}</section>`;
  }
  const d = p.data;
  const byGroup = {};
  const sections = new Map();
  for (const l of d.lines) {
    if (!isHotkey(l)) {
      (byGroup[l.group] = byGroup[l.group] || []).push(l);
      continue;
    }
    if (!sections.has(l.section)) sections.set(l.section, new Map());
    const acts = sections.get(l.section);
    if (!acts.has(l.action)) acts.set(l.action, []);
    acts.get(l.action).push(l);
  }
  const picked = presetPicked(game).length;
  const cur = (d.layouts || []).find((l) => l.id === d.layout) || {};
  const picker = (d.layouts || []).length > 1
    ? `<div class="chips seg">${d.layouts.map((l) => `<button class="chip ${l.id === d.layout ? "on" : ""}" data-act="preset-layout" data-game="${game}" data-layout="${l.id}" type="button" ${p.busy ? "disabled" : ""}>${esc(l.label)}</button>`).join("")}</div>
      <p class="muted small"><b>${esc(cur.label || "")}</b> <span class="muted">${esc(cur.by || "")}.</span> ${esc(cur.desc || "")}</p>`
    : `<p class="muted small">${esc(cur.desc || "")}</p>`;
  const files = d.files.map((f) => `<span title="${esc(f.path)}">${esc(f.kind)}${f.cloud ? " <span class='muted'>(Steam cloud)</span>" : ""}${f.found ? "" : " <span class='loss'>missing</span>"}</span>`).join(", ");
  return `<section class="set">${intro}
    <div class="net-head">${presetStatusHtml(d, name)}</div>
    ${p.error ? `<div class="note err">${esc(p.error)}</div>` : ""}
    <div class="row wrap">
      <button class="btn" data-act="preset-apply" data-game="${game}" type="button" ${!d.canApply || !picked || p.busy ? "disabled" : ""}>${p.busy ? "Working…" : `Apply ${picked ? picked + " " : ""}line${picked === 1 ? "" : "s"}`}</button>
      ${d.lastBackup ? `<button class="btn ghost" data-act="preset-restore" data-game="${game}" type="button" ${d.running || p.busy ? "disabled" : ""}>Restore my settings</button>` : ""}
      ${d.originalKeys ? `<button class="btn ghost" data-act="preset-reset-keys" data-game="${game}" type="button" ${d.running || p.busy ? "disabled" : ""} title="Your key file from ${esc(d.originalKeys)}, before any preset">Reset hotkeys</button>` : ""}
      <button class="btn ghost" data-act="preset-reload" data-game="${game}" type="button" ${p.busy ? "disabled" : ""}>Re-read</button>
      <label class="switch inline"><input type="checkbox" data-change="preset-show-same" data-game="${game}" ${p.showSame ? "checked" : ""} /><span>Show lines already set</span></label>
    </div>
    <p class="hint">Files: ${files}. <span class="ptag">new</span> = not in your file yet. ${presetBackupText(d)}</p>
    <p class="hint">${PRESET_SOURCES[game]}</p>
  </section>
  <div class="hk-grid">${PRESET_GROUPS.filter((g) => byGroup[g]).map((g) => settingGroupHtml(game, g, byGroup[g], p)).join("")}</div>
  <section class="set hk-layout"><h3>Hotkeys</h3>${picker}</section>
  <div class="hk-grid">${[...sections].map(([s, acts]) => hotkeySectionHtml(game, s, [...acts], p)).join("")}</div>`;
}

for (const [game, id] of Object.entries(PRESET_VIEW)) {
  view(id, {
    game, nav: true, icon: "keys", title: "Pro settings",
    sub: () => `What the ${PRESET_GAME[game]} pros run, against your own settings`,
    load(force) {
      const p = presetState(game);
      if (force || !p.data) return loadPreset(game, force);
    },
    render() {
      return proSettingsHtml(game);
    },
  });
}

/// The Settings tab only points here.
function presetsHtml(game) {
  const p = presetState(game);
  const d = p.data;
  const name = PRESET_GAME[game] || game;
  return `<section class="set"><h3>Pro settings</h3>
    <p class="muted">Video, audio, gameplay, visibility and a pro hotkey layout for ${esc(name)}, on the <b>Pro settings</b> page in the sidebar.</p>
    ${d ? `<div class="net-head">${presetStatusHtml(d, name)}</div>` : ""}
    <div class="row wrap"><button class="btn ghost" data-act="go" data-view="${PRESET_VIEW[game]}" type="button">Open Pro settings</button></div>
  </section>`;
}

// ---------- Actions ----------

onChange("preset-action", (el) => {
  const p = presetState(el.dataset.game);
  for (const id of el.dataset.ids.split("|")) {
    if (el.checked) p.skip.delete(id);
    else p.skip.add(id);
  }
  rerender();
});
onChange("preset-show-same", (el) => {
  presetState(el.dataset.game).showSame = el.checked;
  rerender();
});
act("preset-layout", (el) => {
  const p = presetState(el.dataset.game);
  if (p.busy || p.layout === el.dataset.layout) return;
  p.layout = el.dataset.layout;
  // Hotkey ticks belong to the old layout; the rest stay.
  for (const id of [...p.skip]) if (id.includes(".keys.")) p.skip.delete(id);
  p.error = null;
  p.data = null;
  rerender();
  loadPreset(el.dataset.game);
});
act("preset-reload", (el) => {
  const p = presetState(el.dataset.game);
  p.error = null;
  p.data = null;
  rerender();
  loadPreset(el.dataset.game);
});
act("preset-apply", (el) => {
  const game = el.dataset.game;
  const p = presetState(game);
  const name = PRESET_GAME[game] || game;
  const picked = presetPicked(game).length;
  confirmBox({
    title: `Apply the preset to ${name}?`,
    body: `<p class="muted">${picked} line${picked === 1 ? "" : "s"} written to your ${esc(name)} files, backed up first. Keep the game closed. If Steam asks about a cloud conflict, choose <b>upload local</b>.</p>`,
    ok: "Apply",
    then: async () => {
      p.busy = true;
      p.error = null;
      rerender();
      try {
        p.data = await invoke("presets_apply", { game, layout: p.layout, skip: [...p.skip] });
        toast(`${name} is on the preset. Start the game to see it.`);
      } catch (e) {
        p.error = e.message || String(e);
      }
      p.busy = false;
      rerender();
    },
  });
});
act("preset-reset-keys", (el) => {
  const game = el.dataset.game;
  const p = presetState(game);
  const name = PRESET_GAME[game] || game;
  confirmBox({
    title: `Reset ${name} hotkeys?`,
    body: `<p class="muted">Your key file from before any preset (backup ${esc(p.data.originalKeys || "")}) is put back exactly. Video, audio and the rest stay as they are. Keep ${esc(name)} closed.</p>`,
    ok: "Reset hotkeys",
    then: async () => {
      p.busy = true;
      p.error = null;
      rerender();
      try {
        p.data = await invoke("presets_restore_original", { game, kind: "keys", layout: p.layout });
        toast(`${name} hotkeys are back to your originals.`);
      } catch (e) {
        p.error = e.message || String(e);
      }
      p.busy = false;
      rerender();
    },
  });
});
act("preset-restore", (el) => {
  const game = el.dataset.game;
  const p = presetState(game);
  const name = PRESET_GAME[game] || game;
  confirmBox({
    title: `Restore your ${name} settings?`,
    body: `<p class="muted">Your files from the last backup (${esc(p.data.lastBackup || "")}) are put back exactly as they were. Keep ${esc(name)} closed.</p>`,
    ok: "Restore",
    then: async () => {
      p.busy = true;
      p.error = null;
      rerender();
      try {
        p.data = await invoke("presets_restore", { game, layout: p.layout });
        toast(`${name} settings restored.`);
      } catch (e) {
        p.error = e.message || String(e);
      }
      p.busy = false;
      rerender();
    },
  });
});
