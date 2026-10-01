# Ark logo kit

The master artwork for the Ark logo, the icons and rasters generated from it, and the code that draws them. This file
is the design record for whoever maintains the logo; [LOGO_GUIDELINES.md](LOGO_GUIDELINES.md) is the usage guide for
anyone putting the mark on a site, wallet, document or print. The mark is **Whole**, chosen on 28 September 2026; the wordmark was re-cut on 2 October 2026 after a ground-up review that put it beside other symbols.

## Contents

| Folder | What's inside |
|---|---|
| `svg/` | Masters: symbol, small symbol, horizontal, stacked and wordmark, each in navy, black and white (reversed, thinned), plus a flush horizontal lockup without side padding for Markdown (SVG only) |
| `png/` | Transparent PNGs: symbol 512/1024/2048, horizontal 1200/2400, stacked 1024, wordmark 1200, and a horizontal on white |
| `web/` | `favicon.ico`, `favicon.svg` (switches to Tide in dark mode), 16/32/48 PNGs, `apple-touch-icon.png`, `icon-192/512.png`, `maskable-512.png`, `ark-app-icon.svg`, `site.webmanifest`, `head-snippet.html` |
| `social/` | Circle-safe avatar (SVG, 400 and 1024 PNG) and a padded square symbol |
| `motifs/` | The three-deck secondary graphic, in the fund colours and in navy |
| `board/` | Presentation board (`ark-board.html`) and its five slides as PNGs, with README, terminal, website, app-icon, social and sticker mockups |
| `figures/` | Construction and clear-space figures used by the guidelines |
| `tests/` | SVG audit results and the top of the final test sheet |
| `source/` | `kit.py` and `geom.py`, which generate every SVG, plus `build.sh`, which also rebuilds icons and PNGs |

## Design rationale

**Idea.** The ark carried every kind together, so unity sits at the centre of the story. The mark shows it as two
halves of one shape: the peak that stands above the waterline, and the same peak reflected and held inside the hull.
Your eye completes the diamond, so the mark only reads as a whole. It also carries Ark's premise: rise and fall are
planned together.

**Geometry.** The mark uses two figures: a 45° triangle and one circle. The hull is half of a circle of radius 112 on
a 256 grid. The notch is the peak reflected exactly across the waterline. The outline keeps the Ledger app's Hull icon
(peak over half-disc), so wallets that already show Hull will stay recognisable.

**Small sizes.** Below 32 px the 12-unit waterline gap closes, so a separate small drawing widens it to 24 and trims
the peak and notch to 72. It covers the favicons and anything from 16 to 31 px.

**Reversed.** Light shapes on a dark ground spread optically, so every white file is drawn 1.2 units thinner on each
edge. Without that, the gap looks narrower and the mark heavier on dark backgrounds.

**Colour.** Navy `#1E3A7B` is the colour already chosen for the Ledger listing and README, and it reads 10.8 : 1 on
white. Blue is common across crypto, so the palette pairs it with a warm Clay accent and a light Tide for dark UIs.
The logo itself stays one colour.

**Wordmark.** A geometric lowercase "ark" drawn as paths, so no font licence is involved. Stems are 22 on the
256 grid, round strokes 24, so the word sits a little lighter than the symbol and the pair stays balanced. The a
is a circle with its stem inside the bowl's edge. The r's shoulder is a half ring, a small echo of the hull, and
it overshoots the x-height by 2 like the bowl. The k's arm and leg run at 60°, steeper than the peak, which keeps
the word narrow and quiet next to the symbol; the earlier cut ran them at 45° to match the peak and read heavier.
In the horizontal lockup the hull drops below the baseline, like a ship below its waterline.

**Decks motif.** Concept A's three decks are kept as a secondary graphic for the three funds, so that story is still
available outside the logo.

## Test results

- **Audit:** all 23 kit SVGs pass the skill's audit with no warnings: 5 score 100, 15 score 99, 3 score 98 (fractional
  viewBoxes only). Details are in `tests/svg-audit.txt`.
- **Test sheet:** the mark holds at 16, 32 and 48 px in the pixel test, reversed, blurred, in one colour, on navy, photo
  and pattern backgrounds, and in favicon, app-icon, avatar, header and business-card contexts. Only its top section is
  committed (`tests/ark-test-sheet-top.png`), because the full sheet embeds third-party reference logos. Regenerate it
  with the skill's `preview_sheet.py` on `svg/ark-symbol-navy.svg` and `svg/ark-symbol-small-navy.svg`.
- **Shelf test:** the mark stands apart from Ethereum, Bitcoin, Cardano, Arch Linux, Rocky Linux, Atlassian and the old
  Hull icon. The closest echo is Ethereum's diamond, but Ethereum's is tall and faceted, while this is a peak over a
  notched bowl.
- **Rotation:** rotated 180° it reads as a different sign, so the guidelines forbid rotating or flipping it.
- **Icons:** the app tile, maskable circle and avatar keep the mark inside their safe zones. The SVG favicon switches
  to Tide on dark browser tabs, where navy would be nearly invisible.
- **Reproducibility:** `source/kit.py` reproduces every SVG master byte-for-byte.

## Open items

1. **Trademark search.** The name Ark is already used in crypto (ARK, ark.io) and finance (ARK Invest). A professional
   search on the name and the mark is needed before launch.
2. **Pantone proofing.** The Pantone matches were picked by eye, and the CMYK values come from a generic profile. Proof
   both on paper before any print run.
3. **Print masters.** CMYK PDF or EPS files need a vector editor such as Illustrator or Inkscape. Neither is installed
   here, so the kit ships SVG masters.
4. **Ledger app.** `ledger-ark` still ships the Hull glyphs. Moving them to Whole means regenerating `app/ark.svg`, the
   16-grey 32/40/64 px glyphs and the 1-bit Apex 32/48 glyphs from the small drawing, and hand-pixelling the 14 px Nano
   and 16 px `icon_app` glyphs again.
5. **Fonts.** Inter and JetBrains Mono are suggestions; confirm them before rolling out the website or docs styling.
6. **Decks order.** Mapping Buffer, Reserve and Insurance from top to keel is a suggestion; change it if another order
   reads better in the whitepaper.

## Rebuilding

Edit the numbers in `source/kit.py` (`MAIN`, `SMALL`, `D_REV`, the palette, or the wordmark's `s`), then run
`source/build.sh`. It needs Python 3 and the logo-design skill's scripts (set `SKILL` if they aren't in
`~/code/logo-design-skill`). The board is rebuilt separately from `board/ark-board.json` with the skill's
`presentation_board.py`. The stock script prints placeholder commands in the terminal mockup; this board replaces them
with `arkd query oracle reference-denom` and `exchange-rates`.
