package alerting

import (
	"context"
	"strings"
	"testing"
)

// A failed delivery's error reaches the logs and the channel's status. For
// Telegram the bot token is part of the URL, so the URL must not be in it.
func TestDeliveryErrorsDoNotQuoteTheURL(t *testing.T) {
	const secret = "123456:SECRET-bot-token"
	// Nothing listens on port 1: the request fails in the transport.
	err := postJSON(context.Background(), "http://127.0.0.1:1/bot"+secret+"/sendMessage", map[string]any{}, nil)
	if err == nil {
		t.Fatal("expected a delivery error")
	}
	if strings.Contains(err.Error(), secret) {
		t.Fatalf("error leaks the token: %v", err)
	}
}
