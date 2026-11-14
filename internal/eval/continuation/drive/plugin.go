package drive

import (
	"errors"

	"github.com/heyNag/eva-md/internal/host/adapter"
)

// PluginReady reports whether a chat of the host h could load eva-md's
// plugin as setup installed it in home: the host table's Plugin entry,
// run on home, which reads the installed plugin and writes nothing. A
// condition that sets eva-md up calls it after setup, so that a plugin the
// chat could not load is a setup that failed, not a chat with no memory.
func PluginReady(home string, h adapter.Host) error {
	e, ok := hosts[h.ID]
	if !ok || e.Plugin == nil {
		return errors.New("the host has no plugin entry in the host table")
	}
	_, err := e.Plugin(home, h)
	return err
}
