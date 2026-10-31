package hook

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"syscall"
	"unicode"
	"unicode/utf8"

	"github.com/heyNag/eva-md/internal/config"
	"github.com/heyNag/eva-md/internal/paths"
	"github.com/heyNag/eva-md/internal/session"
	"github.com/heyNag/eva-md/internal/systempage"
	"github.com/heyNag/eva-md/internal/wikigit"
)

// A prompt that names a system the wiki has a page for gets a line pointing
// at the page, never the page itself, by the names index systempage keeps;
// and the chat's first prompt, then every reminder_every prompts, gets a
// line reminding the agent of its memory (docs/design.md "Per prompt").
// Both read local files only, each bounded.

// Bounds of the lines a prompt gets.
const (
	promptPointers = 3         // pointers one prompt gets
	chatPointers   = 12        // pointers a chat's prompts get
	pointerCap     = 160       // characters of a pointer
	reminderCap    = 100       // characters of the reminder
	minDescription = 20        // the shortest cut of a description a pointer keeps
	minWord        = 5         // characters of the shortest one-word name a prompt matches
	scanPart       = 32 << 10  // bytes matched of a long prompt's start, and of its end
	mapCap         = 256 << 10 // bytes of the system map read for the descriptions
)

// promptsMarker is the session marker holding the chat's promptState.
const promptsMarker = "prompts"

// promptState is a chat's state across its prompts: how many it has had,
// how many pointers they were sent, and the system pages pointed at in it,
// by its briefs or its prompts.
type promptState struct {
	Prompts  int      `json:"prompts"`
	Pointers int      `json:"pointers,omitempty"`
	Pages    []string `json:"pages,omitempty"`
}

// readState reads the chat's state. A marker that isn't one reads as a new
// chat's.
func readState(ctx context.Context, markers session.Markers, host, id string) (promptState, error) {
	value, found, err := markers.GetContext(ctx, host, id, promptsMarker)
	var s promptState
	if err != nil || !found || json.Unmarshal([]byte(value), &s) != nil {
		return promptState{}, err
	}
	return s, nil
}

// point records pages as pointed at in the chat, and reports whether one was
// new.
func (s *promptState) point(pages ...string) bool {
	added := false
	for _, page := range pages {
		if !slices.Contains(s.Pages, page) {
			s.Pages, added = append(s.Pages, page), true
		}
	}
	return added
}

// nextState returns the chat's state after p and the brief or refresh it
// carries, with a prompt the lines that ride with it (see promptLines), and
// whether the state changed. A brief that starts the chat over, at its
// startup or a clear, starts the state over too, since a host may keep a
// session's id across a clear. It saves nothing, and reads the state only
// when it may change.
func nextState(ctx context.Context, markers session.Markers, host string, p payload, env Env, b briefed) (promptState, []string, bool) {
	pages := briefPages(b.text)
	if p.Event != "UserPromptSubmit" && len(pages) == 0 && !b.fresh {
		return promptState{}, nil, false
	}
	s, err := readState(ctx, markers, host, p.Session)
	if err != nil {
		return promptState{}, nil, false
	}
	changed := false
	if b.fresh && (s.Prompts > 0 || len(s.Pages) > 0) {
		s, changed = promptState{}, true
	}
	changed = s.point(pages...) || changed
	if p.Event != "UserPromptSubmit" {
		return s, nil, changed
	}
	return s, promptLines(ctx, env, p.Prompt, b.brief, &s), true
}

// saveState saves the chat's state in place, with one open and write and
// none of the fsyncs and rename of a marker's atomic save, since it changes
// with every prompt. It is soft state, read and saved under the session
// lock: one that a crash loses or tears reads as a new chat's, which costs
// at most a pointer or a reminder sent again.
func saveState(ctx context.Context, markers session.Markers, host, id string, s promptState) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	parts := []string{host, id}
	for i, part := range parts {
		safe, err := paths.SafeName(part)
		if err != nil {
			return err
		}
		parts[i] = safe
	}
	root, err := paths.OpenRoot(markers.Root, true)
	if err != nil {
		return err
	}
	defer root.Close()
	dir := filepath.Join(parts...)
	if err := root.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	f, err := root.OpenFile(filepath.Join(dir, promptsMarker), os.O_WRONLY|os.O_CREATE|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0o600)
	if err != nil {
		return err
	}
	defer f.Close()
	// The marker is truncated only once it is known to be a regular file of
	// its own, as the session's markers are read.
	info, err := f.Stat()
	if err != nil {
		return err
	}
	if st, ok := info.Sys().(*syscall.Stat_t); !info.Mode().IsRegular() || ok && st.Nlink > 1 {
		return errors.New("hook: the prompts marker is not a regular file of its own")
	}
	if err := f.Truncate(0); err != nil {
		return err
	}
	data, _ := json.Marshal(s)
	_, err = f.WriteAt(data, 0)
	return err
}

// briefShow is how a brief points at a system page: a line that starts
// "System: " and ends with the command. A quoted exchange that names the
// command is no pointer, and the brief indents every quoted line.
var briefShow = regexp.MustCompile(`(?m)^System: .* show (systems/[a-z][a-z0-9-]{1,63})$`)

// briefPages lists the system pages a brief's text points at.
func briefPages(text string) []string {
	var out []string
	for _, m := range briefShow.FindAllStringSubmatch(text, -1) {
		if systempage.CheckSlug(strings.TrimPrefix(m[1], "systems/")) == nil {
			out = append(out, m[1])
		}
	}
	return out
}

// promptLines counts the prompt in s and returns the lines it gets beside
// any brief or refresh: pointers to the system pages it names, or else the
// reminder, when it is due. A prompt that carries a brief gets no reminder,
// since the brief's footer says the same.
func promptLines(ctx context.Context, env Env, prompt string, brief bool, s *promptState) []string {
	s.Prompts++
	lines, systems := pointers(ctx, env, prompt, s)
	if len(lines) == 0 && !brief && reminderDue(env.Config.ReminderEvery, s.Prompts) {
		if line := reminder(env.Dirs, systems); line != "" {
			lines = append(lines, line)
		}
	}
	return lines
}

// reminderDue reports whether the reminder rides with a chat's prompt n: the
// first, then every every prompts, and none when every is 0.
func reminderDue(every, n int) bool { return every > 0 && (n-1)%every == 0 }

// reminder is the line reminding the agent of its memory, naming the system
// map when the wiki has system pages: the fullest of its forms that fits in
// 100 characters, or "" when none does.
func reminder(dirs paths.Dirs, systems bool) string {
	bin := filepath.Join(dirs.Share, "bin", "eva-md")
	search := "eva.md: earlier chats: " + bin + " search <terms>"
	forms := []string{search}
	if systems {
		forms = []string{search + "; system map: " + bin + " show systems/map", search + "; system map: show systems/map", search + "; show systems/map", search}
	}
	for _, line := range forms {
		if runes(line) <= reminderCap {
			return line
		}
	}
	return ""
}

// pointers returns the pointers to the system pages a prompt names, at most
// 3 a prompt and 12 a chat, each page once a chat, recording them in s. It
// reports whether the wiki has system pages, by a names index that lists
// one. A wiki that is missing, is not the config's instance or has no index
// gets none.
func pointers(ctx context.Context, env Env, prompt string, s *promptState) ([]string, bool) {
	c := env.Config
	if c.WikiPath == "" || ctx.Err() != nil {
		return nil, false
	}
	if s.Pointers >= chatPointers {
		return nil, true
	}
	root, err := paths.OpenRoot(c.WikiPath, false)
	if err != nil {
		return nil, false
	}
	defer root.Close()
	index := readNames(root)
	if len(index) == 0 || !instanceOf(c) {
		return nil, false
	}
	bin := filepath.Join(env.Dirs.Share, "bin", "eva-md")
	var lines []string
	var descriptions map[string]string
	for _, n := range named(scanned(prompt), index) {
		if len(lines) == promptPointers || s.Pointers >= chatPointers || ctx.Err() != nil {
			break
		}
		slug := strings.TrimPrefix(n.ID, "systems/")
		if slices.Contains(s.Pages, n.ID) || !isPage(root, slug) {
			continue
		}
		if descriptions == nil {
			descriptions = mapDescriptions(root)
		}
		if line := pointer(n.Title, descriptions[slug], bin, slug); line != "" {
			lines = append(lines, line)
			s.Pointers++
			s.point(n.ID)
		}
	}
	return lines, true
}

// readNames reads the wiki's names index, or nothing.
func readNames(root *os.Root) []systempage.Name {
	data, err := readFile(root, systempage.NamesFile, systempage.MaxNamesSize)
	if err != nil {
		return nil
	}
	return systempage.ParseNames(data)
}

// readFile reads a regular file of the wiki, never through a link, of at
// most limit bytes.
func readFile(root *os.Root, name string, limit int64) ([]byte, error) {
	f, err := root.OpenFile(filepath.FromSlash(name), os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	if info, err := f.Stat(); err != nil || !info.Mode().IsRegular() || info.Size() > limit {
		return nil, errors.New("hook: not a regular file within its bound")
	}
	data, err := io.ReadAll(io.LimitReader(f, limit+1))
	if err != nil || int64(len(data)) > limit {
		return nil, errors.New("hook: not a regular file within its bound")
	}
	return data, nil
}

// instanceOf reports whether the wiki is the config's instance, when the
// config names one, as the brief checks before it reads the wiki.
func instanceOf(c config.Config) bool {
	if c.Instance == "" {
		return true
	}
	s, err := config.LoadSchema(filepath.Join(c.WikiPath, ".eva-md", "schema.toml"))
	return err == nil && s.Instance == c.Instance
}

// isPage reports whether systems/<slug>.md is a regular file of the wiki, so
// an index out of date points at no page that is gone.
func isPage(root *os.Root, slug string) bool {
	info, err := root.Lstat(filepath.Join("systems", slug+".md"))
	return err == nil && info.Mode().IsRegular()
}

// mapDescriptions reads the system pages' descriptions, by slug, from the
// system map, which holds a line for each page.
func mapDescriptions(root *os.Root) map[string]string {
	out := map[string]string{}
	data, err := readFile(root, "systems/map.md", mapCap)
	if err != nil {
		return out
	}
	for l := range strings.SplitSeq(string(data), "\n") {
		if e, ok := systempage.ParseMapLine(strings.TrimRight(l, "\r")); ok {
			out[e.Slug] = e.Description
		}
	}
	return out
}

// pointer is the line pointing at a system page, "eva.md: <title>:
// <description>: <bin> show systems/<slug>", in at most 160 characters. The
// description is cut to fit or left out, then the title cut. A line without
// room for the command, or that the daily commit's scan would take for a
// secret, is "".
func pointer(title, description, bin, slug string) string {
	title, description = plain(title), plain(description)
	if title == "" {
		title = slug
	}
	head, tail := "eva.md: "+title, ": "+bin+" show systems/"+slug
	var lines []string
	if room := pointerCap - runes(head+": "+tail); description != "" && room >= min(runes(description), minDescription) {
		lines = append(lines, head+": "+cut(description, room)+tail)
	}
	if room := pointerCap - runes("eva.md: "+tail); room > 0 {
		lines = append(lines, "eva.md: "+cut(title, room)+tail)
	}
	for _, line := range lines {
		if wikigit.SecretLines(line) == nil {
			return line
		}
	}
	return ""
}

// plain makes wiki text one line: control and format characters become
// spaces, and runs of space one.
func plain(s string) string {
	return strings.Join(strings.Fields(strings.Map(func(r rune) rune {
		if unicode.IsControl(r) || unicode.In(r, unicode.Cf, unicode.Zl, unicode.Zp) {
			return ' '
		}
		return r
	}, s)), " ")
}

func runes(s string) int { return utf8.RuneCountInString(s) }

// cut keeps the first n characters of s, the last of them "…" when it cuts.
func cut(s string, n int) string {
	r := []rune(s)
	switch {
	case len(r) <= n:
		return s
	case n <= 0:
		return ""
	}
	return string(r[:n-1]) + "…"
}

// scanned is what a prompt matches names in: the whole prompt, or of a long
// one its first and last 32 KB.
func scanned(prompt string) string {
	if len(prompt) <= 2*scanPart {
		return prompt
	}
	return strings.ToValidUTF8(prompt[:scanPart]+"\n"+prompt[len(prompt)-scanPart:], "")
}

// word is a word of a prompt or a name: letters and digits, which '-', '_'
// or '.' may join, as in payments-api or auth.v2, with the text between it
// and the word before.
type word struct{ text, gap string }

// words splits s into its words.
func words(s string) []word {
	var out []word
	start, last := -1, 0 // where the word being read starts, and where the one before ended
	end := func(i int) {
		out = append(out, word{s[start:i], s[last:start]})
		start, last = -1, i
	}
	for i, r := range s {
		switch {
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			if start < 0 {
				start = i
			}
		case start >= 0 && (r == '-' || r == '_' || r == '.'):
			if next, _ := utf8.DecodeRuneInString(s[i+1:]); !unicode.IsLetter(next) && !unicode.IsDigit(next) {
				end(i)
			}
		case start >= 0:
			end(i)
		}
	}
	if start >= 0 {
		end(len(s))
	}
	return out
}

// stopWords are common words that never match a one-word name, whichever
// page lists them.
var stopWords = func() map[string]bool {
	m := map[string]bool{}
	for _, w := range strings.Fields(`about above admin after again agent along always another around because
		before being below between branch build cache change check class client could config create debug
		delete deploy doing error event every field files first fixed folder found function going great
		having index input issue later local maybe merge method might model never number object order
		other output please query queue really release remote result review right script search server
		service should since source start state status still store string system table target tests
		thanks their there these thing think those three today token under until update users using value
		version where which while worker would write`) {
		m[w] = true
	}
	return m
}()

// named returns the names a prompt names, one for each page, in the order
// the prompt first names them, the longest first where several start at the
// same word. A name of several words matches them as whole words, ignoring
// case, with the same text between them, any run of space as one. A one-word
// name matches a whole word of 5 or more characters that is not a common
// word: ignoring case when the name is distinctive, and otherwise only in
// the case the index lists it in.
func named(prompt string, index []systempage.Name) []systempage.Name {
	type candidate struct {
		systempage.Name
		words []word
	}
	byFirst := map[string][]candidate{}
	for _, n := range index {
		if ws := words(n.Name); len(ws) > 0 {
			first := strings.ToLower(ws[0].text)
			byFirst[first] = append(byFirst[first], candidate{n, ws})
		}
	}
	for _, list := range byFirst {
		slices.SortStableFunc(list, func(a, b candidate) int { return len(b.words) - len(a.words) })
	}
	var out []systempage.Name
	seen := map[string]bool{}
	ws := words(prompt)
	for i, w := range ws {
		for _, c := range byFirst[strings.ToLower(w.text)] {
			if !seen[c.ID] && matches(ws[i:], c.words, c.Name.Name) {
				seen[c.ID] = true
				out = append(out, c.Name)
			}
		}
	}
	return out
}

// matches reports whether a prompt's words, from the first, which matches
// the name's first word ignoring case, name the name listed as named says.
func matches(prompt, name []word, listed string) bool {
	if len(name) == 1 {
		w := prompt[0].text
		return runes(w) >= minWord && !stopWords[strings.ToLower(w)] && (w == listed || distinctive(listed))
	}
	if len(prompt) < len(name) {
		return false
	}
	for k := 1; k < len(name); k++ {
		if !strings.EqualFold(prompt[k].text, name[k].text) || spaced(prompt[k].gap) != name[k].gap {
			return false
		}
	}
	return true
}

// distinctive reports a one-word name that reads as a name in any case: one
// with a capital letter after its first character, a digit, '-', '.' or
// '_', as LedgerSync, payments-api and auth.v2 have.
func distinctive(name string) bool {
	for i, r := range name {
		if i > 0 && unicode.IsUpper(r) || unicode.IsDigit(r) || r == '-' || r == '.' || r == '_' {
			return true
		}
	}
	return false
}

// spaced makes each run of space in s one space.
func spaced(s string) string {
	if s == " " {
		return s
	}
	var b strings.Builder
	space := false
	for _, r := range s {
		if unicode.IsSpace(r) {
			space = true
			continue
		}
		if space {
			b.WriteByte(' ')
			space = false
		}
		b.WriteRune(r)
	}
	if space {
		b.WriteByte(' ')
	}
	return b.String()
}
