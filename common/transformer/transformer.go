// Package transformer implements DataX/Addax compatible record transformation.
//
// A transformer receives one record, optionally mutates it in place and returns
// it. Returning nil (without error) marks the record as filtered, so it will
// neither reach later transformers nor the writer.
package transformer

import (
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/longkeyy/go-datax/common/config"
	"github.com/longkeyy/go-datax/common/element"
)

// Transformer transforms a single record.
type Transformer interface {
	// Name returns the configured transformer name, e.g. "dx_substr".
	Name() string
	// Evaluate applies the transformation. A nil result without error means
	// the record was filtered out.
	Evaluate(record element.Record) (element.Record, error)
}

// Factory builds a transformer from its column index and raw string parameters.
// Factories validate their parameters eagerly, so configuration mistakes fail
// when the job starts instead of turning every record into dirty data.
type Factory func(columnIndex int, paras []string) (Transformer, error)

// RecordFactory builds a record-level transformer (e.g. dx_script) that reads
// its own parameter block instead of the columnIndex/paras convention.
type RecordFactory func(params config.Configuration) (Transformer, error)

var (
	registryMu     sync.RWMutex
	registry       = make(map[string]Factory)
	recordRegistry = make(map[string]RecordFactory)
)

// Register adds a column-based transformer factory to the global registry. It
// panics on duplicate or empty registrations because those are programming
// errors.
func Register(name string, factory Factory) {
	if name == "" {
		panic("transformer: cannot register empty name")
	}
	if factory == nil {
		panic("transformer: nil factory for " + name)
	}

	registryMu.Lock()
	defer registryMu.Unlock()

	if _, exists := registry[name]; exists {
		panic("transformer: duplicate registration for " + name)
	}
	if _, exists := recordRegistry[name]; exists {
		panic("transformer: duplicate registration for " + name)
	}
	registry[name] = factory
}

// RegisterRecord adds a record-level transformer factory. Such transformers
// operate on the whole record and receive the raw parameter configuration.
func RegisterRecord(name string, factory RecordFactory) {
	if name == "" {
		panic("transformer: cannot register empty name")
	}
	if factory == nil {
		panic("transformer: nil factory for " + name)
	}

	registryMu.Lock()
	defer registryMu.Unlock()

	if _, exists := registry[name]; exists {
		panic("transformer: duplicate registration for " + name)
	}
	if _, exists := recordRegistry[name]; exists {
		panic("transformer: duplicate registration for " + name)
	}
	recordRegistry[name] = factory
}

// Names returns all registered transformer names in alphabetical order.
func Names() []string {
	registryMu.RLock()
	defer registryMu.RUnlock()

	names := make([]string, 0, len(registry)+len(recordRegistry))
	for name := range registry {
		names = append(names, name)
	}
	for name := range recordRegistry {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// New builds a transformer by name. Unsupported Addax transformers get a
// dedicated message so users know the difference between a typo and a feature
// that go-datax cannot provide.
func New(name string, columnIndex int, paras []string) (Transformer, error) {
	factory, err := lookup(name)
	if err != nil {
		return nil, err
	}
	return factory(columnIndex, paras)
}

func lookup(name string) (Factory, error) {
	registryMu.RLock()
	factory, exists := registry[name]
	registryMu.RUnlock()

	if exists {
		return factory, nil
	}

	switch name {
	case "dx_groovy":
		return nil, fmt.Errorf("transformer %q is not supported by go-datax: no Groovy runtime is available; use dx_script or the built-in transformers: %s",
			name, strings.Join(Names(), ", "))
	case "dx_map":
		return nil, fmt.Errorf("transformer %q is not supported by go-datax; supported transformers: %s",
			name, strings.Join(Names(), ", "))
	default:
		return nil, fmt.Errorf("unknown transformer %q; supported transformers: %s",
			name, strings.Join(Names(), ", "))
	}
}

// lookupRecord returns a record-level transformer factory if one is registered.
func lookupRecord(name string) (RecordFactory, bool) {
	registryMu.RLock()
	defer registryMu.RUnlock()

	factory, exists := recordRegistry[name]
	return factory, exists
}
