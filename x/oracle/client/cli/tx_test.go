package cli

import (
	"context"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/require"

	sdkerrors "cosmossdk.io/errors"

	"github.com/cosmos/cosmos-sdk/client"
	sdk "github.com/cosmos/cosmos-sdk/types"
	errortypes "github.com/cosmos/cosmos-sdk/types/errors"

	core "noah/types"
)

func TestAggregateExchangeRateCommandsValidateInput(t *testing.T) {
	fromAddr := sdk.AccAddress([]byte("from________________"))

	tests := []struct {
		name      string
		cmd       func() *cobra.Command
		args      []string
		expectErr error
		errText   string
	}{
		{
			name:      "aggregate prevote rejects invalid exchange rates",
			cmd:       GetCmdAggregateExchangeRatePrevote,
			args:      []string{"salt", "not-a-dec-coin"},
			expectErr: errortypes.ErrInvalidCoins,
			errText:   "parsing exchange rates",
		},
		{
			name:      "aggregate prevote rejects invalid validator address",
			cmd:       GetCmdAggregateExchangeRatePrevote,
			args:      []string{"salt", "1.0" + core.MicroUSDDenom, "invalid"},
			expectErr: errortypes.ErrInvalidAddress,
			errText:   "invalid validator address",
		},
		{
			name:      "aggregate vote rejects invalid exchange rates",
			cmd:       GetCmdAggregateExchangeRateVote,
			args:      []string{"salt", "not-a-dec-coin"},
			expectErr: errortypes.ErrInvalidCoins,
			errText:   "parsing exchange rates",
		},
		{
			name:      "aggregate vote rejects invalid validator address",
			cmd:       GetCmdAggregateExchangeRateVote,
			args:      []string{"salt", "1.0" + core.MicroUSDDenom, "invalid"},
			expectErr: errortypes.ErrInvalidAddress,
			errText:   "invalid validator address",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cmd := tc.cmd()
			cmd.SetContext(context.Background())
			clientCtx := client.Context{}.
				WithFrom(fromAddr.String()).
				WithFromAddress(fromAddr)
			require.NoError(t, client.SetCmdClientContext(cmd, clientCtx))

			cmd.SetArgs(tc.args)

			err := cmd.Execute()
			require.Error(t, err)
			require.True(t, sdkerrors.IsOf(err, tc.expectErr))
			require.ErrorContains(t, err, tc.errText)
		})
	}
}
