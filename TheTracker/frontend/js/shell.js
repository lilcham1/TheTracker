// Inside the desktop window, load the window framework's runtime. It tells
// the shell the page is ready, which the shell waits for before it talks to
// the page. In a plain browser (the development server) there is no shell,
// so nothing is loaded.
if (window.chrome && window.chrome.webview) {
  const s = document.createElement("script");
  s.type = "module";
  s.src = "/wails/runtime.js";
  document.head.appendChild(s);
}
