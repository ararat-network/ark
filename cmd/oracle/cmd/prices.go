package cmd

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	"ark/oracle/types"
	"ark/pkg/encoding"
)

const (
	defaultPricesOutput = pricesOutputTable

	flagOutput = "output"

	pricesOutputTable = "table"
	pricesOutputJSON  = "json"
)

type pricesOptions struct {
	address string
	output  string
}

type pricesView struct {
	Prices    map[string]string `json:"prices"`
	Timestamp time.Time         `json:"timestamp"`
	Age       string            `json:"age"`
	Version   string            `json:"version"`
}

func newPricesCmd() *cobra.Command {
	options := pricesOptions{
		address: defaultAddress,
		output:  defaultPricesOutput,
	}

	pricesCmd := &cobra.Command{
		Use:   "prices",
		Short: "Print the latest oracle price snapshot.",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runPrices(cmd.Context(), cmd.OutOrStdout(), options)
		},
	}

	flags := pricesCmd.Flags()
	flags.StringVar(&options.address, flagAddress, options.address, "Oracle gRPC address.")
	flags.StringVar(&options.output, flagOutput, options.output, "Output format (table or json).")

	return pricesCmd
}

// runPrices performs one read-only snapshot query and formats the complete
// response for operator inspection.
func runPrices(ctx context.Context, w io.Writer, options pricesOptions) error {
	if options.output != pricesOutputTable && options.output != pricesOutputJSON {
		return fmt.Errorf("unsupported prices output %q; expected %s or %s", options.output, pricesOutputTable, pricesOutputJSON)
	}

	resp, err := fetchPrices(ctx, options.address)
	if err != nil {
		return err
	}

	return writePrices(w, resp, options.output, time.Now().UTC())
}

func fetchPrices(
	ctx context.Context,
	address string,
	dialOptions ...grpc.DialOption,
) (resp *types.OraclePricesResponse, err error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if strings.TrimSpace(address) == "" {
		return nil, errors.New("oracle address cannot be empty")
	}

	opts := []grpc.DialOption{
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithNoProxy(),
	}
	opts = append(opts, dialOptions...)

	conn, err := grpc.NewClient(address, opts...)
	if err != nil {
		return nil, fmt.Errorf("dialling oracle endpoint: %w", err)
	}
	defer func() {
		if closeErr := conn.Close(); closeErr != nil {
			err = errors.Join(err, fmt.Errorf("closing oracle connection: %w", closeErr))
		}
	}()

	resp, err = types.NewOracleClient(conn).Prices(ctx, &types.OraclePricesRequest{})
	if err != nil {
		return nil, fmt.Errorf("fetching oracle prices: %w", err)
	}
	if resp == nil {
		return nil, errors.New("oracle price response is nil")
	}

	return resp, nil
}

func writePrices(w io.Writer, resp *types.OraclePricesResponse, output string, now time.Time) error {
	if resp == nil {
		return errors.New("oracle price response is nil")
	}

	prices := make(map[string]string, len(resp.Prices))
	for denom, rawPrice := range resp.Prices {
		price, err := encoding.DecodeLegacyDec(rawPrice)
		if err != nil {
			return fmt.Errorf("decoding oracle price %q: %w", denom, err)
		}
		prices[denom] = price.String()
	}

	view := pricesView{
		Prices:    prices,
		Timestamp: resp.Timestamp,
		Age:       priceSnapshotAge(now, resp.Timestamp),
		Version:   resp.Version,
	}

	switch output {
	case pricesOutputTable:
		return writePricesTable(w, view)
	case pricesOutputJSON:
		encoder := json.NewEncoder(w)
		encoder.SetIndent("", "  ")
		if err := encoder.Encode(view); err != nil {
			return fmt.Errorf("encoding oracle prices: %w", err)
		}
		return nil
	default:
		return fmt.Errorf("unsupported prices output %q; expected %s or %s", output, pricesOutputTable, pricesOutputJSON)
	}
}

// priceSnapshotAge preserves a negative duration when the remote timestamp is
// ahead of the local clock so future skew remains visible to the operator.
func priceSnapshotAge(now, timestamp time.Time) string {
	if timestamp.IsZero() {
		return "unknown"
	}

	return now.Sub(timestamp).Round(time.Millisecond).String()
}

func writePricesTable(w io.Writer, view pricesView) error {
	tw := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
	if _, err := fmt.Fprintf(tw, "TIMESTAMP\t%s\n", view.Timestamp.Format(time.RFC3339Nano)); err != nil {
		return fmt.Errorf("writing oracle price timestamp: %w", err)
	}
	if _, err := fmt.Fprintf(tw, "AGE\t%s\n", view.Age); err != nil {
		return fmt.Errorf("writing oracle price age: %w", err)
	}
	if _, err := fmt.Fprintf(tw, "VERSION\t%s\n\n", view.Version); err != nil {
		return fmt.Errorf("writing oracle price version: %w", err)
	}
	if _, err := fmt.Fprintln(tw, "DENOM\tPRICE"); err != nil {
		return fmt.Errorf("writing oracle price header: %w", err)
	}

	feeds := make([]string, 0, len(view.Prices))
	for denom := range view.Prices {
		feeds = append(feeds, denom)
	}
	sort.Strings(feeds)
	for _, denom := range feeds {
		if _, err := fmt.Fprintf(tw, "%s\t%s\n", denom, view.Prices[denom]); err != nil {
			return fmt.Errorf("writing oracle price %q: %w", denom, err)
		}
	}

	if err := tw.Flush(); err != nil {
		return fmt.Errorf("flushing oracle prices: %w", err)
	}
	return nil
}
