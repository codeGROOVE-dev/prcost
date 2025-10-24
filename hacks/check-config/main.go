// Package main prints the default cost configuration values.
package main

import (
	"fmt"

	"github.com/codeGROOVE-dev/prcost/pkg/cost"
)

func main() {
	cfg := cost.DefaultConfig()
	fmt.Printf("EventDuration: %v\n", cfg.EventDuration)
	fmt.Printf("SessionGapThreshold: %v\n", cfg.SessionGapThreshold)
	fmt.Printf("ContextSwitchInDuration: %v\n", cfg.ContextSwitchInDuration)
	fmt.Printf("ContextSwitchOutDuration: %v\n", cfg.ContextSwitchOutDuration)
}
