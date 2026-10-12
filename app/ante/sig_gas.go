package ante

import (
	"cmp"
	"slices"

	cryptotypes "github.com/cosmos/cosmos-sdk/crypto/types"
	"github.com/cosmos/cosmos-sdk/crypto/types/multisig"
	storetypes "github.com/cosmos/cosmos-sdk/store/v2/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/tx/signing"
	sdkante "github.com/cosmos/cosmos-sdk/x/auth/ante"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
)

// SigGasConsumeDecorator is the SDK's signature gas decorator, except that
// simulation prices the SDK client's multisig placeholder: threshold signatures
// with no bit array, which the SDK's consumer refuses since v0.54.4. CheckTx and
// delivery run the SDK's consumer unchanged.
type SigGasConsumeDecorator struct {
	execute  sdkante.SigGasConsumeDecorator
	simulate sdkante.SigGasConsumeDecorator
}

func NewSigGasConsumeDecorator(ak sdkante.AccountKeeper) SigGasConsumeDecorator {
	return SigGasConsumeDecorator{
		execute:  sdkante.NewSigGasConsumeDecorator(ak, sdkante.DefaultSigVerificationGasConsumer),
		simulate: sdkante.NewSigGasConsumeDecorator(ak, simulationSigGasConsumer),
	}
}

func (d SigGasConsumeDecorator) AnteHandle(ctx sdk.Context, tx sdk.Tx, simulate bool, next sdk.AnteHandler) (sdk.Context, error) {
	if simulate {
		return d.simulate.AnteHandle(ctx, tx, simulate, next)
	}
	return d.execute.AnteHandle(ctx, tx, simulate, next)
}

// simulationSigGasConsumer charges a placeholder as though its signatures came
// from the costliest keys, so the estimate covers any set of signers.
func simulationSigGasConsumer(meter storetypes.GasMeter, sig signing.SignatureV2, params authtypes.Params) error {
	pubKey, isMultisig := sig.PubKey.(multisig.PubKey)
	data, isMultiData := sig.Data.(*signing.MultiSignatureData)
	if isMultisig && isMultiData && data.BitArray == nil {
		if bits := costliestKeys(pubKey.GetPubKeys(), len(data.Signatures), params); bits != nil {
			sig.Data = &signing.MultiSignatureData{BitArray: bits, Signatures: data.Signatures}
		}
	}
	return sdkante.DefaultSigVerificationGasConsumer(meter, sig, params)
}

// costliestKeys marks the n keys whose signatures cost most to verify. It
// returns nil, leaving the SDK's refusal, when n exceeds the key set or a key
// is not a single key, such as a nested multisig.
func costliestKeys(keys []cryptotypes.PubKey, n int, params authtypes.Params) *cryptotypes.CompactBitArray {
	if n > len(keys) {
		return nil
	}
	costs := make([]storetypes.Gas, len(keys))
	for i, key := range keys {
		meter := storetypes.NewInfiniteGasMeter()
		single := signing.SignatureV2{PubKey: key, Data: &signing.SingleSignatureData{}}
		if err := sdkante.DefaultSigVerificationGasConsumer(meter, single, params); err != nil {
			return nil
		}
		costs[i] = meter.GasConsumed()
	}
	order := make([]int, len(keys))
	for i := range order {
		order[i] = i
	}
	slices.SortStableFunc(order, func(a, b int) int { return cmp.Compare(costs[b], costs[a]) })
	bits := cryptotypes.NewCompactBitArray(len(keys))
	for _, i := range order[:n] {
		bits.SetIndex(i, true)
	}
	return bits
}
