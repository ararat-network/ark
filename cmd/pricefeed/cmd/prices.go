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

	"github.com/ararat-network/ark/pkg/encoding"
	"github.com/ararat-network/ark/pkg/grpcconn"
	"github.com/ararat-network/ark/pkg/tlsconfig"
	"github.com/ararat-network/ark/pricefeed/api"
)

const (
	defaultPricesOutput = pricesOutputTable

	flagOutput = "output"

	pricesOutputTable = "table"
	pricesOutputJSON  = "json"
)

type pricesOptions struct {
	address string
	tls     tlsconfig.Client
	output  string
}

// pricesView is the decoded snapshot both output formats render.
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
		Short: "Print the sidecar's latest price snapshot.",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runPrices(cmd.Context(), cmd.OutOrStdout(), options)
		},
	}

	flags := pricesCmd.Flags()
	flags.StringVar(&options.address, flagAddress, options.address, "Sidecar gRPC address.")
	addClientTLSFlags(flags, "", "sidecar", &options.tls)
	flags.StringVar(&options.output, flagOutput, options.output, "Output format (table or json).")

	return pricesCmd
}

// runPrices performs one read-only snapshot query and renders the complete
// response for operator inspection.
func runPrices(ctx context.Context, w io.Writer, options pricesOptions) error {
	write, err := pricesWriter(options.output)
	if err != nil {
		return err
	}

	resp, err := fetchPrices(ctx, options.address, options.tls)
	if err != nil {
		return err
	}
	view, err := newPricesView(resp, time.Now().UTC())
	if err != nil {
		return err
	}

	return write(w, view)
}

// pricesWriter resolves the output flag before anything is dialled.
func pricesWriter(output string) (func(io.Writer, pricesView) error, error) {
	switch output {
	case pricesOutputTable:
		return writePricesTable, nil
	case pricesOutputJSON:
		return writePricesJSON, nil
	default:
		return nil, fmt.Errorf("unsupported prices output %q; expected %s or %s", output, pricesOutputTable, pricesOutputJSON)
	}
}

func fetchPrices(
	ctx context.Context,
	address string,
	files tlsconfig.Client,
	dialOptions ...grpc.DialOption,
) (resp *api.PricesResponse, err error) {
	if strings.TrimSpace(address) == "" {
		return nil, errors.New("sidecar address cannot be empty")
	}

	material, err := grpcconn.LoadClient(files, address)
	if err != nil {
		return nil, fmt.Errorf("sidecar connection credentials: %w", err)
	}

	conn, err := grpc.NewClient(address, grpcconn.DialOptions(material.Config, dialOptions...)...)
	if err != nil {
		return nil, fmt.Errorf("dialling sidecar: %w", err)
	}
	defer func() {
		if closeErr := conn.Close(); closeErr != nil {
			err = errors.Join(err, fmt.Errorf("closing sidecar connection: %w", closeErr))
		}
	}()

	resp, err = api.NewPriceFeedClient(conn).Prices(ctx, &api.PricesRequest{})
	if err != nil {
		return nil, fmt.Errorf("fetching prices: %w", err)
	}

	return resp, nil
}

func newPricesView(resp *api.PricesResponse, now time.Time) (pricesView, error) {
	prices := make(map[string]string, len(resp.Prices))
	for denom, rawPrice := range resp.Prices {
		price, err := encoding.DecodeCompactLegacyDec(rawPrice)
		if err != nil {
			return pricesView{}, fmt.Errorf("decoding price %q: %w", denom, err)
		}
		prices[denom] = price.String()
	}

	return pricesView{
		Prices:    prices,
		Timestamp: resp.Timestamp,
		Age:       priceSnapshotAge(now, resp.Timestamp),
		Version:   resp.Version,
	}, nil
}

// priceSnapshotAge preserves a negative duration when the remote timestamp is
// ahead of the local clock so future skew remains visible to the operator.
func priceSnapshotAge(now, timestamp time.Time) string {
	if timestamp.IsZero() {
		return "unknown"
	}

	return now.Sub(timestamp).Round(time.Millisecond).String()
}

func writePricesJSON(w io.Writer, view pricesView) error {
	encoder := json.NewEncoder(w)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(view); err != nil {
		return fmt.Errorf("encoding prices: %w", err)
	}
	return nil
}

func writePricesTable(w io.Writer, view pricesView) error {
	tw := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
	if _, err := fmt.Fprintf(tw, "TIMESTAMP\t%s\n", view.Timestamp.Format(time.RFC3339Nano)); err != nil {
		return fmt.Errorf("writing price timestamp: %w", err)
	}
	if _, err := fmt.Fprintf(tw, "AGE\t%s\n", view.Age); err != nil {
		return fmt.Errorf("writing price age: %w", err)
	}
	if _, err := fmt.Fprintf(tw, "VERSION\t%s\n\n", view.Version); err != nil {
		return fmt.Errorf("writing price version: %w", err)
	}
	if _, err := fmt.Fprintln(tw, "DENOM\tPRICE"); err != nil {
		return fmt.Errorf("writing price header: %w", err)
	}

	feeds := make([]string, 0, len(view.Prices))
	for denom := range view.Prices {
		feeds = append(feeds, denom)
	}
	sort.Strings(feeds)
	for _, denom := range feeds {
		if _, err := fmt.Fprintf(tw, "%s\t%s\n", denom, view.Prices[denom]); err != nil {
			return fmt.Errorf("writing price %q: %w", denom, err)
		}
	}

	if err := tw.Flush(); err != nil {
		return fmt.Errorf("flushing prices: %w", err)
	}
	return nil
}
