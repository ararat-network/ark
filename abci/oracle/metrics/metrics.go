package metrics

import (
	"context"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

var (
	meter = otel.Meter("ark/abci/oracle/metrics")

	voteReports             metric.Int64Counter
	participatingPowerShare metric.Float64Gauge
	blocks                  metric.Int64Counter
	voteTargets             metric.Int64Gauge
	droppedTargets          metric.Int64Counter
)

func init() {
	var err error
	voteReports, err = meter.Int64Counter(
		"ark.oracle.vote_reports",
		metric.WithDescription("Validator oracle reports processed in preblock by status"),
	)
	if err != nil {
		panic(err)
	}

	participatingPowerShare, err = meter.Float64Gauge(
		"ark.oracle.participating_power_share",
		metric.WithDescription("Share of commit power whose oracle report met the participation floor, last block"),
	)
	if err != nil {
		panic(err)
	}

	blocks, err = meter.Int64Counter(
		"ark.oracle.blocks",
		metric.WithDescription("Blocks whose vote extensions were processed, by whether the block was functioning"),
	)
	if err != nil {
		panic(err)
	}

	voteTargets, err = meter.Int64Gauge(
		"ark.oracle.vote.targets",
		metric.WithDescription("Active targets in this node's last oracle vote extension: priced, omitted by the sidecar, or dropped as undecodable"),
	)
	if err != nil {
		panic(err)
	}

	droppedTargets, err = meter.Int64Counter(
		"ark.oracle.vote.dropped_targets",
		metric.WithDescription("Sidecar rates this node dropped from its vote extension as undecodable, by denom"),
	)
	if err != nil {
		panic(err)
	}
}

// CountVoteReports records how many validator oracle reports in one block were
// valid, carried no payload at all, or carried a payload that failed decoding
// or validation.
func CountVoteReports(valid, empty, invalid int64) {
	addVoteReports("valid", valid)
	addVoteReports("empty", empty)
	addVoteReports("invalid", invalid)
}

// RecordBlockParticipation records one block's attendance verdict: the share
// of commit power that participated and whether that met the functioning
// threshold. A powerless commit counts as a block and records no share.
func RecordBlockParticipation(participatingPower, totalPower int64, functioning bool) {
	ctx := context.Background()
	blocks.Add(ctx, 1, metric.WithAttributes(attribute.Bool("functioning", functioning)))
	if totalPower <= 0 {
		return
	}
	participatingPowerShare.Record(ctx, float64(participatingPower)/float64(totalPower))
}

// RecordVoteCoverage records what this node's own report covered: of the
// targets active this block, how many it priced, how many it dropped as
// undecodable, and by difference how many the sidecar left unpriced. An
// empty extension prices nothing, so the caller passes zero priced for one.
func RecordVoteCoverage(priced, targets int, dropped []string) {
	ctx := context.Background()
	omitted := targets - priced - len(dropped)
	if omitted < 0 {
		omitted = 0
	}
	for status, count := range map[string]int{"priced": priced, "omitted": omitted, "dropped": len(dropped)} {
		voteTargets.Record(ctx, int64(count), metric.WithAttributes(attribute.String("status", status)))
	}
	for _, denom := range dropped {
		droppedTargets.Add(ctx, 1, metric.WithAttributes(attribute.String("denom", denom)))
	}
}

func addVoteReports(status string, count int64) {
	if count == 0 {
		return
	}
	voteReports.Add(
		context.Background(),
		count,
		metric.WithAttributes(attribute.String("status", status)),
	)
}
