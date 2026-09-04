import { defineSchema, defineTable } from "convex/server";
import { v } from "convex/values";

// One row per finished match, per install. `deviceId` is a random id
// generated on first run (see device_id.rs) — there's no sign-in, so it's
// what distinguishes one player's rows from another's. `username` is just
// the display label the player chose in the Profile tab, copied in at sync
// time so the leaderboard can show it without a second lookup.
const checkpoint = v.union(v.object({ lastHits: v.number(), denies: v.number() }), v.null());

export default defineSchema({
  matches: defineTable({
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
    // Upserts look up a specific match belonging to a specific install.
    .index("by_device_match", ["deviceId", "matchid"])
    // "Restore my history onto a new PC".
    .index("by_device", ["deviceId"])
    // Leaderboard filtered to one game type.
    .index("by_gameType", ["gameType"]),

  profiles: defineTable({
    deviceId: v.string(),
    username: v.string(),
    rank: v.union(v.string(), v.null()),
    role: v.union(v.string(), v.null()),
    updatedAt: v.number(),
  }).index("by_device", ["deviceId"]),
});
