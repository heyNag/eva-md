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
// list, shortened to the names this repository already gives as the
// host's: in internal/catchup/parser.go or internal/ledger/audit/host.go,
// which read the records, else where the comment says. A name on no list
// is counted under other. The card's names dropped, as nothing here gives
// them: Claude Code's row types summary, file-history-snapshot and
// queue-operation, its system subtype compact_boundary, its row fields
// requestId, summary and leafUuid, its message field usage, its content
// item types thinking and redacted_thinking and item fields is_error and
// signature; Codex's payload types context_compacted,
// function_call_output, custom_tool_call_output, reasoning, token_count
// and exec_command_end, its payload fields forked_from_id, status and
// exit_code, its sources mcp and unknown, and every originator.
var (
	// Claude Code's row types: user and assistant (parser.go), system
	// (the HF-00 card's "user and system rows"), progress (catchup's
	// drift test), attachment (the Claude Code research paper's
	// "attachment ... rows").
	claudeRowTypes = []string{"user", "assistant", "system", "progress", "attachment"}
	// Claude Code's row fields. uuid, parentUuid and version are named by
	// the recovery audit paper (working-papers/audit/catchup.md) with the
	// branch field; userType, subtype, level and entrypoint by the HF-00
	// card; toolUseResult by catchup's tests; the rest by parser.go.
	claudeRowFields = []string{"type", "uuid", "parentUuid", "sessionId", "promptId", "prompt_id", "timestamp", "cwd", branchField,
		"version", "userType", "entrypoint", "isMeta", "isSidechain", "isCompactSummary", "repeatsCompactSummary", "isApiErrorMessage",
		"isVisibleInTranscriptOnly", "agentId", "agent_id", "turnOrigin", "origin", "subtype", "level", "message", "toolUseResult", "content"}
	// A Claude Code message's fields (parser.go, host.go).
	claudeMessageFields = []string{"role", "model", "content", "stop_reason", "stop_sequence", "id", "type"}
	// A content item's types: text, tool_use and tool_result (host.go),
	// image and document (parser.go's "an image or document attachment").
	claudeItemTypes = []string{"text", "tool_use", "tool_result", "image", "document"}
	// A content item's fields: type, text, id and name (host.go), input
	// and tool_use_id (the audit's and catchup's tests), content
	// (parser.go).
	claudeItemFields = []string{"type", "text", "id", "name", "input", "tool_use_id", "content"}
	// Claude Code's system subtypes: none is given here.
	claudeSubtypes = []string{}
	// Claude Code's entrypoints: internal/hook's headless list and its
	// test.
	claudeEntrypoints = []string{"cli", "sdk-cli", "sdk-ts", "sdk-py", "claude-desktop", "claude-vscode"}
	// turnOrigin's values (parser.go).
	turnOrigins = []string{"human"}

	// Codex's row types: parser.go, and compacted (the Codex research
	// paper's Compacted item).
	codexRowTypes = []string{"session_meta", "turn_context", "event_msg", "response_item", "compacted"}
	// Codex's row fields: parser.go, and ordinal (the Codex research
	// paper's {timestamp, ordinal?, <item>}).
	codexRowFields = []string{"timestamp", "type", "payload", "ordinal"}
	// Codex's payload types: parser.go and host.go, and turn_started (the
	// HF-00 card).
	codexPayloadTypes = []string{"task_started", "turn_started", "user_message", "agent_message", "task_complete", "turn_aborted",
		"message", "function_call", "custom_tool_call", "local_shell_call"}
	// Codex's payload fields: parser.go and host.go; source, originator,
	// turn_attribution and outcome (the HF-00 card); arguments (the
	// audit's tests); output (internal/hook, Codex's tool response);
	// cli_version (the second field report's prompt).
	codexPayloadFields = []string{"type", "id", "cwd", "source", "originator", "turn_id", "turn_attribution", "message", "phase",
		"last_agent_message", "role", "content", "name", "call_id", "arguments", "output", "outcome", "cli_version"}
	// Codex's sources (the Codex research paper's interactive sources,
	// "not exec").
	codexSources = []string{"cli", "vscode", "atlas", "chatgpt", "exec"}
	// Codex's originators: none is given here.
	codexOriginators = []string{}
)

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
// to the sittings and the owner's answers.
var (
	settledInFull = []string{"F-013", "F-133", "F-134"}
	settledInPart = []string{"F-001", "F-004", "F-005", "F-010", "F-018", "F-020", "F-021", "F-022", "F-024", "F-027", "F-034", "F-079", "F-084",
		"F-085", "F-132", "F-136", "F-138", "F-141"}
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
	"end_with_no_start_and_a_turn", "wrapper_prompts", "inbox", "inbox_only", "no_folder", "in_full", "in_part", "not_by_shapes", "not_a_string",
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
