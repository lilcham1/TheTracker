import { mutation, query } from "./_generated/server";
import { v } from "convex/values";

const checkpoint = v.union(v.object({ lastHits: v.number(), denies: v.number() }), v.null());

// The shape the Rust client sends up. Kept deliberately close to
// MatchSummary in model.rs so the sync code is a straight field copy.
const matchFields = {
  deviceId: v.string(),
  username: v.string(),
  matchid: v.string(),
  heroName: v.union(v.string(), v.null()),
  date: v.string(),
  duration: v.string(),
  kills: v.number(),
  totalDeaths: v.number(),
  totalGoldLost: v.number(),
  roshanDeaths: v.number(),
  gameType: v.string(),
  lastHits25: v.union(v.number(), v.null()),
  checkpoints: v.record(v.string(), checkpoint),
  deaths: v.array(v.object({ clock: v.string(), goldLost: v.union(v.number(), v.null()) })),
  keyItems: v.array(v.object({ clock: v.string(), item: v.string() })),
};

/**
 * Insert a match, or update it if this install already synced that matchid.
 * Idempotent on (deviceId, matchid), so re-syncing the whole local history
 * never produces duplicates — and re-tagging a match's game type locally
 * just overwrites the existing row.
 */
export const upsert = mutation({
  args: matchFields,
  handler: async (ctx, args) => {
    const existing = await ctx.db
      .query("matches")
      .withIndex("by_device_match", (q) => q.eq("deviceId", args.deviceId).eq("matchid", args.matchid))
      .unique();

    const doc = { ...args, syncedAt: Date.now() };
    if (existing) {
      await ctx.db.patch(existing._id, doc);
      return { updated: true };
    }
    await ctx.db.insert("matches", doc);
    return { updated: false };
  },
});

/** Every match this install has synced, newest first — used to restore history. */
export const listForDevice = query({
  args: { deviceId: v.string() },
  handler: async (ctx, args) => {
    const rows = await ctx.db
      .query("matches")
      .withIndex("by_device", (q) => q.eq("deviceId", args.deviceId))
      .collect();
    return rows.sort((a, b) => b.date.localeCompare(a.date));
  },
});

/**
 * Deletes everything one install has synced. Exists so a player can pull
 * their data back off the shared leaderboard, and so test rows can be
 * cleaned out without going through the dashboard.
 */
export const removeForDevice = mutation({
  args: { deviceId: v.string() },
  handler: async (ctx, args) => {
    const rows = await ctx.db
      .query("matches")
      .withIndex("by_device", (q) => q.eq("deviceId", args.deviceId))
      .collect();
    for (const row of rows) await ctx.db.delete(row._id);

    const profile = await ctx.db
      .query("profiles")
      .withIndex("by_device", (q) => q.eq("deviceId", args.deviceId))
      .unique();
    if (profile) await ctx.db.delete(profile._id);

    return { deleted: rows.length };
  },
});

/** How many matches this install has in the cloud (for the sync indicator). */
export const countForDevice = query({
  args: { deviceId: v.string() },
  handler: async (ctx, args) => {
    const rows = await ctx.db
      .query("matches")
      .withIndex("by_device", (q) => q.eq("deviceId", args.deviceId))
      .collect();
    return rows.length;
  },
});
