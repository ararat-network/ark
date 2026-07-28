# Per-Denom Oracle Target Transitions Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace `x/asset`'s single global pending oracle-target epoch with per-denom dated transition records, so unrelated assets stop blocking each other's lifecycle transitions.

**Architecture:** `OracleTargets` keeps a materialized active denom set plus a short, sorted list of `OracleTargetTransition` records (`{denom, direction, activation_vote_height}`). The target set at a vote height is the active set folded with every record whose activation height has arrived; the version bumps once per distinct activation height (a "batch"). Records are immutable once written and activate at schedule height + 2, which is what makes voter and tally agree — see the correctness argument in the spec. Scheduling contention becomes per denom instead of chain-global.

**Tech Stack:** Go, Cosmos SDK v0.54.2, `cosmossdk.io/collections`, gogo + pulsar dual protobuf generation (`make proto-gen`, Docker), testify (plain functions for `types/`, suites for `keeper/`).

**Spec:** `docs/superpowers/specs/2026-07-28-oracle-target-transitions-design.md`

---

## Scope

**In scope:** `x/asset` only — proto schema, fold, phase derivation, validation, per-denom scheduling, batched promotion, genesis, and tests.

**Explicitly out of scope** (deferred to the `x/asset` activation milestone, per the spec's "Implementation scoping" section): `abci/`, `x/oracle`, `app/`, and `oracle/sidecar`. Do not modify them. `x/asset` is dormant — nothing outside it imports it — so every commit here leaves the running chain untouched and the repo green.

## Background you need

Read these before starting. They are short.

- `x/asset/types/oracle_targets.go` — the file being rewritten.
- `x/asset/keeper/oracle_targets.go` — scheduling and promotion.
- The spec's "Correctness argument" section. Two invariants do all the safety work: a record is **immutable once written**, and **activation = schedule height + 2**. Never weaken either.

**Repo conventions that matter here** (from `CLAUDE.md`):

- Proto: every `nullable = false` field also needs `amino.dont_omitempty = true`.
- Tests in `x/*/types/` are plain functions, never suites. Tests in `x/*/keeper/` are suites.
- Table-driven tests with `t.Run()`. For validation, use the mutate pattern: start from a valid value, mutate one field per case.
- British spelling in test function names (`Randomised`, not `Randomized`).
- Test validation logic and parsing, not trivial constructors.

## File structure

| File | Responsibility | Change |
|---|---|---|
| `proto/ark/asset/v1/asset.proto` | `OracleTargets`, `OracleTargetTransition`, `OracleTargetDirection` | Modify |
| `proto/ark/asset/v1/event.proto` | `EventOracleTargetTransitionScheduled` | Modify |
| `x/asset/types/oracle_targets.go` | Fold, phase, validation | Rewrite |
| `x/asset/types/genesis.go` | Genesis target/asset consistency | Modify (lines 139-143) |
| `x/asset/keeper/oracle_targets.go` | Scheduling, batched promotion | Rewrite |
| `x/asset/types/oracle_targets_test.go` | Types tests | Rewrite |
| `x/asset/types/genesis_test.go` | Genesis fixtures | Modify |
| `x/asset/keeper/oracle_targets_test.go` | Keeper tests | Rewrite |
| `x/asset/keeper/{lifecycle,lifecycle_completion,settlement,asset_locks,grpc_query}_test.go` | Fixture migration | Modify |
| `docs/ASSET_MODULE_PLAN.md` | Amended invariants | Modify |

`x/asset/keeper/lifecycle.go` is **not** modified. Its seven scheduling call sites keep working because `Phase()` keeps its four values and `scheduleOracleTargetAddition`/`scheduleOracleTargetRemoval` keep their signatures.

---

## Task 1: Proto schema for transition records

**Files:**
- Modify: `proto/ark/asset/v1/asset.proto:81-95`
- Modify: `proto/ark/asset/v1/event.proto:36-42`

- [ ] **Step 1: Replace the `OracleTargets` and `PendingOracleTargets` messages**

In `proto/ark/asset/v1/asset.proto`, replace lines 81-95 (the `OracleTargets` and `PendingOracleTargets` messages) with:

```protobuf
// OracleTargets defines the materialized active target set and any scheduled
// per-denom transitions that have not yet activated. The target set for a vote
// height is the active set folded with every transition whose activation height
// has arrived.
message OracleTargets {
  repeated string denoms = 1;
  uint64 version = 2;
  reserved 3;
  reserved "pending";
  // transitions are sorted by activation height then denom, and hold at most
  // one entry per denom. Each entry is immutable once written.
  repeated OracleTargetTransition transitions = 4
      [ (gogoproto.nullable) = false, (amino.dont_omitempty) = true ];
}

// OracleTargetDirection identifies which way a scheduled transition moves a
// denom's target membership.
enum OracleTargetDirection {
  option (gogoproto.goproto_enum_prefix) = false;

  ORACLE_TARGET_DIRECTION_UNSPECIFIED = 0;
  ORACLE_TARGET_DIRECTION_ADD = 1;
  ORACLE_TARGET_DIRECTION_REMOVE = 2;
}

// OracleTargetTransition is one scheduled membership change for one denom.
message OracleTargetTransition {
  string denom = 1;
  OracleTargetDirection direction = 2 [ (amino.dont_omitempty) = true ];
  int64 activation_vote_height = 3 [ (amino.dont_omitempty) = true ];
}
```

Field 3 is reserved rather than reused: the old `pending` field had a different type, and reserving prevents a stale decoder from silently misreading a transition list as a pending epoch.

- [ ] **Step 2: Replace the scheduled event**

In `proto/ark/asset/v1/event.proto`, replace lines 36-42 (`EventOracleTargetsScheduled`) with:

```protobuf
// EventOracleTargetTransitionScheduled is emitted after one denom's target
// transition is scheduled.
message EventOracleTargetTransitionScheduled {
  string denom = 1;
  OracleTargetDirection direction = 2 [ (amino.dont_omitempty) = true ];
  int64 activation_vote_height = 3;
  // resulting_version is the target version in force once this transition
  // activates.
  uint64 resulting_version = 4;
}
```

Leave `EventOracleTargetsActivated` (lines 44-50) exactly as it is. It is emitted once per activation batch now, but its shape is unchanged.

- [ ] **Step 3: Verify `event.proto` can see the new enum**

Run: `grep -n "^import" proto/ark/asset/v1/event.proto`
Expected: a line importing `ark/asset/v1/asset.proto`. If it is absent, add `import "ark/asset/v1/asset.proto";` alongside the other imports.

- [ ] **Step 4: Format and lint the protos**

```bash
make proto-format && make proto-lint
```

Expected: both succeed with no output about `asset.proto` or `event.proto`.

- [ ] **Step 5: Regenerate**

```bash
make proto-gen
```

Expected: `x/asset/types/asset.pb.go`, `x/asset/types/event.pb.go`, `api/ark/asset/v1/asset.pulsar.go`, and `api/ark/asset/v1/event.pulsar.go` are modified. This runs Docker and takes a few minutes.

- [ ] **Step 6: Confirm the generated Go types**

Run: `grep -n "type OracleTargetTransition struct" -A 5 x/asset/types/asset.pb.go`
Expected: a struct with `Denom string`, `Direction OracleTargetDirection`, `ActivationVoteHeight int64`.

Run: `grep -n "Transitions \[\]OracleTargetTransition" x/asset/types/asset.pb.go`
Expected: one match — a value slice, not `[]*OracleTargetTransition`.

The repo will not compile until Task 4. That is expected: `oracle_targets.go` and `genesis.go` still reference the removed `Pending` field.

- [ ] **Step 7: Commit**

```bash
git add proto/ark/asset/v1/asset.proto proto/ark/asset/v1/event.proto x/asset/types/asset.pb.go x/asset/types/event.pb.go api/ark/asset/v1/
git commit -m "feat(asset)!: define per-denom oracle target transition records

Replace the single pending target epoch with a list of per-denom transition
records carrying denom, direction, and activation vote height. Reserve the
former pending field rather than reusing its number, so a stale decoder cannot
misread a transition list as a pending epoch.

Replace the scheduled event with a per-denom variant carrying the resulting
version. The activated event keeps its shape and is emitted once per activation
batch."
```

---

## Task 2: Fold — `AtHeight` over transition records

**Files:**
- Modify: `x/asset/types/oracle_targets.go:59-72`
- Test: `x/asset/types/oracle_targets_test.go:24-44`

- [ ] **Step 1: Write the failing tests**

Replace `TestOracleTargetsAtHeight` (lines 24-44) in `x/asset/types/oracle_targets_test.go` with:

```go
func TestOracleTargetsAtHeight(t *testing.T) {
	add := assettypes.OracleTargetDirection_ORACLE_TARGET_DIRECTION_ADD
	remove := assettypes.OracleTargetDirection_ORACLE_TARGET_DIRECTION_REMOVE

	tests := []struct {
		name        string
		transitions []assettypes.OracleTargetTransition
		voteHeight  int64
		wantVersion uint64
		wantDenoms  []string
	}{
		{
			name:        "no transitions",
			voteHeight:  100,
			wantVersion: 4,
			wantDenoms:  []string{"agold", "ausd"},
		},
		{
			name: "before activation",
			transitions: []assettypes.OracleTargetTransition{
				{Denom: "asilver", Direction: add, ActivationVoteHeight: 20},
			},
			voteHeight:  19,
			wantVersion: 4,
			wantDenoms:  []string{"agold", "ausd"},
		},
		{
			name: "at activation",
			transitions: []assettypes.OracleTargetTransition{
				{Denom: "asilver", Direction: add, ActivationVoteHeight: 20},
			},
			voteHeight:  20,
			wantVersion: 5,
			wantDenoms:  []string{"agold", "asilver", "ausd"},
		},
		{
			name: "removal at activation",
			transitions: []assettypes.OracleTargetTransition{
				{Denom: "agold", Direction: remove, ActivationVoteHeight: 20},
			},
			voteHeight:  20,
			wantVersion: 5,
			wantDenoms:  []string{"ausd"},
		},
		{
			name: "two records one batch bump version once",
			transitions: []assettypes.OracleTargetTransition{
				{Denom: "agold", Direction: remove, ActivationVoteHeight: 20},
				{Denom: "asilver", Direction: add, ActivationVoteHeight: 20},
			},
			voteHeight:  20,
			wantVersion: 5,
			wantDenoms:  []string{"asilver", "ausd"},
		},
		{
			name: "consecutive heights bump version twice",
			transitions: []assettypes.OracleTargetTransition{
				{Denom: "asilver", Direction: add, ActivationVoteHeight: 20},
				{Denom: "azinc", Direction: add, ActivationVoteHeight: 21},
			},
			voteHeight:  21,
			wantVersion: 6,
			wantDenoms:  []string{"agold", "asilver", "ausd", "azinc"},
		},
		{
			name: "later batch excluded at earlier height",
			transitions: []assettypes.OracleTargetTransition{
				{Denom: "asilver", Direction: add, ActivationVoteHeight: 20},
				{Denom: "azinc", Direction: add, ActivationVoteHeight: 21},
			},
			voteHeight:  20,
			wantVersion: 5,
			wantDenoms:  []string{"agold", "asilver", "ausd"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			targets := assettypes.OracleTargets{
				Denoms:      []string{"agold", "ausd"},
				Version:     4,
				Transitions: tt.transitions,
			}

			got := targets.AtHeight(tt.voteHeight)

			require.Equal(t, tt.wantVersion, got.Version)
			require.Equal(t, tt.wantDenoms, got.Denoms)
		})
	}
}

func TestOracleTargetsAtHeightDoesNotAliasState(t *testing.T) {
	targets := assettypes.OracleTargets{
		Denoms:  []string{"agold"},
		Version: 4,
		Transitions: []assettypes.OracleTargetTransition{
			{
				Denom:                "asilver",
				Direction:            assettypes.OracleTargetDirection_ORACLE_TARGET_DIRECTION_ADD,
				ActivationVoteHeight: 20,
			},
		},
	}

	before := targets.AtHeight(19)
	at := targets.AtHeight(20)
	before.Denoms[0] = "amutated"
	at.Denoms[0] = "amutated"

	require.Equal(t, []string{"agold"}, targets.Denoms)
	require.Equal(t, "asilver", targets.Transitions[0].Denom)
}
```

- [ ] **Step 2: Run tests to verify they fail**

```bash
go test ./x/asset/types/ -run TestOracleTargetsAtHeight -v
```

Expected: compile failure — `Transitions` is used but `AtHeight` still reads `Pending`, and `unknown field Transitions` will surface once the old field is gone. Either way it must not pass.

- [ ] **Step 3: Implement the fold**

In `x/asset/types/oracle_targets.go`, replace `AtHeight` (lines 59-72) with:

```go
// AtHeight returns the target epoch validators must report for voteHeight. It
// folds every transition that has activated by voteHeight into the active set,
// advancing the version once per distinct activation height. Transitions
// activating later are excluded, which is what lets a voter at height V and the
// tally of V agree despite reading different committed states.
func (v OracleTargets) AtHeight(voteHeight int64) OracleTargetSet {
	denoms := slices.Clone(v.Denoms)
	version := v.Version
	// Activation heights are validated positive, so zero is a safe sentinel for
	// "no batch applied yet".
	batchHeight := int64(0)
	for _, transition := range v.Transitions {
		if transition.ActivationVoteHeight > voteHeight {
			break
		}
		if transition.ActivationVoteHeight != batchHeight {
			version++
			batchHeight = transition.ActivationVoteHeight
		}
		denoms = ApplyOracleTargetTransition(denoms, transition)
	}

	return OracleTargetSet{Version: version, Denoms: denoms}
}

// ApplyOracleTargetTransition applies one transition to a sorted denom set. It
// is exported because batch promotion in the keeper applies the same rule.
func ApplyOracleTargetTransition(
	denoms []string,
	transition OracleTargetTransition,
) []string {
	index, found := slices.BinarySearch(denoms, transition.Denom)
	switch {
	case transition.Direction == OracleTargetDirection_ORACLE_TARGET_DIRECTION_ADD && !found:
		return slices.Insert(denoms, index, transition.Denom)
	case transition.Direction == OracleTargetDirection_ORACLE_TARGET_DIRECTION_REMOVE && found:
		return slices.Delete(denoms, index, index+1)
	default:
		return denoms
	}
}
```

The `break` is correct only because transitions are sorted by activation height; `Validate` enforces that in Task 4.

- [ ] **Step 4: Run tests to verify they pass**

```bash
go test ./x/asset/types/ -run TestOracleTargetsAtHeight -v
```

Expected: still failing to compile, because `Validate` and `Phase` in the same file reference `v.Pending`. Do not fix them here — Tasks 3 and 4 do. Instead verify the fold in isolation:

```bash
go vet ./x/asset/types/ 2>&1 | grep -c "AtHeight"
```

Expected: `0` — no vet complaint attributable to `AtHeight`.

- [ ] **Step 5: Commit**

```bash
git add x/asset/types/oracle_targets.go x/asset/types/oracle_targets_test.go
git commit -m "feat(asset): fold transition records into the target set

AtHeight folds every transition that has activated by the requested vote height
into the active denom set, advancing the version once per distinct activation
height so a batch of same-height records counts as one epoch.

Transitions activating after the requested height are excluded. That exclusion
is what lets a validator extending a vote at height V and the tally of V agree
on the target set despite reading different committed states."
```

---

## Task 3: Per-denom phase derivation

**Files:**
- Modify: `x/asset/types/oracle_targets.go:74-92`
- Test: `x/asset/types/oracle_targets_test.go:46-88`

- [ ] **Step 1: Write the failing test**

Replace `TestOracleTargetsPhase` (lines 46-88) in `x/asset/types/oracle_targets_test.go` with:

```go
func TestOracleTargetsPhase(t *testing.T) {
	targets := assettypes.OracleTargets{
		Denoms:  []string{"aactive", "aremoving"},
		Version: 1,
		Transitions: []assettypes.OracleTargetTransition{
			{
				Denom:                "aadding",
				Direction:            assettypes.OracleTargetDirection_ORACLE_TARGET_DIRECTION_ADD,
				ActivationVoteHeight: 10,
			},
			{
				Denom:                "aremoving",
				Direction:            assettypes.OracleTargetDirection_ORACLE_TARGET_DIRECTION_REMOVE,
				ActivationVoteHeight: 10,
			},
		},
	}
	tests := []struct {
		name  string
		denom string
		phase assettypes.OracleTargetPhase
	}{
		{
			name:  "off",
			denom: "aoff",
			phase: assettypes.OracleTargetPhaseOff,
		},
		{
			name:  "adding",
			denom: "aadding",
			phase: assettypes.OracleTargetPhaseAdding,
		},
		{
			name:  "active while other denoms transition",
			denom: "aactive",
			phase: assettypes.OracleTargetPhaseActive,
		},
		{
			name:  "removing",
			denom: "aremoving",
			phase: assettypes.OracleTargetPhaseRemoving,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.phase, targets.Phase(tt.denom))
		})
	}
}
```

The "active while other denoms transition" case is the behavioural change this whole design exists for: under the old model any in-flight transition made every other denom's schedule attempt fail.

- [ ] **Step 2: Run test to verify it fails**

```bash
go test ./x/asset/types/ -run TestOracleTargetsPhase -v
```

Expected: compile failure on `Transitions`.

- [ ] **Step 3: Implement per-denom phase**

In `x/asset/types/oracle_targets.go`, replace `Phase` (lines 74-92) with:

```go
// Phase returns denom's relationship to the active target set and its own
// scheduled transition, if it has one. Transitions scheduled for other denoms
// never affect this denom's phase.
func (v OracleTargets) Phase(denom string) OracleTargetPhase {
	for _, transition := range v.Transitions {
		if transition.Denom != denom {
			continue
		}
		if transition.Direction == OracleTargetDirection_ORACLE_TARGET_DIRECTION_ADD {
			return OracleTargetPhaseAdding
		}

		return OracleTargetPhaseRemoving
	}

	if _, active := slices.BinarySearch(v.Denoms, denom); active {
		return OracleTargetPhaseActive
	}

	return OracleTargetPhaseOff
}
```

Leave `OracleTargetPhase`, its constants, and `RequireOff` (lines 18-47) untouched.

- [ ] **Step 4: Run test to verify it passes**

```bash
go test ./x/asset/types/ -run TestOracleTargetsPhase -v
```

Expected: still a compile failure from `Validate` referencing `Pending`. Task 4 resolves the package. Do not proceed to Step 5 until Task 4 is done, then re-run this command and expect PASS.

- [ ] **Step 5: Commit (after Task 4 compiles the package)**

```bash
git add x/asset/types/oracle_targets.go x/asset/types/oracle_targets_test.go
git commit -m "feat(asset): derive target phase per denom

Phase now consults the denom's own scheduled transition rather than a
chain-global pending set, so a transition in flight for one asset no longer
reports every other asset as transitioning. The four phase values and RequireOff
are unchanged, which is what lets the seven lifecycle call sites keep their
existing gates."
```

---

## Task 4: Validation invariants

**Files:**
- Modify: `x/asset/types/oracle_targets.go:94-150`
- Test: `x/asset/types/oracle_targets_test.go:134-271`

- [ ] **Step 1: Write the failing tests**

Replace `TestOracleTargetsValidate` (lines 134-271) in `x/asset/types/oracle_targets_test.go` with:

```go
func TestOracleTargetsValidate(t *testing.T) {
	add := assettypes.OracleTargetDirection_ORACLE_TARGET_DIRECTION_ADD
	remove := assettypes.OracleTargetDirection_ORACLE_TARGET_DIRECTION_REMOVE
	valid := func() assettypes.OracleTargets {
		return assettypes.OracleTargets{
			Denoms:  []string{"agold", "ausd"},
			Version: 1,
		}
	}
	tooMany := make([]string, assettypes.MaxOracleTargets+1)
	for i := range tooMany {
		tooMany[i] = fmt.Sprintf("aasset%03d", i)
	}

	tests := []struct {
		name      string
		mutate    func(*assettypes.OracleTargets)
		expectErr string
	}{
		{
			name: "valid active",
		},
		{
			name: "valid empty active",
			mutate: func(targets *assettypes.OracleTargets) {
				targets.Denoms = nil
			},
		},
		{
			name: "valid addition",
			mutate: func(targets *assettypes.OracleTargets) {
				targets.Transitions = []assettypes.OracleTargetTransition{
					{Denom: "asilver", Direction: add, ActivationVoteHeight: 10},
				}
			},
		},
		{
			name: "valid same-height batch",
			mutate: func(targets *assettypes.OracleTargets) {
				targets.Transitions = []assettypes.OracleTargetTransition{
					{Denom: "agold", Direction: remove, ActivationVoteHeight: 10},
					{Denom: "asilver", Direction: add, ActivationVoteHeight: 10},
				}
			},
		},
		{
			name: "valid consecutive heights",
			mutate: func(targets *assettypes.OracleTargets) {
				targets.Transitions = []assettypes.OracleTargetTransition{
					{Denom: "asilver", Direction: add, ActivationVoteHeight: 10},
					{Denom: "azinc", Direction: add, ActivationVoteHeight: 11},
				}
			},
		},
		{
			name: "zero active version",
			mutate: func(targets *assettypes.OracleTargets) {
				targets.Version = 0
			},
			expectErr: "active vote-target version must be positive",
		},
		{
			name: "too many active targets",
			mutate: func(targets *assettypes.OracleTargets) {
				targets.Denoms = slices.Clone(tooMany)
			},
			expectErr: "exceeds maximum vote targets",
		},
		{
			name: "unsorted active targets",
			mutate: func(targets *assettypes.OracleTargets) {
				targets.Denoms = []string{"ausd", "agold"}
			},
			expectErr: "must be sorted by unique denom",
		},
		{
			name: "duplicate active target",
			mutate: func(targets *assettypes.OracleTargets) {
				targets.Denoms = []string{"agold", "agold"}
			},
			expectErr: "must be sorted by unique denom",
		},
		{
			name: "invalid target denom",
			mutate: func(targets *assettypes.OracleTargets) {
				targets.Denoms = []string{"aGOLD"}
			},
			expectErr: "canonical lowercase Ark-native base denom",
		},
		{
			name: "native target denom",
			mutate: func(targets *assettypes.OracleTargets) {
				targets.Denoms = []string{chain.NoahBaseDenom}
			},
			expectErr: "must not contain native denom anoah",
		},
		{
			name: "invalid transition denom",
			mutate: func(targets *assettypes.OracleTargets) {
				targets.Transitions = []assettypes.OracleTargetTransition{
					{Denom: "aSILVER", Direction: add, ActivationVoteHeight: 10},
				}
			},
			expectErr: "canonical lowercase Ark-native base denom",
		},
		{
			name: "native transition denom",
			mutate: func(targets *assettypes.OracleTargets) {
				targets.Transitions = []assettypes.OracleTargetTransition{
					{Denom: chain.NoahBaseDenom, Direction: add, ActivationVoteHeight: 10},
				}
			},
			expectErr: "must not contain native denom anoah",
		},
		{
			name: "unspecified direction",
			mutate: func(targets *assettypes.OracleTargets) {
				targets.Transitions = []assettypes.OracleTargetTransition{
					{Denom: "asilver", ActivationVoteHeight: 10},
				}
			},
			expectErr: "unspecified direction",
		},
		{
			name: "non-positive activation height",
			mutate: func(targets *assettypes.OracleTargets) {
				targets.Transitions = []assettypes.OracleTargetTransition{
					{Denom: "asilver", Direction: add},
				}
			},
			expectErr: "activation height must be positive",
		},
		{
			name: "duplicate denom transitions",
			mutate: func(targets *assettypes.OracleTargets) {
				targets.Transitions = []assettypes.OracleTargetTransition{
					{Denom: "asilver", Direction: add, ActivationVoteHeight: 10},
					{Denom: "asilver", Direction: remove, ActivationVoteHeight: 11},
				}
			},
			expectErr: "more than one scheduled Oracle target transition",
		},
		{
			name: "unsorted transitions by height",
			mutate: func(targets *assettypes.OracleTargets) {
				targets.Transitions = []assettypes.OracleTargetTransition{
					{Denom: "asilver", Direction: add, ActivationVoteHeight: 11},
					{Denom: "azinc", Direction: add, ActivationVoteHeight: 10},
				}
			},
			expectErr: "must be sorted by activation height and denom",
		},
		{
			name: "unsorted transitions by denom within a batch",
			mutate: func(targets *assettypes.OracleTargets) {
				targets.Transitions = []assettypes.OracleTargetTransition{
					{Denom: "azinc", Direction: add, ActivationVoteHeight: 10},
					{Denom: "asilver", Direction: add, ActivationVoteHeight: 10},
				}
			},
			expectErr: "must be sorted by activation height and denom",
		},
		{
			name: "addition of an active denom",
			mutate: func(targets *assettypes.OracleTargets) {
				targets.Transitions = []assettypes.OracleTargetTransition{
					{Denom: "agold", Direction: add, ActivationVoteHeight: 10},
				}
			},
			expectErr: "is already an active target",
		},
		{
			name: "removal of an absent denom",
			mutate: func(targets *assettypes.OracleTargets) {
				targets.Transitions = []assettypes.OracleTargetTransition{
					{Denom: "asilver", Direction: remove, ActivationVoteHeight: 10},
				}
			},
			expectErr: "is not an active target",
		},
		{
			name: "additions exceed the target cap",
			mutate: func(targets *assettypes.OracleTargets) {
				targets.Denoms = slices.Clone(tooMany[:assettypes.MaxOracleTargets])
				targets.Transitions = []assettypes.OracleTargetTransition{
					{Denom: "azinc", Direction: add, ActivationVoteHeight: 10},
				}
			},
			expectErr: "exceeds maximum vote targets",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			targets := valid()
			if tt.mutate != nil {
				tt.mutate(&targets)
			}

			err := targets.Validate()
			if tt.expectErr == "" {
				require.NoError(t, err)
				return
			}
			require.ErrorContains(t, err, tt.expectErr)
		})
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

```bash
go test ./x/asset/types/ -run TestOracleTargetsValidate -v
```

Expected: compile failure on `Transitions`.

- [ ] **Step 3: Implement validation**

In `x/asset/types/oracle_targets.go`, replace `Validate` (lines 94-126) with the following. Leave `validateVoteTargetDenoms` (lines 128-150) exactly as it is.

```go
// Validate checks active target and scheduled transition invariants.
func (v OracleTargets) Validate() error {
	if v.Version == 0 {
		return fmt.Errorf("active vote-target version must be positive")
	}
	if err := validateVoteTargetDenoms("active vote targets", v.Denoms); err != nil {
		return err
	}

	additions := 0
	scheduled := make(map[string]struct{}, len(v.Transitions))
	for i, transition := range v.Transitions {
		if err := transition.Validate(); err != nil {
			return err
		}
		if i > 0 {
			previous := v.Transitions[i-1]
			if transition.ActivationVoteHeight < previous.ActivationVoteHeight ||
				(transition.ActivationVoteHeight == previous.ActivationVoteHeight &&
					transition.Denom <= previous.Denom) {
				return fmt.Errorf(
					"oracle target transitions must be sorted by activation height and denom",
				)
			}
		}
		if _, duplicate := scheduled[transition.Denom]; duplicate {
			return fmt.Errorf(
				"asset %s has more than one scheduled Oracle target transition",
				transition.Denom,
			)
		}
		scheduled[transition.Denom] = struct{}{}

		_, active := slices.BinarySearch(v.Denoms, transition.Denom)
		switch transition.Direction {
		case OracleTargetDirection_ORACLE_TARGET_DIRECTION_ADD:
			if active {
				return fmt.Errorf(
					"scheduled Oracle target addition %s is already an active target",
					transition.Denom,
				)
			}
			additions++
		case OracleTargetDirection_ORACLE_TARGET_DIRECTION_REMOVE:
			if !active {
				return fmt.Errorf(
					"scheduled Oracle target removal %s is not an active target",
					transition.Denom,
				)
			}
		}
	}

	// The cap must hold at every future height, not only now, so pending
	// additions count against it.
	if scheduledCount := len(v.Denoms) + additions; scheduledCount > MaxOracleTargets {
		return fmt.Errorf(
			"scheduled vote targets count %d exceeds maximum vote targets %d",
			scheduledCount,
			MaxOracleTargets,
		)
	}

	return nil
}

// Validate checks one scheduled transition in isolation.
func (t OracleTargetTransition) Validate() error {
	if err := chain.ValidateNativeBaseDenom(t.Denom); err != nil {
		return fmt.Errorf("oracle target transition %w", err)
	}
	if t.Denom == chain.NoahBaseDenom {
		return fmt.Errorf(
			"oracle target transition must not contain native denom %s",
			t.Denom,
		)
	}
	if t.Direction != OracleTargetDirection_ORACLE_TARGET_DIRECTION_ADD &&
		t.Direction != OracleTargetDirection_ORACLE_TARGET_DIRECTION_REMOVE {
		return fmt.Errorf(
			"oracle target transition for %s has an unspecified direction",
			t.Denom,
		)
	}
	if t.ActivationVoteHeight <= 0 {
		return fmt.Errorf(
			"oracle target transition activation height must be positive: %d",
			t.ActivationVoteHeight,
		)
	}

	return nil
}
```

- [ ] **Step 4: Fix the genesis staged-set derivation**

`x/asset/types/genesis.go:139-143` still reads `Pending`. Replace those lines:

```go
	active := denomSet(gs.OracleTargets.Denoms)
	staged := make(map[string]bool, len(active)+len(gs.OracleTargets.Transitions))
	for denom := range active {
		staged[denom] = true
	}
	for _, transition := range gs.OracleTargets.Transitions {
		staged[transition.Denom] =
			transition.Direction == OracleTargetDirection_ORACLE_TARGET_DIRECTION_ADD
	}
```

Then, because `staged` can now hold explicit `false` entries for scheduled removals, change the staged existence loop at lines 150-154 from:

```go
	for denom := range staged {
		if _, exists := assets[denom]; !exists {
			return fmt.Errorf("pending vote target %s has no registered asset", denom)
		}
	}
```

to:

```go
	for denom, isStaged := range staged {
		if !isStaged {
			continue
		}
		if _, exists := assets[denom]; !exists {
			return fmt.Errorf("staged vote target %s has no registered asset", denom)
		}
	}
```

`validateAssetTargetState` (lines 222-267) needs no change: it takes `active` and `staged` booleans, and both still mean exactly what they meant before.

- [ ] **Step 5: Run the full types package**

```bash
go test ./x/asset/types/ 2>&1 | tail -30
```

Expected at this point: `TestOracleTargetsAtHeight`, `TestOracleTargetsPhase`, `TestOracleTargetsValidate`, and `TestOracleTargetPhaseRequireOff` all pass. `TestGenesisStateValidate` still fails — its fixtures use `PendingOracleTargets`. Task 5 fixes those.

- [ ] **Step 6: Commit**

```bash
git add x/asset/types/oracle_targets.go x/asset/types/oracle_targets_test.go x/asset/types/genesis.go
git commit -m "feat(asset): validate per-denom target transition invariants

Replace the single-pending-epoch rules with per-record invariants: at most one
transition per denom, sorted by activation height then denom, additions absent
from the active set, removals present in it, and a positive activation height
with a specified direction.

The target cap now counts pending additions, so it holds at every future height
rather than only at the current one. Sortedness is load-bearing beyond tidiness:
the fold stops at the first transition past the requested height.

Derive the genesis staged set by applying transitions to the active set, which
keeps validateAssetTargetState and its per-status rules unchanged."
```

---

## Task 5: Migrate genesis test fixtures

**Files:**
- Modify: `x/asset/types/genesis_test.go` (lines 85, 97, 108, 143, 158, 341, 375, 462)

- [ ] **Step 1: Inspect each fixture site**

```bash
grep -n "PendingOracleTargets" -A 6 x/asset/types/genesis_test.go
```

Each site builds a pending epoch. Convert each to the equivalent transition list. The mechanical rule:

- A pending set that **adds** denom `X` to the active set becomes
  `Transitions: []assettypes.OracleTargetTransition{{Denom: "X", Direction: add, ActivationVoteHeight: H}}`.
- A pending set that **removes** denom `X` becomes the same with `Direction: remove`.
- The pending set's `Version` field disappears; the fold derives it.

- [ ] **Step 2: Add direction shorthands to the test file**

At the top of the first test function that needs them, add:

```go
	add := assettypes.OracleTargetDirection_ORACLE_TARGET_DIRECTION_ADD
	remove := assettypes.OracleTargetDirection_ORACLE_TARGET_DIRECTION_REMOVE
```

If a case needs only one direction, declare only that one — an unused variable is a compile error in Go.

- [ ] **Step 3: Convert each site**

Work through them one at a time. Example conversion, for a site that staged an addition of `assets[7].Denom`:

```go
// Before:
genesis.OracleTargets.Pending = &assettypes.PendingOracleTargets{
	Denoms:               append(slices.Clone(genesis.OracleTargets.Denoms), assets[7].Denom),
	Version:              2,
	ActivationVoteHeight: 10,
}

// After:
genesis.OracleTargets.Transitions = []assettypes.OracleTargetTransition{
	{Denom: assets[7].Denom, Direction: add, ActivationVoteHeight: 10},
}
```

If a converted case's `expectErr` string referenced "pending vote target", update it to "staged vote target" to match the new message from Task 4 Step 4.

- [ ] **Step 4: Run the genesis tests**

```bash
go test ./x/asset/types/ -run TestGenesis -v 2>&1 | tail -40
```

Expected: PASS. If a case fails on an error-string mismatch, reconcile it against the messages written in Task 4 — do not loosen an assertion to make it pass.

- [ ] **Step 5: Run the whole types package**

```bash
go test ./x/asset/types/
```

Expected: `ok  	ark/x/asset/types`.

- [ ] **Step 6: Commit**

```bash
git add x/asset/types/genesis_test.go
git commit -m "test(asset): migrate genesis fixtures to transition records

Convert pending-epoch fixtures to the equivalent per-denom transition records
and align the staged-target error string with the new genesis message."
```

---

## Task 6: Per-denom scheduling

**Files:**
- Modify: `x/asset/keeper/oracle_targets.go:27-189`
- Test: `x/asset/keeper/oracle_targets_test.go`

- [ ] **Step 1: Write the failing tests**

In `x/asset/keeper/oracle_targets_test.go`, replace `TestScheduleOracleTargetAddition` (lines 56 onward, through the end of that function) with these three tests. Keep the suite scaffolding (lines 1-54) as it is.

```go
func (s *OracleTargetsTestSuite) TestScheduleOracleTargetAddition() {
	assets := assettypes.DefaultGenesisState().Assets
	addedAsset := assets[0]
	addedAsset.Status = assettypes.AssetStatus_ASSET_STATUS_PENDING
	activeAsset := assets[1]
	s.setAssets(addedAsset, activeAsset)
	s.Require().NoError(s.keeper.OracleTargets.Set(s.ctx, assettypes.OracleTargets{
		Denoms:  []string{activeAsset.Denom},
		Version: 1,
	}))

	s.Require().NoError(s.keeper.scheduleOracleTargetAddition(s.ctx, addedAsset.Denom))

	blockHeight := sdk.UnwrapSDKContext(s.ctx).BlockHeight()
	activationHeight := blockHeight + assettypes.OracleTargetActivationDelayBlocks
	expected := assettypes.OracleTargets{
		Denoms:  []string{activeAsset.Denom},
		Version: 1,
		Transitions: []assettypes.OracleTargetTransition{
			{
				Denom:                addedAsset.Denom,
				Direction:            assettypes.OracleTargetDirection_ORACLE_TARGET_DIRECTION_ADD,
				ActivationVoteHeight: activationHeight,
			},
		},
	}
	actual, err := s.keeper.OracleTargets.Get(s.ctx)
	s.Require().NoError(err)
	s.Require().Equal(expected, actual)

	before, err := s.keeper.GetOracleTargets(s.ctx, activationHeight-1)
	s.Require().NoError(err)
	s.Require().Equal(uint64(1), before.Version)
	s.Require().Equal([]string{activeAsset.Denom}, before.Denoms)

	at, err := s.keeper.GetOracleTargets(s.ctx, activationHeight)
	s.Require().NoError(err)
	s.Require().Equal(uint64(2), at.Version)
	s.Require().Equal(
		[]string{addedAsset.Denom, activeAsset.Denom},
		at.Denoms,
	)

	s.requireEvent(&assettypes.EventOracleTargetTransitionScheduled{
		Denom:                addedAsset.Denom,
		Direction:            assettypes.OracleTargetDirection_ORACLE_TARGET_DIRECTION_ADD,
		ActivationVoteHeight: activationHeight,
		ResultingVersion:     2,
	})
}

func (s *OracleTargetsTestSuite) TestScheduleOracleTargetsForDistinctDenomsShareOneBatch() {
	assets := assettypes.DefaultGenesisState().Assets
	addedAsset := assets[0]
	addedAsset.Status = assettypes.AssetStatus_ASSET_STATUS_PENDING
	removedAsset := assets[1]
	removedAsset.Status = assettypes.AssetStatus_ASSET_STATUS_SUSPENDED
	s.setAssets(addedAsset, removedAsset)
	s.Require().NoError(s.keeper.OracleTargets.Set(s.ctx, assettypes.OracleTargets{
		Denoms:  []string{removedAsset.Denom},
		Version: 1,
	}))

	// The second schedule must succeed. Under the previous chain-global pending
	// epoch it failed with ErrOracleTargetTransitionPending, which is what made
	// emergency multi-suspend revert.
	s.Require().NoError(s.keeper.scheduleOracleTargetAddition(s.ctx, addedAsset.Denom))
	s.Require().NoError(s.keeper.scheduleOracleTargetRemoval(s.ctx, removedAsset.Denom))

	actual, err := s.keeper.OracleTargets.Get(s.ctx)
	s.Require().NoError(err)
	s.Require().Len(actual.Transitions, 2)

	activationHeight := sdk.UnwrapSDKContext(s.ctx).BlockHeight() +
		assettypes.OracleTargetActivationDelayBlocks
	at, err := s.keeper.GetOracleTargets(s.ctx, activationHeight)
	s.Require().NoError(err)
	// Both records share one activation height, so they form one batch and
	// advance the version once.
	s.Require().Equal(uint64(2), at.Version)
	s.Require().Equal([]string{addedAsset.Denom}, at.Denoms)
}

func (s *OracleTargetsTestSuite) TestScheduleOracleTargetIsIdempotent() {
	assets := assettypes.DefaultGenesisState().Assets
	addedAsset := assets[0]
	addedAsset.Status = assettypes.AssetStatus_ASSET_STATUS_PENDING
	s.setAssets(addedAsset)
	s.Require().NoError(s.keeper.OracleTargets.Set(s.ctx, assettypes.OracleTargets{
		Denoms:  nil,
		Version: 1,
	}))

	s.Require().NoError(s.keeper.scheduleOracleTargetAddition(s.ctx, addedAsset.Denom))
	s.Require().NoError(s.keeper.scheduleOracleTargetAddition(s.ctx, addedAsset.Denom))

	actual, err := s.keeper.OracleTargets.Get(s.ctx)
	s.Require().NoError(err)
	s.Require().Len(actual.Transitions, 1)
}

func (s *OracleTargetsTestSuite) TestScheduleOracleTargetRejectsOppositeDirection() {
	assets := assettypes.DefaultGenesisState().Assets
	asset := assets[0]
	asset.Status = assettypes.AssetStatus_ASSET_STATUS_SUSPENDED
	s.setAssets(asset)
	s.Require().NoError(s.keeper.OracleTargets.Set(s.ctx, assettypes.OracleTargets{
		Denoms:  []string{asset.Denom},
		Version: 1,
	}))

	s.Require().NoError(s.keeper.scheduleOracleTargetRemoval(s.ctx, asset.Denom))

	err := s.keeper.scheduleOracleTargetAddition(s.ctx, asset.Denom)
	s.Require().ErrorIs(err, assettypes.ErrOracleTargetTransitionPending)
}
```

- [ ] **Step 2: Add the event assertion helper**

The suite needs `requireEvent`. Check whether it already exists:

```bash
grep -n "func (s \*OracleTargetsTestSuite) requireEvent" x/asset/keeper/oracle_targets_test.go
```

If it does not exist, add it at the end of the file:

```go
func (s *OracleTargetsTestSuite) requireEvent(expected proto.Message) {
	typedEvent, err := sdk.TypedEventToEvent(expected)
	s.Require().NoError(err)
	s.Require().Contains(
		sdk.UnwrapSDKContext(s.ctx).EventManager().Events(),
		typedEvent,
	)
}
```

- [ ] **Step 3: Run tests to verify they fail**

```bash
go test ./x/asset/keeper/ -run TestOracleTargetsTestSuite -v 2>&1 | tail -20
```

Expected: compile failure — `Transitions`, `OracleTargetDirection`, and `EventOracleTargetTransitionScheduled` are referenced but the keeper still builds pending epochs.

- [ ] **Step 4: Implement per-denom scheduling**

In `x/asset/keeper/oracle_targets.go`, replace the block from line 54 (the `return k.scheduleOracleTargets(` inside `scheduleOracleTargetAddition`) through line 189 (`oracleTargetDenomsWith`). Keep the status precondition checks at lines 27-53 and 61-89 exactly as they are — only the tail call of each changes.

`scheduleOracleTargetAddition` ends with:

```go
	return k.scheduleOracleTargetTransition(
		ctx,
		denom,
		types.OracleTargetDirection_ORACLE_TARGET_DIRECTION_ADD,
	)
}
```

`scheduleOracleTargetRemoval` ends with:

```go
	return k.scheduleOracleTargetTransition(
		ctx,
		denom,
		types.OracleTargetDirection_ORACLE_TARGET_DIRECTION_REMOVE,
	)
}
```

Both drop their now-unused `oracleTargets, err := k.OracleTargets.Get(ctx)` prelude, since `scheduleOracleTargetTransition` reads the item itself.

Then replace `scheduleOracleTargets`, `prepareOracleTargetSchedule`, and `oracleTargetDenomsWith` with:

```go
func (k Keeper) scheduleOracleTargetTransition(
	ctx context.Context,
	denom string,
	direction types.OracleTargetDirection,
) error {
	oracleTargets, err := k.OracleTargets.Get(ctx)
	if err != nil {
		return fmt.Errorf("getting Oracle targets: %w", err)
	}
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	scheduled, transition, changed, err := prepareOracleTargetTransition(
		oracleTargets,
		denom,
		direction,
		sdkCtx.BlockHeight(),
	)
	if err != nil {
		return err
	}
	if !changed {
		return nil
	}

	if err := k.OracleTargets.Set(ctx, scheduled); err != nil {
		return fmt.Errorf("scheduling Oracle target transition: %w", err)
	}

	if err := sdkCtx.EventManager().EmitTypedEvent(
		&types.EventOracleTargetTransitionScheduled{
			Denom:                transition.Denom,
			Direction:            transition.Direction,
			ActivationVoteHeight: transition.ActivationVoteHeight,
			ResultingVersion: scheduled.
				AtHeight(transition.ActivationVoteHeight).
				Version,
		},
	); err != nil {
		return fmt.Errorf("emitting scheduled Oracle target transition: %w", err)
	}

	return nil
}

// prepareOracleTargetTransition stages one denom's transition. Contention is
// per denom: a transition in flight for another denom is irrelevant here.
func prepareOracleTargetTransition(
	oracleTargets types.OracleTargets,
	denom string,
	direction types.OracleTargetDirection,
	blockHeight int64,
) (types.OracleTargets, types.OracleTargetTransition, bool, error) {
	for _, scheduled := range oracleTargets.Transitions {
		if scheduled.Denom != denom {
			continue
		}
		if scheduled.Direction == direction {
			return oracleTargets, types.OracleTargetTransition{}, false, nil
		}

		// A record is immutable once written, so the opposite direction has to
		// wait for activation rather than amending or cancelling this one.
		return types.OracleTargets{}, types.OracleTargetTransition{}, false, sdkerrors.Wrapf(
			types.ErrOracleTargetTransitionPending,
			"asset %s has a conflicting Oracle target transition activating at vote height %d",
			denom,
			scheduled.ActivationVoteHeight,
		)
	}

	_, active := slices.BinarySearch(oracleTargets.Denoms, denom)
	switch {
	case direction == types.OracleTargetDirection_ORACLE_TARGET_DIRECTION_ADD && active,
		direction == types.OracleTargetDirection_ORACLE_TARGET_DIRECTION_REMOVE && !active:
		return oracleTargets, types.OracleTargetTransition{}, false, nil
	}

	transition := types.OracleTargetTransition{
		Denom:                denom,
		Direction:            direction,
		ActivationVoteHeight: blockHeight + types.OracleTargetActivationDelayBlocks,
	}
	scheduled := oracleTargets
	scheduled.Transitions = append(
		slices.Clone(oracleTargets.Transitions),
		transition,
	)
	slices.SortFunc(scheduled.Transitions, compareOracleTargetTransitions)
	if err := scheduled.Validate(); err != nil {
		return types.OracleTargets{}, types.OracleTargetTransition{}, false, fmt.Errorf(
			"validating scheduled Oracle target transition: %w",
			err,
		)
	}

	return scheduled, transition, true, nil
}

func compareOracleTargetTransitions(a, b types.OracleTargetTransition) int {
	if a.ActivationVoteHeight != b.ActivationVoteHeight {
		return cmp.Compare(a.ActivationVoteHeight, b.ActivationVoteHeight)
	}

	return cmp.Compare(a.Denom, b.Denom)
}
```

Add `"cmp"` to the import block and remove `"math"` if the version-overflow guard was its only user (Task 7 confirms).

- [ ] **Step 5: Run tests to verify they pass**

```bash
go test ./x/asset/keeper/ -run TestOracleTargetsTestSuite/TestScheduleOracleTarget -v 2>&1 | tail -30
```

Expected: the four scheduling tests PASS. Other tests in the package still fail on `Pending` fixtures; Tasks 7 and 8 clear those.

- [ ] **Step 6: Commit**

```bash
git add x/asset/keeper/oracle_targets.go x/asset/keeper/oracle_targets_test.go
git commit -m "feat(asset): schedule oracle target transitions per denom

Scheduling now appends one immutable record for the requested denom instead of
staging a whole replacement epoch, so a transition in flight for another asset no
longer blocks this one. Two assets scheduled in the same block share an
activation height and therefore one version bump.

Re-requesting the same direction is a no-op, which keeps governance retries
safe. The opposite direction is rejected while a record is in flight: amending
it would break the immutability the height-addressable read depends on."
```

---

## Task 7: Batched promotion

**Files:**
- Modify: `x/asset/keeper/oracle_targets.go:191-386`
- Test: `x/asset/keeper/oracle_targets_test.go`

- [ ] **Step 1: Write the failing tests**

Append to `x/asset/keeper/oracle_targets_test.go`:

```go
func (s *OracleTargetsTestSuite) TestAdvanceOracleTargetsPromotesOneBatch() {
	assets := assettypes.DefaultGenesisState().Assets
	addedAsset := assets[0]
	addedAsset.Status = assettypes.AssetStatus_ASSET_STATUS_PENDING
	keptAsset := assets[1]
	s.setAssets(addedAsset, keptAsset)
	s.Require().NoError(s.keeper.OracleTargets.Set(s.ctx, assettypes.OracleTargets{
		Denoms:  []string{keptAsset.Denom},
		Version: 1,
		Transitions: []assettypes.OracleTargetTransition{
			{
				Denom:                addedAsset.Denom,
				Direction:            assettypes.OracleTargetDirection_ORACLE_TARGET_DIRECTION_ADD,
				ActivationVoteHeight: 10,
			},
		},
	}))

	removed, err := s.keeper.AdvanceOracleTargets(s.ctx)
	s.Require().NoError(err)
	s.Require().Empty(removed)

	actual, err := s.keeper.OracleTargets.Get(s.ctx)
	s.Require().NoError(err)
	s.Require().Empty(actual.Transitions)
	s.Require().Equal(uint64(2), actual.Version)
	s.Require().Equal(
		[]string{addedAsset.Denom, keptAsset.Denom},
		actual.Denoms,
	)
}

func (s *OracleTargetsTestSuite) TestAdvanceOracleTargetsPromotesConsecutiveBatches() {
	assets := assettypes.DefaultGenesisState().Assets
	firstAsset := assets[0]
	firstAsset.Status = assettypes.AssetStatus_ASSET_STATUS_PENDING
	secondAsset := assets[1]
	secondAsset.Status = assettypes.AssetStatus_ASSET_STATUS_PENDING
	s.setAssets(firstAsset, secondAsset)
	s.Require().NoError(s.keeper.OracleTargets.Set(s.ctx, assettypes.OracleTargets{
		Denoms:  nil,
		Version: 1,
		Transitions: []assettypes.OracleTargetTransition{
			{
				Denom:                firstAsset.Denom,
				Direction:            assettypes.OracleTargetDirection_ORACLE_TARGET_DIRECTION_ADD,
				ActivationVoteHeight: 9,
			},
			{
				Denom:                secondAsset.Denom,
				Direction:            assettypes.OracleTargetDirection_ORACLE_TARGET_DIRECTION_ADD,
				ActivationVoteHeight: 10,
			},
		},
	}))

	_, err := s.keeper.AdvanceOracleTargets(s.ctx)
	s.Require().NoError(err)

	actual, err := s.keeper.OracleTargets.Get(s.ctx)
	s.Require().NoError(err)
	s.Require().Empty(actual.Transitions)
	// Two distinct activation heights are two batches, so the version advances
	// twice even though both are promoted in one block.
	s.Require().Equal(uint64(3), actual.Version)
	s.Require().Equal(
		[]string{firstAsset.Denom, secondAsset.Denom},
		actual.Denoms,
	)
}

func (s *OracleTargetsTestSuite) TestAdvanceOracleTargetsLeavesFutureBatches() {
	assets := assettypes.DefaultGenesisState().Assets
	asset := assets[0]
	asset.Status = assettypes.AssetStatus_ASSET_STATUS_PENDING
	s.setAssets(asset)
	scheduled := assettypes.OracleTargets{
		Denoms:  nil,
		Version: 1,
		Transitions: []assettypes.OracleTargetTransition{
			{
				Denom:                asset.Denom,
				Direction:            assettypes.OracleTargetDirection_ORACLE_TARGET_DIRECTION_ADD,
				ActivationVoteHeight: 11,
			},
		},
	}
	s.Require().NoError(s.keeper.OracleTargets.Set(s.ctx, scheduled))

	removed, err := s.keeper.AdvanceOracleTargets(s.ctx)
	s.Require().NoError(err)
	s.Require().Empty(removed)

	actual, err := s.keeper.OracleTargets.Get(s.ctx)
	s.Require().NoError(err)
	s.Require().Equal(scheduled, actual)
}
```

The suite's block height is 10 (`SetupTest`, line 51), so activation height 9 is overdue, 10 is due, and 11 is not.

- [ ] **Step 2: Run tests to verify they fail**

```bash
go test ./x/asset/keeper/ -run TestOracleTargetsTestSuite/TestAdvanceOracleTargets -v 2>&1 | tail -20
```

Expected: FAIL — `AdvanceOracleTargets` still reads `Pending`.

- [ ] **Step 3: Implement batched promotion**

In `x/asset/keeper/oracle_targets.go`, replace `AdvanceOracleTargets` (lines 191-359) and `oracleTargetChanges` (lines 366-386) with the following. Keep the `assetStatusChange` struct (lines 361-364).

```go
// AdvanceOracleTargets promotes every due transition batch after the previous
// vote height has been consumed. It must run after vote extensions are
// processed: the tally for vote height V reads the fold at V, and promoting a
// batch due at V before that read would validate votes against a set no
// validator could have seen. Removed denoms are returned for subsequent Oracle
// rate pruning; x/asset does not own exchange rates.
func (k Keeper) AdvanceOracleTargets(ctx context.Context) ([]string, error) {
	oracleTargets, err := k.OracleTargets.Get(ctx)
	if err != nil {
		return nil, fmt.Errorf("getting Oracle targets: %w", err)
	}
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	blockHeight := sdkCtx.BlockHeight()

	var removed []string
	for {
		batch := dueOracleTargetBatch(oracleTargets.Transitions, blockHeight)
		if len(batch) == 0 {
			break
		}
		promoted, batchRemoved, err := k.activateOracleTargetBatch(
			ctx,
			oracleTargets,
			batch,
			blockHeight,
		)
		if err != nil {
			return nil, err
		}
		oracleTargets = promoted
		removed = append(removed, batchRemoved...)
	}
	if removed == nil {
		return nil, nil
	}

	return removed, nil
}

// dueOracleTargetBatch returns the leading run of transitions that share the
// earliest activation height, when that height has arrived.
func dueOracleTargetBatch(
	transitions []types.OracleTargetTransition,
	blockHeight int64,
) []types.OracleTargetTransition {
	if len(transitions) == 0 || transitions[0].ActivationVoteHeight > blockHeight {
		return nil
	}
	batchHeight := transitions[0].ActivationVoteHeight
	size := 0
	for _, transition := range transitions {
		if transition.ActivationVoteHeight != batchHeight {
			break
		}
		size++
	}

	return transitions[:size]
}

// activateOracleTargetBatch applies one activation batch: it re-validates each
// asset, completes retirement status moves, records retirement residuals,
// advances the target set by one version, and emits the batch's events.
func (k Keeper) activateOracleTargetBatch(
	ctx context.Context,
	oracleTargets types.OracleTargets,
	batch []types.OracleTargetTransition,
	blockHeight int64,
) (types.OracleTargets, []string, error) {
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	added := make([]string, 0, len(batch))
	removed := make([]string, 0, len(batch))
	statusChanges := make([]assetStatusChange, 0, len(batch))
	residualRecords := make([]types.ResolutionRecord, 0, len(batch))

	for _, transition := range batch {
		asset, err := k.getAsset(ctx, transition.Denom)
		if err != nil {
			return types.OracleTargets{}, nil, err
		}

		if transition.Direction == types.OracleTargetDirection_ORACLE_TARGET_DIRECTION_ADD {
			if asset.Status != types.AssetStatus_ASSET_STATUS_PENDING &&
				asset.Status != types.AssetStatus_ASSET_STATUS_SUSPENDED {
				return types.OracleTargets{}, nil, sdkerrors.Wrapf(
					types.ErrInvalidAssetTransition,
					"%s asset %s cannot activate an Oracle target addition",
					asset.Status,
					transition.Denom,
				)
			}
			added = append(added, transition.Denom)

			continue
		}

		removed = append(removed, transition.Denom)

		var newStatus types.AssetStatus
		switch asset.Status {
		case types.AssetStatus_ASSET_STATUS_PENDING,
			types.AssetStatus_ASSET_STATUS_ISSUANCE_HALTED:
			newStatus = types.AssetStatus_ASSET_STATUS_RETIRED
		case types.AssetStatus_ASSET_STATUS_SUSPENDED,
			types.AssetStatus_ASSET_STATUS_WRITTEN_OFF:
			// Suspension and write-off both schedule removal and move
			// immediately; activation only completes the Oracle side.
			continue
		default:
			return types.OracleTargets{}, nil, sdkerrors.Wrapf(
				types.ErrInvalidAssetTransition,
				"%s asset %s cannot activate an Oracle target removal",
				asset.Status,
				transition.Denom,
			)
		}
		updated := asset
		// Epoch activation is an automatic completion and never advances the
		// asset version.
		if err := updated.Complete(newStatus); err != nil {
			return types.OracleTargets{}, nil, err
		}
		statusChanges = append(statusChanges, assetStatusChange{
			before: asset,
			after:  updated,
		})

		// Retirement finalised while priced leaves redemption open until
		// removal activates. Whatever supply survives is the residual
		// governance already approved, and entering RETIRED must disclose it.
		if asset.Status != types.AssetStatus_ASSET_STATUS_ISSUANCE_HALTED {
			continue
		}
		supply := k.bankKeeper.GetSupply(ctx, transition.Denom)
		if !supply.IsPositive() {
			continue
		}
		record := types.ResolutionRecord{
			Denom: transition.Denom,
			Kind:  types.ResolutionKind_RESOLUTION_KIND_RETIREMENT_RESIDUAL,
			// The completion does not advance the version, so the record
			// carries the version the last governance transition set. It is
			// still unique: leaving RETIRED requires ReactivateAsset, which
			// advances the version past every record already appended.
			Version:           updated.Version,
			ResolutionHeight:  blockHeight,
			OutstandingSupply: supply,
		}
		if err := record.Validate(); err != nil {
			return types.OracleTargets{}, nil, sdkerrors.Wrapf(
				types.ErrInvalidAssetTransition,
				"recording retirement residual for asset %s: %v",
				transition.Denom,
				err,
			)
		}
		// Batch activation writes directly to the block store, so the
		// uniqueness guard has to run before any of the writes below.
		if err := k.requireResolutionRecordAbsent(ctx, record); err != nil {
			return types.OracleTargets{}, nil, err
		}
		residualRecords = append(residualRecords, record)
	}

	promoted := types.OracleTargets{
		Denoms:      slices.Clone(oracleTargets.Denoms),
		Version:     oracleTargets.Version + 1,
		Transitions: slices.Clone(oracleTargets.Transitions[len(batch):]),
	}
	for _, transition := range batch {
		promoted.Denoms = types.ApplyOracleTargetTransition(promoted.Denoms, transition)
	}
	if err := promoted.Validate(); err != nil {
		return types.OracleTargets{}, nil, fmt.Errorf(
			"validating promoted Oracle targets: %w",
			err,
		)
	}

	for _, change := range statusChanges {
		if err := k.Assets.Set(ctx, change.after.Denom, change.after); err != nil {
			return types.OracleTargets{}, nil, fmt.Errorf(
				"setting activated asset %s: %w",
				change.after.Denom,
				err,
			)
		}
	}
	for _, record := range residualRecords {
		if err := k.appendResolutionRecord(ctx, record); err != nil {
			return types.OracleTargets{}, nil, err
		}
	}
	if err := k.OracleTargets.Set(ctx, promoted); err != nil {
		return types.OracleTargets{}, nil, fmt.Errorf("advancing Oracle targets: %w", err)
	}
	if err := sdkCtx.EventManager().EmitTypedEvent(
		&types.EventOracleTargetsActivated{
			Version:       promoted.Version,
			AddedDenoms:   added,
			RemovedDenoms: removed,
		},
	); err != nil {
		return types.OracleTargets{}, nil, fmt.Errorf("emitting activated Oracle targets: %w", err)
	}
	for _, record := range residualRecords {
		if err := sdkCtx.EventManager().EmitTypedEvent(
			&types.EventAssetResolved{ResolutionRecord: record},
		); err != nil {
			return types.OracleTargets{}, nil, fmt.Errorf(
				"emitting retirement residual for asset %s: %w",
				record.Denom,
				err,
			)
		}
	}
	for _, change := range statusChanges {
		if err := sdkCtx.EventManager().EmitTypedEvent(
			&types.EventAssetStatusChanged{
				Denom:     change.after.Denom,
				OldStatus: change.before.Status,
				NewStatus: change.after.Status,
				Version:   change.after.Version,
			},
		); err != nil {
			return types.OracleTargets{}, nil, fmt.Errorf(
				"emitting activated status for asset %s: %w",
				change.after.Denom,
				err,
			)
		}
	}

	return promoted, removed, nil
}
```

`types.ApplyOracleTargetTransition` is the exported helper written in Task 2, so batch promotion and the fold apply the same rule from one definition.

One invariant worth understanding rather than just trusting: `promoted.Validate()` re-checks that every *remaining* record still satisfies "addition absent from the active set, removal present in it" against the newly promoted set. It holds because at most one record exists per denom, so a batch only ever changes membership for denoms that have no remaining record.

- [ ] **Step 4: Remove the dead version-overflow guard**

The old code guarded `oracleTargets.Version == math.MaxUint64` at line 152. That guard lived in `prepareOracleTargetSchedule`, which Task 6 deleted. Confirm `"math"` is no longer imported:

```bash
grep -n '"math"' x/asset/keeper/oracle_targets.go
```

Expected: no output. If present, remove the import.

- [ ] **Step 5: Run tests to verify they pass**

```bash
go test ./x/asset/keeper/ -run TestOracleTargetsTestSuite -v 2>&1 | tail -40
```

Expected: all `TestAdvanceOracleTargets` and `TestScheduleOracleTarget` cases PASS. Other suites in the package still fail on fixtures; Task 8 clears them.

- [ ] **Step 6: Commit**

```bash
git add x/asset/keeper/oracle_targets.go x/asset/types/oracle_targets.go x/asset/keeper/oracle_targets_test.go
git commit -m "feat(asset): promote due target transitions in batches

Promotion drains every batch whose activation height has arrived, one version
bump per distinct height, so records scheduled in consecutive blocks both land
without either being dropped or silently delayed.

Per-asset re-validation, retirement status completion, and residual recording
carry over unchanged from epoch promotion and now run per record. Removed denoms
are accumulated across batches and returned for rate pruning, keeping exchange
rates behind the oracle keeper boundary.

Document why this must run after vote-extension processing: the tally for vote
height V reads the fold at V, and promoting a batch due at V first would
validate votes against a set no validator could have seen."
```

---

## Task 8: Migrate remaining keeper test fixtures

**Files:**
- Modify: `x/asset/keeper/lifecycle_test.go`, `lifecycle_completion_test.go`, `settlement_test.go`, `asset_locks_test.go`, `grpc_query_test.go`

- [ ] **Step 1: Enumerate the remaining sites**

```bash
grep -rn "PendingOracleTargets\|\.Pending" x/asset/keeper/*_test.go | grep -v SettlementPlan
```

Expected: sites in the five files listed above. `oracle_targets_test.go` should no longer appear — Tasks 6 and 7 rewrote it.

- [ ] **Step 2: Convert fixtures**

Apply the same mechanical rule as Task 5. Three shapes recur:

Staging an addition:

```go
// Before:
targets.Pending = &types.PendingOracleTargets{
	Denoms:               []string{priced.Denom, assets[7].Denom},
	Version:              2,
	ActivationVoteHeight: 12,
}

// After — the record is the delta against targets.Denoms, not the whole set:
targets.Transitions = []types.OracleTargetTransition{
	{
		Denom:                assets[7].Denom,
		Direction:            types.OracleTargetDirection_ORACLE_TARGET_DIRECTION_ADD,
		ActivationVoteHeight: 12,
	},
}
```

Staging a removal (the pending set omits a denom the active set has):

```go
targets.Transitions = []types.OracleTargetTransition{
	{
		Denom:                priced.Denom,
		Direction:            types.OracleTargetDirection_ORACLE_TARGET_DIRECTION_REMOVE,
		ActivationVoteHeight: 12,
	},
}
```

Assertions of the form `s.Require().NotNil(targets.Pending)` plus `s.Require().Empty(targets.Pending.Denoms)` were asserting "a removal of the only target is staged". They become:

```go
s.Require().Len(targets.Transitions, 1)
s.Require().Equal(
	types.OracleTargetDirection_ORACLE_TARGET_DIRECTION_REMOVE,
	targets.Transitions[0].Direction,
)
```

Assertions of `s.Require().Nil(actual.Pending)` become `s.Require().Empty(actual.Transitions)`.

- [ ] **Step 3: Convert the scheduled-event assertions**

`lifecycle_test.go:805-812` and `:866-873` assert `EventOracleTargetsScheduled`. Replace each with the per-denom event. For example:

```go
// Before:
&types.EventOracleTargetsScheduled{
	ActiveVersion:        targets.Version,
	PendingVersion:       targets.Pending.Version,
	ActivationVoteHeight: targets.Pending.ActivationVoteHeight,
}

// After:
&types.EventOracleTargetTransitionScheduled{
	Denom:                priced.Denom,
	Direction:            types.OracleTargetDirection_ORACLE_TARGET_DIRECTION_REMOVE,
	ActivationVoteHeight: targets.Transitions[0].ActivationVoteHeight,
	ResultingVersion:     targets.Version + 1,
}
```

Substitute the denom and direction each call site actually schedules — read the surrounding test to determine which.

- [ ] **Step 4: Add the genesis roundtrip test**

`x/asset/keeper/genesis.go` needs no change — it stores and loads the `OracleTargets` item wholesale — but nothing yet proves records survive a roundtrip, or that an overdue record imported from genesis is absorbed rather than stranded. Append to `x/asset/keeper/genesis_test.go`:

```go
func (s *GenesisTestSuite) TestGenesisRoundTripPreservesTransitions() {
	genesis := types.DefaultGenesisState()
	// assets[0] is ACTIVE and an active target in the default genesis, so a
	// scheduled removal is the consistent in-flight record here.
	removedDenom := genesis.Assets[0].Denom
	genesis.OracleTargets.Transitions = []types.OracleTargetTransition{
		{
			Denom:                removedDenom,
			Direction:            types.OracleTargetDirection_ORACLE_TARGET_DIRECTION_REMOVE,
			ActivationVoteHeight: 100,
		},
	}
	s.Require().NoError(genesis.Validate())

	s.Require().NoError(s.keeper.InitGenesis(s.ctx, genesis))
	exported, err := s.keeper.ExportGenesis(s.ctx)
	s.Require().NoError(err)

	s.Require().Equal(genesis.OracleTargets, exported.OracleTargets)
}

func (s *GenesisTestSuite) TestAdvanceAbsorbsOverdueImportedTransition() {
	genesis := types.DefaultGenesisState()
	addedDenom := genesis.Assets[0].Denom
	// Drop the denom from the active set and import a scheduled addition whose
	// activation height is already behind the current block height.
	genesis.OracleTargets.Denoms = slices.Delete(
		slices.Clone(genesis.OracleTargets.Denoms),
		0,
		1,
	)
	genesis.Assets[0].Status = types.AssetStatus_ASSET_STATUS_PENDING
	genesis.OracleTargets.Transitions = []types.OracleTargetTransition{
		{
			Denom:                addedDenom,
			Direction:            types.OracleTargetDirection_ORACLE_TARGET_DIRECTION_ADD,
			ActivationVoteHeight: 1,
		},
	}
	s.Require().NoError(genesis.Validate())
	s.Require().NoError(s.keeper.InitGenesis(s.ctx, genesis))

	removed, err := s.keeper.AdvanceOracleTargets(s.ctx)
	s.Require().NoError(err)
	s.Require().Empty(removed)

	actual, err := s.keeper.OracleTargets.Get(s.ctx)
	s.Require().NoError(err)
	s.Require().Empty(actual.Transitions)
	s.Require().Contains(actual.Denoms, addedDenom)
}
```

Check the suite's actual name and context height before pasting:

```bash
grep -n "type.*TestSuite struct" -A 8 x/asset/keeper/genesis_test.go | head -20
grep -n "WithBlockHeight" x/asset/keeper/genesis_test.go
```

If the suite uses a different name than `GenesisTestSuite`, or its block height is not above 1, adjust the receiver and the activation heights so the first test's record stays in the future and the second's stays in the past. Add `"slices"` to the file's imports if absent.

- [ ] **Step 5: Run the keeper package**

```bash
go test ./x/asset/keeper/ 2>&1 | tail -40
```

Expected: `ok  	ark/x/asset/keeper`. If a lifecycle test fails on a *behavioural* assertion rather than a fixture shape, stop and investigate — Tasks 6 and 7 were meant to preserve every lifecycle behaviour, so a real behavioural failure is a bug in those tasks, not a fixture to adjust.

- [ ] **Step 6: Run the full module**

```bash
go test ./x/asset/...
```

Expected: `ok` for `ark/x/asset/keeper`, `ark/x/asset/types`, and any other package in the module.

- [ ] **Step 7: Confirm nothing outside x/asset broke**

```bash
go build ./... && go test ./abci/... ./x/oracle/... 2>&1 | tail -20
```

Expected: build succeeds; `abci` and `x/oracle` tests pass untouched. If `go build ./...` fails on a package unrelated to this change, report the exact blocker rather than fixing it here — the repo has pre-existing checkout drift.

- [ ] **Step 8: Commit**

```bash
git add x/asset/keeper/
git commit -m "test(asset): migrate keeper fixtures to transition records

Convert pending-epoch fixtures to per-denom transition records and replace the
scheduled-event assertions with the per-denom event. Assertions that used a
non-nil pending epoch with an empty denom set to mean a staged removal now
assert the removal record directly, which is what they were always testing.

Add genesis coverage for records in flight: a roundtrip proving transitions
survive export and import, and an overdue imported record proving the first
promotion after import absorbs it rather than stranding it."
```

---

## Task 9: Amend the plan document

**Files:**
- Modify: `docs/ASSET_MODULE_PLAN.md:314-323`, `:645-656`

- [ ] **Step 1: Amend the preserved-invariants list**

`docs/ASSET_MODULE_PLAN.md:649-656` lists what `x/asset` preserves when it absorbs the target scheduler. Replace lines 651-652:

```markdown
- one immutable pending target set at a time;
- complete sorted target snapshots rather than deltas;
```

with:

```markdown
- immutable transition records, each activating at its scheduled height;
- one scheduled transition per denom, with contention scoped to that denom;
```

Leave lines 649, 650, 653, 654, 655, and 656 unchanged — vote-height-based epoch selection, the two-height activation boundary, old-epoch aggregation before promotion, rate pruning after promotion, explicit target versioning, and empty target sets all still hold.

- [ ] **Step 2: Reframe the derived-state table**

Replace the table at lines 318-323 and its following sentence:

```markdown
| Current set | Effective next set | Meaning |
| --- | --- | --- |
| absent | absent | Oracle off |
| absent | present | target addition pending |
| present | present | Oracle active |
| present | absent | target removal pending |
```

with:

```markdown
| Active set | Scheduled transition | Meaning |
| --- | --- | --- |
| absent | none | Oracle off |
| absent | addition | target addition pending |
| present | none | Oracle active |
| present | removal | target removal pending |
```

Then replace the sentence beginning "When no pending epoch exists, the effective next set equals the current set." with:

```markdown
A denom with no scheduled transition keeps its active membership. Transitions
scheduled for other denoms never affect this denom's phase.
```

- [ ] **Step 3: Add a pointer to the design document**

Under the "Oracle target epochs" heading at line 645, add after the first paragraph:

```markdown
The per-denom transition model is specified in
`docs/superpowers/specs/2026-07-28-oracle-target-transitions-design.md`,
including the correctness argument for multiple transitions in flight and the
consume-before-promote ordering the preblock depends on.
```

- [ ] **Step 4: Check for stale references**

```bash
grep -n "pending target set\|pending epoch\|pending vote target" docs/ASSET_MODULE_PLAN.md
```

Review each hit. Update any that assert the single-pending model. Lines describing a denom's *own* pending transition are still accurate and should be left alone.

- [ ] **Step 5: Commit**

```bash
git add docs/ASSET_MODULE_PLAN.md
git commit -m "docs(asset): amend target invariants for per-denom transitions

Replace the single-pending-set and snapshot invariants with the per-denom record
invariants that superseded them. Immutability is retained per record and is what
the height-addressable read depends on; the one-at-a-time constraint is what the
redesign removed.

Reframe the derived-state table around a denom's own scheduled transition rather
than a chain-global effective next set, and point at the design document for the
correctness argument."
```

---

## Task 10: Final verification

**Files:** none

- [ ] **Step 1: Run the module**

```bash
go test ./x/asset/...
```

Expected: `ok` for every package.

- [ ] **Step 2: Confirm the old model is fully gone**

```bash
grep -rn "PendingOracleTargets" x/asset/ --include="*.go" | grep -v "\.pb\.go"
```

Expected: no output. Hits in `.pb.go` are acceptable only if the generated file retains a reserved-field artifact; a hit in hand-written code means a site was missed.

- [ ] **Step 3: Confirm no accidental blast radius**

```bash
git diff --stat main -- abci/ x/oracle/ app/ oracle/
```

Expected: no output. This milestone must not touch those trees.

- [ ] **Step 4: Build the binary**

```bash
go build -o build/arkd ./cmd/arkd
```

Expected: succeeds.

- [ ] **Step 5: Report**

State plainly which commands were run and their actual results. If anything failed, say so with the output rather than describing the work as complete.

---

## Verification summary

| Check | Command |
|---|---|
| Types tests | `go test ./x/asset/types/` |
| Keeper tests | `go test ./x/asset/keeper/` |
| Whole module | `go test ./x/asset/...` |
| Untouched neighbours | `go test ./abci/... ./x/oracle/...` |
| Blast radius | `git diff --stat main -- abci/ x/oracle/ app/ oracle/` |
| Binary | `go build -o build/arkd ./cmd/arkd` |
