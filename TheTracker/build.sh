#!/usr/bin/env bash
# Builds TheTracker: the exe, the installer, and (with --sign) the signed
# update manifest that the in-app updater reads.
#
#   ./build.sh 1.0.0            exe + installer in build/bin
#   ./build.sh 1.0.0 --sign     also signs the installer and writes latest.json
#
# Run from Git Bash. Needs Go, NSIS and go-winres; signing also needs the
# release key at ~/.tauri/thetracker.key and the Tauri CLI (npm install), the
# same signer every release has used, so existing installs accept the update.
set -euo pipefail

VERSION="${1:?usage: ./build.sh <version> [--sign]}"
SIGN="${2:-}"
cd "$(dirname "$0")"
export PATH="/c/Program Files/Go/bin:$HOME/go/bin:/c/Program Files (x86)/NSIS:/c/Program Files/nodejs:$PATH"

OUT=build/bin
mkdir -p "$OUT"

echo "== tests"
go vet ./...
go test ./...
for f in frontend/js/*.js; do node --check "$f"; done

echo "== resources"
go-winres make --in build/windows/winres/winres.json --out rsrc --arch amd64 \
  --product-version "$VERSION.0" --file-version "$VERSION.0"

echo "== thetracker.exe"
# production: no dev tools or debug logging. windowsgui: no console window.
go build -tags production -trimpath \
  -ldflags "-H windowsgui -s -w -X thetracker/internal/core.Version=$VERSION" \
  -o "$OUT/thetracker.exe" .

echo "== installer"
SETUP="TheTracker_${VERSION}_x64-setup.exe"
# Absolute Windows paths: Git Bash rewrites anything that looks like a
# POSIX path in an argument, and NSIS resolves relative ones from the script.
MSYS2_ARG_CONV_EXCL="*" makensis -V2 "-DVERSION=$VERSION" "-DEXE=$(cygpath -w "$PWD/$OUT/thetracker.exe")" \
  "-DOUTFILE=$(cygpath -w "$PWD/$OUT/$SETUP")" "$(cygpath -w "$PWD/build/windows/installer.nsi")"

if [ "$SIGN" = "--sign" ]; then
  echo "== signing"
  rm -f "$OUT/$SETUP.sig"
  npx tauri signer sign -f "$HOME/.tauri/thetracker.key" -p "" "$OUT/$SETUP" >/dev/null
  NOTES="${NOTES:-}" VERSION="$VERSION" SETUP="$SETUP" OUT="$OUT" node -e '
    const fs = require("fs");
    const { VERSION, SETUP, OUT, NOTES } = process.env;
    const latest = {
      version: VERSION,
      notes: NOTES,
      pub_date: new Date().toISOString(),
      platforms: { "windows-x86_64": {
        signature: fs.readFileSync(`${OUT}/${SETUP}.sig`, "utf8").trim(),
        url: `https://github.com/lilcham1/TheTracker/releases/download/v${VERSION}/${SETUP}`,
      } },
    };
    fs.writeFileSync(`${OUT}/latest.json`, JSON.stringify(latest, null, 2) + "\n");'
  echo "signed: $OUT/$SETUP.sig, $OUT/latest.json"
fi

ls -la "$OUT"
