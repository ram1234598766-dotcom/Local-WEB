package registry

import (
	"fmt"
	"strings"
	"sync"
	"time"
)

// MemoryRegistry is an in-memory implementation of Registry.
type MemoryRegistry struct {
	mu       sync.RWMutex
	packages map[string]*PackageMeta
	store    map[string]*LWPKG
}

// NewMemoryRegistry creates a new in-memory registry.
func NewMemoryRegistry() *MemoryRegistry {
	return &MemoryRegistry{
		packages: make(map[string]*PackageMeta),
		store:    make(map[string]*LWPKG),
	}
}

// Publish adds a package to the registry.
func (r *MemoryRegistry) Publish(pkg *LWPKG, authorPubKey [32]byte, privKey [32]byte) (string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if pkg == nil || pkg.Manifest == nil {
		return "", ErrManifestInvalid
	}
	if err := Validate(pkg.Manifest); err != nil {
		return "", fmt.Errorf("%w: %v", ErrManifestInvalid, err)
	}

	id := PackageID(pkg.Manifest.Name, pkg.Manifest.Version, pkg.Manifest.Author)
	if _, exists := r.packages[id]; exists {
		return "", ErrPackageExists
	}

	now := timeNow()
	meta := &PackageMeta{
		ID:          id,
		Name:        pkg.Manifest.Name,
		Version:     pkg.Manifest.Version,
		Description: pkg.Manifest.Description,
		Author:      pkg.Manifest.Author,
		Platform:    pkg.Manifest.Platform,
		Entry:       pkg.Manifest.Entry,
		Published:   now,
		Updated:     now,
		Downloads:   0,
		Verified:    pkg.Signature != nil && len(pkg.Signature) > 0,
		PublisherID: authorPubKey,
	}

	r.packages[id] = meta
	r.store[id] = pkg
	return id, nil
}

// Install retrieves a package by ID.
func (r *MemoryRegistry) Install(packageID string) (*LWPKG, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	pkg, ok := r.store[packageID]
	if !ok {
		return nil, ErrPackageNotFound
	}
	return pkg, nil
}

// List returns all registered packages.
func (r *MemoryRegistry) List() ([]PackageMeta, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]PackageMeta, 0, len(r.packages))
	for _, m := range r.packages {
		cp := *m
		out = append(out, cp)
	}
	return out, nil
}

// Search filters packages by the given query.
func (r *MemoryRegistry) Search(q SearchQuery) (*SearchResult, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	var filtered []PackageMeta
	for _, m := range r.packages {
		if matchesQueryFilter(m, q) {
			filtered = append(filtered, *m)
		}
	}

	total := len(filtered)
	limit := q.Limit
	if limit <= 0 {
		limit = DefaultSearchLimit
	}
	if limit > MaxSearchLimit {
		limit = MaxSearchLimit
	}
	start := q.Offset
	if start > total {
		start = total
	}
	end := start + limit
	if end > total {
		end = total
	}

	return &SearchResult{
		Packages: filtered[start:end],
		Total:    total,
		Limit:    limit,
		Offset:   start,
	}, nil
}

// Get returns a single package by ID.
func (r *MemoryRegistry) Get(packageID string) (*PackageMeta, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	m, ok := r.packages[packageID]
	if !ok {
		return nil, ErrPackageNotFound
	}
	cp := *m
	return &cp, nil
}

// Delete removes a package from the registry.
func (r *MemoryRegistry) Delete(packageID string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.packages[packageID]; !ok {
		return ErrPackageNotFound
	}
	delete(r.packages, packageID)
	delete(r.store, packageID)
	return nil
}

// matchesQueryFilter checks if a PackageMeta matches a SearchQuery.
func matchesQueryFilter(m *PackageMeta, q SearchQuery) bool {
	if q.Query != "" {
		qStr := stringsToLower(q.Query)
		if !containsFold(m.Name, qStr) &&
			!containsFold(m.Description, qStr) &&
			!containsFold(m.Author, qStr) {
			return false
		}
	}
	if q.Platform != "" && !isValidPlatform(m.Platform, q.Platform) {
		return false
	}
	if q.Author != "" && !stringsEqualFold(m.Author, q.Author) {
		return false
	}
	if q.Verified && !m.Verified {
		return false
	}
	return true
}

func stringsToLower(s string) string {
	return strings.ToLower(s)
}

func containsFold(s, substr string) bool {
	return strings.Contains(strings.ToLower(s), substr)
}

func stringsEqualFold(a, b string) bool {
	return strings.EqualFold(a, b)
}

func isValidPlatform(platforms []string, target string) bool {
	for _, p := range platforms {
		if strings.EqualFold(p, target) || p == "any" || p == "*" {
			return true
		}
	}
	return false
}

// timeNow returns the current UTC time. Overridable for testing.
var timeNow = func() time.Time { return time.Now().UTC() }

// Register registers a memory registry as both Registry and DHTDistributor.
func (r *MemoryRegistry) RegisterDistributor(d DHTDistributor) {
	_ = d
}
