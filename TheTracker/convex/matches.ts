import { mutation, query } from "./_generated/server";
import { getAuthUserId } from "@convex-dev/auth/server";
import { v } from "convex/values";

const checkpoint = v.union(v.object({ lastHits: v.number(), denies: v.number() }), v.null());

// The shape the Rust client sends up. Kept deliberately close to
// MatchSummary in model.rs so the sync code is a straight field copy.
//
// Note what is NOT here: userId. It never comes from the caller — it's read
// off the auth token server-side. A client can't publish as another account
// even if it forges every other argument.
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

// Accounts are free, so reject values no real game produces — otherwise
// anyone could top the public leaderboard with made-up numbers. Bounds are
// deliberately generous so genuine outliers still go through.
export const MAX_USERNAME = 40;

function checkRange(name: string, value: number | null, max: number) {
  if (value === null) return;
  if (!Number.isFinite(value) || value < 0 || value > max) {
    throw new Error(`${name} out of range`);
  }
}

function checkLength(name: string, value: string | null, max: number) {
  if (value !== null && value.length > max) throw new Error(`${name} too long`);
}

function validateMatch(args: {
  deviceId: string; username: string; matchid: string; heroName: string | null;
  date: string; duration: string; gameType: string; kills: number;
  totalDeaths: number; totalGoldLost: number; roshanDeaths: number;
  lastHits25: number | null; checkpoints: Record<string, unknown>;
  deaths: unknown[]; keyItems: unknown[];
}) {
  checkLength("deviceId", args.deviceId, 64);
  checkLength("username", args.username, MAX_USERNAME);
  checkLength("matchid", args.matchid, 32);
  checkLength("heroName", args.heroName, 64);
  checkLength("date", args.date, 64);
  checkLength("duration", args.duration, 16);
  checkLength("gameType", args.gameType, 32);
  checkRange("kills", args.kills, 150);
  checkRange("totalDeaths", args.totalDeaths, 150);
  checkRange("totalGoldLost", args.totalGoldLost, 1_000_000);
  checkRange("roshanDeaths", args.roshanDeaths, 50);
  checkRange("lastHits25", args.lastHits25, 1500);
  if (Object.keys(args.checkpoints).length > 100) throw new Error("Too many checkpoints");
  if (args.deaths.length > 300) throw new Error("Too many deaths");
  if (args.keyItems.length > 200) throw new Error("Too many items");
}

/**
 * Insert a match, or update it if this account already published that
 * matchid. Idempotent on (userId, matchid), so re-syncing a whole local
 * history never duplicates, and re-tagging a game type just overwrites.
 *
 * Requires a signed-in caller.
 */
export const upsert = mutation({
  args: matchFields,
  handler: async (ctx, args) => {
    const userId = await getAuthUserId(ctx);
    if (userId === null) throw new Error("Sign in to publish matches");
    validateMatch(args);

    const existing = await ctx.db
      .query("matches")
      .withIndex("by_user_match", (q) => q.eq("userId", userId).eq("matchid", args.matchid))
      .unique();

    const doc = { ...args, userId, syncedAt: Date.now() };
    if (existing) {
      await ctx.db.patch(existing._id, doc);
      return { updated: true };
    }
    await ctx.db.insert("matches", doc);
    return { updated: false };
  },
});

/**
 * Attaches matches synced by this install before it had an account to the
 * account signing in now. Only touches rows that are already owned by the
 * caller or were left by the same install, so signing in on a shared PC
 * can't hoover up someone else's games.
 */
export const claimDevice = mutation({
  args: { deviceId: v.string() },
  handler: async (ctx, args) => {
    const userId = await getAuthUserId(ctx);
    if (userId === null) throw new Error("Sign in first");

    const rows = await ctx.db
      .query("matches")
      .withIndex("by_device", (q) => q.eq("deviceId", args.deviceId))
      .collect();

    let claimed = 0;
    for (const row of rows) {
      if (row.userId === userId) continue;
      // Never take a match another existing account owns — deviceId comes
      // from the caller, so it alone doesn't prove the rows are theirs.
      if (row.userId && (await ctx.db.get(row.userId)) !== null) continue;
      // Don't steal a match another account already published.
      const clash = await ctx.db
        .query("matches")
        .withIndex("by_user_match", (q) => q.eq("userId", userId).eq("matchid", row.matchid))
        .unique();
      if (clash) continue;
      await ctx.db.patch(row._id, { userId });
      claimed++;
    }
    return { claimed };
  },
});

/** Every match this account has published, newest first. */
export const listMine = query({
  args: {},
  handler: async (ctx) => {
    const userId = await getAuthUserId(ctx);
    if (userId === null) return [];
    const rows = await ctx.db
      .query("matches")
      .withIndex("by_user", (q) => q.eq("userId", userId))
      .collect();
    return rows.sort((a, b) => b.date.localeCompare(a.date));
  },
});

/** Removes everything this account has published. */
export const removeMine = mutation({
  args: {},
  handler: async (ctx) => {
    const userId = await getAuthUserId(ctx);
    if (userId === null) throw new Error("Sign in first");

    const rows = await ctx.db
      .query("matches")
      .withIndex("by_user", (q) => q.eq("userId", userId))
      .collect();
    for (const row of rows) await ctx.db.delete(row._id);

    const profile = await ctx.db
      .query("profiles")
      .withIndex("by_user", (q) => q.eq("userId", userId))
      .unique();
    if (profile) await ctx.db.delete(profile._id);

    return { deleted: rows.length };
  },
});
