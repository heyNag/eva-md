package spike

import "testing"

func TestShowPrompt(t *testing.T) {
	t.Log("\n" + Prompt(FormQuotes))
	t.Log("\n" + Prompt(FormSegments))
	t.Log("\n" + string(Schema(FormQuotes)))
}
