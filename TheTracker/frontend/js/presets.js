// Pro settings presets: a card in each game's Settings tab that previews
// the competitive preset against the player's own files, line by line, and
// applies the ticked lines with a backup the Restore button puts back.

const PRESETS = {};

function presetState(game) {
  if (!PRESETS[game]) PRESETS[game] = { data: null, error: null, loading: false, busy: false, skip: new Set(), open: new Set(), showSame: new Set() };
  return PRESETS[game];
}

async function loadPreset(game) {
  const p = presetState(game);
  if (p.loading) return;
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

const PRESET_GROUPS = ["Video", "Audio", "Gameplay", "Visibility", "Hotkeys"];
const PRESET_GROUP_LABEL = { Video: "Video", Audio: "Audio", Gameplay: "Gameplay", Visibility: "Visibility & accessibility", Hotkeys: "Hotkeys" };
const PRESET_GAME = { dota: "Dota 2", deadlock: "Deadlock", cs2: "Counter-Strike 2" };

function presetValueText(v) {
  if (v === "") return "—";
  if (v === "true" || v === "1") return v;
  return v;
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

/// The card for one game.
function presetsHtml(game) {
  const p = presetState(game);
  const name = PRESET_GAME[game] || game;
  const intro = `<p class="muted">The settings the competitive players use, tuned for comfort and visibility: a clean picture with the effects that hide things turned off, game sounds over music, quick cast and a full keyboard layout with items on Z X C V B N. Every line is shown against what you have now, so you can untick anything you'd rather keep. Your files are backed up first and one click puts them back.</p>`;
  if (!p.data && !p.error && !p.loading) setTimeout(() => loadPreset(game), 0);
  if (!p.data) {
    return `<section class="set"><h3>Pro settings</h3>${intro}${p.error ? `<div class="note err">${esc(p.error)}</div><button class="btn ghost" data-act="preset-reload" data-game="${game}" type="button">Try again</button>` : `<p class="muted">Reading your ${esc(name)} settings…</p>`}</section>`;
  }
  const d = p.data;
  const byGroup = {};
  for (const l of d.lines) (byGroup[l.group] = byGroup[l.group] || []).push(l);
  const picked = d.lines.filter((l) => l.changed && !p.skip.has(l.id)).length;
  const status = !d.installed
    ? `<span class="check-dot idle"></span><b>${esc(name)} isn't installed</b> <span class="muted">The preset is shown from the files Steam kept; it applies once the game is installed.</span>`
    : d.running ? `<span class="check-dot bad"></span><b>${esc(name)} is open</b> <span class="muted">Close it to apply: it rewrites its settings when it exits.</span>`
    : !d.canApply ? `<span class="check-dot bad"></span><b>Can't apply yet</b> <span class="muted">${esc(d.reasons.join(" "))}</span>`
    : d.changes === 0 ? `<span class="check-dot ok"></span><b>Already on the preset</b>`
    : `<span class="check-dot ok"></span><b>Ready</b> <span class="muted">${d.changes} line${d.changes === 1 ? "" : "s"} differ from the preset.</span>`;
  const files = d.files.map((f) => `<span title="${esc(f.path)}">${esc(f.kind)}${f.cloud ? " <span class='muted'>(Steam cloud)</span>" : ""}${f.found ? "" : " <span class='loss'>missing</span>"}</span>`).join(", ");
  return `<section class="set"><h3>Pro settings</h3>${intro}
    <div class="net-head">${status}</div>
    <div class="preset-groups">${PRESET_GROUPS.filter((g) => byGroup[g]).map((g) => presetGroupHtml(game, g, byGroup[g], p)).join("")}</div>
    ${p.error ? `<div class="note err">${esc(p.error)}</div>` : ""}
    <div class="row wrap">
      <button class="btn" data-act="preset-apply" data-game="${game}" type="button" ${!d.canApply || !picked || p.busy ? "disabled" : ""}>${p.busy ? "Working…" : `Apply ${picked ? picked + " " : ""}line${picked === 1 ? "" : "s"}`}</button>
      ${d.lastBackup ? `<button class="btn ghost" data-act="preset-restore" data-game="${game}" type="button" ${d.running || p.busy ? "disabled" : ""}>Restore my settings</button>` : ""}
      <button class="btn ghost" data-act="preset-reload" data-game="${game}" type="button" ${p.busy ? "disabled" : ""}>Re-read</button>
    </div>
    <p class="hint">Files: ${files}. Lines marked <span class="ptag">new</span> aren't in your file yet; the game reads them the same way. ${d.lastBackup ? `Last backup ${esc(d.lastBackup.replace("_", " ").replace(/-(\d\d)-(\d\d)$/, ":$1:$2"))}.` : "No backup yet."}</p>
  </section>`;
}

onChange("preset-line", (el) => {
  const p = presetState(el.dataset.game);
  if (el.checked) p.skip.delete(el.dataset.id);
  else p.skip.add(el.dataset.id);
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
  const p = presetState(game);
  const name = PRESET_GAME[game] || game;
  const picked = p.data.lines.filter((l) => l.changed && !p.skip.has(l.id)).length;
  confirmBox({
    title: `Apply the preset to ${name}?`,
    body: `<p class="muted">${picked} line${picked === 1 ? "" : "s"} will be written to your ${esc(name)} settings files. Your current files are backed up first, and <b>Restore my settings</b> puts them back exactly.</p>
      <p class="muted">Keep ${esc(name)} closed until this is done. If Steam asks about a cloud sync conflict at the next launch, choose to <b>upload the local files</b>.</p>`,
    ok: "Apply",
    then: async () => {
      p.busy = true;
      p.error = null;
      rerender();
      try {
        p.data = await invoke("presets_apply", { game, skip: [...p.skip] });
        toast(`${name} is on the preset. Start the game to see it.`);
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
