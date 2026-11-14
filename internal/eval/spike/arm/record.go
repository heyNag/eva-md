package arm

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"slices"
	"unicode/utf8"
)

// recordVersion is the format of a recording's lines.
const recordVersion = 1

// MaxLines is the most lines a recording Replay reads may hold.
const MaxLines = 1 << 14

// maxLine is the longest line Replay reads: a kept answer and the rest of
// its line.
const maxLine = MaxOutput + 64<<10

// line is one call of a recording, its keys in the card's order. Output is
// the answer as the arm kept it, or null; TokensCachedIn null when the
// runner reported none.
type line struct {
	V              int             `json:"v"`
	Host           string          `json:"host"`
	HostVersion    string          `json:"host_version"`
	Tier           string          `json:"tier"`
	Effort         string          `json:"effort"`
	Form           string          `json:"form"`
	Round          int             `json:"round"`
	Script         string          `json:"script"`
	Pass           int             `json:"pass"`
	Call           string          `json:"call"`
	Output         json.RawMessage `json:"output"`
	Reason         string          `json:"reason"`
	TokensIn       int             `json:"tokens_in"`
	TokensOut      int             `json:"tokens_out"`
	TokensCachedIn *int            `json:"tokens_cached_in"`
	Attempts       int             `json:"attempts"`
	Seconds        float64         `json:"seconds"`
	EffortUsed     string          `json:"effort_used"`
}

// lineKeys are a line's keys, each required; nullable are those that may
// be null.
var (
	lineKeys = []string{"v", "host", "host_version", "tier", "effort", "form", "round", "script", "pass", "call",
		"output", "reason", "tokens_in", "tokens_out", "tokens_cached_in", "attempts", "seconds", "effort_used"}
	nullable = []string{"output", "tokens_cached_in"}
)

// keep is the answer an arm keeps of output: its compact form, when it is
// valid UTF-8, one JSON value other than null and at most MaxOutput bytes;
// else nil, which reads as no JSON. What is kept is what is parsed, sent
// back in a repair input and recorded, so a replay reads the very bytes
// the run read; keep of what keep gave is the same.
func keep(output []byte) []byte {
	if len(output) == 0 || len(output) > MaxOutput || !utf8.Valid(output) || !json.Valid(output) {
		return nil
	}
	var b bytes.Buffer
	if json.Compact(&b, output) != nil || b.String() == "null" {
		return nil
	}
	return b.Bytes()
}

// record writes call c, its reply and its kept answer as one line on w, in
// one write.
func record(w io.Writer, a Arm, c Call, r Reply, out []byte) error {
	l := line{V: recordVersion, Host: a.Host, HostVersion: a.HostVersion, Tier: a.Tier, Effort: a.Effort, Form: string(a.Form),
		Round: c.Round, Script: c.Script, Pass: c.Pass, Call: c.Kind, Output: out, Reason: r.Reason,
		TokensIn: r.TokensIn, TokensOut: r.TokensOut, TokensCachedIn: r.CachedIn, Attempts: r.Attempts, Seconds: r.Seconds,
		EffortUsed: r.Effort}
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(l); err != nil {
		return fmt.Errorf("arm: cannot write the recording: %w", err)
	}
	if _, err := w.Write(b.Bytes()); err != nil {
		return fmt.Errorf("arm: cannot write the recording: %w", err)
	}
	return nil
}

// key names one call of a run.
type key struct {
	round, pass int
	script      string
	kind        string
}

// Replay reads the recording on r of a run of arm, and gives an Ask that
// answers each call from its line, with no other call, and the host's
// version every line gives. It refuses, before any call can be made, a
// recording of another host, tier, effort or form, or of another host
// version than arm gives when it gives one; one whose lines do not share
// a host version; a line given twice; a reason that is not a short id;
// and any line that is not one of the format's. Its Ask fails on a call
// it lacks or was asked already, on a call whose context is not one Run
// gave, and on a call of a Run of another arm: another host, host
// version, tier, effort or form than the recording's. Once a Run that
// used it has made every call, that Run fails if a line was left unused.
// A replay is used by one Run.
func Replay(r io.Reader, arm Arm) (Ask, string, error) {
	if err := arm.check(false); err != nil {
		return nil, "", err
	}
	lines := map[key]Reply{}
	version := ""
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64<<10), maxLine)
	n := 0
	for sc.Scan() {
		n++
		if n > MaxLines {
			return nil, "", fmt.Errorf("arm: the recording holds more than %d lines", MaxLines)
		}
		l, err := readLine(sc.Bytes())
		if err != nil {
			return nil, "", fmt.Errorf("arm: recording line %d: %w", n, err)
		}
		switch {
		case l.Host != arm.Host || l.Tier != arm.Tier || l.Effort != arm.Effort || l.Form != string(arm.Form):
			return nil, "", fmt.Errorf("arm: recording line %d is of another arm", n)
		case version != "" && l.HostVersion != version:
			return nil, "", fmt.Errorf("arm: recording line %d gives another host version", n)
		case arm.HostVersion != "" && l.HostVersion != arm.HostVersion:
			return nil, "", fmt.Errorf("arm: recording line %d is of another host version than the arm's", n)
		}
		version = l.HostVersion
		k := key{round: l.Round, pass: l.Pass, script: l.Script, kind: l.Call}
		if _, ok := lines[k]; ok {
			return nil, "", fmt.Errorf("arm: recording line %d is a call given twice", n)
		}
		lines[k] = Reply{Output: l.Output, Reason: l.Reason, TokensIn: l.TokensIn, TokensOut: l.TokensOut, Attempts: l.Attempts,
			CachedIn: l.TokensCachedIn, Seconds: l.Seconds, Effort: l.EffortUsed}
	}
	if err := sc.Err(); err != nil {
		return nil, "", fmt.Errorf("arm: cannot read the recording at line %d: %w", n+1, err)
	}
	if n == 0 {
		return nil, "", errors.New("arm: the recording holds no line")
	}
	used := map[key]bool{}
	registered := map[*thisRun]bool{}
	ask := func(ctx context.Context, c Call) (Reply, error) {
		run, ok := ctx.Value(runKey{}).(*thisRun)
		switch {
		case !ok:
			return Reply{}, errors.New("a replay answers only a call made by Run, with the context Run gives")
		case run.arm.Host != arm.Host || run.arm.Tier != arm.Tier || run.arm.Effort != arm.Effort || run.arm.Form != arm.Form ||
			run.arm.HostVersion != version:
			return Reply{}, errors.New("the run is of another arm than the recording")
		}
		if !registered[run] {
			registered[run] = true
			run.checks = append(run.checks, func() error {
				if len(used) < len(lines) {
					return fmt.Errorf("arm: the replay left %d of the recording's %d lines unused", len(lines)-len(used), len(lines))
				}
				return nil
			})
		}
		k := key{round: c.Round, pass: c.Pass, script: c.Script, kind: c.Kind}
		reply, ok := lines[k]
		switch {
		case !ok:
			return Reply{}, errors.New("the recording holds no such call")
		case used[k]:
			return Reply{}, errors.New("the recording's call was asked already")
		}
		used[k] = true
		reply.Output = slices.Clone(reply.Output)
		return reply, nil
	}
	return ask, version, nil
}

// readLine reads one line strictly: one JSON object with every key of the
// format once and no other, null only where nullable allows it, nothing
// after it, and every value in its range.
func readLine(data []byte) (line, error) {
	if !utf8.Valid(data) {
		return line{}, errors.New("not UTF-8")
	}
	d := json.NewDecoder(bytes.NewReader(data))
	tok, err := d.Token()
	if err != nil {
		return line{}, errors.New("not a JSON object")
	}
	if delim, ok := tok.(json.Delim); !ok || delim != '{' {
		return line{}, errors.New("not a JSON object")
	}
	seen := map[string]bool{}
	for d.More() {
		tok, err := d.Token()
		if err != nil {
			return line{}, errors.New("not a JSON object")
		}
		k, _ := tok.(string)
		switch {
		case !slices.Contains(lineKeys, k):
			return line{}, errors.New("a key is not the format's")
		case seen[k]:
			return line{}, fmt.Errorf("key %s is given twice", k)
		}
		seen[k] = true
		var v json.RawMessage
		if err := d.Decode(&v); err != nil {
			return line{}, errors.New("not a JSON object")
		}
		if string(v) == "null" && !slices.Contains(nullable, k) {
			return line{}, fmt.Errorf("key %s is null", k)
		}
	}
	if _, err := d.Token(); err != nil {
		return line{}, errors.New("not a JSON object")
	}
	if _, err := d.Token(); err != io.EOF {
		return line{}, errors.New("data after the object")
	}
	if len(seen) != len(lineKeys) {
		return line{}, errors.New("a key is missing")
	}
	var l line
	if err := json.Unmarshal(data, &l); err != nil {
		return line{}, errors.New("a value is not of its key's type")
	}
	if string(l.Output) == "null" {
		l.Output = nil
	}
	switch {
	case l.V != recordVersion:
		return line{}, fmt.Errorf("its format is not %d", recordVersion)
	case !versionPattern.MatchString(l.HostVersion):
		return line{}, errors.New("the host version is not a version")
	case l.Round < 1 || l.Pass < 1:
		return line{}, errors.New("a round or a pass is not counted from 1")
	case !idPattern.MatchString(l.Script):
		return line{}, errors.New("the script is not an id")
	case l.Call != KindFirst && l.Call != KindRepair:
		return line{}, errors.New("the call is neither first nor repair")
	case l.Reason != "" && l.Output != nil:
		return line{}, errors.New("a failed call gives an answer")
	case len(l.Output) > MaxOutput:
		return line{}, errors.New("the answer is longer than an arm keeps")
	}
	r := Reply{Reason: l.Reason, TokensIn: l.TokensIn, TokensOut: l.TokensOut, Attempts: l.Attempts, CachedIn: l.TokensCachedIn,
		Seconds: l.Seconds, Effort: l.EffortUsed}
	if err := r.check(); err != nil {
		return line{}, err
	}
	return l, nil
}
