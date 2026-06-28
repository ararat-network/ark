package types

import (
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
				{Denom: "uusd", Symbol: "USDTUSD"},
				{Denom: "ukrw", Symbol: "KRWUSD"},
			},
		},
		{
			name:    "empty",
			markets: Markets{},
			wantErr: "markets is empty",
		},
		{
			name: "empty denom",
			markets: Markets{
				{Denom: "", Symbol: "USDTUSD"},
			},
			wantErr: "denom is empty",
		},
		{
			name: "empty symbol",
			markets: Markets{
				{Denom: "uusd", Symbol: ""},
			},
			wantErr: "symbol is empty",
		},
		{
			name: "duplicate denom",
			markets: Markets{
				{Denom: "uusd", Symbol: "USDTUSD"},
				{Denom: "uusd", Symbol: "USDCUSD"},
			},
			wantErr: `duplicate denom "uusd"`,
		},
		{
			name: "duplicate symbol",
			markets: Markets{
				{Denom: "uusd", Symbol: "USDTUSD"},
				{Denom: "uusdc", Symbol: "usdtusd"},
			},
			wantErr: `duplicate symbol "USDTUSD"`,
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

func TestMarketsTickerToDenom(t *testing.T) {
	markets := Markets{
		{Denom: "uusd", Symbol: "USDTUSD"},
		{Denom: "ukrw", Symbol: "KRWUSD"},
	}

	denom, ok := markets.TickerToDenom("usdtusd")
	if !ok {
		t.Fatal("TickerToDenom() ok = false, want true")
	}
	if denom != "uusd" {
		t.Fatalf("TickerToDenom() denom = %q, want %q", denom, "uusd")
	}

	_, ok = markets.TickerToDenom("ATOMUSD")
	if ok {
		t.Fatal("TickerToDenom() ok = true, want false")
	}
}

func TestMarketsDenomToTicker(t *testing.T) {
	markets := Markets{
		{Denom: "uusd", Symbol: "USDTUSD"},
		{Denom: "ukrw", Symbol: "KRWUSD"},
	}

	ticker, ok := markets.DenomToTicker("uusd")
	if !ok {
		t.Fatal("DenomToTicker() ok = false, want true")
	}
	if ticker != "USDTUSD" {
		t.Fatalf("DenomToTicker() ticker = %q, want %q", ticker, "USDTUSD")
	}

	_, ok = markets.DenomToTicker("uatom")
	if ok {
		t.Fatal("DenomToTicker() ok = true, want false")
	}
}
