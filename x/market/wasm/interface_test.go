package wasm

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	sdkerrors "cosmossdk.io/errors"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/errors"

	markettypes "ark/x/market/types"
	wasmexported "ark/x/wasm/exported"
)

func TestMsgParserParseCustom(t *testing.T) {
	tests := []struct {
		name      string
		payload   json.RawMessage
		expectErr error
		errText   string
	}{
		{
			name:      "invalid json",
			payload:   json.RawMessage("{"),
			expectErr: errors.ErrJSONUnmarshal,
			errText:   "unmarshalling market custom message",
		},
		{
			name:      "unknown variant",
			payload:   json.RawMessage(`{}`),
			expectErr: wasmexported.ErrInvalidMsg,
			errText:   "unknown market message variant",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := MsgParser{}.ParseCustom(nil, tc.payload)
			require.Error(t, err)
			require.True(t, sdkerrors.IsOf(err, tc.expectErr))
			require.ErrorContains(t, err, tc.errText)
		})
	}
}

// TestMsgParserAcceptsClassicShapedSwap pins the compatibility the optional
// minimum bought: Classic contracts emit swap JSON with no minimum_receive
// key at all, which decodes to the zero coin — the "accept market execution"
// spelling — and must parse rather than force ported contracts to fabricate
// a dust floor.
func TestMsgParserAcceptsClassicShapedSwap(t *testing.T) {
	contract := sdk.AccAddress([]byte("contract____________"))
	recipient := sdk.AccAddress([]byte("recipient___________"))

	swapMsg, err := MsgParser{}.ParseCustom(contract, json.RawMessage(
		`{"swap":{"offer_coin":{"denom":"ausd","amount":"1000000"},"ask_denom":"akrw"}}`,
	))
	require.NoError(t, err)
	swap, ok := swapMsg.(*markettypes.MsgSwap)
	require.True(t, ok)
	require.Equal(t, contract.String(), swap.Trader)
	require.True(t, swap.MinimumReceive.Amount.IsNil())
	require.Empty(t, swap.MinimumReceive.Denom)

	swapSendMsg, err := MsgParser{}.ParseCustom(contract, json.RawMessage(
		`{"swap_send":{"to_address":"`+recipient.String()+
			`","offer_coin":{"denom":"ausd","amount":"1000000"},"ask_denom":"akrw"}}`,
	))
	require.NoError(t, err)
	swapSend, ok := swapSendMsg.(*markettypes.MsgSwapSend)
	require.True(t, ok)
	require.Equal(t, contract.String(), swapSend.FromAddress)
	require.True(t, swapSend.MinimumReceive.Amount.IsNil())
	require.Empty(t, swapSend.MinimumReceive.Denom)
}
