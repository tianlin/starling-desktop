#!/bin/bash
set -euo pipefail
root="$(cd "$(dirname "$0")/.." && pwd)"
dmg="$1"
out="$(cd "$(dirname "$dmg")" && pwd)"
scratch="$(mktemp -d "${TMPDIR:-/tmp}/starling-smoke.XXXXXX")"
pid=''
mounted=false
cleanup() {
 if [[ -n "$pid" ]] && kill -0 "$pid" 2>/dev/null; then kill "$pid"; fi
 if $mounted; then hdiutil detach "$scratch/mount" || true; fi
 rm -rf "$scratch"
}
trap cleanup EXIT
mkdir "$scratch/mount" "$scratch/home"
hdiutil attach "$dmg" -nobrowse -readonly -mountpoint "$scratch/mount"
mounted=true
[[ "$(readlink "$scratch/mount/Applications")" == /Applications ]]
ditto "$scratch/mount/Starling.app" "$scratch/Starling.app"
# Fresh HOME means no settings, user database or saved-session lookup. Account
# integration remains disabled. No provider writes or synthetic login are used.
HOME="$scratch/home" "$scratch/Starling.app/Contents/MacOS/Starling" > "$out/startup-smoke.txt" 2>&1 &
pid=$!
ready=false
for ((i=0;i<60;i++)); do
 kill -0 "$pid" 2>/dev/null || { cat "$out/startup-smoke.txt"; exit 1; }
 if grep -q 'Starling application ready' "$out/startup-smoke.txt"; then ready=true; break; fi
 sleep 1
done
$ready || { echo 'Frontend initialization did not complete.' >&2; exit 1; }
[[ -f "$scratch/home/Library/Application Support/Starling/metadata.db" ]]
# Apple events ask the running application to quit, exercising OnBeforeClose.
xcrun swift "$root/scripts/quit-macos.swift" "$pid" "$scratch/Starling.app"
for ((i=0;i<20;i++)); do
 if ! kill -0 "$pid" 2>/dev/null; then
  wait "$pid"; pid=''
  grep -q 'Starling progress flushed' "$out/startup-smoke.txt"
  if grep -q 'Starling quit fallback' "$out/startup-smoke.txt"; then exit 1; fi
  echo 'Guest initialization and progress-flush quit passed.' >> "$out/startup-smoke.txt"
  exit 0
 fi
 sleep 1
done
echo 'Application did not quit within 20 seconds.' >&2
exit 1
