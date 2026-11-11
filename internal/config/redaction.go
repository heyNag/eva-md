package config

// RedactionCurrentRulesOnly runs the current redaction rules alone, with no
// run of the frozen baseline rules beside them (decision 7 of the plan).
// Off, as by default, redaction removes the union of what both remove, as
// v1 does. LoadContext hands it to the redact package, which cannot import
// config, through redact.SetBaselineAtRunTime.
var RedactionCurrentRulesOnly = NewBoolSwitch("redaction_current_rules_only", false)
