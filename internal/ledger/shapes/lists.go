package shapes

import (
	"reflect"
	"slices"
	"strings"

	"github.com/heyNag/eva-md/internal/host/adapter"
)

// other is what a name no list holds is counted under.
const other = "other"

// The names counted by their own name. Each list is the card's (HF-01)
// list, shortened to the names a source gives as the host's: each name is
// in a from with its source, and the lists' test holds every name to one.
// A name on no list is counted under other. The card's names dropped, as
// no source here gives them: Claude Code's row types summary,
// file-history-snapshot and queue-operation, its system subtype
// compact_boundary, its row fields requestId, summary and leafUuid, its
// message field usage, its content item types thinking and
// redacted_thinking and item fields is_error and signature; Codex's
// payload types context_compacted, function_call_output,
// custom_tool_call_output, reasoning, token_count and exec_command_end,
// its payload fields status and exit_code, its sources mcp and unknown,
// and every originator.
var (
	// Claude Code's row types.
	claudeRowTypes = slices.Concat(
		from(parserGo, "user", "assistant"),
		from(hf00Card, "system"), // its "user and system rows"
		from(driftTestGo, "progress"),
		from(claudePaper, "attachment"))
	// Claude Code's row fields.
	claudeRowFields = slices.Concat(
		from(parserGo, "type", "sessionId", "promptId", "prompt_id", "timestamp", "cwd", "isMeta", "isSidechain", "isCompactSummary",
			"repeatsCompactSummary", "isApiErrorMessage", "isVisibleInTranscriptOnly", "agentId", "agent_id", "turnOrigin", "origin",
			"message", "content"),
		from(catchupPaper, "uuid", "parentUuid", branchField, "version"),
		from(hf00Card, "userType", "entrypoint", "subtype", "level"),
		from(scanTestGo, "toolUseResult"))
	// A Claude Code message's fields.
	claudeMessageFields = from(auditHostGo, "role", "model", "content", "stop_reason", "stop_sequence", "id", "type")
	// A content item's types.
	claudeItemTypes = slices.Concat(
		from(auditHostGo, "text", "tool_use", "tool_result"),
		from(parserGo, "image", "document")) // its "an image or document attachment"
	// A content item's fields.
	claudeItemFields = slices.Concat(
		from(auditHostGo, "type", "text", "id", "name", "content"),
		from(auditTestGo, "input", "tool_use_id"))
	// Claude Code's system subtypes: none is given here.
	claudeSubtypes = []string{}
	// Claude Code's entrypoints: the hook's headless list and its test.
	claudeEntrypoints = from(headlessTestGo, "cli", "sdk-cli", "sdk-ts", "sdk-py", "claude-desktop", "claude-vscode")
	// turnOrigin's values.
	turnOrigins = from(parserGo, "human")

	// Codex's row types.
	codexRowTypes = slices.Concat(
		from(parserGo, "session_meta", "turn_context", "event_msg", "response_item"),
		from(codexRolloutItem, "compacted"))
	// Codex's row fields.
	codexRowFields = slices.Concat(
		from(parserGo, "timestamp", "type", "payload"),
		from(codexPaper, "ordinal")) // its {timestamp, ordinal?, <item>}
	// Codex's payload types.
	codexPayloadTypes = slices.Concat(
		from(parserGo, "task_started", "user_message", "agent_message", "task_complete", "message"),
		from(auditHostGo, "turn_aborted", "function_call", "custom_tool_call", "local_shell_call"),
		from(hf00Card, "turn_started"))
	// Codex's payload fields. forked_from_id names, in a forked session's
	// first record, the session it was forked from: a fork's file need not
	// repeat that session's rows, so the report joins a fork to its chat by
	// it.
	codexPayloadFields = slices.Concat(
		from(parserGo, "type", "id", "cwd", "turn_id", "message", "phase", "last_agent_message", "role", "content"),
		from(auditHostGo, "name", "call_id"),
		from(hf00Card, "source", "originator", "turn_attribution", "outcome"),
		from(auditTestGo, "arguments"),
		from(hookGo, "output"), // Codex's tool response
		from(fieldPrompt2, "cli_version"),
		from(codexSessionMeta, "forked_from_id"))
	// Codex's sources: the paper's interactive sources, "not exec".
	codexSources = from(codexPaper, "cli", "vscode", "atlas", "chatgpt", "exec")
	// Codex's originators: none is given here.
	codexOriginators = []string{}
)

// source is where names on a list come from: a file of this repository,
// by its path from the repository's root, which holds each name taken
// from it as a word; or a host's published source or documents, outside
// the repository, after "published: ".
type source string

// The lists' sources.
const (
	parserGo       source = "internal/catchup/parser.go"
	auditHostGo    source = "internal/ledger/audit/host.go"
	auditTestGo    source = "internal/ledger/audit/audit_test.go"
	scanTestGo     source = "internal/catchup/scan_test.go"
	driftTestGo    source = "internal/catchup/drift_test.go"
	hookGo         source = "internal/hook/hook.go"
	headlessTestGo source = "internal/hook/headless_test.go"
	hf00Card       source = "docs/v2/cards/HF-00.md"
	catchupPaper   source = "docs/v2/working-papers/audit/catchup.md"
	claudePaper    source = "docs/v2/working-papers/research/claude-surface.md"
	codexPaper     source = "docs/v2/working-papers/research/codex-surface.md"
	fieldPrompt2   source = "docs/v2/field-report-2-prompt.md"
	// A session file's first record, as Codex's published source declares
	// it (SessionMeta in codex-rs/protocol/src/protocol.rs) and writes it
	// (codex-rs/rollout/src/recorder.rs).
	codexSessionMeta source = "published: Codex's source, codex-rs/protocol/src/protocol.rs"
	// A row's type, as Codex's published source names a rollout item's
	// kinds, in snake case (codex-rs/history/src/rollout_payload.rs).
	codexRolloutItem source = "published: Codex's source, codex-rs/history/src/rollout_payload.rs"
)

// from is names, which its source gives as the host's. The lists' test reads
// the source of each list's names from its calls.
func from(_ source, names ...string) []string {
	return names
}

// branchField is the name of Claude Code's row field for the git branch.
const branchField = "git" + "Branch"

// The buckets sizes, gaps in time and files of a chat are counted in.
var (
	sizeBuckets = []struct {
		below int
		name  string
	}{{1 << 10, "under 1 KiB"}, {16 << 10, "1 to 16 KiB"}, {64 << 10, "16 to 64 KiB"}, {1 << 20, "64 KiB to 1 MiB"}, {-1, "over 1 MiB"}}
	gapBuckets = []struct {
		below float64 // seconds
		name  string
	}{{1, "under 1 s"}, {10, "1 to 10 s"}, {60, "10 to 60 s"}, {600, "1 to 10 min"}, {3600, "10 to 60 min"}, {86400, "1 to 24 h"}, {-1, "over 24 h"}}
	fileBuckets = []struct {
		upTo int
		name string
	}{{1, "1"}, {2, "2"}, {5, "3 to 5"}, {10, "6 to 10"}, {-1, "over 10"}}
)

// The facts of docs/v2/host-facts.md marked "by the field report", by
// what shapes can do for them. Settled are the facts whose figures the
// report gives under their number, in full or in part; the rest are left
// to the sittings and the owner's answers. F-013 is settled in part: the
// report sees a session id only in the names of a main session file and an
// inbox file, never in a payload, nor in a subagent's file, and no name
// can hold a "/". F-134 is settled in part: recovery's opener reads only a
// Codex file's first 64 KiB for the first record that names the session,
// so a file whose first record is longer than that is refused before it
// is read; such files are counted, with any other first record that does
// not name the session and its folder, as hosts.codex.files.not_identified.
var (
	settledInFull = []string{"F-133"}
	settledInPart = []string{"F-001", "F-004", "F-005", "F-010", "F-013", "F-018", "F-020", "F-021", "F-022", "F-024", "F-027", "F-034", "F-079", "F-084",
		"F-085", "F-132", "F-134", "F-136", "F-138", "F-141"}
	notByShapes = []string{"F-025", "F-028", "F-029", "F-030", "F-031", "F-032", "F-038", "F-046", "F-047", "F-057", "F-087",
		"F-126", "F-127", "F-137", "F-147", "F-148", "F-149", "F-150", "F-156", "F-160", "F-161", "F-173", "F-176", "F-177",
		"F-178", "F-180", "F-182", "F-185", "F-189", "F-202"}
)

// figureWords are the words the report's figures are named by, beside the
// lists' names, the buckets and the facts' numbers.
var figureWords = []string{
	"rows", "chats", "other_names", "files", "listed", "subagent_files", "read", "not_known", "off_the_record", "left_out",
	"refused", "gone", "unreadable", "not_reached", "not_read_whole", "files_per_chat", "too_long", "not_json", "no_time",
	"row_types", "fields", "message_fields", "item_types", "item_fields", "subtypes", "entrypoint", "turn_origin", "versions",
	"flags_true", "payload_types", "payload_fields", "source", "originator", "first_row", "first_row_bytes", "bytes",
	"with_turn_id", "inbox_turns", "one_id", "more_ids", "no_id", "inbox_ids", "on_rows", "not_on_rows", "replies_by_hook",
	"replies_recovered", "missing_reply_gaps", "records", "no_record", "path_named", "missing", "none_named", "not_written",
	"why_not_kept", "prompts", "began_turn", "while_working", "file_ids", "uuid_form", "other_form", "with_dot", "order",
	"later", "same_time", "earlier", "gaps", "file_written_after_last_row", "time", "folder", "branch", "folder_changed",
	"branch_changed", "assistant_rows", "text_only", "tools_only", "text_and_tools", "neither", "tool_results",
	"with_tool_use_result", "no_eva_md_file", "sidechain", "agent_id", "of_another_chat", "compact_summary", "compacted",
	"summary_bytes", "with_ordinal", "ordinal_rising", "ordinal_not_rising", "with_a_turn", "end_with_no_start",
	"end_with_no_start_and_a_turn", "wrapper_prompts", "inbox", "inbox_only", "no_folder", "not_identified", "out_of_range", "in_full", "in_part", "not_by_shapes", "not_a_string",
}

// Words is every word the report may print but numbers, days and
// versions: the lists' names, other, the figures' names, the buckets, the
// facts' numbers, the hosts' ids, the parts' states and the report's own
// keys. Write refuses a report that prints any other
// string.
func Words() []string {
	var w []string
	for _, l := range [][]string{claudeRowTypes, claudeRowFields, claudeMessageFields, claudeItemTypes, claudeItemFields,
		claudeSubtypes, claudeEntrypoints, turnOrigins, codexRowTypes, codexRowFields, codexPayloadTypes, codexPayloadFields,
		codexSources, codexOriginators, settledInFull, settledInPart, notByShapes, figureWords,
		{other, under3, PartCounted, PartNone, PartIncomplete, PartUnreadable}} {
		w = append(w, l...)
	}
	for _, b := range sizeBuckets {
		w = append(w, b.name)
	}
	for _, b := range gapBuckets {
		w = append(w, b.name)
	}
	for _, b := range fileBuckets {
		w = append(w, b.name)
	}
	for _, h := range adapter.All() {
		w = append(w, string(h.ID))
	}
	t := reflect.TypeOf(Report{})
	for i := range t.NumField() {
		name, _, _ := strings.Cut(t.Field(i).Tag.Get("json"), ",")
		w = append(w, name)
	}
	slices.Sort(w)
	return slices.Compact(w)
}

// listed is name when list holds it, and other when not.
func listed(list []string, name string) string {
	if slices.Contains(list, name) {
		return name
	}
	return other
}
