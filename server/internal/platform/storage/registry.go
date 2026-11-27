package storage

import (
	"errors"
	"fmt"
	"io"
	"maps"
	"strings"
)

// ErrDriverNotConfigured is returned when a persisted file references a storage driver that is not registered.
var ErrDriverNotConfigured = errors.New("storage driver not configured")

// Registry manages one or more Storage drivers so writes use the active default driver
// while reads and deletes always route by the file record's persisted storage_driver.
type Registry struct {
	defaultDriver string
	drivers       map[string]Storage
}

// NewRegistry creates a Registry with the specified defaultDriver and registered drivers.
func NewRegistry(defaultDriver string, drivers ...Storage) (*Registry, error) {
	normalizedDefault := strings.ToLower(strings.TrimSpace(defaultDriver))
	if normalizedDefault == "" {
		normalizedDefault = DriverLocal
	}

	reg := make(map[string]Storage, len(drivers))
	for _, d := range drivers {
		if d == nil {
			continue
		}
		name := strings.ToLower(strings.TrimSpace(d.Driver()))
		if name == "" {
			return nil, errors.New("storage driver name must not be empty")
		}
		reg[name] = d
	}

	if _, ok := reg[normalizedDefault]; !ok {
		return nil, fmt.Errorf("default storage driver %q is not registered", normalizedDefault)
	}

	return &Registry{
		defaultDriver: normalizedDefault,
		drivers:       reg,
	}, nil
}

// Default returns the active Storage driver used for new uploads.
func (r *Registry) Default() Storage {
	return r.drivers[r.defaultDriver]
}

// DefaultDriverName returns the canonical driver name used for new uploads.
func (r *Registry) DefaultDriverName() string {
	return r.defaultDriver
}

// Driver resolves a registered Storage backend by name (case-insensitive).
func (r *Registry) Driver(name string) (Storage, error) {
	normalized := strings.ToLower(strings.TrimSpace(name))
	d, ok := r.drivers[normalized]
	if !ok {
		return nil, fmt.Errorf("%w: %s", ErrDriverNotConfigured, name)
	}
	return d, nil
}

// Drivers returns a defensive copy of registered drivers keyed by driver name.
func (r *Registry) Drivers() map[string]Storage {
	return maps.Clone(r.drivers)
}

// Close releases resources held by any registered drivers implementing io.Closer.
func (r *Registry) Close() error {
	if r == nil {
		return nil
	}
	var errs []error
	for name, d := range r.drivers {
		if closer, ok := d.(io.Closer); ok {
			if err := closer.Close(); err != nil {
				errs = append(errs, fmt.Errorf("close storage driver %s: %w", name, err))
			}
		}
	}
	return errors.Join(errs...)
}
