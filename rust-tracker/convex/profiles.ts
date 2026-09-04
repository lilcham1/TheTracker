import { mutation, query } from "./_generated/server";
import { getAuthUserId } from "@convex-dev/auth/server";
import { v } from "convex/values";

/**
 * Display name/rank/role for the signed-in account. Upserted whenever the
 * player saves their profile, so leaderboard rows carry a current name.
 */
export const upsert = mutation({
  args: {
    deviceId: v.string(),
    username: v.string(),
    rank: v.union(v.string(), v.null()),
    role: v.union(v.string(), v.null()),
  },
  handler: async (ctx, args) => {
    const userId = await getAuthUserId(ctx);
    if (userId === null) throw new Error("Sign in to save a profile");

    const existing = await ctx.db
      .query("profiles")
      .withIndex("by_user", (q) => q.eq("userId", userId))
      .unique();

    const doc = { ...args, userId, updatedAt: Date.now() };
    if (existing) {
      await ctx.db.patch(existing._id, doc);
    } else {
      await ctx.db.insert("profiles", doc);
    }
  },
});

/** The signed-in account's profile, or null when signed out. */
export const mine = query({
  args: {},
  handler: async (ctx) => {
    const userId = await getAuthUserId(ctx);
    if (userId === null) return null;
    return await ctx.db
      .query("profiles")
      .withIndex("by_user", (q) => q.eq("userId", userId))
      .unique();
  },
});

/** Who am I? Used by the app to show signed-in state. */
export const whoami = query({
  args: {},
  handler: async (ctx) => {
    const userId = await getAuthUserId(ctx);
    if (userId === null) return null;
    const user = await ctx.db.get(userId);
    return { userId, email: user?.email ?? null };
  },
});
