package systemtests

import (
	"fmt"
	"os/exec"
	"testing"
)

func TestThing(t *testing.T) {
	out, err := exec.Command("forge", "--help").CombinedOutput()
	if err != nil {
		panic(err)
	}
	fmt.Println(string(out))
}
