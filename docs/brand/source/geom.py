"""Geometry helpers for the Ark logo sketches (256-unit canvas, y down)."""
import math, os

def f(v):
    s = f"{v:.2f}".rstrip("0").rstrip(".")
    return "0" if s in ("-0", "") else s

def dx(R, dy):
    return math.sqrt(max(R * R - dy * dy, 0.0))

def band(cx, cy, R, y1, y2):
    """Slice of the lower half-disc (centre cx,cy radius R) between y1 < y2, both >= cy."""
    a, b = dx(R, y1 - cy), dx(R, y2 - cy)
    if b < 1e-6:  # bottom cap
        return f"M{f(cx-a)} {f(y1)}H{f(cx+a)}A{f(R)} {f(R)} 0 0 1 {f(cx-a)} {f(y1)}Z"
    return (f"M{f(cx-a)} {f(y1)}H{f(cx+a)}A{f(R)} {f(R)} 0 0 1 {f(cx+b)} {f(y2)}"
            f"H{f(cx-b)}A{f(R)} {f(R)} 0 0 1 {f(cx-a)} {f(y1)}Z")

def cap_above(cx, cy, R, y0):
    """Part of the disc above the chord y0 (y0 may be above or below the centre)."""
    a = dx(R, y0 - cy)
    large = 1 if y0 > cy else 0
    return f"M{f(cx-a)} {f(y0)}A{f(R)} {f(R)} 0 {large} 1 {f(cx+a)} {f(y0)}Z"

def cap_below(cx, cy, R, y0):
    a = dx(R, y0 - cy)
    large = 1 if y0 < cy else 0
    return f"M{f(cx+a)} {f(y0)}A{f(R)} {f(R)} 0 {large} 1 {f(cx-a)} {f(y0)}Z"

def ring_above(cx, cy, R, r, y0):
    """Ring (R outer, r inner) above chord y0 > cy."""
    a, b = dx(R, y0 - cy), dx(r, y0 - cy)
    return (f"M{f(cx-a)} {f(y0)}A{f(R)} {f(R)} 0 1 1 {f(cx+a)} {f(y0)}H{f(cx+b)}"
            f"A{f(r)} {f(r)} 0 1 0 {f(cx-b)} {f(y0)}Z")

def poly(*pts):
    return "M" + "L".join(f"{f(x)} {f(y)}" for x, y in pts) + "Z"

def svg(name, paths, title, vb="0 0 256 256", w=256, h=256, fill="#111111", evenodd=False):
    rule = ' fill-rule="evenodd"' if evenodd else ""
    body = "\n".join(f'  <path fill="{fill}"{rule} d="{d}"/>' for d in paths)
    return (f'<svg xmlns="http://www.w3.org/2000/svg" viewBox="{vb}" width="{w}" height="{h}" role="img" '
            f'aria-labelledby="title">\n  <title id="title">{title}</title>\n{body}\n</svg>\n')

def write(path, text):
    os.makedirs(os.path.dirname(path), exist_ok=True)
    with open(path, "w") as fh:
        fh.write(text)
