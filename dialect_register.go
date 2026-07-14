package pop

import (
	"fmt"
	"strings"
)

// DialectRegistration describes a custom database dialect to be registered with
// RegisterDialect. Once registered, the dialect works transparently with
// DialectSupported, CanonicalDialect, NewConnection, and driver loading.
type DialectRegistration struct {
	// Name is the canonical dialect name (e.g. "postgres"). Required. It is
	// lowercased on registration to match the canonicalization performed by
	// CanonicalDialect.
	Name string

	// NewConnection creates a Dialect from the given connection details.
	// Required.
	NewConnection func(*ConnectionDetails) (Dialect, error)

	// Synonyms are alternative names that canonicalize to Name (e.g. "pg" for
	// "postgres"). Optional.
	Synonyms []string

	// URLParser, if set, parses a connection URL into ConnectionDetails for this
	// dialect. Optional; a generic parser is used when nil.
	URLParser func(*ConnectionDetails) error

	// Finalizer, if set, normalizes ConnectionDetails after parsing. Optional.
	Finalizer func(*ConnectionDetails)
}

// RegisterDialect registers a custom database dialect. It is safe to call
// concurrently with connection creation, but must be called before a connection
// for the dialect is created. To avoid racing with unsynchronized readers of
// the exported AvailableDialects slice, register during package init or program
// startup, before any goroutines that use pop are spawned, or read the list via
// GetAvailableDialects. It returns an error if the registration is invalid or
// collides with an already-registered dialect name or synonym.
func RegisterDialect(reg DialectRegistration) error {
	name := strings.ToLower(strings.TrimSpace(reg.Name))
	if name == "" {
		return fmt.Errorf("pop: dialect name must not be empty")
	}
	if reg.NewConnection == nil {
		return fmt.Errorf("pop: dialect %q must provide a NewConnection function", name)
	}

	dialectsMu.Lock()
	defer dialectsMu.Unlock()

	if _, ok := newConnection[name]; ok {
		return fmt.Errorf("pop: dialect %q is already registered", name)
	}
	if _, ok := dialectSynonyms[name]; ok {
		return fmt.Errorf("pop: dialect %q collides with an existing synonym", name)
	}

	synonyms := make([]string, 0, len(reg.Synonyms))
	for _, s := range reg.Synonyms {
		syn := strings.ToLower(strings.TrimSpace(s))
		if syn == "" {
			return fmt.Errorf("pop: dialect %q has an empty synonym", name)
		}
		if _, ok := newConnection[syn]; ok {
			return fmt.Errorf("pop: synonym %q collides with an existing dialect", syn)
		}
		if _, ok := dialectSynonyms[syn]; ok {
			return fmt.Errorf("pop: synonym %q is already registered", syn)
		}
		synonyms = append(synonyms, syn)
	}

	AvailableDialects = append(AvailableDialects, name)
	newConnection[name] = reg.NewConnection
	for _, syn := range synonyms {
		dialectSynonyms[syn] = name
	}
	if reg.URLParser != nil {
		urlParser[name] = reg.URLParser
	}
	if reg.Finalizer != nil {
		finalizer[name] = reg.Finalizer
	}

	return nil
}
