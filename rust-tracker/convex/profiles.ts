import { mutation, query } from "./_generated/server";
import { v } from "convex/values";

/**
 * Stores the display name/rank/role for one install. Upserted whenever the
 * player saves their profile, so leaderboard rows synced later carry the
 * current name.
 */
export const upsert = mutation({
  args: {
    deviceId: v.string(),
    username: v.string(),
    rank: v.union(v.string(), v.null()),
    role: v.union(v.string(), v.null()),
  },
  handler: async (ctx, args) => {
    const existing = await ctx.db
      .query("profiles")
      .withIndex("by_device", (q) => q.eq("deviceId", args.deviceId))
      .unique();

    const doc = { ...args, updatedAt: Date.now() };
    if (existing) {
      await ctx.db.patch(existing._id, doc);
    } else {
      await ctx.db.insert("profiles", doc);
    }
  },
});

export const forDevice = query({
  args: { deviceId: v.string() },
  handler: async (ctx, args) =>
    await ctx.db
      .query("profiles")
      .withIndex("by_device", (q) => q.eq("deviceId", args.deviceId))
      .unique(),
});
