package types

import (
	"math/big"
	"strings"
	"testing"
)

func TestParsePrice(t *testing.T) {
	tests := []struct {
		name    string
		value   string
		want    string
		wantErr string
	}{
		{
			name:  "integer",
			value: "123",
			want:  "123",
		},
		{
			name:  "decimal",
			value: "123.456",
			want:  "123.456",
		},
		{
			name:  "small decimal",
			value: "0.0000000000000001",
			want:  "0.0000000000000001",
		},
		{
			name:    "invalid",
			value:   "bad_price",
			wantErr: `failed to parse oracle price "bad_price"`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			price, err := ParsePrice(tt.value)
			if tt.wantErr != "" {
				if err == nil {
					t.Fatalf("ParsePrice() error = nil, want %q", tt.wantErr)
				}
				if !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("ParsePrice() error = %q, want containing %q", err, tt.wantErr)
				}
				return
			}

			if err != nil {
				t.Fatalf("ParsePrice() error = %v, want nil", err)
			}
			want, ok := new(big.Float).SetString(tt.want)
			if !ok {
				t.Fatalf("invalid test price %q", tt.want)
			}
			if price.Cmp(want) != 0 {
				t.Fatalf("ParsePrice() = %s, want %s", price.Text('f', -1), want.Text('f', -1))
			}
		})
	}
}
