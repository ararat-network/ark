package types_test

import (
	"math/big"
	. "noah/oracle/sidecar/types"
	"testing"
)

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
