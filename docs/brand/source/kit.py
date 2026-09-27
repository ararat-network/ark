"""Ark logo kit (concept D, Whole): masters, lockups, colour variants and motifs.

Geometry is on a 256-unit canvas. The positive master is exact: the peak and the notch cut into the hull
are mirror images across the waterline. The reversed (white-on-dark) drawings are thinned by D_REV units,
because light shapes on a dark ground spread optically.
"""
import math, os, sys
HERE = os.path.dirname(os.path.abspath(__file__))
sys.path.insert(0, HERE)
sys.dont_write_bytecode = True   # the repo does not ignore __pycache__
from geom import f, poly, write

KIT = os.path.dirname(HERE)   # this file lives in <kit>/source/
NAVY, INK, PAPER, CLAY, TIDE = "#1E3A7B", "#0F1A2E", "#F6F3EC", "#B4532A", "#9BB0DC"
D_REV = 1.2
S2 = math.sqrt(2)

# ---------------------------------------------------------------- symbol

MAIN = dict(apex=18, h=80, gap=12, R=112)     # use at 32 px and up
SMALL = dict(apex=22, h=72, gap=24, R=112)    # use at 24 px and below

def symbol(apex, h, gap, R, d=0.0):
    """Peak above the waterline; its reflection is the notch cut into the hull. d > 0 thins every edge."""
    base = apex + h
    cy = base + gap
    a2, b2 = apex + d * S2, base - d
    hp = b2 - a2
    peak = poly((128, a2), (128 + hp, b2), (128 - hp, b2))
    Rr, yd = R - d, cy + d
    xo = math.sqrt(Rr * Rr - d * d)
    ny = cy + h + d * S2
    nh = ny - yd
    hull = (f"M{f(128-xo)} {f(yd)}H{f(128-nh)}L128 {f(ny)}L{f(128+nh)} {f(yd)}H{f(128+xo)}"
            f"A{f(Rr)} {f(Rr)} 0 0 1 {f(128-xo)} {f(yd)}Z")
    return [peak, hull]

def symbol_bounds(apex, h, gap, R):
    return (128 - R, apex, 128 + R, apex + h + gap + R)

# ---------------------------------------------------------------- wordmark "ark"

XT, BL, ASC = 72, 184, 32   # x-height line, baseline, ascender line

def rect(x, y, w, hh):
    return f"M{f(x)} {f(y)}H{f(x+w)}V{f(y+hh)}H{f(x)}Z"

def circle(cx, cy, r, ccw=False):
    s = 0 if ccw else 1
    return f"M{f(cx-r)} {f(cy)}A{f(r)} {f(r)} 0 1 {s} {f(cx+r)} {f(cy)}A{f(r)} {f(r)} 0 1 {s} {f(cx-r)} {f(cy)}Z"

def wordmark(s=24, d=0.0):
    """Geometric lowercase ark. s = stem width; round strokes are s + 2 (optical); d thins for reversed use."""
    s_ = s - 2 * d
    paths, x = [], 0.0
    # a: circle bowl (overshoots x-height and baseline by 2) + stem; counter is a real hole (evenodd)
    Ra = 58 - d
    b = s + 2
    cx = x + 58
    paths.append(("evenodd", circle(cx, 128, Ra) + circle(cx, 128, 58 - b + d)))
    paths.append(("nonzero", rect(x + 116 - s + d, XT + d, s_, BL - XT - 2 * d)))
    x += 116 + 28
    # r: stem + flat-topped arm with a round inner crotch
    Wr = 52
    ri = Wr - s
    paths.append(("nonzero", rect(x + d, XT + d, s_, BL - XT - 2 * d)))
    paths.append(("nonzero", f"M{f(x+d)} {f(XT+d)}H{f(x+Wr-d)}V{f(XT+s-d)}"
                             f"A{f(ri+d)} {f(ri+d)} 0 0 0 {f(x+s-d)} {f(XT+s+ri)}H{f(x+d)}Z"))
    x += Wr + 16
    # k: stem + 45-degree arm and leg that run 6 units into the stem (no seams). Kw puts the arm's terminal
    # right above the leg's foot. Thinning moves each 45-degree edge by d, which is d*sqrt(2) along x.
    Kw, hk, t = s + 56 + s / S2, s * S2, d * S2
    Ly = Kw - s + XT
    lx = x + s + (BL - Ly)
    rx = lx + hk
    jy = (x + s + Ly - rx + BL) / 2
    jx = x + s + Ly - jy
    paths.append(("nonzero", rect(x + d, ASC + d, s_, BL - ASC - 2 * d)))
    paths.append(("nonzero", poly((x + s - 6, Ly - hk + 6 + t), (x + s + Ly - hk - XT - d + t, XT + d),
                                  (x + s + Ly - XT - d - t, XT + d), (x + s - 6, Ly + 6 - t))))
    paths.append(("nonzero", poly((x + s - 6, Ly - 6 - t), (jx - t, jy), (rx - d - t, BL - d), (lx - d + t, BL - d))))
    right = max(x + Kw, rx)
    return paths, (0, XT - 2, right, BL + 2), (0, ASC, right, BL + 2)

# ---------------------------------------------------------------- composition

def g(paths, fill, transform=None):
    out = []
    for p in paths:
        rule, d = p if isinstance(p, tuple) else ("nonzero", p)
        r = ' fill-rule="evenodd"' if rule == "evenodd" else ""
        out.append(f'<path fill="{fill}"{r} d="{d}"/>')
    inner = "\n    ".join(out)
    return f'<g transform="{transform}">\n    {inner}\n  </g>' if transform else "  " + "\n  ".join(out)

def doc(vb, w, hh, title, body):
    return (f'<svg xmlns="http://www.w3.org/2000/svg" viewBox="{vb}" width="{f(w)}" height="{f(hh)}" role="img" '
            f'aria-labelledby="title">\n  <title id="title">{title}</title>\n  {body}\n</svg>\n')

def symbol_file(fill, reversed_, small=False):
    p = symbol(**(SMALL if small else MAIN), d=D_REV if reversed_ else 0.0)
    return doc("0 0 256 256", 256, 256, "Ark", g(p, fill))

SYM_SCALE, WM_GAP_K, PAD = 0.86, 0.75, 24

def horizontal_file(fill, reversed_):
    d = D_REV if reversed_ else 0.0
    sym = symbol(**MAIN, d=d)
    x0, y0, x1, y1 = symbol_bounds(**MAIN)
    k = SYM_SCALE
    ty = (256 - 256 * k) / 2
    gap = WM_GAP_K * MAIN["h"] * k
    wm, _, wb = wordmark(d=d * 0.5)
    wx = x1 * k + gap
    left, right = x0 * k - PAD, wx + wb[2] + PAD
    body = (g(sym, fill, f"translate(0 {f(ty)}) scale({k})") + "\n  " +
            g(wm, fill, f"translate({f(wx)} 0)"))
    return doc(f"{f(left)} 0 {f(right-left)} 256", right - left, 256, "Ark", body)

def stacked_file(fill, reversed_):
    d = D_REV if reversed_ else 0.0
    sym = symbol(**MAIN, d=d)
    x0, y0, x1, y1 = symbol_bounds(**MAIN)
    wm, _, wb = wordmark(d=d * 0.5)
    ws = 0.8
    wm_w = wb[2] * ws
    gap = 0.5 * MAIN["h"]
    width = max(x1 - x0, wm_w) + 2 * PAD
    sym_tx = (width - 256) / 2
    wm_tx = (width - wm_w) / 2
    wm_ty = y1 + gap - wb[1] * ws
    height = wm_ty + wb[3] * ws + PAD
    top = y0 - PAD
    body = (g(sym, fill, f"translate({f(sym_tx)} 0)") + "\n  " +
            g(wm, fill, f"translate({f(wm_tx)} {f(wm_ty)}) scale({ws})"))
    return doc(f"0 {f(top)} {f(width)} {f(height-top)}", width, height - top, "Ark", body)

def wordmark_file(fill, reversed_):
    wm, _, wb = wordmark(d=(D_REV * 0.5) if reversed_ else 0.0)
    ty = 128 - (wb[1] + wb[3]) / 2
    left, right = -PAD, wb[2] + PAD
    return doc(f"{f(left)} 0 {f(right-left)} 256", right - left, 256, "Ark", g(wm, fill, f"translate(0 {f(ty)})"))

def decks_file(colours):
    """Secondary motif: the three-deck hull, one fill per deck (top to keel)."""
    cy, R = 64, 112
    bands, y = [], cy
    for hh in (28, 28, 32):
        a = math.sqrt(R * R - (y - cy) ** 2)
        bb = math.sqrt(max(R * R - (y + hh - cy) ** 2, 0))
        if bb < 1e-6:
            bands.append(f"M{f(128-a)} {f(y)}H{f(128+a)}A{R} {R} 0 0 1 {f(128-a)} {f(y)}Z")
        else:
            bands.append(f"M{f(128-a)} {f(y)}H{f(128+a)}A{R} {R} 0 0 1 {f(128+bb)} {f(y+hh)}H{f(128-bb)}"
                         f"A{R} {R} 0 0 1 {f(128-a)} {f(y)}Z")
        y += hh + 12
    body = "\n  ".join(f'<path fill="{c}" d="{b}"/>' for c, b in zip(colours, bands))
    return doc("0 40 256 160", 256, 160, "Ark decks motif", body)

def main():
    out = {}
    for name, fn in (("ark-symbol", lambda c, r: symbol_file(c, r)),
                     ("ark-symbol-small", lambda c, r: symbol_file(c, r, small=True)),
                     ("ark-logo-horizontal", horizontal_file),
                     ("ark-logo-stacked", stacked_file),
                     ("ark-wordmark", wordmark_file)):
        for cname, colour, rev in (("navy", NAVY, False), ("black", "#000000", False), ("white", "#FFFFFF", True)):
            path = os.path.join(KIT, "svg", f"{name}-{cname}.svg")
            write(path, fn(colour, rev))
            out[path] = True
    write(os.path.join(KIT, "motifs", "ark-decks-navy.svg"), decks_file((NAVY, NAVY, NAVY)))
    write(os.path.join(KIT, "motifs", "ark-decks-funds.svg"), decks_file((NAVY, CLAY, TIDE)))
    print(f"wrote {len(out)} masters + 2 motifs")

if __name__ == "__main__":
    main()

def avatar_file():
    """Social avatar: white (thinned) symbol on a navy square, sized to stay inside a circular crop."""
    k = 0.58
    x0, y0, x1, y1 = symbol_bounds(**MAIN)
    cx, cy = (x0 + x1) / 2, 128 + 6   # optical centre: the hull carries the weight, so sit the bbox a touch high
    tx, ty = 128 - cx * k, 128 - ((y0 + y1) / 2) * k - 4
    body = f'<rect width="256" height="256" fill="{NAVY}"/>\n  ' + g(symbol(**MAIN, d=D_REV), "#FFFFFF", f"translate({f(tx)} {f(ty)}) scale({k})")
    return doc("0 0 256 256", 256, 256, "Ark", body)

if __name__ == "__main__" and "avatar" in sys.argv:
    write(os.path.join(KIT, "social", "ark-avatar.svg"), avatar_file())
    print("wrote avatar")

# ---------------------------------------------------------------- guideline figures (documentation, not logo files)

FONT = "font-family=\"Inter, -apple-system, 'Segoe UI', Helvetica, Arial, sans-serif\""

def construction_figure():
    apex, h, gap, R = MAIN["apex"], MAIN["h"], MAIN["gap"], MAIN["R"]
    base, cy = apex + h, apex + h + gap
    axis = base + gap / 2
    m, sc = 60, 2.0
    def T(x, y): return (m + x * sc, m + y * sc)
    body = [f'<rect width="100%" height="100%" fill="{PAPER}"/>',
            f'<g transform="translate({m} {m}) scale({sc})">' + g(symbol(**MAIN), NAVY).strip() + "</g>"]
    guide = 'fill="none" stroke="#B4532A" stroke-width="1.5" stroke-dasharray="6 5"'
    cx0, cy0 = T(128, cy)
    body.append(f'<circle cx="{f(cx0)}" cy="{f(cy0)}" r="{f(R*sc)}" {guide}/>')
    ax1, ay = T(-12, axis); ax2, _ = T(268, axis)
    body.append(f'<line x1="{f(ax1)}" y1="{f(ay)}" x2="{f(ax2)}" y2="{f(ay)}" {guide}/>')
    pts = " ".join(f"{f(a)},{f(b)}" for a, b in (T(128, apex), T(128 + h + gap / 2, axis), T(128, cy + h), T(128 - h - gap / 2, axis)))
    body.append(f'<polygon points="{pts}" {guide}/>')
    lx = m + 256 * sc + 70
    items = [("Hull", "half of one circle, radius 112, cut flat at the waterline"),
             ("Peak", "45-degree sides, 80 high"),
             ("Notch", "the peak reflected across the waterline; together they make the dashed diamond"),
             ("Gap", "12 on the 256 grid; the small drawing widens it to 24"),
             ("Waterline", "the dashed horizontal line, halfway through the gap")]
    y = m + 40
    for head, text in items:
        body.append(f'<text x="{lx}" y="{y}" {FONT} font-size="17" font-weight="600" fill="{INK}">{head}</text>')
        words, line, lines = text.split(), "", []
        for w in words:
            if len(line) + len(w) > 34:
                lines.append(line); line = w
            else:
                line = (line + " " + w).strip()
        lines.append(line)
        for i, ln in enumerate(lines):
            body.append(f'<text x="{lx}" y="{y + 24 + i * 21}" {FONT} font-size="15" fill="{INK}">{ln}</text>')
        y += 24 + len(lines) * 21 + 22
    W_, H_ = lx + 330, 256 * sc + 2 * m
    return doc(f"0 0 {f(W_)} {f(H_)}", W_, H_, "Ark symbol construction", "\n  ".join(body))

def clearspace_figure():
    src = open(os.path.join(KIT, "svg", "ark-logo-horizontal-navy.svg")).read()
    import re
    vb = [float(v) for v in re.search(r'viewBox="([^"]+)"', src).group(1).split()]
    inner = re.search(r"</title>\s*(.*)</svg>", src, re.S).group(1)
    P = MAIN["h"] * SYM_SCALE                     # clear-space unit: the peak's height, as drawn in the lockup
    ink_l = symbol_bounds(**MAIN)[0] * SYM_SCALE
    ink_t = (256 - 256 * SYM_SCALE) / 2 + MAIN["apex"] * SYM_SCALE
    ink_b = (256 - 256 * SYM_SCALE) / 2 + symbol_bounds(**MAIN)[3] * SYM_SCALE
    ink_r = vb[0] + vb[2] - PAD
    x0, y0, x1, y1 = ink_l - P, ink_t - P, ink_r + P, ink_b + P
    pad = 30
    body = [f'<rect x="{f(x0-pad)}" y="{f(y0-pad)}" width="{f(x1-x0+2*pad)}" height="{f(y1-y0+2*pad)}" fill="{PAPER}"/>',
            f'<rect x="{f(x0)}" y="{f(y0)}" width="{f(x1-x0)}" height="{f(y1-y0)}" fill="#E4E9F4"/>',
            f'<rect x="{f(ink_l)}" y="{f(ink_t)}" width="{f(ink_r-ink_l)}" height="{f(ink_b-ink_t)}" fill="{PAPER}"/>',
            inner.strip()]
    s = P / MAIN["h"]
    for (px, py) in ((x0, y0), (x1 - 2 * P / 2, y0), (x0, y1 - P), (x1 - P, y1 - P)):
        body.append(f'<path fill="{TIDE}" d="M{f(px + P/2)} {f(py)}L{f(px + P)} {f(py + P/2)}L{f(px)} {f(py + P/2)}Z" '
                    f'transform="translate(0 {f(P/4)})"/>')
    body.append(f'<text x="{f(x0)}" y="{f(y1 + 22)}" {FONT} font-size="14" fill="{INK}">Clear space on every side: '
                f'the height of the peak (P). Keep the shaded zone free of text and other marks.</text>')
    return doc(f"{f(x0-pad)} {f(y0-pad)} {f(x1-x0+2*pad)} {f(y1-y0+2*pad+16)}", (x1 - x0 + 2 * pad) * 1.5,
               (y1 - y0 + 2 * pad + 16) * 1.5, "Ark clear space", "\n  ".join(body))

if __name__ == "__main__" and "figures" in sys.argv:
    write(os.path.join(KIT, "figures", "construction.svg"), construction_figure())
    write(os.path.join(KIT, "figures", "clear-space.svg"), clearspace_figure())
    print("wrote figures")
