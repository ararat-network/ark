package app_test

import (
	"reflect"
	"runtime"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ararat-network/ark/app"
	apptestutil "github.com/ararat-network/ark/app/testutil"
)

// TestUpgradesAreNamedForTheirPackage checks that registered plans match their app/upgrade/v<N>
// directory names, which rehearsal and nightly e2e tooling schedule.
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
