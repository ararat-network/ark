package app_test

import (
	"reflect"
	"runtime"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ararat-network/ark/app"
	apptestutil "github.com/ararat-network/ark/app/testutil"
)

// TestUpgradesAreNamedForTheirPackage holds the convention upgrade.Upgrade
// states and the tooling reads: a plan is named for the app/upgrade/v<N>
// package that builds it. The upgrade rehearsal and the nightly e2e run both
// schedule the newest such directory's name, so a plan named otherwise would
// be rehearsed under a name no binary registers.
func TestUpgradesAreNamedForTheirPackage(t *testing.T) {
	if len(app.Upgrades) == 0 {
		t.Skip("no upgrade is registered")
	}
	arkApp := apptestutil.Setup(t, false)
	for _, build := range app.Upgrades {
		u := build(arkApp)
		handler := runtime.FuncForPC(reflect.ValueOf(u.Handler).Pointer()).Name()
		require.Contains(t, handler, "/app/upgrade/"+u.PlanName+".",
			"plan %q is built by %s, not by app/upgrade/%s", u.PlanName, handler, u.PlanName)
	}
}
