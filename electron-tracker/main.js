// Electron entry point. This runs the existing server.js directly inside
// Electron's main process (it's just Node under the hood), then opens a
// dedicated window pointed at it — no browser tab, no terminal needed.

const { app, BrowserWindow, Menu, dialog } = require('electron');
const path = require('path');
const { autoUpdater } = require('electron-updater');

// Starting the server here runs server.js top-to-bottom, which begins
// listening on http://localhost:3000 immediately.
// Server needs a writable location for its log/history files — the
// packaged app runs from inside a read-only app.asar archive, so it can't
// create folders next to itself. Point it at Electron's per-user data
// folder instead (e.g. %APPDATA%\dota-match-tracker on Windows).
process.env.DOTA_TRACKER_LOG_DIR = path.join(app.getPath('userData'), 'logs');

require('./server.js');

function createWindow() {
  const win = new BrowserWindow({
    width: 720,
    height: 900,
    minWidth: 480,
    minHeight: 600,
    title: 'Dota 2 Match Tracker',
    backgroundColor: '#0f0f14',
    autoHideMenuBar: true,
    webPreferences: {
      contextIsolation: true,
      nodeIntegration: false,
    },
  });

  Menu.setApplicationMenu(null);
  win.loadURL('http://localhost:3000');
}

app.whenReady().then(() => {
  createWindow();
  setupAutoUpdate();

  app.on('activate', () => {
    if (BrowserWindow.getAllWindows().length === 0) createWindow();
  });
});

// Auto-update via GitHub Releases. Only runs in the installed app (not
// during `npm start`, since electron-updater needs a real packaged build to
// check against). Silently does nothing if there's no internet or no
// release is published yet — never blocks the app from opening.
function setupAutoUpdate() {
  if (!app.isPackaged) return;

  autoUpdater.on('update-downloaded', () => {
    dialog.showMessageBox({
      type: 'info',
      title: 'Update ready',
      message: 'A new version of Dota Tracker has been downloaded.',
      detail: 'Restart now to install it, or keep playing and it\'ll install next time you close the app.',
      buttons: ['Restart now', 'Later'],
      defaultId: 0,
    }).then(result => {
      if (result.response === 0) autoUpdater.quitAndInstall();
    });
  });

  autoUpdater.on('error', (err) => {
    console.error('Auto-update check failed (this is fine if offline):', err.message);
  });

  autoUpdater.checkForUpdates();
}

app.on('window-all-closed', () => {
  if (process.platform !== 'darwin') app.quit();
});
