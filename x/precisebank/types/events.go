package types

import (
	sdk "github.com/cosmos/cosmos-sdk/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
)

// Event types for precisebank operations
const (
	EventTypePreciseTransfer     = "precise_transfer"
	EventTypePreciseCoinSpent    = "precise_coin_spent"
	EventTypePreciseCoinReceived = "precise_coin_received"
)

// Attribute keys (reuse existing ones for consistency)
const (
	AttributeKeyPreciseRecipient = banktypes.AttributeKeyRecipient
	AttributeKeyPreciseSender    = banktypes.AttributeKeySender
	AttributeKeyPreciseAmount    = sdk.AttributeKeyAmount
)

// NewPreciseTransferEvent creates a new precise transfer event
func NewPreciseTransferEvent(from, to string, amount sdk.Coins) sdk.Event {
	return sdk.NewEvent(
		EventTypePreciseTransfer,
		sdk.NewAttribute(AttributeKeyPreciseSender, from),
		sdk.NewAttribute(AttributeKeyPreciseRecipient, to),
		sdk.NewAttribute(AttributeKeyPreciseAmount, amount.String()),
	)
}

// NewPreciseCoinSpentEvent creates a new precise coin spent event
func NewPreciseCoinSpentEvent(spender sdk.AccAddress, amount sdk.Coins) sdk.Event {
	return sdk.NewEvent(
		EventTypePreciseCoinSpent,
		sdk.NewAttribute(banktypes.AttributeKeySpender, spender.String()),
		sdk.NewAttribute(AttributeKeyPreciseAmount, amount.String()),
	)
}

// NewPreciseCoinReceivedEvent creates a new precise coin received event
func NewPreciseCoinReceivedEvent(receiver sdk.AccAddress, amount sdk.Coins) sdk.Event {
	return sdk.NewEvent(
		EventTypePreciseCoinReceived,
		sdk.NewAttribute(banktypes.AttributeKeyReceiver, receiver.String()),
		sdk.NewAttribute(AttributeKeyPreciseAmount, amount.String()),
	)
}