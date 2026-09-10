# Upgrade implementation

This directory holds the shared upgrade descriptor and versioned handlers. [types.go](types.go) couples a plan name,
store changes, and migration handler; [template/upgrade.go](template/upgrade.go) is a compiled scaffold that is not registered.
The live registry is [app/upgrades.go](../upgrades.go).

## Adding an upgrade

1. Copy the template to `app/upgrade/v<N>` for the actual release and rename its package and plan name to `v<N>`.
   The upgrade rehearsal and the nightly e2e workflow schedule the newest such directory's name as the plan, and
   `TestUpgradesAreNamedForTheirPackage` in [app/upgrades_test.go](../upgrades_test.go) holds the two together.
2. Give `Build` exactly the keepers the migration needs. Keep one-shot state changes before or after `RunMigrations`
   according to whether they require the old or new schema.
3. Register a builder in `pendingUpgrades` in `app/upgrades.go`; include the store changes in the same descriptor.
4. Extend the migration test to prove the intended state transition. Run `go test ./app/upgrade/...` and relevant app tests.
5. Use the [upgrade rehearsal](../../contrib/scripts/README.md#upgrade-rehearsal) with an old binary and the newly registered plan.

After an upgrade has executed, the following release removes its handler package and registry entry; git history retains
it. An empty registry is valid. The template's presence does not mean there is a scheduled or registered upgrade.

[Node operations](../../docs/operations/NODE_OPERATIONS.md) describes deployment and continuation export.
[The emergency runbook](../../docs/governance/EMERGENCY_SUBMISSION_RUNBOOK.md#committee-upgrades) describes committee scheduling and
operator binary custody. Scheduling a plan does not install a binary on an operator's behalf.
