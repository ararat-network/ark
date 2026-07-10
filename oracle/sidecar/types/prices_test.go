package types_test

import (
	. "ark/oracle/sidecar/types"
	"math/big"
	"testing"
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

func TestPricesByDenomProjectsPairPricesToVoteTargets(t *testing.T) {
	prices := Prices{
		"USDT/USD": big.NewFloat(1.23),
		"USDT/KRW": big.NewFloat(1300),
		"USDT/JPY": big.NewFloat(160),
	}

	got := PricesByDenom(prices, []string{"uusd", "ukrw"})

	if len(got) != 2 {
		t.Fatalf("PricesByDenom() len = %d, want 2", len(got))
	}
	if got["uusd"].Cmp(big.NewFloat(1.23)) != 0 {
		t.Fatalf("PricesByDenom()[uusd] = %s, want 1.23", got["uusd"].Text('f', -1))
	}
	if got["ukrw"].Cmp(big.NewFloat(1300)) != 0 {
		t.Fatalf("PricesByDenom()[ukrw] = %s, want 1300", got["ukrw"].Text('f', -1))
	}
	if _, ok := got["ujpy"]; ok {
		t.Fatal("PricesByDenom() included non-target ujpy")
	}
}

func TestPricesByDenomReturnsDeepCopy(t *testing.T) {
	prices := Prices{
		"USDT/USD": big.NewFloat(1.23),
	}

	got := PricesByDenom(prices, []string{"uusd"})
	got["uusd"].SetFloat64(9.99)

	got = PricesByDenom(prices, []string{"uusd"})
	if got["uusd"].Cmp(big.NewFloat(1.23)) != 0 {
		t.Fatalf("PricesByDenom() = %s, want 1.23", got["uusd"].Text('f', -1))
	}
}
