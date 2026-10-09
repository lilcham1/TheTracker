// Checks the overlay's timings without a game: node frontend/js/overlay.test.cjs
// build.sh runs it with the Go tests.
const assert = require("assert");
const ov = require("./overlay.js");

const base = { leadSeconds: 5, compact: false, nextUp: false, dota: { runes: true, lotus: true, stacks: true } };
ov.setSettings(base);

const t = (m, s = 0) => m * 60 + s;
const kinds = (clock, type) => ov.upcoming(clock, type).map((e) => (e.now ? e.kind + "!" : `${e.kind}:${e.secs}`));
// Every clock second in [from, to] where `kind` is counting down.
const counting = (kind, from, to, type) => {
  const out = [];
  for (let c = from; c <= to; c++) if (ov.upcoming(c, type).some((e) => e.kind === kind && !e.now)) out.push(c);
  return out;
};
const nowAt = (kind, from, to, type) => {
  const out = [];
  for (let c = from; c <= to; c++) if (ov.upcoming(c, type).some((e) => e.kind === kind && e.now)) out.push(c);
  return out;
};

// Bounty: every 4 minutes from 0:00.
assert.deepStrictEqual(nowAt("bounty", 1, t(13)), [1, t(4), t(4, 1), t(8), t(8, 1), t(12), t(12, 1)]);
assert.deepStrictEqual(counting("bounty", t(3, 50), t(4, 10)), [t(3, 55), t(3, 56), t(3, 57), t(3, 58), t(3, 59)]);
// Water: 2:00 and 4:00 only.
assert.deepStrictEqual(nowAt("water", 1, t(12)), [t(2), t(2, 1), t(4), t(4, 1)]);
// Power: from 6:00 every 2 minutes, never before.
assert.deepStrictEqual(nowAt("power", 1, t(11)), [t(6), t(6, 1), t(8), t(8, 1), t(10), t(10, 1)]);
// Shrines of Wisdom: 7:00 then every 7 minutes.
assert.deepStrictEqual(nowAt("wisdom", 1, t(22)), [t(7), t(7, 1), t(14), t(14, 1), t(21), t(21, 1)]);
// Lotus: 3:00 every 3 minutes; Turbo twice as fast.
assert.deepStrictEqual(nowAt("lotus", 1, t(10)), [t(3), t(3, 1), t(6), t(6, 1), t(9), t(9, 1)]);
assert.deepStrictEqual(nowAt("lotus", 1, t(5), "turbo"), [t(1, 30), t(1, 31), t(3), t(3, 1), t(4, 30), t(4, 31)]);
// ...and grows: Great after 35:00, Greater after 60:00.
const lotusLabel = (clock) => ov.labelFor(ov.upcoming(clock).find((e) => e.kind === "lotus"));
assert.strictEqual(lotusLabel(t(32, 58)), "Lotus");
assert.strictEqual(lotusLabel(t(35, 58)), "Great lotus");
assert.strictEqual(lotusLabel(t(62, 58)), "Greater lotus");

// The camp pull: countdown from xx:45, pull at xx:52, from 1:52 on, whatever the lead.
ov.setSettings({ ...base, leadSeconds: 3 });
assert.deepStrictEqual(counting("stack", t(1, 30), t(2, 0)), [t(1, 45), t(1, 46), t(1, 47), t(1, 48), t(1, 49), t(1, 50), t(1, 51)]);
assert.deepStrictEqual(counting("stack", t(0, 30), t(1, 10)), [], "nothing to pull before the camps exist");
assert.deepStrictEqual(nowAt("stack", t(2, 40), t(3, 0)), [t(2, 52), t(2, 53)]);
ov.setSettings(base);

// Soonest first, "now" ahead of countdowns.
assert.deepStrictEqual(kinds(t(1, 50)).sort(), ["stack:2"]);
assert.deepStrictEqual(kinds(t(5, 57)).sort(), ["lotus:3", "power:3"]);
assert.deepStrictEqual(kinds(t(6, 1)).sort(), ["lotus!", "power!"]);
assert.deepStrictEqual(kinds(t(7, 58)).sort(), ["bounty:2", "power:2"]);
assert.deepStrictEqual(kinds(t(8)).sort(), ["bounty!", "power!"]);
assert.deepStrictEqual(kinds(t(2, 51)).sort(), ["stack:1"]);
assert.deepStrictEqual(kinds(t(2, 52)).sort(), ["stack!"]);

// Switches.
ov.setSettings({ ...base, dota: { runes: false, lotus: false, stacks: true } });
assert.deepStrictEqual(kinds(t(8)), []);
assert.deepStrictEqual(kinds(t(7, 50)), ["stack:2"]);
ov.setSettings(base);

// Next up: the two soonest that aren't on screen already.
const shown = ov.upcoming(t(5, 0));
assert.deepStrictEqual(ov.nextUp(t(5, 0), undefined, shown).map((e) => `${e.kind}:${e.secs}`), ["stack:52", "power:60"]);
const shownLate = ov.upcoming(t(5, 55));
assert.ok(shownLate.some((e) => e.kind === "power"));
assert.ok(!ov.nextUp(t(5, 55), undefined, shownLate).some((e) => e.kind === "power"), "a kind already counting down is not repeated");

// The drawn frame follows the settings.
ov.setSettings({ ...base, compact: true, nextUp: true });
const html = ov.frame(t(7, 58.5));
assert.ok(html.includes('class="chip compact'), "compact chips");
assert.ok(!html.includes('class="label"'), "compact chips have no label");
assert.ok(html.includes('class="next"'), "next up line");
ov.setSettings(base);
assert.ok(!ov.frame(t(5, 0)).includes('class="next"'), "next up is off by default");
assert.ok(ov.frame(t(8)).includes(">Now<"), "an event that just happened says Now");

// ---------- Windows: reminders stop when they stop mattering ----------
const core = { ...base, dota: { runes: true, bounty: true, water: true, power: true, wisdom: true, lotus: true, stacks: true, lotusLate: true, until: { stack: 10, bounty: 12, lotus: 15, power: 0, wisdom: 0 } } };
ov.setSettings(core);
// The pull: still at 9:45 (for 9:52), gone from 10:45.
assert.deepStrictEqual(counting("stack", t(9, 40), t(9, 52)), [t(9, 45), t(9, 46), t(9, 47), t(9, 48), t(9, 49), t(9, 50), t(9, 51)]);
assert.deepStrictEqual(counting("stack", t(10, 30), t(14, 0)), [], "no pull after 10:00");
// Bounties: 12:00 is the last.
assert.deepStrictEqual(nowAt("bounty", t(11, 0), t(20, 0)), [t(12), t(12, 1)]);
// Lotus: 15:00 the last early one, then back from 35:00.
assert.deepStrictEqual(nowAt("lotus", t(14, 0), t(40, 0)), [t(15), t(15, 1), t(36), t(36, 1), t(39), t(39, 1)]);
// Power and shrines all game.
assert.ok(nowAt("power", t(58, 0), t(60, 5)).includes(t(60)));
assert.ok(nowAt("wisdom", t(62, 0), t(63, 5)).includes(t(63)));
// Turbo halves the windows: the pull ends at 5:00, the lotus is back from 17:30.
assert.deepStrictEqual(counting("stack", t(5, 30), t(8, 0), "turbo"), []);
assert.deepStrictEqual(counting("stack", t(4, 40), t(4, 52), "turbo"), [t(4, 45), t(4, 46), t(4, 47), t(4, 48), t(4, 49), t(4, 50), t(4, 51)]);
assert.ok(nowAt("lotus", t(17, 0), t(19, 0), "turbo").includes(t(18)), "turbo great lotus at 18:00");
assert.ok(!nowAt("lotus", t(9, 0), t(12, 0), "turbo").length, "turbo lotus quiet between 7:30 and 17:30");
// Without lotusLate the lotus stays quiet after its window.
ov.setSettings({ ...core, dota: { ...core.dota, lotusLate: false } });
assert.deepStrictEqual(nowAt("lotus", t(16, 0), t(60, 0)), []);
// Next up skips what is out of its window.
ov.setSettings({ ...core, nextUp: true });
assert.ok(!ov.nextUp(t(20, 0), undefined, []).some((e) => e.kind === "stack" || e.kind === "bounty"), "no pull or bounty in next up at 20:00");
// One rune switched off on its own.
ov.setSettings({ ...core, dota: { ...core.dota, wisdom: false } });
assert.deepStrictEqual(nowAt("wisdom", t(6, 0), t(8, 0)), []);
// Old settings: only the all-runes switch, no windows: everything all game.
ov.setSettings({ ...base, dota: { runes: true, lotus: true, stacks: true } });
assert.ok(counting("stack", t(30, 40), t(30, 52)).length === 7, "old settings keep the pull all game");
ov.setSettings({ ...base, dota: { runes: false, lotus: true, stacks: true } });
assert.deepStrictEqual(nowAt("bounty", 1, t(9)), [], "the old all-runes switch still turns runes off");

console.log("overlay timings ok");
