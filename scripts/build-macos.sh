#!/bin/bash
set -euo pipefail
root="$(cd "$(dirname "$0")/.." && pwd)"
cd "$root"
[[ "$(uname -s)" == Darwin ]] || { echo 'Build on macOS 14 or newer.' >&2; exit 1; }
[[ "$(sw_vers -productVersion | cut -d. -f1)" -ge 14 ]] || exit 1
case "$(uname -m)" in arm64) arch=arm64 ;; x86_64) arch=amd64 ;; *) exit 1 ;; esac
[[ "${1:-$arch}" == "$arch" ]] || { echo 'Use the native runner for each architecture.' >&2; exit 1; }
export CGO_ENABLED=1 MACOSX_DEPLOYMENT_TARGET=14.0
# Wails 2 defaults to 10.13 unless both flags already contain an explicit target.
export CGO_CFLAGS="${CGO_CFLAGS:-} -mmacosx-version-min=14.0"
export CGO_LDFLAGS="${CGO_LDFLAGS:-} -mmacosx-version-min=14.0"
[[ "$(go env GOVERSION)" == go1.26.8 ]] || { echo 'Go 1.26.8 is required.' >&2; exit 1; }
version="$(node -p "JSON.parse(require('fs').readFileSync('desktop/wails.json')).info.productVersion")"
out="$root/build/macos-$arch"
mkdir -p "$out"
work="$(mktemp -d "${TMPDIR:-/tmp}/starling-build.XXXXXX")"
trap 'rm -rf "$work"' EXIT
(
 export GOWORK="$work/go.work"
 go work init "$root" "$root/desktop"
 go mod download
 go mod verify
)
go test -race ./...
go vet ./...
(cd frontend && npm ci --ignore-scripts && npm test)
(
 cd desktop
 go test ./...
 go vet ./...
 # The sole Call bridge is unchanged; skip the binding helper which executes main.
 go run github.com/wailsapp/wails/v2/cmd/wails@v2.11.0 build -s -skipbindings -platform "darwin/$arch" -clean
)
app="$root/desktop/build/bin/Starling.app"
[[ -x "$app/Contents/MacOS/Starling" ]] || { echo 'Missing application bundle.' >&2; exit 1; }
codesign --force --sign - --timestamp=none "$app"
python3 scripts/verify-macos.py "$app" "$arch" "$version" | tee "$out/bundle-verification.txt"
node scripts/dependency-report.mjs "$app/Contents/MacOS/Starling" "$out/compliance"
go run golang.org/x/vuln/cmd/govulncheck@v1.8.0 -mode=binary "$app/Contents/MacOS/Starling" 2>&1 | tee "$out/vulnerability-scan.txt"
mkdir "$work/stage"
ditto "$app" "$work/stage/Starling.app"
ln -s /Applications "$work/stage/Applications"
cp docs/MACOS.md "$work/stage/READ-ME.md"
dmg="$out/Starling-$version-macos-$arch.dmg"
hdiutil create -ov -volname "Starling $version" -srcfolder "$work/stage" -format UDZO "$dmg"
(
 cd "$out"
 shasum -a 256 "$(basename "$dmg")" > SHA256SUMS.txt
 { git -C "$root" rev-parse HEAD; sw_vers; uname -m; go version; xcodebuild -version; } > build-environment.txt
)
echo "Candidate: $dmg (ad-hoc signed, not notarized; manual Mac acceptance remains required)."
