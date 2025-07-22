package systemtests

import (
	"os"
	"os/exec"
	"os/signal"
	"syscall"
	"testing"

	"github.com/stretchr/testify/require"

	"cosmossdk.io/systemtests"
)

func StartChain(t *testing.T, sut *systemtests.SystemUnderTest) {
	sut.StartChain(t, "--json-rpc.api=eth,txpool,personal,net,debug,web3", "--chain-id", "local-4221", "--api.enable=true")
}

func TestSomething(t *testing.T) {
	sut := systemtests.Sut
	StartChain(t, sut)

	// Create a buffered channel to receive OS signals
	sigs := make(chan os.Signal, 1)

	signal.Notify(sigs, os.Interrupt, syscall.SIGTERM)
	<-sigs

	// forge script script/Counter.s.sol --rpc-url http://127.0.0.1:8545 --broadcast --private-key $PRIVATE_KEY
	pk := "0x88cbead91aee890d27bf06e003ade3d4e952427e88f88d31d61d3ef5e5d54305"
	res, err := exec.Command(
		"forge",
		"script", "script/Counter.s.sol",
		"--rpc-url", "http://127.0.0.1:8545",
		"--broadcast",
		"--private-key", pk,
		"--root", "/Users/tyler/Dev/cosmos/evm/tests/systemtests/Counter",
	).CombinedOutput()
	require.NoError(t, err)
	require.NotEmpty(t, string(res))
	err = os.WriteFile("output", res, 0644)
	require.NoError(t, err)
}
