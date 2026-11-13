package config

// RedactionCurrentRulesOnly runs the current redaction rules alone, with no
// run of the frozen baseline rules beside them (decision 7 of the plan).
// Off, as by default, redaction removes the union of what both remove, as
// v1 does. LoadContext hands it to the redact package, which cannot import
// config, through redact.SetBaselineAtRunTime. On, it keeps every secret in
// a shape of code the current rules keep on purpose and the baseline rules
// remove, such as a YAML alias, a name before an index, a type annotation
// or a regular expression literal (redact's TestOracle records them as gap
// classes, in internal/redact/testdata/corpus/oracle_gaps.txt). It stays off
// until a rule change has closed each of those shapes and the oracle holds
// the rules to each.
var RedactionCurrentRulesOnly = NewBoolSwitch("redaction_current_rules_only", false)
