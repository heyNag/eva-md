package update

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
)

// Rejection names why the checker refused an answer whole: the rule, one of
// the names below, and the place, a path of the schema's own key names
// with each index written [], as items[].kind, or output for the answer as
// a whole. Neither holds any of the answer's text.
type Rejection struct {
	Rule  string `json:"rule"`
	Place string `json:"place"`
}

// The rules that refuse an answer whole.
const (
	RuleNotObject  = "not_object"         // a value that must be an object is not one
	RuleMissingKey = "missing_key"        // an object lacks a key the schema requires
	RuleExtraKey   = "extra_key"          // an object has a key the schema doesn't allow
	RuleNotString  = "not_string"         // a value that must be a string is not one
	RuleNotNull    = "not_string_or_null" // a value that must be a string or null is neither
	RuleNotEnum    = "not_enum"           // a string is not one of the schema's values
	RuleNotArray   = "not_array"          // a value that must be an array is not one
	RuleArraySize  = "array_size"         // an array is shorter or longer than the schema allows
	RuleNotInteger = "not_integer"        // a value that must be an integer is not one
	RuleDates      = "dates"              // too many lines wrote a date no cited entry holds
	RuleCarried    = "carried"            // over half the page's items had no action
	RuleOther      = "other"              // a refusal no rule above names
)

// rejected is a whole answer's refusal: the error the checker returns,
// ErrRejected with the place and the cause in words, and its Rejection.
type rejected struct {
	err error
	why Rejection
}

func (r *rejected) Error() string { return r.err.Error() }
func (r *rejected) Unwrap() error { return r.err }

// reject refuses an answer whole by rule at where, a place in the answer,
// and says why in format's words.
func reject(rule, where, format string, args ...any) error {
	return &rejected{err: fmt.Errorf("%w: "+format, append([]any{ErrRejected}, args...)...), why: Rejection{Rule: rule, Place: place(where)}}
}

// index is an array index in a place.
var index = regexp.MustCompile(`\[\d+\]`)

// place is where as a Rejection keeps it: each index written [], and the
// answer as a whole as output.
func place(where string) string {
	if where == "" || where == "the output" {
		return "output"
	}
	return index.ReplaceAllString(strings.TrimPrefix(where, "the "), "[]")
}

// rejectionOf is the Rejection err carries, or RuleOther at output for an
// error the checker gave without one.
func rejectionOf(err error) *Rejection {
	var r *rejected
	if errors.As(err, &r) {
		why := r.why
		return &why
	}
	return &Rejection{Rule: RuleOther, Place: "output"}
}
