package keeper_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/cosmos/evm/x/precisebank/types"

	sdkmath "cosmossdk.io/math"
	"cosmossdk.io/store/prefix"

	sdk "github.com/cosmos/cosmos-sdk/types"
)

func TestSetGetFractionalBalance(t *testing.T) {
	addr := sdk.AccAddress([]byte("test-address"))
	conversionFactor := types.ConversionFactor() // 1e12 for 6-decimal precision
	maxValidAmount := conversionFactor.SubRaw(1) // 999999999999

	tests := []struct {
		name        string
		address     sdk.AccAddress
		amount      sdkmath.Int
		setPanicMsg string
	}{
		{
			"valid - min amount",
			addr,
			sdkmath.NewInt(1),
			"",
		},
		{
			"valid - positive amount",
			addr,
			sdkmath.NewInt(100),
			"",
		},
		{
			"valid - max amount",
			addr,
			maxValidAmount,
			"",
		},
		{
			"valid - zero amount (deletes)",
			addr,
			sdkmath.ZeroInt(),
			"",
		},
		{
			"invalid - negative amount",
			addr,
			sdkmath.NewInt(-1),
			"amount is invalid: non-positive amount -1",
		},
		{
			"invalid - over max amount",
			addr,
			conversionFactor,
			"amount is invalid: amount 1000000000000 exceeds max of 999999999999",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			td := newMockedTestData(t)
			ctx, k := td.ctx, td.keeper

			if tt.setPanicMsg != "" {
				require.PanicsWithError(t, tt.setPanicMsg, func() {
					k.SetFractionalBalance(ctx, tt.address, tt.amount)
				})

				return
			}

			require.NotPanics(t, func() {
				k.SetFractionalBalance(ctx, tt.address, tt.amount)
			})

			// If its zero balance, check it was deleted in store
			if tt.amount.IsZero() {
				store := prefix.NewStore(ctx.KVStore(td.storeKey), types.FractionalBalancePrefix)
				bz := store.Get(types.FractionalBalanceKey(tt.address))
				require.Nil(t, bz)

				return
			}

			gotAmount := k.GetFractionalBalance(ctx, tt.address)
			require.Equal(t, tt.amount, gotAmount)

			// Delete balance
			k.DeleteFractionalBalance(ctx, tt.address)

			store := prefix.NewStore(ctx.KVStore(td.storeKey), types.FractionalBalancePrefix)
			bz := store.Get(types.FractionalBalanceKey(tt.address))
			require.Nil(t, bz)
		})
	}
}

func TestSetFractionalBalance_InvalidAddr(t *testing.T) {
	tk := newMockedTestData(t)
	ctx, k := tk.ctx, tk.keeper

	require.PanicsWithError(
		t,
		"address cannot be empty",
		func() {
			k.SetFractionalBalance(ctx, sdk.AccAddress{}, sdkmath.NewInt(100))
		},
		"setting balance with empty address should panic",
	)
}

func TestSetFractionalBalance_ZeroDeletes(t *testing.T) {
	td := newMockedTestData(t)
	ctx, k := td.ctx, td.keeper

	addr := sdk.AccAddress([]byte("test-address"))

	// Set balance
	k.SetFractionalBalance(ctx, addr, sdkmath.NewInt(100))

	bal := k.GetFractionalBalance(ctx, addr)
	require.Equal(t, sdkmath.NewInt(100), bal)

	// Set zero balance
	k.SetFractionalBalance(ctx, addr, sdkmath.ZeroInt())

	// Check balance was deleted
	store := prefix.NewStore(ctx.KVStore(td.storeKey), types.FractionalBalancePrefix)
	bz := store.Get(types.FractionalBalanceKey(addr))
	require.Nil(t, bz)

	// Set zero balance again on non-existent balance
	require.NotPanics(
		t,
		func() {
			k.SetFractionalBalance(ctx, addr, sdkmath.ZeroInt())
		},
		"deleting non-existent balance should not panic",
	)
}

func TestIterateFractionalBalances(t *testing.T) {
	tk := newMockedTestData(t)
	ctx, k := tk.ctx, tk.keeper

	addrs := []sdk.AccAddress{}

	for i := 1; i < 10; i++ {
		addr := sdk.AccAddress([]byte{byte(i)})
		addrs = append(addrs, addr)

		// Set balance same as their address byte
		k.SetFractionalBalance(ctx, addr, sdkmath.NewInt(int64(i)))
	}

	seenAddrs := []sdk.AccAddress{}

	k.IterateFractionalBalances(ctx, func(addr sdk.AccAddress, bal sdkmath.Int) bool {
		seenAddrs = append(seenAddrs, addr)

		// Balance is same as first address byte
		require.Equal(t, int64(addr.Bytes()[0]), bal.Int64())

		return false
	})

	require.ElementsMatch(t, addrs, seenAddrs, "all addresses should be seen")
}

func TestGetAggregateSumFractionalBalances(t *testing.T) {
	tk := newMockedTestData(t)
	ctx, k := tk.ctx, tk.keeper

	// Set balances from 1 to 10
	sum := sdkmath.ZeroInt()
	for i := 1; i < 10; i++ {
		addr := sdk.AccAddress([]byte{byte(i)})
		amt := sdkmath.NewInt(int64(i))

		sum = sum.Add(amt)

		require.NotPanics(t, func() {
			k.SetFractionalBalance(ctx, addr, amt)
		})
	}

	gotSum := k.GetTotalSumFractionalBalances(ctx)
	require.Equal(t, sum, gotSum)
}

func TestUpdateFractionalBalance(t *testing.T) {
	addr := sdk.AccAddress([]byte("test-address"))
	conversionFactor := types.ConversionFactor() // 1e12 for 6-decimal precision
	maxValidAmount := conversionFactor.SubRaw(1) // 999999999999

	tests := []struct {
		name                 string
		initialBalance       sdkmath.Int
		newAmount            sdkmath.Int
		expectedFinalBalance sdkmath.Int
		expectedDelta        sdkmath.Int
		setupPanicMsg        string
	}{
		{
			name:                 "update from zero to positive",
			initialBalance:       sdkmath.ZeroInt(),
			newAmount:            sdkmath.NewInt(100),
			expectedFinalBalance: sdkmath.NewInt(100),
			expectedDelta:        sdkmath.NewInt(100),
		},
		{
			name:                 "update from positive to higher positive",
			initialBalance:       sdkmath.NewInt(50),
			newAmount:            sdkmath.NewInt(150),
			expectedFinalBalance: sdkmath.NewInt(150),
			expectedDelta:        sdkmath.NewInt(100),
		},
		{
			name:                 "update from positive to lower positive",
			initialBalance:       sdkmath.NewInt(200),
			newAmount:            sdkmath.NewInt(75),
			expectedFinalBalance: sdkmath.NewInt(75),
			expectedDelta:        sdkmath.NewInt(-125),
		},
		{
			name:                 "update to same amount (no change)",
			initialBalance:       sdkmath.NewInt(100),
			newAmount:            sdkmath.NewInt(100),
			expectedFinalBalance: sdkmath.NewInt(100),
			expectedDelta:        sdkmath.ZeroInt(),
		},
		{
			name:                 "update from positive to zero (delete)",
			initialBalance:       sdkmath.NewInt(100),
			newAmount:            sdkmath.ZeroInt(),
			expectedFinalBalance: sdkmath.ZeroInt(),
			expectedDelta:        sdkmath.NewInt(-100),
		},
		{
			name:                 "update to max valid amount",
			initialBalance:       sdkmath.NewInt(500),
			newAmount:            maxValidAmount,
			expectedFinalBalance: maxValidAmount,
			expectedDelta:        maxValidAmount.Sub(sdkmath.NewInt(500)),
		},
		{
			name:           "invalid - negative amount",
			initialBalance: sdkmath.NewInt(100),
			newAmount:      sdkmath.NewInt(-50),
			setupPanicMsg:  "amount is invalid: non-positive amount -50",
		},
		{
			name:           "invalid - amount exceeds conversion factor",
			initialBalance: sdkmath.NewInt(100),
			newAmount:      conversionFactor,
			setupPanicMsg:  "amount is invalid: amount 1000000000000 exceeds max of 999999999999",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			td := newMockedTestData(t)
			ctx, k := td.ctx, td.keeper

			// Set initial balance if not zero
			if !tt.initialBalance.IsZero() {
				k.SetFractionalBalance(ctx, addr, tt.initialBalance)
			}

			// Verify initial balance
			initialBal := k.GetFractionalBalance(ctx, addr)
			require.Equal(t, tt.initialBalance, initialBal, "initial balance should match")

			// Clear any existing events
			ctx = ctx.WithEventManager(sdk.NewEventManager())

			if tt.setupPanicMsg != "" {
				require.PanicsWithError(t, tt.setupPanicMsg, func() {
					k.UpdateFractionalBalance(ctx, addr, tt.newAmount)
				})
				return
			}

			// Call UpdateFractionalBalance
			require.NotPanics(t, func() {
				k.UpdateFractionalBalance(ctx, addr, tt.newAmount)
			})

			// Verify fractional balance was updated correctly
			finalBalance := k.GetFractionalBalance(ctx, addr)
			require.Equal(t, tt.expectedFinalBalance, finalBalance, "final balance should match expected")

			// Verify event was emitted correctly
			events := ctx.EventManager().Events()
			require.Len(t, events, 1, "exactly one event should be emitted")

			event := events[0]
			require.Equal(t, types.EventTypeFractionalBalanceUpdated, event.Type, "event type should match")

			// Check event attributes
			addressAttr, found := event.GetAttribute(types.AttributeKeyAddress)
			require.True(t, found, "address attribute should be present")
			require.Equal(t, addr.String(), addressAttr.Value, "address attribute should match")

			deltaAttr, found := event.GetAttribute(types.AttributeKeyDelta)
			require.True(t, found, "delta attribute should be present")
			require.Equal(t, tt.expectedDelta.String(), deltaAttr.Value, "delta attribute should match expected")
		})
	}
}

func TestUpdateFractionalBalance_EmptyAddress(t *testing.T) {
	td := newMockedTestData(t)
	ctx, k := td.ctx, td.keeper

	// Clear any existing events
	ctx = ctx.WithEventManager(sdk.NewEventManager())

	require.PanicsWithError(t, "address cannot be empty", func() {
		k.UpdateFractionalBalance(ctx, sdk.AccAddress{}, sdkmath.NewInt(100))
	})

	// Verify no events were emitted due to panic
	events := ctx.EventManager().Events()
	require.Len(t, events, 0, "no events should be emitted on panic")
}

func TestUpdateFractionalBalance_MultipleUpdates(t *testing.T) {
	td := newMockedTestData(t)
	ctx, k := td.ctx, td.keeper

	addr1 := sdk.AccAddress([]byte("test-address-1"))
	addr2 := sdk.AccAddress([]byte("test-address-2"))

	// Clear any existing events
	ctx = ctx.WithEventManager(sdk.NewEventManager())

	// Update balance for addr1: 0 -> 100
	k.UpdateFractionalBalance(ctx, addr1, sdkmath.NewInt(100))

	// Update balance for addr2: 0 -> 200
	k.UpdateFractionalBalance(ctx, addr2, sdkmath.NewInt(200))

	// Update balance for addr1 again: 100 -> 150
	k.UpdateFractionalBalance(ctx, addr1, sdkmath.NewInt(150))

	// Verify final balances
	require.Equal(t, sdkmath.NewInt(150), k.GetFractionalBalance(ctx, addr1))
	require.Equal(t, sdkmath.NewInt(200), k.GetFractionalBalance(ctx, addr2))

	// Verify all events were emitted correctly
	events := ctx.EventManager().Events()
	require.Len(t, events, 3, "three events should be emitted")

	// Check first event (addr1: 0 -> 100, delta = 100)
	event1 := events[0]
	require.Equal(t, types.EventTypeFractionalBalanceUpdated, event1.Type)
	addr1Attr, _ := event1.GetAttribute(types.AttributeKeyAddress)
	delta1Attr, _ := event1.GetAttribute(types.AttributeKeyDelta)
	require.Equal(t, addr1.String(), addr1Attr.Value)
	require.Equal(t, "100", delta1Attr.Value)

	// Check second event (addr2: 0 -> 200, delta = 200)
	event2 := events[1]
	require.Equal(t, types.EventTypeFractionalBalanceUpdated, event2.Type)
	addr2Attr, _ := event2.GetAttribute(types.AttributeKeyAddress)
	delta2Attr, _ := event2.GetAttribute(types.AttributeKeyDelta)
	require.Equal(t, addr2.String(), addr2Attr.Value)
	require.Equal(t, "200", delta2Attr.Value)

	// Check third event (addr1: 100 -> 150, delta = 50)
	event3 := events[2]
	require.Equal(t, types.EventTypeFractionalBalanceUpdated, event3.Type)
	addr3Attr, _ := event3.GetAttribute(types.AttributeKeyAddress)
	delta3Attr, _ := event3.GetAttribute(types.AttributeKeyDelta)
	require.Equal(t, addr1.String(), addr3Attr.Value)
	require.Equal(t, "50", delta3Attr.Value)
}
