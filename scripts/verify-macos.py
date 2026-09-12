"""Verify the actual app, including every Mach-O image it contains."""
import pathlib
import plistlib
import re
import subprocess
import sys

app = pathlib.Path(sys.argv[1]).resolve()
arch = {"arm64": "arm64", "amd64": "x86_64"}[sys.argv[2]]
version = sys.argv[3]

def run(*args):
    return subprocess.check_output(args, text=True, stderr=subprocess.STDOUT)

with (app / "Contents/Info.plist").open("rb") as file:
    info = plistlib.load(file)
assert info["CFBundleIdentifier"] == "io.github.tianlin.starling"
assert info["CFBundleVersion"] == info["CFBundleShortVersionString"] == version
assert info["LSMinimumSystemVersion"] == "14.0"
assert (app / "Contents/Resources/iconfile.icns").is_file()
run("codesign", "--verify", "--deep", "--strict", str(app))
images = []
for path in app.rglob("*"):
    if not path.is_file() or path.is_symlink():
        continue
    if "Mach-O" not in run("file", "-b", str(path)):
        continue
    images.append(path)
    assert run("lipo", "-archs", str(path)).strip() == arch, path
    headers = run("otool", "-l", str(path))
    minimum = re.findall(r"\bminos\s+([\d.]+)", headers)
    assert minimum and all(v in ("14.0", "14.0.0") for v in minimum), (path, minimum)
    for line in run("otool", "-L", str(path)).splitlines()[1:]:
        dependency = line.strip().split(" (", 1)[0]
        assert dependency.startswith(("/System/Library/", "/usr/lib/")), (path, dependency)
    assert not re.search(r"cmd LC_RPATH\b", headers), (path, "unexpected runtime search path")
    print(f"Verified {path.relative_to(app)}: {arch}, macOS 14.0, system libraries only")
assert app / "Contents/MacOS/Starling" in images
print(f"Bundle {version}: signature integrity and metadata verified; not a notarization check")
