package inbox

import (
	"context"
	"strings"
	"testing"
)

// AppendRedacted trusts only the text: it stores the caller's text and
// redaction count as given, without a second redaction pass, and still checks
// identities, redacts the other metadata and cuts text over the limit.
func TestAppendRedactedKeepsTextAndChecksMetadata(t *testing.T) {
	s := fixture(t)
	ctx := context.Background()
	secret := strings.Join([]string{"gh", "p_", strings.Repeat("ab12", 10)}, "")
	e := entry()
	e.Role = "assistant"
	e.Kind = "reply"
	// A synthetic value the redactor would replace. It is stored unchanged,
	// which shows the text gets no second pass; callers redact it first.
	e.Text = "caller-redacted text: token=" + secret
	e.Red = 1
	e.Model = "token=" + secret
	if err := s.AppendRedacted(ctx, e); err != nil {
		t.Fatal(err)
	}
	long := e
	long.Model = ""
	long.Red = 0
	long.Text = strings.Repeat("a", maxText) + "MIDDLE" + strings.Repeat("z", maxText)
	if err := s.AppendRedacted(ctx, long); err != nil {
		t.Fatal(err)
	}
	file := s.Path(e.Project, e.Host, e.Session)
	rows, _, err := s.ReadFrom(file, 0)
	if err != nil || len(rows) != 2 {
		t.Fatalf("rows=%d err=%v", len(rows), err)
	}
	// Red is the caller's count plus the model field's redaction: the text
	// itself was not counted again.
	if rows[0].Text != e.Text || rows[0].Red != 2 {
		t.Fatalf("text changed: %q red=%d", rows[0].Text, rows[0].Red)
	}
	if strings.Contains(rows[0].Model, secret) || rows[0].Model == "" {
		t.Fatalf("metadata not redacted: %q", rows[0].Model)
	}
	if len(rows[1].Text) > maxText || strings.Contains(rows[1].Text, "MIDDLE") || rows[1].Trunc == 0 {
		t.Fatalf("long text not cut: len=%d trunc=%d", len(rows[1].Text), rows[1].Trunc)
	}
	bad := entry()
	bad.Session = secret
	if err := s.AppendRedacted(ctx, bad); err == nil {
		t.Fatal("secret-bearing identity accepted")
	}
}
