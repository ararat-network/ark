// Package telemetrytest checks a process's exported series against a golden
// file. Dashboards and alert rules in another repository address series by
// name and label key, so a rename has to be a deliberate diff here first.
package telemetrytest

import (
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"
)

// Series lists gatherer's families whose name keep accepts, sorted, one line
// each: name, type, and sorted label keys. Values are not part of the contract.
func Series(gatherer prometheus.Gatherer, keep func(name string) bool) ([]string, error) {
	families, err := gatherer.Gather()
	if err != nil {
		return nil, err
	}
	lines := make([]string, 0, len(families))
	for _, family := range families {
		name := family.GetName()
		if !keep(name) {
			continue
		}
		keys := map[string]struct{}{}
		for _, m := range family.GetMetric() {
			for _, label := range m.GetLabel() {
				keys[label.GetName()] = struct{}{}
			}
		}
		line := fmt.Sprintf("%s %s %s", name, family.GetType(), strings.Join(slices.Sorted(maps.Keys(keys)), " "))
		lines = append(lines, strings.TrimSpace(line))
	}
	slices.Sort(lines)
	return lines, nil
}

// ArkSeries keeps Ark's own families and the resource series.
func ArkSeries(name string) bool {
	return strings.HasPrefix(name, "ark_") || name == "target_info"
}

// RequireGolden compares gatherer's kept series with the file at path, or
// rewrites the file when update is set.
func RequireGolden(tb testing.TB, gatherer prometheus.Gatherer, keep func(name string) bool, path string, update bool) {
	tb.Helper()

	lines, err := Series(gatherer, keep)
	require.NoError(tb, err)
	got := strings.Join(lines, "\n") + "\n"

	if update {
		require.NoError(tb, os.MkdirAll(filepath.Dir(path), 0o750))
		require.NoError(tb, os.WriteFile(path, []byte(got), 0o600))
		return
	}
	want, err := os.ReadFile(path)
	require.NoError(tb, err, "no golden file; run with -update-golden to write it")
	require.Equal(tb, string(want), got,
		"exported series changed. A renamed series breaks dashboards and alert rules outside this repository; if the change is deliberate, rerun with -update-golden and announce it")
}
