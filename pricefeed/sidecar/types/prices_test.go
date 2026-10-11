package types_test

import (
	"math/big"
	"strings"
	"testing"

	. "github.com/ararat-network/ark/pricefeed/sidecar/types"
)

func TestParsePriceUsesOraclePrecision(t *testing.T) {
	price, err := ParsePrice("1234.123456789123456789")
	if err != nil {
		t.Fatalf("ParsePrice() error = %v, want nil", err)
	}
	if price.Prec() != 256 {
		t.Fatalf("ParsePrice() precision = %d, want 256", price.Prec())
	}
}

func TestParsePriceAcceptsPlainDecimalTextOnly(t *testing.T) {
	tests := []struct {
		text    string
		want    string
		wantErr string
	}{
		{text: "1371.25", want: "1371.25"},
		{text: "42", want: "42"},
		{text: "0.5", want: "0.5"},
		{text: ".5", want: "0.5"},
		{text: "5.", want: "5"},
		{text: "1.5e3", want: "1500"},
		{text: "2E-2", want: "0.02"},
		{text: "2.5e+1", want: "25"},
		{text: "-1", want: "-1"}, // the resolver refuses non-positive samples
		{text: "0x10", wantErr: "not a plain decimal"},
		{text: "0x1p-2", wantErr: "not a plain decimal"},
		{text: "0b101", wantErr: "not a plain decimal"},
		{text: "0o17", wantErr: "not a plain decimal"},
		{text: "1p4", wantErr: "not a plain decimal"},
		{text: "1_000", wantErr: "not a plain decimal"},
		{text: "+1", wantErr: "not a plain decimal"},
		{text: "Inf", wantErr: "not a plain decimal"},
		{text: "NaN", wantErr: "not a plain decimal"},
		{text: " 1", wantErr: "not a plain decimal"},
		{text: "1 ", wantErr: "not a plain decimal"},
		{text: "1,000", wantErr: "not a plain decimal"},
		{text: "", wantErr: "not a plain decimal"},
		{text: ".", wantErr: "not a plain decimal"},
		{text: "1e", wantErr: "not a plain decimal"},
		{text: "e5", wantErr: "not a plain decimal"},
		{text: "１", wantErr: "not a plain decimal"},
	}

	for _, tc := range tests {
		t.Run(tc.text, func(t *testing.T) {
			price, err := ParsePrice(tc.text)
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("ParsePrice(%q) error = %v, want %q", tc.text, err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("ParsePrice(%q) error = %v, want nil", tc.text, err)
			}
			if got := price.Text('f', -1); got != tc.want {
				t.Fatalf("ParsePrice(%q) = %s, want %s", tc.text, got, tc.want)
			}
		})
	}
}

func TestParsePriceRejectsInfinity(t *testing.T) {
	if _, err := ParsePrice("+Inf"); err == nil {
		t.Fatal("ParsePrice() error = nil, want infinity rejection")
	}
}

func TestParsePriceRejectsOversizedText(t *testing.T) {
	_, err := ParsePrice(strings.Repeat("1", MaxPriceTextBytes+1))
	if err == nil || !strings.Contains(err.Error(), "exceeds maximum") {
		t.Fatalf("ParsePrice() error = %v, want maximum-length error", err)
	}
}

func TestParsePriceBoundsLegacyDecMagnitude(t *testing.T) {
	maxAccepted := new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), 256), big.NewInt(1))
	if _, err := ParsePrice(maxAccepted.String()); err != nil {
		t.Fatalf("ParsePrice(max accepted) error = %v, want nil", err)
	}

	firstRejected := new(big.Int).Lsh(big.NewInt(1), 256)
	if _, err := ParsePrice(firstRejected.String()); err == nil || !strings.Contains(err.Error(), "magnitude") {
		t.Fatalf("ParsePrice(first rejected) error = %v, want magnitude error", err)
	}

	if _, err := ParsePrice("1e600000000"); err == nil || !strings.Contains(err.Error(), "magnitude") {
		t.Fatalf("ParsePrice(huge exponent) error = %v, want magnitude error", err)
	}
}

// TestValidateFeedPrice pins the floor at one LegacyDec unit: anything smaller
// renders as zero at 18 places, which the transport cannot encode.
func TestValidateFeedPrice(t *testing.T) {
	tests := []struct {
		name    string
		price   string
		wantErr string
	}{
		{name: "one unit", price: "0.000000000000000001"},
		{name: "ordinary rate", price: "2"},
		{name: "largest magnitude", price: new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), 256), big.NewInt(1)).String()},
		{name: "below one unit", price: "0.0000000000000000004", wantErr: "below one LegacyDec unit"},
		{name: "zero", price: "0", wantErr: "not positive"},
		{name: "negative", price: "-1", wantErr: "not positive"},
		{name: "beyond range", price: new(big.Int).Lsh(big.NewInt(1), 256).String(), wantErr: "magnitude"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			price, ok := new(big.Float).SetPrec(PricePrecisionBits).SetString(tc.price)
			if !ok {
				t.Fatalf("SetString(%q) failed", tc.price)
			}
			err := ValidateFeedPrice(price)
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("ValidateFeedPrice(%s) error = %v, want nil", tc.price, err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("ValidateFeedPrice(%s) error = %v, want %q", tc.price, err, tc.wantErr)
			}
		})
	}
}

// TestPricesByFeedProjectsPairPricesToFeeds pins that a resolved UNIT/NOAH
// price is the published price: the projection re-keys and copies, and turns
// nothing over.
func TestPricesByFeedProjectsPairPricesToFeeds(t *testing.T) {
	prices := Prices{
		"USD/NOAH": big.NewFloat(1.23),
		"KRW/NOAH": big.NewFloat(1300),
		"JPY/NOAH": big.NewFloat(160),
		"EUR/NOAH": nil,
	}

	got := PricesByFeed(prices, []string{"ausd", "akrw", "aeur"})

	if len(got) != 2 {
		t.Fatalf("PricesByFeed() len = %d, want 2", len(got))
	}
	if got["ausd"].Cmp(big.NewFloat(1.23)) != 0 {
		t.Fatalf("PricesByFeed()[ausd] = %s, want 1.23", got["ausd"].Text('f', -1))
	}
	if got["akrw"].Cmp(big.NewFloat(1300)) != 0 {
		t.Fatalf("PricesByFeed()[akrw] = %s, want 1300", got["akrw"].Text('f', -1))
	}
	if _, ok := got["ajpy"]; ok {
		t.Fatal("PricesByFeed() included non-target ajpy")
	}
	if _, ok := got["aeur"]; ok {
		t.Fatal("PricesByFeed() published a nil pair price")
	}
}

func TestPricesByFeedEmptyFeedsProjectsNothing(t *testing.T) {
	prices := Prices{
		"USD/NOAH": big.NewFloat(1.23),
		"KRW/NOAH": big.NewFloat(1300),
	}

	if got := PricesByFeed(prices, nil); len(got) != 0 {
		t.Fatalf("PricesByFeed(nil feeds) len = %d, want 0", len(got))
	}
	if got := PricesByFeed(prices, []string{}); len(got) != 0 {
		t.Fatalf("PricesByFeed(empty feeds) len = %d, want 0", len(got))
	}
}

func TestPricesByFeedReturnsDeepCopy(t *testing.T) {
	prices := Prices{
		"USD/NOAH": big.NewFloat(1.23),
	}

	got := PricesByFeed(prices, []string{"ausd"})
	got["ausd"].SetFloat64(9.99)

	got = PricesByFeed(prices, []string{"ausd"})
	if got["ausd"].Cmp(big.NewFloat(1.23)) != 0 {
		t.Fatalf("PricesByFeed() = %s, want 1.23", got["ausd"].Text('f', -1))
	}
	if prices["USD/NOAH"].Cmp(big.NewFloat(1.23)) != 0 {
		t.Fatalf("PricesByFeed() mutated its input: %s", prices["USD/NOAH"].Text('f', -1))
	}
}
