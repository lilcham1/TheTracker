import { defineSchema, defineTable } from "convex/server";
import { authTables } from "@convex-dev/auth/server";
import { v } from "convex/values";

// Ownership model: `userId` is the authenticated account that published a
// match, and it's the only thing writes are keyed on — the server takes it
// from the auth token, never from client arguments, so a caller can't
// publish as someone else. That's what stops leaderboard spam.
//
// `deviceId` is still recorded, but purely as a bookkeeping detail: it's how
// matches synced before accounts existed get claimed by the account that
// signs in on that install (see matches.claimDevice).
const checkpoint = v.union(v.object({ lastHits: v.number(), denies: v.number() }), v.null());

export default defineSchema({
  // users, authSessions, authAccounts, authRefreshTokens, ...
  ...authTables,

  matches: defineTable({
    userId: v.id("users"),
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

    // Denormalized so leaderboard queries don't have to dig into the map.
    lastHits25: v.union(v.number(), v.null()),

    checkpoints: v.record(v.string(), checkpoint),
    deaths: v.array(v.object({ clock: v.string(), goldLost: v.union(v.number(), v.null()) })),
    keyItems: v.array(v.object({ clock: v.string(), item: v.string() })),

    syncedAt: v.number(),
  })
    // Upserts look up one match belonging to one account.
    .index("by_user_match", ["userId", "matchid"])
    // "Restore my history onto a new PC".
    .index("by_user", ["userId"])
    // Claiming pre-account rows on first sign-in.
    .index("by_device", ["deviceId"])
    // Leaderboard filtered to one game type.
    .index("by_gameType", ["gameType"]),

  profiles: defineTable({
    userId: v.id("users"),
    deviceId: v.string(),
    username: v.string(),
    rank: v.union(v.string(), v.null()),
    role: v.union(v.string(), v.null()),
    updatedAt: v.number(),
  }).index("by_user", ["userId"]),
});
