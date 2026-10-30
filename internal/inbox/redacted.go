package inbox

import "context"

// AppendRedacted appends an entry whose Text the caller has already passed
// through the redactor, so the text is not redacted a second time. Transcript
// recovery uses it: it redacts each recovered row once, within a time budget,
// and redacting the same long text again here could take longer than that
// budget. Everything else is handled exactly as in AppendContext: identity
// fields are checked, other metadata is redacted, and text over the inbox
// limit is cut. Never pass text that has not been redacted.
func (s Store) AppendRedacted(ctx context.Context, e Entry) error {
	return s.append(ctx, e, alreadyRedacted)
}

func alreadyRedacted(text string) (string, int) { return text, 0 }
