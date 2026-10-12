// Pro settings presets. Video, audio, gameplay and visibility live in a card
// in each game's Settings tab; the hotkey layout has its own sidebar page.
// Both preview the preset against the player's own files line by line and
// apply the ticked lines with a backup the Restore button puts back.

const PRESETS = {};

function presetState(game) {
  if (!PRESETS[game]) PRESETS[game] = { data: null, error: null, loading: false, busy: false, skip: new Set(), open: new Set(), showSame: new Set(), showSameKeys: false };
  return PRESETS[game];
}

async function loadPreset(game, force = false) {
  const p = presetState(game);
  if (p.loading) return;
  if (force) p.data = null;
  p.loading = true;
  p.error = null;
  try {
    p.data = await invoke("presets_preview", { game });
  } catch (e) {
    p.error = e.message || String(e);
  }
  p.loading = false;
  rerender();
}

const PRESET_GROUPS = ["Video", "Audio", "Gameplay", "Visibility"];
const PRESET_GROUP_LABEL = { Video: "Video", Audio: "Audio", Gameplay: "Gameplay", Visibility: "Visibility & accessibility" };
const PRESET_GAME = { dota: "Dota 2", deadlock: "Deadlock", cs2: "Counter-Strike 2" };
const HOTKEY_VIEW = { dota: "hotkeys", deadlock: "dl-hotkeys", cs2: "cs-hotkeys" };

const isHotkey = (l) => l.group === "Hotkeys";

/// Lines a scope ("settings" or "hotkeys") would write: ticked and changed.
function presetPicked(game, scope) {
  const p = presetState(game);
  if (!p.data) return [];
  return p.data.lines.filter((l) => l.changed && !p.skip.has(l.id) && isHotkey(l) === (scope === "hotkeys"));
}

/// What to skip for an apply of one scope: the other scope entirely, plus
/// whatever was unticked.
function presetSkipFor(game, scope) {
  const p = presetState(game);
  return p.data.lines.filter((l) => p.skip.has(l.id) || isHotkey(l) !== (scope === "hotkeys")).map((l) => l.id);
}

function presetStatusHtml(d, name) {
  return !d.installed
    ? `<span class="check-dot idle"></span><b>${esc(name)} isn't installed</b> <span class="muted">The preset is shown from the files Steam kept; it applies once the game is installed.</span>`
    : d.running ? `<span class="check-dot bad"></span><b>${esc(name)} is open</b> <span class="muted">Close it to apply: it rewrites its settings when it exits.</span>`
    : !d.canApply ? `<span class="check-dot bad"></span><b>Can't apply yet</b> <span class="muted">${esc(d.reasons.join(" "))}</span>`
    : `<span class="check-dot ok"></span><b>Ready</b>`;
}

function presetBackupText(d) {
  return d.lastBackup ? `Last backup ${esc(d.lastBackup.replace("_", " ").replace(/-(\d\d)-(\d\d)$/, ":$1:$2"))}.` : "No backup yet.";
}

// ---------- Settings card: video, audio, gameplay, visibility ----------

function presetValueText(v) {
  return v === "" ? "—" : v;
}

function presetLineHtml(game, l, p) {
  const on = !p.skip.has(l.id);
  const current = l.present ? `<code>${esc(presetValueText(l.current))}</code>` : `<span class="muted">not set</span>`;
  const target = l.remove ? `<span class="muted">removed</span>` : `<code>${esc(presetValueText(l.value))}</code>`;
  return `<label class="switch preset-line ${l.changed ? "" : "same"}">
    <input type="checkbox" data-change="preset-line" data-game="${game}" data-id="${esc(l.id)}" ${on ? "checked" : ""} ${p.busy || !l.changed ? "disabled" : ""} />
    <span class="grow"><span>${esc(l.label)}</span>${l.note ? `<span class="muted"> ${esc(l.note)}</span>` : ""}${l.present ? "" : `<span class="ptag">new</span>`}</span>
    <span class="preset-vals">${l.changed ? `${current} <span class="muted">→</span> ${target}` : `<span class="muted">already ${target}</span>`}</span>
  </label>`;
}

function presetGroupHtml(game, group, lines, p) {
  const changing = lines.filter((l) => l.changed);
  const same = lines.length - changing.length;
  const open = p.open.has(group);
  const picked = changing.filter((l) => !p.skip.has(l.id)).length;
  const showSame = p.showSame.has(group);
  return `<div class="preset-group ${open ? "open" : ""}">
    <button class="preset-head" data-act="preset-toggle" data-game="${game}" data-group="${group}" type="button" aria-expanded="${open}">
      <span class="chev"></span><b>${PRESET_GROUP_LABEL[group]}</b>
      <span class="muted">${changing.length ? `${picked} of ${changing.length} change${changing.length === 1 ? "" : "s"} ticked` : "nothing to change"}${same ? `, ${same} already set` : ""}</span>
    </button>
    ${open ? `<div class="preset-lines">
      ${changing.map((l) => presetLineHtml(game, l, p)).join("") || `<p class="muted small">Everything in this group already matches.</p>`}
      ${same ? `<button class="link small" data-act="preset-same" data-game="${game}" data-group="${group}" type="button">${showSame ? "Hide" : "Show"} the ${same} already set</button>` : ""}
      ${showSame ? lines.filter((l) => !l.changed).map((l) => presetLineHtml(game, l, p)).join("") : ""}
    </div>` : ""}
  </div>`;
}

/// The Settings card for one game.
function presetsHtml(game) {
  const p = presetState(game);
  const name = PRESET_GAME[game] || game;
  const intro = `<p class="muted">The settings the competitive players use, tuned for comfort and visibility: a clean picture with the effects that hide things turned off, game sounds over music, and the client behaviour pros rely on. Every line is shown against what you have now, so you can untick anything you'd rather keep. Your files are backed up first and one click puts them back. The keyboard layout is on the <button class="link" data-act="go" data-view="${HOTKEY_VIEW[game]}" type="button">Hotkeys</button> page.</p>`;
  if (!p.data && !p.error && !p.loading) setTimeout(() => loadPreset(game), 0);
  if (!p.data) {
    return `<section class="set"><h3>Pro settings</h3>${intro}${p.error ? `<div class="note err">${esc(p.error)}</div><button class="btn ghost" data-act="preset-reload" data-game="${game}" type="button">Try again</button>` : `<p class="muted">Reading your ${esc(name)} settings…</p>`}</section>`;
  }
  const d = p.data;
  const byGroup = {};
  for (const l of d.lines) if (!isHotkey(l)) (byGroup[l.group] = byGroup[l.group] || []).push(l);
  const changes = d.lines.filter((l) => l.changed && !isHotkey(l)).length;
  const picked = presetPicked(game, "settings").length;
  const status = d.canApply && changes === 0 ? `<span class="check-dot ok"></span><b>Already on the preset</b>` : presetStatusHtml(d, name) + (d.canApply ? ` <span class="muted">${changes} line${changes === 1 ? "" : "s"} differ from the preset.</span>` : "");
  const files = d.files.map((f) => `<span title="${esc(f.path)}">${esc(f.kind)}${f.cloud ? " <span class='muted'>(Steam cloud)</span>" : ""}${f.found ? "" : " <span class='loss'>missing</span>"}</span>`).join(", ");
  return `<section class="set"><h3>Pro settings</h3>${intro}
    <div class="net-head">${status}</div>
    <div class="preset-groups">${PRESET_GROUPS.filter((g) => byGroup[g]).map((g) => presetGroupHtml(game, g, byGroup[g], p)).join("")}</div>
    ${p.error ? `<div class="note err">${esc(p.error)}</div>` : ""}
    <div class="row wrap">
      <button class="btn" data-act="preset-apply" data-game="${game}" data-scope="settings" type="button" ${!d.canApply || !picked || p.busy ? "disabled" : ""}>${p.busy ? "Working…" : `Apply ${picked ? picked + " " : ""}line${picked === 1 ? "" : "s"}`}</button>
      ${d.lastBackup ? `<button class="btn ghost" data-act="preset-restore" data-game="${game}" type="button" ${d.running || p.busy ? "disabled" : ""}>Restore my settings</button>` : ""}
      <button class="btn ghost" data-act="preset-reload" data-game="${game}" type="button" ${p.busy ? "disabled" : ""}>Re-read</button>
    </div>
    <p class="hint">Files: ${files}. Lines marked <span class="ptag">new</span> aren't in your file yet; the game reads them the same way. ${presetBackupText(d)}</p>
  </section>`;
}

// ---------- Hotkeys page ----------

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
    // Deadlock's cast modes and Dota's per-item modes hold a 0/1.
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
    <span class="hk-label">${esc(label)}${key && !key.present ? `<span class="ptag">new</span>` : ""}</span>
    <span class="hk-keys">${same ? target : `${current} <span class="muted">→</span> ${target}`}${extra}</span>
  </label>`;
}

function hotkeySectionHtml(game, section, actions, p) {
  const rows = actions.map(([action, lines]) => ({ action, lines, changed: lines.some((l) => l.changed) }));
  const changing = rows.filter((r) => r.changed);
  const same = rows.length - changing.length;
  const show = p.showSameKeys;
  return `<section class="set hk-section">
    <h3>${esc(section)} <span class="muted small">${changing.length ? `${changing.length} change${changing.length === 1 ? "" : "s"}` : "all set"}${same && !show ? `, ${same} already set` : ""}</span></h3>
    <div class="hk-rows">${(show ? rows : changing).map((r) => hotkeyRowHtml(game, r.action, r.lines, p)).join("") || `<p class="muted small">Already on the layout.</p>`}</div>
  </section>`;
}

function hotkeysHtml(game) {
  const p = presetState(game);
  const name = PRESET_GAME[game] || game;
  const intro = game === "dota"
    ? `<p class="muted">The layout most pros play on: abilities on <kbd>Q</kbd> <kbd>W</kbd> <kbd>E</kbd> <kbd>D</kbd> <kbd>F</kbd> <kbd>R</kbd>, items on <kbd>Z</kbd> <kbd>X</kbd> <kbd>C</kbd> <kbd>V</kbd> <kbd>B</kbd> <kbd>N</kbd>, teleport on <kbd>T</kbd>, neutral item on <kbd>G</kbd>, control groups on the number row, quick cast everywhere except the aimed items (Blink, Force Staff, Euls and the like stay on normal cast, since a mis-aimed one is a death). Untick any row to keep your own key.</p>`
    : game === "deadlock"
    ? `<p class="muted">Melee on <kbd>Q</kbd>, items on <kbd>Z</kbd> <kbd>X</kbd> <kbd>C</kbd> <kbd>V</kbd>, quick-buy on <kbd>B</kbd>, hold-to-crouch on <kbd>Ctrl</kbd>, pings on the mouse side buttons, abilities and items on quick cast. Untick any row to keep your own key.</p>`
    : `<p class="muted">The standard competitive binds: jump on the wheel, grenades on <kbd>F</kbd> <kbd>C</kbd> <kbd>X</kbd> <kbd>Z</kbd>, knife on <kbd>T</kbd>, buy on <kbd>B</kbd>, push to talk on <kbd>V</kbd>, ping on <kbd>Mouse 4</kbd>. Untick any row to keep your own key.</p>`;
  if (!p.data) {
    if (!p.error && !p.loading) setTimeout(() => loadPreset(game), 0);
    return `<section class="set">${intro}${p.error ? `<div class="note err">${esc(p.error)}</div><button class="btn ghost" data-act="preset-reload" data-game="${game}" type="button">Try again</button>` : loadingState(`Reading your ${name} keys…`)}</section>`;
  }
  const d = p.data;
  const sections = new Map();
  for (const l of d.lines) {
    if (!isHotkey(l)) continue;
    if (!sections.has(l.section)) sections.set(l.section, new Map());
    const acts = sections.get(l.section);
    if (!acts.has(l.action)) acts.set(l.action, []);
    acts.get(l.action).push(l);
  }
  const changes = d.lines.filter((l) => l.changed && isHotkey(l)).length;
  const picked = presetPicked(game, "hotkeys").length;
  const status = d.canApply && changes === 0 ? `<span class="check-dot ok"></span><b>You're on the pro layout</b>` : presetStatusHtml(d, name);
  return `<section class="set">${intro}
    <div class="net-head">${status}</div>
    ${p.error ? `<div class="note err">${esc(p.error)}</div>` : ""}
    <div class="row wrap">
      <button class="btn" data-act="preset-apply" data-game="${game}" data-scope="hotkeys" type="button" ${!d.canApply || !picked || p.busy ? "disabled" : ""}>${p.busy ? "Working…" : `Apply ${picked ? picked + " " : ""}line${picked === 1 ? "" : "s"}`}</button>
      ${d.lastBackup ? `<button class="btn ghost" data-act="preset-restore" data-game="${game}" type="button" ${d.running || p.busy ? "disabled" : ""}>Restore my keys</button>` : ""}
      <button class="btn ghost" data-act="preset-reload" data-game="${game}" type="button" ${p.busy ? "disabled" : ""}>Re-read</button>
      <label class="switch inline"><input type="checkbox" data-change="preset-show-same" data-game="${game}" ${p.showSameKeys ? "checked" : ""} /><span>Show keys already set</span></label>
    </div>
    <p class="hint">Keys are read from your ${esc(name)} key file${d.files.some((f) => f.kind === "keys" && f.cloud) ? " (Steam cloud)" : ""} and written there with the game closed. ${presetBackupText(d)}</p>
  </section>
  <div class="hk-grid">${[...sections].map(([s, acts]) => hotkeySectionHtml(game, s, [...acts], p)).join("")}</div>`;
}

for (const [game, id] of Object.entries(HOTKEY_VIEW)) {
  view(id, {
    game, nav: true, icon: "keys", title: "Hotkeys",
    sub: () => `The pro ${PRESET_GAME[game]} keyboard layout, against your own keys`,
    load(force) {
      const p = presetState(game);
      if (force || !p.data) return loadPreset(game, force);
    },
    render() {
      return hotkeysHtml(game);
    },
  });
}

// ---------- Actions ----------

onChange("preset-line", (el) => {
  const p = presetState(el.dataset.game);
  if (el.checked) p.skip.delete(el.dataset.id);
  else p.skip.add(el.dataset.id);
  rerender();
});
onChange("preset-action", (el) => {
  const p = presetState(el.dataset.game);
  for (const id of el.dataset.ids.split("|")) {
    if (el.checked) p.skip.delete(id);
    else p.skip.add(id);
  }
  rerender();
});
onChange("preset-show-same", (el) => {
  presetState(el.dataset.game).showSameKeys = el.checked;
  rerender();
});
act("preset-toggle", (el) => {
  const p = presetState(el.dataset.game);
  if (p.open.has(el.dataset.group)) p.open.delete(el.dataset.group);
  else p.open.add(el.dataset.group);
  rerender();
});
act("preset-same", (el) => {
  const p = presetState(el.dataset.game);
  if (p.showSame.has(el.dataset.group)) p.showSame.delete(el.dataset.group);
  else p.showSame.add(el.dataset.group);
  rerender();
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
  const scope = el.dataset.scope || "settings";
  const p = presetState(game);
  const name = PRESET_GAME[game] || game;
  const picked = presetPicked(game, scope).length;
  const what = scope === "hotkeys" ? "key file" : "settings files";
  confirmBox({
    title: scope === "hotkeys" ? `Apply the pro layout to ${name}?` : `Apply the preset to ${name}?`,
    body: `<p class="muted">${picked} line${picked === 1 ? "" : "s"} will be written to your ${esc(name)} ${what}. Your current file${scope === "hotkeys" ? " is" : "s are"} backed up first, and <b>Restore</b> puts ${scope === "hotkeys" ? "it" : "them"} back exactly.</p>
      <p class="muted">Keep ${esc(name)} closed until this is done. If Steam asks about a cloud sync conflict at the next launch, choose to <b>upload the local files</b>.</p>`,
    ok: "Apply",
    then: async () => {
      p.busy = true;
      p.error = null;
      rerender();
      try {
        p.data = await invoke("presets_apply", { game, skip: presetSkipFor(game, scope) });
        toast(scope === "hotkeys" ? `${name} is on the pro layout. Start the game to use it.` : `${name} is on the preset. Start the game to see it.`);
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
        p.data = await invoke("presets_restore", { game });
        toast(`${name} settings restored.`);
      } catch (e) {
        p.error = e.message || String(e);
      }
      p.busy = false;
      rerender();
    },
  });
});
