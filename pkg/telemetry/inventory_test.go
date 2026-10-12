package telemetry

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// inventoryRow matches a table row's first cell, a backticked series name.
var inventoryRow = regexp.MustCompile("^\\| `([a-z_]+)` \\|")

// The process monitoring inventory names exactly the series each process's golden fixture pins, so a new
// series cannot ship undocumented and a removed one cannot linger in the table.
func TestSeriesInventoryMatchesFixtures(t *testing.T) {
	root := filepath.Join("..", "..")
	doc, err := os.ReadFile(filepath.Join(root, "docs", "operations", "PROCESS_MONITORING.md"))
	require.NoError(t, err)
	tables := inventoryTables(t, string(doc))

	for _, tc := range []struct {
		process string
		fixture string
	}{
		{"arkd", filepath.Join(root, "cmd", "arkd", "cmd", "testdata", "exported_series.txt")},
		{"pricefeed", filepath.Join(root, "cmd", "pricefeed", "cmd", "testdata", "exported_series.txt")},
	} {
		t.Run(tc.process, func(t *testing.T) {
			raw, err := os.ReadFile(tc.fixture)
			require.NoError(t, err)
			var pinned []string
			for _, line := range strings.Split(strings.TrimSpace(string(raw)), "\n") {
				pinned = append(pinned, strings.Fields(line)[0])
			}
			require.ElementsMatch(t, pinned, tables[tc.process])
		})
	}
}

// inventoryTables reads the series names under each process heading of the Series inventory section.
func inventoryTables(t *testing.T, doc string) map[string][]string {
	t.Helper()
	start := strings.Index(doc, "\n## Series inventory\n")
	require.NotEqual(t, -1, start, "process monitoring has no Series inventory section")
	section := doc[start+1:]
	if end := strings.Index(section[1:], "\n## "); end != -1 {
		section = section[:end+1]
	}

	tables := map[string][]string{}
	process := ""
	for _, line := range strings.Split(section, "\n") {
		if heading, ok := strings.CutPrefix(line, "### "); ok {
			process = strings.Trim(heading, "`")
			continue
		}
		if m := inventoryRow.FindStringSubmatch(line); m != nil && process != "" {
			tables[process] = append(tables[process], m[1])
		}
	}
	return tables
}
