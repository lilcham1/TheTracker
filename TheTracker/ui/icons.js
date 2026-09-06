// Inline SVG icon set.
//
// Unicode glyphs (⌂ ▤ ✦ ★) were the single clearest "hobby project" tell in
// the old UI: they render differently on every machine, sit on the text
// baseline rather than optically centred, and can't inherit stroke weight.
// These are drawn on a 24×24 grid with a consistent 1.75 stroke so the whole
// rail reads as one family.
//
// `currentColor` throughout, so an icon simply takes the colour of whatever
// it sits in — no per-state variants needed.

const ICON_PATHS = {
  overview:
    '<path d="M3 10.5 12 3l9 7.5"/><path d="M5.5 9.5V20h13V9.5"/><path d="M9.5 20v-6h5v6"/>',
  live:
    '<path d="M3 12h3.5l2-6 3.5 12 2.5-8 1.8 2h4.7"/>',
  matches:
    '<rect x="3" y="4.5" width="18" height="15" rx="2"/><path d="M3 9.5h18"/><path d="M8.5 9.5V19"/>',
  heroes:
    '<circle cx="9" cy="8" r="3.2"/><path d="M3.2 19.5a6 6 0 0 1 11.6 0"/><path d="M16.5 5.4a3.2 3.2 0 0 1 0 5.2"/><path d="M18 19.5a6 6 0 0 0-2.2-4.6"/>',
  star: '<path d="M12 3.6l2.6 5.3 5.9.9-4.25 4.14 1 5.86L12 17l-5.25 2.8 1-5.86L3.5 9.8l5.9-.9z"/>',
  builds:
    '<path d="M12 3 3.8 7.2v9.6L12 21l8.2-4.2V7.2z"/><path d="M3.8 7.2 12 11.5l8.2-4.3"/><path d="M12 11.5V21"/>',
  sessions:
    '<circle cx="12" cy="12" r="8.6"/><path d="M12 7.2V12l3.2 1.9"/>',
  leaderboard:
    '<path d="M4 20V11"/><path d="M10 20V5"/><path d="M16 20v-6"/><path d="M22 20H2"/>',
  overlay:
    '<rect x="3" y="4.5" width="18" height="13" rx="2"/><path d="M8 21h8"/><path d="M12 17.5V21"/>',
  profile:
    '<circle cx="12" cy="8" r="3.6"/><path d="M4.5 20a7.5 7.5 0 0 1 15 0"/>',
  accounts:
    '<circle cx="8.5" cy="12" r="3.6"/><path d="M12 12h8.5"/><path d="M17.5 12v3.2"/><path d="M20.5 12v2.2"/>',
  about:
    '<path d="M20.5 12a8.5 8.5 0 1 1-2.6-6.1"/><path d="M20.8 3.8v4.6h-4.6"/>',
  search:
    '<circle cx="10.8" cy="10.8" r="6.6"/><path d="M15.6 15.6 20.5 20.5"/>',
  refresh:
    '<path d="M20.5 12a8.5 8.5 0 1 1-2.6-6.1"/><path d="M20.8 3.8v4.6h-4.6"/>',
  chevron: '<path d="M9 5.5 15.5 12 9 18.5"/>',
  close: '<path d="M6 6l12 12"/><path d="M18 6 6 18"/>',
  plus: '<path d="M12 5v14"/><path d="M5 12h14"/>',
  trash:
    '<path d="M4 6.5h16"/><path d="M9.5 6.5V4.6h5v1.9"/><path d="M6.2 6.5 7 20h10l.8-13.5"/>',
  edit: '<path d="M4 20h4.2L19 9.2a2.1 2.1 0 0 0-3-3L5 17z"/><path d="M14.5 6.8l2.7 2.7"/>',
  link: '<path d="M10 13.6a4 4 0 0 0 5.7 0l2.6-2.6a4 4 0 1 0-5.7-5.7l-1.5 1.5"/><path d="M14 10.4a4 4 0 0 0-5.7 0l-2.6 2.6a4 4 0 1 0 5.7 5.7l1.5-1.5"/>',
  check: '<path d="M4.5 12.5 9.5 17.5 19.5 7"/>',
  download: '<path d="M12 3.5v11"/><path d="M7.5 10.5 12 15l4.5-4.5"/><path d="M4.5 19.5h15"/>',
};

/// Returns an inline SVG string. `size` is in px; stroke scales with it so
/// small icons stay crisp instead of turning to mush.
function icon(name, size = 16) {
  const path = ICON_PATHS[name];
  if (!path) return "";
  return `<svg class="ico" width="${size}" height="${size}" viewBox="0 0 24 24" fill="none"
    stroke="currentColor" stroke-width="1.75" stroke-linecap="round" stroke-linejoin="round"
    aria-hidden="true" focusable="false">${path}</svg>`;
}

