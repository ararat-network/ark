package ante_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	kmultisig "github.com/cosmos/cosmos-sdk/crypto/keys/multisig"
	"github.com/cosmos/cosmos-sdk/crypto/keys/secp256k1"
	cryptotypes "github.com/cosmos/cosmos-sdk/crypto/types"
	storetypes "github.com/cosmos/cosmos-sdk/store/v2/types"
	"github.com/cosmos/cosmos-sdk/types/tx/signing"
	sdkante "github.com/cosmos/cosmos-sdk/x/auth/ante"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"

	"github.com/ararat-network/ark/app/ante"
)

func TestSimulationSigGasConsumer(t *testing.T) {
	keys := []cryptotypes.PubKey{secp256k1.GenPrivKey().PubKey(), secp256k1.GenPrivKey().PubKey(), secp256k1.GenPrivKey().PubKey()}
	committee := kmultisig.NewLegacyAminoPubKey(2, keys)
	params := authtypes.DefaultParams()
	threshold := 2 * params.SigVerifyCostSecp256k1

	// placeholder is the SDK client's simulation signature: n empty
	// signatures and no bit array.
	placeholder := func(n int) *signing.MultiSignatureData {
		sigs := make([]signing.SignatureData, n)
		for i := range sigs {
			sigs[i] = &signing.SingleSignatureData{SignMode: signing.SignMode_SIGN_MODE_LEGACY_AMINO_JSON}
		}
		return &signing.MultiSignatureData{Signatures: sigs}
	}
	signedBits := cryptotypes.NewCompactBitArray(len(keys))
	signedBits.SetIndex(0, true)
	signedBits.SetIndex(2, true)
	signed := &signing.MultiSignatureData{
		BitArray:   signedBits,
		Signatures: []signing.SignatureData{&signing.SingleSignatureData{}, &signing.SingleSignatureData{}},
	}

	tests := []struct {
		name     string
		consumer sdkante.SignatureVerificationGasConsumer
		data     signing.SignatureData
		wantErr  string
		wantGas  storetypes.Gas
	}{
		{name: "placeholder charges its signatures", consumer: ante.SimulationSigGasConsumer, data: placeholder(2), wantGas: threshold},
		{name: "placeholder beyond the key set is refused", consumer: ante.SimulationSigGasConsumer, data: placeholder(4), wantErr: "bit array size is incorrect"},
		{name: "signed multisig charges as the SDK does", consumer: ante.SimulationSigGasConsumer, data: signed, wantGas: threshold},
		{name: "SDK consumer charges the signed multisig", consumer: sdkante.DefaultSigVerificationGasConsumer, data: signed, wantGas: threshold},
		{name: "SDK consumer refuses the placeholder", consumer: sdkante.DefaultSigVerificationGasConsumer, data: placeholder(2), wantErr: "bit array size is incorrect"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			meter := storetypes.NewInfiniteGasMeter()
			err := tc.consumer(meter, signing.SignatureV2{PubKey: committee, Data: tc.data}, params)
			if tc.wantErr != "" {
				require.ErrorContains(t, err, tc.wantErr)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tc.wantGas, meter.GasConsumed())
		})
	}
}
