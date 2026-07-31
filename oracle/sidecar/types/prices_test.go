package types_test

import (
	. "ark/oracle/sidecar/types"
	"math/big"
	"strings"
	"testing"

	"ark/pkg/encoding"
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

func TestParsePriceRejectsInfinity(t *testing.T) {
	if _, err := ParsePrice("+Inf"); err == nil {
		t.Fatal("ParsePrice() error = nil, want infinity rejection")
	}
}

func TestParsePriceRejectsOversizedText(t *testing.T) {
	_, err := ParsePrice(strings.Repeat("1", encoding.MaxEncodedLegacyDecBytes+1))
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

func TestPricesByFeedProjectsPairPricesToFeeds(t *testing.T) {
	prices := Prices{
		"USDT/USD": big.NewFloat(1.23),
		"USDT/KRW": big.NewFloat(1300),
		"USDT/JPY": big.NewFloat(160),
	}

	got := PricesByFeed(prices, []string{"ausd", "akrw"})

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
}

func TestPricesByFeedEmptyFeedsProjectsNothing(t *testing.T) {
	prices := Prices{
		"USDT/USD": big.NewFloat(1.23),
		"NOAH/KRW": big.NewFloat(1300),
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
		"USDT/USD": big.NewFloat(1.23),
	}

	got := PricesByFeed(prices, []string{"ausd"})
	got["ausd"].SetFloat64(9.99)

	got = PricesByFeed(prices, []string{"ausd"})
	if got["ausd"].Cmp(big.NewFloat(1.23)) != 0 {
		t.Fatalf("PricesByFeed() = %s, want 1.23", got["ausd"].Text('f', -1))
	}
}
