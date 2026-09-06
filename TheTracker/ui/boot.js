// Entry point. Kept separate so it runs after app.js and deadlock.js have
// both defined their globals — boot() touches both.
//
// Anything thrown in here used to leave the window blank with the default
// title still showing and no clue why: no devtools in a release build, and
// nothing written anywhere. So a failure is drawn on the page instead.
// Every line of it is worth having the one time it happens.

boot().catch((err) => {
  const content = document.getElementById("content");
  if (!content) return;
  content.innerHTML = `
    <section class="home-section" style="padding-top:0">
      <div class="home-head"><h2 class="home-title">TheTracker couldn't start</h2></div>
      <p class="hint" style="max-width:72ch">
        Something failed while loading. Your match history is untouched — it
        lives in files this screen never writes to.
      </p>
      <pre class="boot-error">${String((err && err.stack) || err)
        .replace(/[&<>]/g, (c) => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;" }[c]))}</pre>
    </section>`;
  const title = document.getElementById("viewTitle");
  if (title) title.textContent = "Error";
});

window.addEventListener("error", (e) => console.error("Uncaught:", e.error || e.message));
