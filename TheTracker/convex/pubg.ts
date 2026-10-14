import { action, internalMutation } from "./_generated/server";
import { internal } from "./_generated/api";
import { v } from "convex/values";

/**
 * PUBG's official API, for the app's PUBG pages. PUBG requires its key to
 * stay on a server ("your API key should never be stored client side"), so
 * the call is made here with the key the deployment's owner sets once:
 *
 *   npx convex env set --prod PUBG_API_KEY <key from developer.pubg.com>
 *
 * Only the reads the app needs pass through (player lookup, seasons, season
 * and ranked stats, matches), and the rate-limited ones are cached, since a
 * free key allows 10 requests a minute for everyone together. Without the
 * key the action answers { ok: false, reason: "not_set_up" }.
 *
 * The action is public, so the budget is kept here too: at most BUDGET
 * rate-limited calls a minute reach PUBG, and when PUBG (or the budget) says
 * no, the last answer for the same path is served even if it has expired.
 */

const BUDGET = 9;

const SHARD = "(steam|kakao|psn|xbox|console)";
const ACCOUNT = "account\\.[0-9a-f]{32}";
// Path pattern, and how long its answer is kept (seconds; 0: not kept).
const ROUTES: [RegExp, number][] = [
  [new RegExp(`^/shards/${SHARD}/players\\?filter\\[playerNames\\]=[^&#]{1,64}$`), 300],
  [new RegExp(`^/shards/${SHARD}/players/${ACCOUNT}$`), 120],
  [new RegExp(`^/shards/${SHARD}/seasons$`), 86400],
  [new RegExp(`^/shards/${SHARD}/players/${ACCOUNT}/seasons/(division\\.[a-z0-9.\\-]{1,60}|lifetime)(/ranked)?$`), 600],
  [new RegExp(`^/shards/${SHARD}/players/${ACCOUNT}/(weapon_mastery|survival_mastery)$`), 1800],
  [/^\/shards\/pc-(as|eu|jp|kakao|krjp|na|oc|ru|sa|sea)\/leaderboards\/division\.[a-z0-9.\-]{1,60}\/(squad-fpp|squad|duo-fpp|duo|solo-fpp|solo)$/, 1800],
  // Matches aren't rate-limited and never change; the app keeps them itself.
  [new RegExp(`^/shards/${SHARD}/matches/[0-9a-f\\-]{36}$`), 0],
];

type Result = { ok: true; data: unknown; stale?: boolean } | { ok: false; reason: string };

/** Counts one rate-limited call against this minute's budget; false when it's spent. */
export const spend = internalMutation({
  args: {},
  handler: async (ctx): Promise<boolean> => {
    const now = Date.now();
    const row = await ctx.db.query("kv").withIndex("by_key", (q) => q.eq("key", "pubg_budget")).unique();
    if (!row || row.expires <= now) {
      const expires = now - (now % 60_000) + 60_000;
      if (row) await ctx.db.patch(row._id, { value: "1", expires });
      else await ctx.db.insert("kv", { key: "pubg_budget", value: "1", expires });
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
    const key = process.env.PUBG_API_KEY;
    if (!key) return { ok: false, reason: "not_set_up" };
    const ttl = route[1];
    const cacheKey = `pubg:${path}`;
    const cached = ttl > 0 ? await ctx.runQuery(internal.twitch.getKv, { key: cacheKey }) : null;
    if (cached && cached.expires > Date.now()) return { ok: true, data: JSON.parse(cached.value) };
    const stale = (reason: string): Result => (cached ? { ok: true, data: JSON.parse(cached.value), stale: true } : { ok: false, reason });
    if (ttl > 0 && !(await ctx.runMutation(internal.pubg.spend, {}))) return stale("rate_limited");
    const r = await fetch(`https://api.pubg.com${path}`, {
      headers: { Authorization: `Bearer ${key}`, Accept: "application/vnd.api+json" },
    });
    if (r.status === 404) return { ok: false, reason: "not_found" };
    if (r.status === 429) return stale("rate_limited");
    if (r.status === 401) return { ok: false, reason: "bad_key" };
    if (!r.ok) return stale(`http_${r.status}`);
    const data = await r.json();
    if (ttl > 0) {
      await ctx.runMutation(internal.twitch.setKv, { key: cacheKey, value: JSON.stringify(data), expires: Date.now() + ttl * 1000 });
    }
    return { ok: true, data };
  },
});
