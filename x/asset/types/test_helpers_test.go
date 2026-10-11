package types_test

import (
	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"

	"github.com/ararat-network/ark/pkg/chain"
	assettypes "github.com/ararat-network/ark/x/asset/types"
)

func testAsset(denom string) assettypes.Asset {
	return assettypes.Asset{
		Denom:    denom,
		Metadata: chain.NativeAssetMetadata(denom),
		Status:   assettypes.AssetStatus_ASSET_STATUS_ACTIVE,
		Version:  1,
	}
}

func testPlan(denom string) assettypes.SettlementPlan {
	return assettypes.SettlementPlan{
		Denom:               denom,
		RedemptionRate:      math.LegacyOneDec(),
		ActivationTime:      10,
		EarliestClosingTime: 110,
		OpenedHeight:        5,
		OpenedTime:          5,
	}
}

func testWriteOff(denom string, version uint64) assettypes.ResolutionRecord {
	return assettypes.ResolutionRecord{
		Denom:             denom,
		Kind:              assettypes.ResolutionKind_RESOLUTION_KIND_WRITE_OFF,
		Version:           version,
		ResolutionHeight:  20,
		OutstandingSupply: sdk.NewInt64Coin(denom, 100),
	}
}

// testAppointment is a complete committee appointment, the only mandate that may carry suspensions.
func testAppointment() assettypes.EmergencyMandate {
	appointment := assettypes.NewDisabledEmergencyMandate(1)
	appointment.Committee = authtypes.NewModuleAddress("emergency-committee").String()
	appointment.ActivationTime = 10
	appointment.ExpiryTime = 20
	return appointment
}
