package types_test

import (
	"testing"

	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/ararat-network/ark/pkg/chain"
	"github.com/ararat-network/ark/x/oracle/types"
)

var benchmarkConverted sdk.DecCoin

// BenchmarkRateSetConvert separates the two shapes a conversion can take. Every
// rate set carries the numeraire at one, and nearly every conversion the chain
// performs has it on a leg — valuing supply, sizing a fund, quoting a NOAH pair,
// paying a settlement entitlement — so the unit-rate cases are the path that
// matters. The cross case is the control: neither leg is one, nothing is
// skipped, and it is here to catch the skip's own guard becoming a cost.
func BenchmarkRateSetConvert(b *testing.B) {
	rates := types.NewRateSetFrom(map[string]math.LegacyDec{
		chain.USDBaseDenom: math.LegacyMustNewDecFromStr("1.234567890123456789"),
		chain.KRWBaseDenom: math.LegacyNewDec(1300),
	})
	amount := math.LegacyNewDecFromInt(chain.NativeBaseAmount(1))

	benchmarks := []struct {
		name      string
		offerCoin sdk.DecCoin
		askDenom  string
	}{
		{"numeraire_ask", sdk.NewDecCoinFromDec(chain.USDBaseDenom, amount), chain.NoahBaseDenom},
		{"numeraire_offer", sdk.NewDecCoinFromDec(chain.NoahBaseDenom, amount), chain.USDBaseDenom},
		{"cross", sdk.NewDecCoinFromDec(chain.USDBaseDenom, amount), chain.KRWBaseDenom},
	}

	for _, bm := range benchmarks {
		b.Run(bm.name, func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				converted, err := rates.Convert(bm.offerCoin, bm.askDenom)
				if err != nil {
					b.Fatal(err)
				}
				benchmarkConverted = converted
			}
		})
	}
}
