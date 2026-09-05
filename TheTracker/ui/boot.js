// Entry point. Kept separate so it runs after app.js and deadlock.js have
// both defined their globals — boot() touches both.
boot();
