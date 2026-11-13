package adapter

import "slices"

// Plugin is how eva-md's plugin is rendered for a host and registered there
// through the host's own CLI: what install, setup and uninstall do
// differently for each host. Every field is set for both hosts in plugins; a
// host without a step has the field's zero value, said beside it.
type Plugin struct {
	// MarketplaceManifest is the marketplace's manifest, relative to the
	// marketplace's folder.
	MarketplaceManifest string
	// PluginManifest is the plugin's manifest, relative to the plugin's
	// folder.
	PluginManifest string
	// AddArgs register eva-md's plugin from eva-md's marketplace, and
	// RemoveArgs remove it.
	AddArgs, RemoveArgs []string
	// ValidateArgs, followed by a folder, check the marketplace's folder and
	// the plugin's each time install registers or keeps them; nil for a host
	// with no such check.
	ValidateArgs []string
	// ListPerMarketplace says the host's plugin list is asked one
	// marketplace at a time, with --marketplace=<name>: Codex's list of
	// every plugin also asks chatgpt.com for the account's remote ones.
	ListPerMarketplace bool
	// SourceKeys are the keys, outermost first, under which an entry of the
	// host's plugin list names the folder the host loads the plugin from.
	SourceKeys []string
	// FolderVersion, when not "", is the folderVersion an entry of the
	// host's plugin list must give for the folder to be eva-md's as rendered.
	FolderVersion string
	// SourceDiffers is install's reason when an entry's folder, or its
	// folder version, is not eva-md's.
	SourceDiffers string
	// MissingFolderFails says the host fails its lists while eva-md's
	// marketplace folder has no manifest, in the words
	// MarketplaceFolderMissing reads.
	MissingFolderFails bool
	// FeaturesArgs list the host's features, whose hooks rows HooksRows
	// reads; nil for a host with no such list.
	FeaturesArgs []string
	// NeedsReview says a changed plugin needs a review in the host before
	// its hooks run, since the host trusts the plugin's contents.
	NeedsReview bool
}

// plugins is each host's Plugin, by its id.
var plugins = map[ID]Plugin{
	ClaudeCode: {
		MarketplaceManifest: ".claude-plugin/marketplace.json",
		PluginManifest:      ".claude-plugin/plugin.json",
		AddArgs:             []string{"plugin", "install", "eva-md@eva-md", "--scope", "user", "--json"},
		RemoveArgs:          []string{"plugin", "uninstall", "eva-md@eva-md", "--scope", "user", "--json"},
		ValidateArgs:        []string{"plugin", "validate", "--strict", "--json"},
		ListPerMarketplace:  false,
		SourceKeys:          []string{"readFromFolder"},
		FolderVersion:       "1",
		SourceDiffers:       "Claude source folder or folder version differs",
		MissingFolderFails:  false,
		FeaturesArgs:        nil,
		NeedsReview:         false,
	},
	Codex: {
		MarketplaceManifest: ".agents/plugins/marketplace.json",
		PluginManifest:      ".codex-plugin/plugin.json",
		AddArgs:             []string{"plugin", "add", "eva-md@eva-md", "--json"},
		RemoveArgs:          []string{"plugin", "remove", "eva-md@eva-md", "--json"},
		ValidateArgs:        nil,
		ListPerMarketplace:  true,
		SourceKeys:          []string{"source", "path"},
		FolderVersion:       "",
		SourceDiffers:       "Codex source folder differs",
		MissingFolderFails:  true,
		FeaturesArgs:        []string{"features", "list"},
		NeedsReview:         true,
	},
}

// Plugin gives how eva-md's plugin is rendered and registered on the host;
// the zero Plugin for a host eva-md doesn't know. It is a copy: a change to
// it leaves the table alone.
func (h Host) Plugin() Plugin {
	p := plugins[h.ID]
	p.AddArgs = slices.Clone(p.AddArgs)
	p.RemoveArgs = slices.Clone(p.RemoveArgs)
	p.ValidateArgs = slices.Clone(p.ValidateArgs)
	p.SourceKeys = slices.Clone(p.SourceKeys)
	p.FeaturesArgs = slices.Clone(p.FeaturesArgs)
	return p
}

// LoadsFrom says whether an entry of the host's plugin list loads the plugin
// from folder, as eva-md renders it there: the folder under SourceKeys is
// folder exactly, and when FolderVersion is set, folderVersion is it.
func (p Plugin) LoadsFrom(entry map[string]any, folder string) bool {
	if len(p.SourceKeys) == 0 {
		return false
	}
	obj := entry
	for _, key := range p.SourceKeys[:len(p.SourceKeys)-1] {
		obj, _ = obj[key].(map[string]any)
	}
	if str(obj, p.SourceKeys[len(p.SourceKeys)-1]) != folder {
		return false
	}
	return p.FolderVersion == "" || str(entry, "folderVersion") == p.FolderVersion
}

// str is the first of keys whose value in obj is a string other than "", or
// "". A nil obj has none.
func str(obj map[string]any, keys ...string) string {
	for _, key := range keys {
		if s, ok := obj[key].(string); ok && s != "" {
			return s
		}
	}
	return ""
}
