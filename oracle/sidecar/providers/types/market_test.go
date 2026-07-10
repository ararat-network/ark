package types_test

import (
	. "ark/oracle/sidecar/providers/types"
	"strings"
	"testing"
)

func TestMarketsValidate(t *testing.T) {
	tests := []struct {
		name    string
		markets Markets
		wantErr string
	}{
		{
			name: "valid",
			markets: Markets{
				{Pair: "USD/KRW", Symbol: "USDKRW"},
				{Pair: "USD/JPY", Symbol: "USDJPY"},
			},
		},
		{
			name:    "empty",
			markets: Markets{},
			wantErr: "markets is empty",
		},
		{
			name: "empty pair",
			markets: Markets{
				{Pair: "", Symbol: "USDTUSD"},
			},
			wantErr: "pair is empty",
		},
		{
			name: "empty symbol",
			markets: Markets{
				{Pair: "USD/KRW", Symbol: ""},
			},
			wantErr: "symbol is empty",
		},
		{
			name: "duplicate pair",
			markets: Markets{
				{Pair: "USD/KRW", Symbol: "USDKRW"},
				{Pair: "USD/KRW", Symbol: "USDJPY"},
			},
			wantErr: `duplicate pair "USD/KRW"`,
		},
		{
			name: "duplicate symbol",
			markets: Markets{
				{Pair: "USD/KRW", Symbol: "USDKRW"},
				{Pair: "USD/JPY", Symbol: "usdkrw"},
			},
			wantErr: `duplicate symbol "USDKRW"`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.markets.Validate()
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("Validate() error = %v, want nil", err)
				}
				return
			}

			if err == nil {
				t.Fatalf("Validate() error = nil, want %q", tt.wantErr)
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("Validate() error = %q, want containing %q", err, tt.wantErr)
			}
		})
	}
}

func TestMarketsTickerToPair(t *testing.T) {
	markets := Markets{
		{Pair: "USD/KRW", Symbol: "USDKRW"},
		{Pair: "USD/JPY", Symbol: "USDJPY"},
	}

	pair, ok := markets.TickerToPair("usdkrw")
	if !ok {
		t.Fatal("TickerToPair() ok = false, want true")
	}
	if pair != "USD/KRW" {
		t.Fatalf("TickerToPair() pair = %q, want %q", pair, "USD/KRW")
	}

	_, ok = markets.TickerToPair("USDCHF")
	if ok {
		t.Fatal("TickerToPair() ok = true, want false")
	}
}

func TestMarketsPairToTicker(t *testing.T) {
	markets := Markets{
		{Pair: "USD/KRW", Symbol: "USDKRW"},
		{Pair: "USD/JPY", Symbol: "USDJPY"},
	}

	ticker, ok := markets.PairToTicker("USD/KRW")
	if !ok {
		t.Fatal("PairToTicker() ok = false, want true")
	}
	if ticker != "USDKRW" {
		t.Fatalf("PairToTicker() ticker = %q, want %q", ticker, "USDKRW")
	}

	_, ok = markets.PairToTicker("USD/CHF")
	if ok {
		t.Fatal("PairToTicker() ok = true, want false")
	}
}

func TestMarketsEqual(t *testing.T) {
	usdMarket := Market{Pair: "USDT/USD", Symbol: "USDTUSD"}
	krwMarket := Market{Pair: "USDT/KRW", Symbol: "KRWUSD"}
	eurMarket := Market{Pair: "USDT/EUR", Symbol: "EURUSD"}

	testCases := []struct {
		name string
		a    Markets
		b    Markets
		want bool
	}{
		{
			name: "same order",
			a:    Markets{usdMarket, krwMarket},
			b:    Markets{usdMarket, krwMarket},
			want: true,
		},
		{
			name: "different order",
			a:    Markets{usdMarket, krwMarket},
			b:    Markets{krwMarket, usdMarket},
			want: true,
		},
		{
			name: "different duplicate count",
			a:    Markets{usdMarket, usdMarket},
			b:    Markets{usdMarket, krwMarket},
			want: false,
		},
		{
			name: "missing market",
			a:    Markets{usdMarket, krwMarket},
			b:    Markets{usdMarket, eurMarket},
			want: false,
		},
		{
			name: "different length",
			a:    Markets{usdMarket},
			b:    Markets{usdMarket, krwMarket},
			want: false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.a.Equal(tc.b); got != tc.want {
				t.Fatalf("Equal() = %v, want %v", got, tc.want)
			}
		})
	}
}
