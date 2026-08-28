package binance_test

import (
	. "ark/pricefeed/sidecar/providers/websocket/binance"
	"encoding/json"
	"testing"

	"cosmossdk.io/log/v2"
	"github.com/stretchr/testify/require"
)

func TestNewSubscribeRequestMessageUsesSequentialIDs(t *testing.T) {
	handler, err := NewHandler(log.NewNopLogger(), DefaultWebSocketConfig)
	require.NoError(t, err)

	messages, err := handler.(*Handler).NewSubscribeRequestMessage([]string{"BTCUSDT", "ETHUSDT"})
	require.NoError(t, err)
	require.Len(t, messages, 2)

	requireSubscribeMessageID(t, messages[0], 1)
	requireSubscribeMessageID(t, messages[1], 2)
}

func TestCopyRestartsSequentialIDs(t *testing.T) {
	handler, err := NewHandler(log.NewNopLogger(), DefaultWebSocketConfig)
	require.NoError(t, err)

	_, err = handler.(*Handler).NewSubscribeRequestMessage([]string{"BTCUSDT"})
	require.NoError(t, err)

	copyHandler := handler.(*Handler).Copy().(*Handler)
	messages, err := copyHandler.NewSubscribeRequestMessage([]string{"ETHUSDT"})
	require.NoError(t, err)
	require.Len(t, messages, 1)

	requireSubscribeMessageID(t, messages[0], 1)
}

func requireSubscribeMessageID(t *testing.T, message []byte, id int64) {
	t.Helper()

	var request SubscribeMessageRequest
	require.NoError(t, json.Unmarshal(message, &request))
	require.Equal(t, id, request.ID)
}
