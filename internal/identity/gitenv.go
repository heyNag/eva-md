package identity

import (
	"os"
	"strings"
)

// gitDropped reports whether eva-md leaves the variable name out of the
// environment of every git it runs. The identity and code reads, the
// brief's live git, the update's commit subjects and the wiki's runner all
// leave out the same ones, from this one list: the variables that point git
// at another repository (those git itself clears before it runs a command
// in another repository, and GIT_NAMESPACE), carry config
// (GIT_CONFIG_PARAMETERS, which `git -c` hands down, GIT_CONFIG_COUNT with
// its keys and values, and the old GIT_CONFIG), change how git reads a
// pathspec, write a trace of what git does to its stderr or to a file
// (every GIT_TRACE* and GIT_CURL_VERBOSE), or turn its prompts on, and
// LC_ALL, which GitEnviron sets. The files git reads its config from, such
// as GIT_CONFIG_GLOBAL, stay, and so does what signs and authenticates,
// such as GIT_SSH_COMMAND.
func gitDropped(name string) bool {
	switch name {
	case "GIT_DIR", "GIT_WORK_TREE", "GIT_IMPLICIT_WORK_TREE", "GIT_INDEX_FILE", "GIT_COMMON_DIR",
		"GIT_OBJECT_DIRECTORY", "GIT_ALTERNATE_OBJECT_DIRECTORIES", "GIT_GRAFT_FILE", "GIT_SHALLOW_FILE",
		"GIT_REPLACE_REF_BASE", "GIT_NO_REPLACE_OBJECTS", "GIT_PREFIX", "GIT_NAMESPACE",
		"GIT_CONFIG", "GIT_CONFIG_PARAMETERS", "GIT_CONFIG_COUNT",
		"GIT_LITERAL_PATHSPECS", "GIT_GLOB_PATHSPECS", "GIT_NOGLOB_PATHSPECS", "GIT_ICASE_PATHSPECS",
		"GIT_CURL_VERBOSE", "GIT_TERMINAL_PROMPT", "LC_ALL":
		return true
	}
	return strings.HasPrefix(name, "GIT_CONFIG_KEY_") || strings.HasPrefix(name, "GIT_CONFIG_VALUE_") || strings.HasPrefix(name, "GIT_TRACE")
}

// GitEnviron is environ as every git eva-md runs gets it: without the
// variables gitDropped names, with prompts off and with git's messages in
// English (LC_ALL=C), which eva-md reads to tell why a command failed, as a
// git built with translations, such as Homebrew's, would otherwise word
// them in the locale's language. The wiki's runner uses it as it is; the
// read-only calls use GitEnv.
func GitEnviron(environ []string) []string {
	env := make([]string, 0, len(environ)+2)
	for _, kv := range environ {
		if name, _, _ := strings.Cut(kv, "="); !gitDropped(name) {
			env = append(env, kv)
		}
	}
	return append(env, "GIT_TERMINAL_PROMPT=0", "LC_ALL=C")
}

// GitEnv is the environment of eva-md's read-only git calls, the identity
// and code reads, the brief's live git and the update's commit subjects:
// GitEnviron's, with optional locks and lazy fetches off too, so a partial
// clone never fetches an object it lacks from its remote; git older than
// 2.44 ignores GIT_NO_LAZY_FETCH. coderef reads paths with
// --literal-pathspecs, which git refuses beside a GIT_GLOB_PATHSPECS or
// GIT_ICASE_PATHSPECS setting.
func GitEnv() []string { return gitEnv() }

func gitEnv() []string {
	base := GitEnviron(os.Environ())
	env := make([]string, 0, len(base)+2)
	for _, kv := range base {
		if name, _, _ := strings.Cut(kv, "="); name != "GIT_OPTIONAL_LOCKS" && name != "GIT_NO_LAZY_FETCH" {
			env = append(env, kv)
		}
	}
	return append(env, "GIT_OPTIONAL_LOCKS=0", "GIT_NO_LAZY_FETCH=1")
}
