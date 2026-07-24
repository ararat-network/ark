package cmd

import (
	"context"
	"math"
	"testing"

	sdkmath "cosmossdk.io/math"

	"github.com/cosmos/cosmos-sdk/server"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/require"
)

func TestValidateMinGasPrices(t *testing.T) {
	maximumSafeIntegerPrice := sdkmath.LegacyNewDecFromInt(
		maximumCoinAmount.TruncateInt().Quo(sdkmath.NewIntFromUint64(math.MaxUint64)),
	)

	tests := []struct {
		name        string
		value       string
		errorSubstr string
	}{
		{
			name:  "zero default",
			value: "0unoah",
		},
		{
			name:  "ordinary prices",
			value: "0.01unoah,0.1uusd",
		},
		{
			name:  "maximum safe integer price",
			value: maximumSafeIntegerPrice.String() + "unoah",
		},
		{
			name:        "malformed price",
			value:       "not-a-price",
			errorSubstr: "invalid minimum gas prices",
		},
		{
			name:        "price overflows maximum gas",
			value:       maximumCoinAmount.String() + "unoah",
			errorSubstr: "too large to multiply by the maximum gas limit",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateMinGasPrices(tt.value)
			if tt.errorSubstr == "" {
				require.NoError(t, err)
				return
			}

			require.ErrorIs(t, err, sdkerrors.ErrAppConfig)
			require.ErrorContains(t, err, tt.errorSubstr)
		})
	}
}

func TestAddModuleInitFlagsValidatesMinGasPrices(t *testing.T) {
	startCmd := &cobra.Command{}
	serverCtx := server.NewDefaultContext()
	serverCtx.Viper.Set(
		server.FlagMinGasPrices,
		maximumCoinAmount.String()+"unoah",
	)
	startCmd.SetContext(context.WithValue(
		context.Background(),
		server.ServerContextKey,
		serverCtx,
	))

	addModuleInitFlags(startCmd)

	require.NotNil(t, startCmd.PreRunE)
	err := startCmd.PreRunE(startCmd, nil)
	require.ErrorIs(t, err, sdkerrors.ErrAppConfig)
	require.ErrorContains(t, err, "too large to multiply by the maximum gas limit")
}
