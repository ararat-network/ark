#!/bin/sh
# Rebuild the Ark logo kit from source/kit.py. Needs Python 3 and the logo-design skill's scripts
# (render_png.py, export_variants.py); set SKILL if they live elsewhere. The board is not rebuilt here.
set -eu
HERE=$(cd "$(dirname "$0")" && pwd)
KIT=$(dirname "$HERE")
SKILL=${SKILL:-$HOME/code/logo-design-skill/skills/logo-design/scripts}
cd "$KIT"

python3 source/kit.py
python3 source/kit.py avatar
python3 source/kit.py figures

# Web icons: app tiles from the main drawing, favicons from the small drawing.
python3 "$SKILL/export_variants.py" svg/ark-symbol-navy.svg --name ark --title Ark --icon-bg "#1E3A7B" \
  --web-icons --only app-icon square --out-dir web
python3 "$SKILL/export_variants.py" svg/ark-symbol-navy.svg --name ark --title Ark --icon-bg "#1E3A7B" \
  --favicon-source svg/ark-symbol-small-navy.svg --web-icons --only favicon --out-dir .webtmp
cp .webtmp/favicon.ico .webtmp/favicon-16.png .webtmp/favicon-32.png .webtmp/favicon-48.png .webtmp/favicon.svg web/
rm -rf .webtmp web/ark-favicon.svg
mv web/ark-square.svg social/ark-symbol-square.svg
python3 - <<'PY'
p = "web/favicon.svg"
s = open(p).read().replace('\n  <title id="title">Ark</title>', "", 1)
s = s.replace("<title>Ark</title>", "<title>Ark</title><style>@media (prefers-color-scheme: dark){path{fill:#9BB0DC}}</style>", 1)
open(p, "w").write(s)
PY

# Rasters.
R="$SKILL/render_png.py"
for c in navy black white; do
  for s in 512 1024 2048; do python3 "$R" "svg/ark-symbol-$c.svg" -o "png/ark-symbol-$c-$s.png" --size "$s"; done
done
python3 - "$R" <<'PY'
import re, subprocess, sys
R = sys.argv[1]
def aspect(p):
    v = [float(x) for x in re.search(r'viewBox="([^"]+)"', open(p).read()).group(1).split()]
    return v[2] / v[3]
for c in ("navy", "black", "white"):
    for name, sizes in (("ark-logo-horizontal", (1200, 2400)), ("ark-wordmark", (1200,)), ("ark-logo-stacked", (1024,))):
        a = aspect(f"svg/{name}-{c}.svg")
        for n in sizes:
            w, h = (round(n * a), n) if name == "ark-logo-stacked" else (n, round(n / a))
            subprocess.run(["python3", R, f"svg/{name}-{c}.svg", "-o", f"png/{name}-{c}-{max(w, h)}.png",
                            "--width", str(w), "--height", str(h)], check=True)
a = aspect("svg/ark-logo-horizontal-navy.svg")
subprocess.run(["python3", R, "svg/ark-logo-horizontal-navy.svg", "-o", "png/ark-logo-horizontal-navy-on-white-1200.png",
                "--width", "1200", "--height", str(round(1200 / a)), "--bg", "#ffffff"], check=True)
for s in (400, 1024):
    subprocess.run(["python3", R, "social/ark-avatar.svg", "-o", f"social/ark-avatar-{s}.png", "--size", str(s)], check=True)
subprocess.run(["python3", R, "figures/construction.svg", "-o", "figures/construction.png",
                "--width", "1162", "--height", "632", "--bg", "#F6F3EC"], check=True)
subprocess.run(["python3", R, "figures/clear-space.svg", "-o", "figures/clear-space.png",
                "--width", "1400", "--height", "640", "--bg", "#F6F3EC"], check=True)
PY
echo "kit rebuilt"
