package types_test

import (
	"strings"
	"testing"

	. "github.com/ararat-network/ark/pricefeed/sidecar/types"
)

func TestNewPair(t *testing.T) {
	tests := []struct {
		name    string
		base    string
		quote   string
		want    Pair
		wantErr string
	}{
		{
			name:  "constructs canonical pair",
			base:  "BTC",
			quote: "USD",
			want:  Pair("BTC/USD"),
		},
		{
			name:  "normalises whitespace and case",
			base:  " btc ",
			quote: " usd ",
			want:  Pair("BTC/USD"),
		},
		{
			name:    "rejects empty base",
			quote:   "USD",
			wantErr: "pair base is empty",
		},
		{
			name:    "rejects empty quote",
			base:    "BTC",
			wantErr: "pair quote is empty",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := NewPair(tt.base, tt.quote)
			if tt.wantErr != "" {
				assertErrorContains(t, err, tt.wantErr)
				return
			}
			if err != nil {
				t.Fatalf("NewPair() error = %v, want nil", err)
			}
			if got != tt.want {
				t.Fatalf("NewPair() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestFromDenom(t *testing.T) {
	tests := []struct {
		name    string
		denom   string
		want    Pair
		wantErr string
	}{
		{
			name:  "converts canonical feed denom",
			denom: "ausd",
			want:  "NOAH/USD",
		},
		{
			name:    "rejects uppercase feed denom",
			denom:   "aUSD",
			wantErr: "Ark-native base denom matching",
		},
		{
			name:    "rejects path denom",
			denom:   "afoo/bar",
			wantErr: "Ark-native base denom matching",
		},
		{
			name:    "rejects surrounding whitespace",
			denom:   " ausd ",
			wantErr: "Ark-native base denom matching",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := FromDenom(tt.denom)
			if tt.wantErr != "" {
				assertErrorContains(t, err, tt.wantErr)
				return
			}
			if err != nil {
				t.Fatalf("FromDenom() error = %v, want nil", err)
			}
			if got != tt.want {
				t.Fatalf("FromDenom() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestParsePair(t *testing.T) {
	tests := []struct {
		name    string
		raw     string
		want    Pair
		wantErr string
	}{
		{
			name: "parses canonical pair",
			raw:  "BTC/USD",
			want: Pair("BTC/USD"),
		},
		{
			name: "normalises whitespace and case",
			raw:  " btc / usd ",
			want: Pair("BTC/USD"),
		},
		{
			name:    "rejects missing slash",
			raw:     "BTCUSD",
			wantErr: "expected BASE/QUOTE",
		},
		{
			name:    "rejects extra slash",
			raw:     "BTC/USD/USDT",
			wantErr: "expected BASE/QUOTE",
		},
		{
			name:    "rejects empty raw value",
			wantErr: "pair is empty",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParsePair(tt.raw)
			if tt.wantErr != "" {
				assertErrorContains(t, err, tt.wantErr)
				return
			}
			if err != nil {
				t.Fatalf("ParsePair() error = %v, want nil", err)
			}
			if got != tt.want {
				t.Fatalf("ParsePair() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestPairValidate(t *testing.T) {
	tests := []struct {
		name    string
		pair    Pair
		wantErr string
	}{
		{
			name: "valid",
			pair: Pair("BTC/USD"),
		},
		{
			name:    "empty",
			wantErr: "pair is empty",
		},
		{
			name:    "missing slash",
			pair:    Pair("BTCUSD"),
			wantErr: "expected BASE/QUOTE",
		},
		{
			name:    "empty base",
			pair:    Pair("/USD"),
			wantErr: "pair base is empty",
		},
		{
			name:    "empty quote",
			pair:    Pair("BTC/"),
			wantErr: "pair quote is empty",
		},
		{
			name:    "lowercase base",
			pair:    Pair("btc/USD"),
			wantErr: "pair base must be uppercase",
		},
		{
			name:    "lowercase quote",
			pair:    Pair("BTC/usd"),
			wantErr: "pair quote must be uppercase",
		},
		{
			name:    "component whitespace",
			pair:    Pair("BTC /USD"),
			wantErr: "pair base contains whitespace",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.pair.Validate()
			if tt.wantErr != "" {
				assertErrorContains(t, err, tt.wantErr)
				return
			}
			if err != nil {
				t.Fatalf("Validate() error = %v, want nil", err)
			}
		})
	}
}

func TestPairComponents(t *testing.T) {
	pair := Pair("BTC/USD")

	if got := pair.Base(); got != "BTC" {
		t.Fatalf("Base() = %q, want %q", got, "BTC")
	}

	if got := pair.Quote(); got != "USD" {
		t.Fatalf("Quote() = %q, want %q", got, "USD")
	}

	if pair.String() != "BTC/USD" {
		t.Fatalf("String() = %q, want %q", pair.String(), "BTC/USD")
	}
}

func TestPairDenom(t *testing.T) {
	tests := []struct {
		name string
		pair Pair
		want string
	}{
		{
			name: "projects quote to native base denom",
			pair: "USDT/USD",
			want: "ausd",
		},
		{
			name: "preserves terra sdr feed denom spelling",
			pair: "USDT/SDR",
			want: "asdr",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.pair.Denom(); got != tt.want {
				t.Fatalf("Denom() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestPairInvert(t *testing.T) {
	pair := Pair("BTC/USD")

	if got := pair.Inverse(); got != Pair("USD/BTC") {
		t.Fatalf("Invert() = %q, want %q", got, "USD/BTC")
	}
}

func assertErrorContains(t *testing.T, err error, want string) {
	t.Helper()

	if err == nil {
		t.Fatalf("error = nil, want containing %q", want)
	}
	if !strings.Contains(err.Error(), want) {
		t.Fatalf("error = %q, want containing %q", err, want)
	}
}
