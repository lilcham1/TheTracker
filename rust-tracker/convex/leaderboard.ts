import { query } from "./_generated/server";
import { v } from "convex/values";

// metric -> [field to rank on, higher-is-better]
const METRICS = {
  last_hits_25: ["lastHits25", true],
  fewest_deaths: ["totalDeaths", false],
  least_gold_lost: ["totalGoldLost", false],
  most_kills: ["kills", true],
} as const;

/**
 * Global leaderboard across every install that has synced, not just this
 * one. Ranking happens in the handler rather than via an index because
 * each metric would otherwise need its own index, and this dataset is
 * small (a personal tracker shared between friends).
 */
export const globalTop = query({
  args: {
    metric: v.string(),
    gameType: v.optional(v.string()), // omitted / "all" means every type
    limit: v.optional(v.number()),
  },
  handler: async (ctx, args) => {
    const spec = METRICS[args.metric as keyof typeof METRICS];
    if (!spec) return [];
    const [field, higherIsBetter] = spec;

    const rows =
      args.gameType && args.gameType !== "all"
        ? await ctx.db
            .query("matches")
            .withIndex("by_gameType", (q) => q.eq("gameType", args.gameType as string))
            .collect()
        : await ctx.db.query("matches").collect();

    const ranked = rows
      .filter((r) => r[field] !== null && r[field] !== undefined)
      .sort((a, b) => (higherIsBetter ? b[field] - a[field] : a[field] - b[field]))
      .slice(0, args.limit ?? 10);

    return ranked.map((r) => ({
      deviceId: r.deviceId,
      username: r.username,
      heroName: r.heroName,
      gameType: r.gameType,
      date: r.date,
      value: r[field],
    }));
  },
});
