package hostfacts

import (
	"strconv"
	"strings"
)

// A voice hand-over, as Codex's source writes it: the wrapper tag, an
// optional source line, the request, an optional transcript line, and the
// closing tag, each line indented by two spaces.
const (
	// VoiceTag opens a hand-over; a test holds it equal to the ledger's
	// inboxv1.Wrapper.
	VoiceTag     = "<realtime_delegation>"
	voiceClose   = "</realtime_delegation>"
	sourceName   = "source"
	inputName    = "input"
	deltaName    = "transcript_delta"
	voiceIndent  = "  "
	voiceSpace   = " \t\r\n"
	TailFlush    = "transcript_tail_flush"
	CutMark      = "…"
	HeadUser     = "user: "
	HeadAssist   = "assistant: "
	HeadOther    = "other"
	sourceOther  = "other"
	voiceDrawKey = "\x00v"
)

// Voice is the skeleton of a hand-over: which line is whose, where the
// request is, whether the host cut a field, whether a tail-flush source
// line is there, and no character of anyone's words. Each part of the
// request and the transcript is kept as its byte length and its keyed
// hash, so a request equal to a line, or a line repeated in another
// hand-over or row, shows as equal hashes.
type Voice struct {
	// Lead and Trail are the white space before and after the wrapper.
	Lead  string `json:"lead,omitempty"`
	Trail string `json:"trail,omitempty"`
	// Source is there when the source line is.
	Source *VoiceSource `json:"source,omitempty"`
	Input  VoiceField   `json:"input"`
	// Transcript is there when the transcript line is.
	Transcript *VoiceTranscript `json:"transcript_delta,omitempty"`
}

// VoiceSource is the source line's value: transcript_tail_flush as it is,
// any other as "other" and its byte length.
type VoiceSource struct {
	Value string `json:"value"`
	Len   int    `json:"len,omitempty"`
}

// VoiceField is the request: its byte length and keyed hash as written,
// escapes and cut marks included, and whether it begins or ends with the
// cut mark.
type VoiceField struct {
	Len      int    `json:"len"`
	Hash     string `json:"hash,omitempty"`
	CutStart bool   `json:"cut_start,omitempty"`
	CutEnd   bool   `json:"cut_end,omitempty"`
}

// VoiceTranscript is the transcript as VoiceField keeps the request, and
// its lines in order.
type VoiceTranscript struct {
	Len      int              `json:"len"`
	Hash     string           `json:"hash,omitempty"`
	CutStart bool             `json:"cut_start,omitempty"`
	CutEnd   bool             `json:"cut_end,omitempty"`
	Lines    []TranscriptLine `json:"lines"`
}

// TranscriptLine is one line of the transcript: its head, "user: ",
// "assistant: " or "other", and the byte length and keyed hash of the rest
// of it, the whole line when the head is other.
type TranscriptLine struct {
	Head string `json:"head"`
	Len  int    `json:"len"`
	Hash string `json:"hash,omitempty"`
}

// voiceText is a hand-over read into its parts, with their text.
type voiceText struct {
	lead, trail string
	source      *string
	input       string
	delta       *string
}

// parseVoice reads s as a hand-over: after white space, exactly what
// Codex's source writes, then white space only, with no < or > in the
// source, the request or the transcript.
func parseVoice(s string) (voiceText, bool) {
	var v voiceText
	body := strings.TrimLeft(s, voiceSpace)
	v.lead = s[:len(s)-len(body)]
	trimmed := strings.TrimRight(body, voiceSpace)
	v.trail = body[len(trimmed):]
	rest, ok := strings.CutPrefix(trimmed, VoiceTag+"\n")
	if !ok {
		return v, false
	}
	line := func(name string) (string, bool) {
		r, ok := strings.CutPrefix(rest, voiceIndent+"<"+name+">")
		if !ok {
			return "", false
		}
		i := strings.IndexAny(r, "<>")
		if i < 0 {
			return "", false
		}
		r, ok = strings.CutPrefix(r[i:], "</"+name+">\n")
		if !ok {
			return "", false
		}
		value := rest[len(voiceIndent)+len(name)+2 : len(voiceIndent)+len(name)+2+i]
		rest = r
		return value, true
	}
	if strings.HasPrefix(rest, voiceIndent+"<"+sourceName+">") {
		src, ok := line(sourceName)
		if !ok {
			return v, false
		}
		v.source = &src
	}
	if v.input, ok = line(inputName); !ok {
		return v, false
	}
	if strings.HasPrefix(rest, voiceIndent+"<"+deltaName+">") {
		d, ok := line(deltaName)
		if !ok {
			return v, false
		}
		v.delta = &d
	}
	return v, rest == voiceClose
}

// cutMarks gives whether a field begins and ends with the cut mark, and the
// field between them.
func cutMarks(s string) (start, end bool, inner string) {
	inner = s
	if rest, ok := strings.CutPrefix(inner, CutMark); ok {
		start, inner = true, rest
	}
	if rest, ok := strings.CutSuffix(inner, CutMark); ok {
		end, inner = true, rest
	}
	return start, end, inner
}

// voiceLines splits a transcript into its lines, after a leading cut mark
// is set aside: each line's head and the rest of it. A trailing cut mark
// stays in the last line's rest.
func voiceLines(delta string) (heads, rests []string) {
	for _, l := range strings.Split(strings.TrimPrefix(delta, CutMark), "\n") {
		head, rest := HeadOther, l
		for _, h := range []string{HeadUser, HeadAssist} {
			if r, ok := strings.CutPrefix(l, h); ok {
				head, rest = h, r
				break
			}
		}
		heads, rests = append(heads, head), append(rests, rest)
	}
	return heads, rests
}

// VoiceOf gives the skeleton of a string in the form of a hand-over, its
// parts hashed with key, or false for any other string.
func VoiceOf(key []byte, s string) (*Voice, bool) {
	t, ok := parseVoice(s)
	if !ok {
		return nil, false
	}
	v := &Voice{Lead: t.lead, Trail: t.trail}
	if t.source != nil {
		v.Source = &VoiceSource{Value: TailFlush}
		if *t.source != TailFlush {
			v.Source = &VoiceSource{Value: sourceOther, Len: len(*t.source)}
		}
	}
	start, end, _ := cutMarks(t.input)
	v.Input = VoiceField{Len: len(t.input), Hash: Hash(key, t.input), CutStart: start, CutEnd: end}
	if t.delta != nil {
		start, end, _ := cutMarks(*t.delta)
		tr := &VoiceTranscript{Len: len(*t.delta), Hash: Hash(key, *t.delta), CutStart: start, CutEnd: end, Lines: []TranscriptLine{}}
		heads, rests := voiceLines(*t.delta)
		for i := range heads {
			tr.Lines = append(tr.Lines, TranscriptLine{Head: heads[i], Len: len(rests[i]), Hash: Hash(key, rests[i])})
		}
		v.Transcript = tr
	}
	return v, true
}

// voiceParts gives the text of each part of a hand-over: an unlisted
// source, the request as written and between its cut marks, the
// transcript, and each line's rest as written and between its cut marks;
// none for any other string.
func voiceParts(s string) []string {
	t, ok := parseVoice(s)
	if !ok {
		return nil
	}
	_, _, body := cutMarks(t.input)
	out := []string{t.input, body}
	if t.source != nil && *t.source != TailFlush {
		out = append(out, *t.source)
	}
	if t.delta != nil {
		out = append(out, *t.delta)
		_, rests := voiceLines(*t.delta)
		for _, r := range rests {
			_, _, body := cutMarks(r)
			out = append(out, r, body)
		}
	}
	return out
}

// PayloadVoices gives the skeleton of each string of a decoded payload that
// is in the form of a hand-over, by its field path as PathString writes
// it. An object key keeps no skeleton.
func PayloadVoices(key []byte, v any) map[string]*Voice {
	out := map[string]*Voice{}
	walk(v, nil, func(path []string, x any) {
		if s, ok := x.(string); ok {
			if sk, ok := VoiceOf(key, s); ok {
				out[PathString(key, path)] = sk
			}
		}
	})
	if len(out) == 0 {
		return nil
	}
	return out
}

// voiceWords are the words a skeleton keeps beside the vocabulary's tags:
// the tail-flush source, the heads and the cut mark, each on the
// vocabulary, and "other", which is on it as a host word.
var voiceWords = []string{TailFlush, HeadUser, HeadAssist, CutMark, HeadOther}

// madeVoice makes the k-th made-up text of n bytes: lower-case ASCII
// letters and single spaces, from the generator's seed, beginning and
// ending with a letter.
func madeVoice(seed []byte, n, k int) string {
	st := newStream(seed, "v", strconv.Itoa(n), k)
	b := make([]byte, n)
	for i := range b {
		if i > 0 && i < n-1 && b[i-1] != ' ' && st.next(6) == 0 {
			b[i] = ' '
			continue
		}
		b[i] = byte('a' + st.next(26))
	}
	return string(b)
}

// cuts are the cut marks the text of each hash is known to carry: a
// request's own, and the trailing one of a cut transcript's last line. A
// hash has one text, so what one part shows of it holds wherever it stands.
type cuts map[string][2]bool

// knownCuts gathers the cut marks of every hash of the skeletons.
func knownCuts(skels []*Voice) cuts {
	c := cuts{}
	mark := func(hash string, start, end bool) {
		m := c[hash]
		c[hash] = [2]bool{m[0] || start, m[1] || end}
	}
	for _, v := range skels {
		mark(v.Input.Hash, v.Input.CutStart, v.Input.CutEnd)
		if t := v.Transcript; t != nil && t.CutEnd && len(t.Lines) > 0 {
			mark(t.Lines[len(t.Lines)-1].Hash, false, true)
		}
	}
	return c
}

// text makes up the text of a part of n bytes with the hash given: a draw
// between the cut marks its text is known to carry. A part too short for
// both holds one, as "…" does.
func (c cuts) text(hash string, n int, draw func(n int) string) string {
	m := c[hash]
	if m[0] && m[1] && n < 2*len(CutMark) {
		m[1] = false
	}
	return marked(m[0], m[1], draw(max(n-markLen(m[0], m[1]), 0)))
}

// assignVoice gives every part of the skeletons, in order, its made-up
// text in texts, by hash: first each line's rest, drawn for its byte
// length; then each transcript, written from its lines; then each request,
// which takes the text of a line or a transcript with its hash, and is
// otherwise drawn. Each draw keeps the cut marks c knows its hash to carry,
// which knownCuts gathers from every skeleton of the recording. A hash
// that has a text keeps it, so equal hashes get equal text across the
// recording.
func assignVoice(skels []*Voice, c cuts, texts map[string]string, draw func(n int) string) {
	for _, v := range skels {
		if t := v.Transcript; t != nil {
			for _, l := range t.Lines {
				if _, ok := texts[l.Hash]; !ok {
					texts[l.Hash] = c.text(l.Hash, l.Len, draw)
				}
			}
		}
	}
	for _, v := range skels {
		if t := v.Transcript; t != nil {
			if _, ok := texts[t.Hash]; !ok {
				texts[t.Hash] = t.text(texts)
			}
		}
	}
	for _, v := range skels {
		if _, ok := texts[v.Input.Hash]; !ok {
			texts[v.Input.Hash] = c.text(v.Input.Hash, v.Input.Len, draw)
		}
	}
}

// rebuild writes a hand-over from its skeleton alone: the same white space,
// lines, heads, source value and cut marks, each part's text from texts,
// as assignVoice gave it, and source as an unlisted source's text.
func (v *Voice) rebuild(texts map[string]string, source string) string {
	var b strings.Builder
	b.WriteString(v.Lead + VoiceTag + "\n")
	if v.Source != nil {
		s := TailFlush
		if v.Source.Value != TailFlush {
			s = source
		}
		b.WriteString(voiceIndent + "<" + sourceName + ">" + s + "</" + sourceName + ">\n")
	}
	b.WriteString(voiceIndent + "<" + inputName + ">" + texts[v.Input.Hash] + "</" + inputName + ">\n")
	if t := v.Transcript; t != nil {
		b.WriteString(voiceIndent + "<" + deltaName + ">" + texts[t.Hash] + "</" + deltaName + ">\n")
	}
	b.WriteString(voiceClose + v.Trail)
	return b.String()
}

// text writes the transcript from its lines' texts, after its leading cut
// mark; a trailing one is in its last line's text.
func (t *VoiceTranscript) text(texts map[string]string) string {
	lines := make([]string, len(t.Lines))
	for i, l := range t.Lines {
		head := l.Head
		if head == HeadOther {
			head = ""
		}
		lines[i] = head + texts[l.Hash]
	}
	return marked(t.CutStart, false, strings.Join(lines, "\n"))
}

func marked(start, end bool, s string) string {
	if start {
		s = CutMark + s
	}
	if end {
		s += CutMark
	}
	return s
}

func markLen(start, end bool) int {
	n := 0
	if start {
		n += len(CutMark)
	}
	if end {
		n += len(CutMark)
	}
	return n
}
