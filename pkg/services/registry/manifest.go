package registry

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"strings"
	"time"

	"go.yaml.in/yaml/v3"
)

// ValidationError describes a manifest validation failure.
type ValidationError struct {
	Field   string
	Message string
}

func (e *ValidationError) Error() string {
	return fmt.Sprintf("manifest field %q: %s", e.Field, e.Message)
}

// marshalManifest serializes a Manifest to YAML bytes.
func marshalManifest(m *Manifest) ([]byte, error) {
	return yaml.Marshal(m)
}

// unmarshalManifest deserializes YAML bytes into a Manifest.
func unmarshalManifest(data []byte) (*Manifest, error) {
	var m Manifest
	if err := yaml.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("yaml unmarshal: %w", err)
	}
	return &m, nil
}

// Validate checks that all required manifest fields are present and well-formed.
func Validate(m *Manifest) error {
	if m == nil {
		return &ValidationError{Field: "", Message: "manifest is nil"}
	}
	if strings.TrimSpace(m.Name) == "" {
		return &ValidationError{Field: "name", Message: "must not be empty"}
	}
	if strings.TrimSpace(m.Version) == "" {
		return &ValidationError{Field: "version", Message: "must not be empty"}
	}
	if strings.TrimSpace(m.Entry) == "" {
		return &ValidationError{Field: "entry", Message: "must not be empty"}
	}
	if strings.TrimSpace(m.Author) == "" {
		return &ValidationError{Field: "author", Message: "must not be empty"}
	}
	if m.Size < 0 {
		return &ValidationError{Field: "size", Message: "must be non-negative"}
	}
	if m.Checksums == nil {
		return &ValidationError{Field: "checksums", Message: "must not be nil"}
	}
	return nil
}

// IsValid returns true if the manifest passes validation.
func IsValid(m *Manifest) bool {
	return Validate(m) == nil
}

// ValidationErrors collects multiple validation errors.
type ValidationErrors []ValidationError

func (v ValidationErrors) Error() string {
	var sb strings.Builder
	for i, e := range v {
		if i > 0 {
			sb.WriteString("; ")
		}
		sb.WriteString(e.Error())
	}
	return sb.String()
}

// ValidateDetailed returns all validation errors found.
func ValidateDetailed(m *Manifest) ValidationErrors {
	if m == nil {
		return ValidationErrors{{Field: "", Message: "manifest is nil"}}
	}
	var errs ValidationErrors
	if strings.TrimSpace(m.Name) == "" {
		errs = append(errs, ValidationError{Field: "name", Message: "must not be empty"})
	}
	if strings.TrimSpace(m.Version) == "" {
		errs = append(errs, ValidationError{Field: "version", Message: "must not be empty"})
	}
	if strings.TrimSpace(m.Entry) == "" {
		errs = append(errs, ValidationError{Field: "entry", Message: "must not be empty"})
	}
	if strings.TrimSpace(m.Author) == "" {
		errs = append(errs, ValidationError{Field: "author", Message: "must not be empty"})
	}
	if m.Size < 0 {
		errs = append(errs, ValidationError{Field: "size", Message: "must be non-negative"})
	}
	if m.Checksums == nil {
		errs = append(errs, ValidationError{Field: "checksums", Message: "must not be nil"})
	}
	if len(errs) == 0 {
		return nil
	}
	return errs
}

// SetDefaults fills in zero-value fields with sensible defaults.
func SetDefaults(m *Manifest) {
	if m.Checksums == nil {
		m.Checksums = make(map[string]string)
	}
	if m.Platform == nil {
		m.Platform = []string{}
	}
	if m.Dependencies == nil {
		m.Dependencies = []string{}
	}
	if m.Created.IsZero() {
		m.Created = time.Now().UTC()
	}
	if m.License == "" {
		m.License = "MIT"
	}
}

// PackageID generates a deterministic package ID from name + version + author.
func PackageID(name, version, author string) string {
	h := sha256Hash([]byte(name + "|" + version + "|" + author))
	return fmt.Sprintf("%x", h[:16])
}

// sha256Hash is a local helper to avoid importing crypto/sha256 in the public API.
func sha256Hash(data []byte) [32]byte {
	var out [32]byte
	h := sha256.New()
	if _, err := h.Write(data); err == nil {
		h.Sum(out[:0])
	}
	return out
}

// IsValidPlatform returns true if the platform string is in the list.
func IsValidPlatform(platforms []string, target string) bool {
	if target == "" {
		return true
	}
	for _, p := range platforms {
		if p == target || p == "any" || p == "*" {
			return true
		}
	}
	return false
}

// MarshalManifest is a convenience wrapper for external callers.
func MarshalManifest(m *Manifest) ([]byte, error) {
	return marshalManifest(m)
}

// UnmarshalManifest is a convenience wrapper for external callers.
func UnmarshalManifest(data []byte) (*Manifest, error) {
	return unmarshalManifest(data)
}

// ErrMissingField is a sentinel for missing required manifest fields.
var ErrMissingField = errors.New("missing required field")
