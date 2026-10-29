import { action, internalMutation } from "./_generated/server";
import { internal } from "./_generated/api";
import { v } from "convex/values";

/**
 * VALORANT, for the app's Valorant pages, through HenrikDev's community API
 * (api.henrikdev.xyz): Riot's own match API is open only to approved
 * production apps. HenrikDev hands out keys on its Discord; the deployment's
 * owner sets one once, and it never leaves this server:
 *
 *   npx convex env set --prod HENRIKDEV_API_KEY <key>
 *
 * Only the reads the app needs pass through (account by Riot ID, rank and
 * rank history, matches, one match, the leaderboard). Answers are cached,
 * at most BUDGET calls a minute reach HenrikDev, and when it (or the budget)
 * says no, the last answer for the same path is served even if it has
 * expired. Without the key the action answers { ok: false, reason:
 * "not_set_up" }.
 */

const BUDGET = 25;
const AFF = "(eu|na|latam|br|ap|kr)";
const PLAT = "(pc|console)";
const PUUID = "[0-9a-f\\-]{36}";
const SEG = "[^/?#&]{1,48}";
const NUM = "\\d{1,3}";
// Path pattern, and how long its answer is kept (seconds; 0: not kept).
const ROUTES: [RegExp, number][] = [
  [new RegExp(`^/valorant/v2/account/${SEG}/${SEG}$`), 3600],
  [new RegExp(`^/valorant/v3/by-puuid/mmr/${AFF}/${PLAT}/${PUUID}$`), 300],
  [new RegExp(`^/valorant/v2/by-puuid/mmr-history/${AFF}/${PLAT}/${PUUID}$`), 300],
  [new RegExp(`^/valorant/v4/by-puuid/matches/${AFF}/${PLAT}/${PUUID}\\?size=${NUM}(&mode=[a-z]{1,20})?$`), 120],
  [new RegExp(`^/valorant/v1/by-puuid/stored-matches/${AFF}/${PUUID}\\?size=${NUM}&page=${NUM}$`), 300],
  [new RegExp(`^/valorant/v3/leaderboard/${AFF}/${PLAT}\\?size=${NUM}&page=${NUM}$`), 1800],
  // A match never changes; the app keeps each one itself.
  [new RegExp(`^/valorant/v4/match/${AFF}/${PUUID}$`), 0],
];

type Result = { ok: true; data: unknown; stale?: boolean } | { ok: false; reason: string };

/** Counts one call against this minute's budget; false when it's spent. */
export const spend = internalMutation({
  args: {},
  handler: async (ctx): Promise<boolean> => {
    const now = Date.now();
    const row = await ctx.db.query("kv").withIndex("by_key", (q) => q.eq("key", "valorant_budget")).unique();
    if (!row || row.expires <= now) {
      const expires = now - (now % 60_000) + 60_000;
      if (row) await ctx.db.patch(row._id, { value: "1", expires });
      else await ctx.db.insert("kv", { key: "valorant_budget", value: "1", expires });
      return true;
    }
    const used = Number(row.value) || 0;
    if (used >= BUDGET) return false;
    await ctx.db.patch(row._id, { value: String(used + 1) });
    return true;
  },
});

export const get = action({
  args: { path: v.string() },
  handler: async (ctx, { path }): Promise<Result> => {
    const route = ROUTES.find(([re]) => re.test(path));
    if (!route) return { ok: false, reason: "not_allowed" };
    const key = process.env.HENRIKDEV_API_KEY;
    if (!key) return { ok: false, reason: "not_set_up" };
    const ttl = route[1];
    const cacheKey = `valorant:${path}`;
    const cached = ttl > 0 ? await ctx.runQuery(internal.twitch.getKv, { key: cacheKey }) : null;
    if (cached && cached.expires > Date.now()) return { ok: true, data: JSON.parse(cached.value) };
    const stale = (reason: string): Result => (cached ? { ok: true, data: JSON.parse(cached.value), stale: true } : { ok: false, reason });
    if (!(await ctx.runMutation(internal.valorant.spend, {}))) return stale("rate_limited");
    const r = await fetch(`https://api.henrikdev.xyz${path}`, { headers: { Authorization: key, Accept: "application/json" } });
    if (r.status === 404) return { ok: false, reason: "not_found" };
    if (r.status === 429) return stale("rate_limited");
    if (r.status === 401 || r.status === 403) return { ok: false, reason: "bad_key" };
    if (!r.ok) return stale(`http_${r.status}`);
    const body = (await r.json()) as { data?: unknown };
    const data = body.data ?? null;
    if (ttl > 0) {
      await ctx.runMutation(internal.twitch.setKv, { key: cacheKey, value: JSON.stringify(data), expires: Date.now() + ttl * 1000 });
    }
    return { ok: true, data };
  },
});
