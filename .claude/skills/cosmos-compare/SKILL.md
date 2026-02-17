# Cosmos SDK Pattern Comparison

Compare how a pattern or concept works in Terra Classic (cosmos-sdk v0.45) vs our modern chain (cosmos-sdk v0.53.5).

## Steps

1. Check `go.mod` for the exact cosmos-sdk version in this project
2. Find the Terra Classic implementation in `../classic-core/` — show the relevant code
3. Find or explain the modern cosmos-sdk v0.53 equivalent pattern
4. Highlight breaking changes, renamed packages, and migration steps
5. If the pattern already exists in `x/market/`, check whether it follows the modern convention or still uses legacy patterns

## Format

Structure the response as:

### Terra Classic (v0.45)
[Code/explanation of the old approach]

### Modern SDK (v0.53)
[Code/explanation of the new approach]

### Key Differences
[Bullet points of what changed and why]

### Action Items
[What needs to change in our codebase, if anything]
