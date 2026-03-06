package types

import (
	"github.com/cosmos/cosmos-sdk/codec"
	"github.com/cosmos/cosmos-sdk/codec/legacy"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/msgservice"
)

// RegisterLegacyAminoCodec registers concrete types on the LegacyAmino codec
func RegisterLegacyAminoCodec(cdc *codec.LegacyAmino) {
	legacy.RegisterAminoMsg(cdc, &MsgAggregateExchangeRatePrevote{}, "noah/x/oracle/MsgAggregateExchangeRatePrevote")
	legacy.RegisterAminoMsg(cdc, &MsgAggregateExchangeRateVote{}, "noah/x/oracle/MsgAggregateExchangeRateVote")
	legacy.RegisterAminoMsg(cdc, &MsgDelegateFeedConsent{}, "noah/x/oracle/MsgDelegateFeedConsent")
	legacy.RegisterAminoMsg(cdc, &MsgUpdateParams{}, "noah/x/oracle/MsgUpdateParams")

	cdc.RegisterConcrete(Params{}, "noah/x/oracle/Params", nil)
}

// RegisterInterfaces registers the x/oracle interfaces types with the interface registry
func RegisterInterfaces(registry codectypes.InterfaceRegistry) {
	registry.RegisterImplementations(
		(*sdk.Msg)(nil),
		&MsgAggregateExchangeRatePrevote{},
		&MsgAggregateExchangeRateVote{},
		&MsgDelegateFeedConsent{},
		&MsgUpdateParams{},
	)

	msgservice.RegisterMsgServiceDesc(registry, &_Msg_serviceDesc)
}
