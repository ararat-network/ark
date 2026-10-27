package telemetry

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"go.yaml.in/yaml/v3"
)

var (
	// seriesRef matches an Ark series name inside a PromQL expression.
	seriesRef = regexp.MustCompile(`\bark_[a-z0-9_]+`)
	// membershipSeries matches a backticked membership-service series in the protocol monitoring guide.
	membershipSeries = regexp.MustCompile("`(ark_membership_[a-z_]+)`")
)

type ruleFile struct {
	Groups []struct {
		Name  string `yaml:"name"`
		Rules []struct {
			Alert  string            `yaml:"alert"`
			Expr   string            `yaml:"expr"`
			Labels map[string]string `yaml:"labels"`
		} `yaml:"rules"`
	} `yaml:"groups"`
}

// The alert rules read only series the process fixtures pin or the membership service exports, and cite only
// alert IDs the monitoring guides define, so a renamed series or a retired alert fails here rather than
// blanking a rule at the scraper.
func TestAlertRulesReadPinnedSeries(t *testing.T) {
	root := filepath.Join("..", "..")
	raw, err := os.ReadFile(filepath.Join(root, "contrib", "observability", "rules", "ark.yml"))
	require.NoError(t, err)
	var rules ruleFile
	require.NoError(t, yaml.Unmarshal(raw, &rules))

	process, err := os.ReadFile(filepath.Join(root, "docs", "operations", "PROCESS_MONITORING.md"))
	require.NoError(t, err)
	protocol, err := os.ReadFile(filepath.Join(root, "docs", "operations", "PROTOCOL_MONITORING.md"))
	require.NoError(t, err)

	known := map[string]bool{}
	for _, fixture := range []string{
		filepath.Join(root, "cmd", "arkd", "cmd", "testdata", "exported_series.txt"),
		filepath.Join(root, "cmd", "pricefeed", "cmd", "testdata", "exported_series.txt"),
	} {
		pinned, err := os.ReadFile(fixture)
		require.NoError(t, err)
		for _, line := range strings.Split(strings.TrimSpace(string(pinned)), "\n") {
			fields := strings.Fields(line)
			known[fields[0]] = true
			if fields[1] == "HISTOGRAM" {
				for _, suffix := range []string{"_bucket", "_sum", "_count"} {
					known[fields[0]+suffix] = true
				}
			}
		}
	}
	for _, m := range membershipSeries.FindAllStringSubmatch(string(protocol), -1) {
		known[m[1]] = true
	}

	seen := 0
	for _, group := range rules.Groups {
		for _, rule := range group.Rules {
			seen++
			t.Run(rule.Alert, func(t *testing.T) {
				require.NotEmpty(t, rule.Expr)
				for _, series := range seriesRef.FindAllString(rule.Expr, -1) {
					require.True(t, known[series], "series %s is pinned by no fixture and named by no guide", series)
				}
				if id, ok := rule.Labels["ark_alert"]; ok {
					inProcess := strings.Contains(string(process), "**"+id+" — ")
					inProtocol := strings.Contains(string(protocol), "#### "+id+" — ")
					require.True(t, inProcess || inProtocol, "alert %s is defined in neither guide", id)
				}
			})
		}
	}
	require.NotZero(t, seen)
}
