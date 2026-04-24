package wasm

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	sdkerrors "cosmossdk.io/errors"

	"github.com/cosmos/cosmos-sdk/types/errors"

	wasmexported "noah/x/wasm/exported"
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
