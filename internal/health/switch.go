package health

import "github.com/heyNag/eva-md/internal/config"

// Switch turns on v2's health: each stage's lag beside its bar, the counted
// losses, one row for a host nobody uses, the search index's note and the
// verdict (see stages.go, losses.go and verdict.go). Off, the default, is
// v1: every row, the output of eva-md status and doctor and their exit
// codes are as they were.
var Switch = config.NewBoolSwitch("health", false)
