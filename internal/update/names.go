package update

import (
	"slices"
	"strings"
)

// Outcomes lists the outcomes a record of this release can carry.
var Outcomes = []string{OutcomeOK, OutcomeNoChange, OutcomeSkipped, OutcomeFailed, OutcomeBusy}

// Reasons lists the reasons a record of this release can carry: the job's
// and the runners'.
var Reasons = []string{
	ReasonPageRefused, ReasonWikiError, ReasonConflict, ReasonWatermark, ReasonLeft, ReasonLocked, ReasonVerified, ReasonDamaged,
	ReasonQuarantined, ReasonSmall, ReasonHeadless,
	ReasonCLIMissing, ReasonAuth, ReasonRateLimit, ReasonHostError, ReasonModelRejected, ReasonTimeout, ReasonInvalidOutput, ReasonToolUsed, ReasonMCPFailed,
}

// RefusalRules lists the rules a whole refusal can name.
var RefusalRules = []string{
	RuleNotObject, RuleMissingKey, RuleExtraKey, RuleNotString, RuleNotNull, RuleNotEnum, RuleNotArray, RuleArraySize, RuleNotInteger,
	RuleDates, RuleCarried, RuleOther,
}

// placeKeys are the key names of the answer's schema, of the answer and of
// the objects in its lists, from which every place a refusal names is made.
var placeKeys = append(slices.Clone(outputKeys), "text", "refs", "id", "kind", "action", "tag", "reason", "replacement", "title", "scope", "paths")

// KnownPlace reports whether p is a place a refusal of this release can
// name: output, or a path of the schema's key names, each optionally
// followed by [] for an index, and output only at its head.
func KnownPlace(p string) bool {
	if p == "output" {
		return true
	}
	for _, part := range strings.Split(strings.TrimPrefix(p, "output."), ".") {
		key, _, _ := strings.Cut(part, "[")
		if !slices.Contains(placeKeys, key) || strings.ReplaceAll(part[len(key):], "[]", "") != "" {
			return false
		}
	}
	return true
}
