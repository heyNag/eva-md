package redact

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

// benchReply is a made-up Markdown reply whose prose and code mention
// tokens, keys and passwords, as a reply on auth work does, and holds one
// value the rules replace.
var benchReply = []string{
	"## Rotating the deploy token\n\nThe deploy token lives in the CI settings; the code reads it from the environment and never logs it.\n\n",
	"```go\nfunc client() *http.Client {\n\ttoken := os.Getenv(\"DEPLOY_TOKEN\")\n\tif token == \"\" {\n\t\tlog.Fatal(\"set DEPLOY_TOKEN\")\n\t}\n\treturn &http.Client{Transport: auth{token: token}}\n}\n```\n\n",
	"Then call the API with the header set from the variable:\n\n```sh\ncurl -H \"Authorization: Bearer $DEPLOY_TOKEN\" https://api.acme.example/v1/deploys\n```\n\n",
	"- `token_ttl` is how long a session token lives.\n- `api_key_header` names the header the gateway reads.\n- The tokenizer counts input_tokens and output_tokens apart.\n\n",
	"```yaml\nauth:\n  token_endpoint: https://auth.acme.example/oauth/token\n  client_id: deploy-bot\n  client_secret: ${CLIENT_SECRET}\n```\n\n",
	"```python\ndef refresh(session):\n    token = session.fetch_token(scope=\"deploy\")\n    return {\"Authorization\": f\"Bearer {token}\"}\n```\n\n",
	"I checked commit 4e2bf4c14e2bf4c14e2bf4c14e2bf4c14e2bf4c1: the secret rotation landed there, and the password policy did not change.\n\n",
	"The test fixture sets `DB_PASSWORD=" + "Fx7Kq2" + "Wm9Zp4` for the local database only.\n\n",
}

// benchText returns about size bytes of benchReply's paragraphs, in turn,
// cut at the end of a line.
func benchText(size int) string {
	var b strings.Builder
	for i := 0; b.Len() < size; i++ {
		b.WriteString(benchReply[i%len(benchReply)])
	}
	s := b.String()[:size]
	return s[:strings.LastIndexByte(s, '\n')+1]
}

// BenchmarkRedact times Redact with the baseline rules at run time (v1) and
// with the current rules alone, on Markdown replies of 2, 16 and 64 KiB, one
// sub-benchmark for each size. go test's -count runs each sub-benchmark its
// count of times in a row, so the engines would not alternate as separate
// sub-benchmarks: each iteration runs both instead, in turn, the first of
// them changing from one iteration to the next, so that a change in the
// machine's load falls on both. The metrics v1-ns/op and current-ns/op are
// each engine's time, and current/v1 their ratio; ns/op is both. Run it as
//
//	go test -run '^$' -bench '^BenchmarkRedact$' -count 10 ./internal/redact
func BenchmarkRedact(b *testing.B) {
	hook := redacted
	redacted = nil
	defer func() { redacted = hook; SetBaselineAtRunTime(true) }()
	for _, size := range []int{2 << 10, 16 << 10, 64 << 10} {
		text := benchText(size)
		b.Run(fmt.Sprintf("%dKiB", size>>10), func(b *testing.B) {
			var v1, cur time.Duration
			n := 0
			for b.Loop() {
				for k := 0; k < 2; k++ {
					baseline := (n+k)%2 == 0
					SetBaselineAtRunTime(baseline)
					start := time.Now()
					Redact(text)
					if d := time.Since(start); baseline {
						v1 += d
					} else {
						cur += d
					}
				}
				n++
			}
			b.ReportMetric(float64(v1.Nanoseconds())/float64(n), "v1-ns/op")
			b.ReportMetric(float64(cur.Nanoseconds())/float64(n), "current-ns/op")
			b.ReportMetric(float64(cur)/float64(v1), "current/v1")
		})
	}
}

// The benchmark's texts are of their size, and the rules replace something
// in each, so both engines do the work of a reply that holds a secret.
func TestBenchText(t *testing.T) {
	isolate(t)
	for _, size := range []int{2 << 10, 16 << 10, 64 << 10} {
		text := benchText(size)
		if len(text) > size || len(text) < size-200 {
			t.Fatalf("a text of %d bytes for %d", len(text), size)
		}
		if _, n := Redact(text); n == 0 {
			t.Fatalf("the rules replace nothing in the %d-byte text", size)
		}
	}
}
