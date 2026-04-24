package wasm

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	sdkerrors "cosmossdk.io/errors"

	sdk "github.com/cosmos/cosmos-sdk/types"
	errortypes "github.com/cosmos/cosmos-sdk/types/errors"
)

func TestQuerierQueryCustom(t *testing.T) {
	tests := []struct {
		name      string
		payload   json.RawMessage
		expectErr error
		errText   string
	}{
		{
			name:      "invalid json",
			payload:   json.RawMessage("{"),
			expectErr: errortypes.ErrJSONUnmarshal,
			errText:   "unmarshalling treasury custom query",
		},
		{
			name:      "unknown variant",
			payload:   json.RawMessage(`{}`),
			expectErr: errortypes.ErrInvalidRequest,
			errText:   "unknown treasury query variant",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Querier{}.QueryCustom(sdk.Context{}, tc.payload)
			require.Error(t, err)
			require.True(t, sdkerrors.IsOf(err, tc.expectErr))
			require.ErrorContains(t, err, tc.errText)
		})
	}
}
