package health

import (
	"cmp"
	"context"
	"fmt"
	"slices"
	"time"

	"github.com/heyNag/eva-md/internal/config"
	"github.com/heyNag/eva-md/internal/paths"
)

// A rowProducer adds the rows of one concern to the report of eva-md
// doctor and eva-md status: zero or more, in its own order. Each is
// registered, from the file that implements it, at a place; the report
// runs them in the order of their places, so a new row is a new producer
// at a place between two others, and no shared list is edited.
type rowProducer struct {
	place int
	name  string // what it checks, for a duplicate place's message
	add   func(r *Report, in *inspection)
}

// producers are the registered producers, in the order of their places.
var producers []rowProducer

// registerRows registers a producer at place. Two producers at one place
// would leave their order to chance, so the second panics, naming both;
// registering is done from init, so that fails every test of the package.
func registerRows(place int, name string, add func(r *Report, in *inspection)) {
	for _, p := range producers {
		if p.place == place {
			panic(fmt.Sprintf("health: row producers %q and %q both take place %d; give each its own", p.name, name, place))
		}
	}
	producers = append(producers, rowProducer{place, name, add})
	slices.SortFunc(producers, func(a, b rowProducer) int { return cmp.Compare(a.place, b.place) })
}

// inspection is what a producer reads: what was asked for, and what the
// producers before it loaded.
type inspection struct {
	ctx    context.Context
	d      paths.Dirs
	hosts  bool // ask the hosts' own commands, as eva-md doctor does
	repair bool // make the doctor's two repairs
	status bool // add eva-md status's own rows

	loaded    bool
	cfg       config.Config
	cfgErr    error
	at        time.Time
	statusRan bool
	statusCfg config.Config
	statusErr error

	// With the switch health on, the stages and losses the stage rows read,
	// for the verdict (see verdict.go).
	staged bool
	stages []Stage
	losses map[string]loss
}

// config is eva-md's config, loaded once, by the first producer that asks:
// Default() and the error when it is missing or invalid.
func (in *inspection) config() (config.Config, error) {
	if !in.loaded {
		in.cfg, in.cfgErr = config.LoadContext(in.ctx, in.d.ConfigFile())
		in.loaded = true
	}
	return in.cfg, in.cfgErr
}

// now is the time the rows are judged at, taken once, when a producer
// first asks.
func (in *inspection) now() time.Time {
	if in.at.IsZero() {
		in.at = time.Now()
	}
	return in.at
}

// statusWiki is the config eva-md status's own rows read, and whether they
// are asked for and it names a wiki. It is loaded once more, by the first
// of them, after the doctor's checks, so a config that can no longer be
// read by then, or a deadline that has passed, leaves them out.
func (in *inspection) statusWiki() (config.Config, bool) {
	if !in.status {
		return config.Config{}, false
	}
	if !in.statusRan {
		in.statusCfg, in.statusErr = config.LoadContext(in.ctx, in.d.ConfigFile())
		in.statusRan = true
	}
	return in.statusCfg, in.statusErr == nil && in.statusCfg.WikiPath != ""
}

// rows runs every producer in the order of their places.
func rows(in *inspection) Report {
	r := Report{}
	for _, p := range producers {
		p.add(&r, in)
	}
	return r
}
