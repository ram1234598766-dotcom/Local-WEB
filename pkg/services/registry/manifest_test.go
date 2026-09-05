package registry

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestValidateNilManifest(t *testing.T) {
	err := Validate(nil)
	require.Error(t, err)
	require.Contains(t, err.Error(), "nil")
}

func TestValidateEmptyFields(t *testing.T) {
	m := &Manifest{
		Name:      "",
		Version:   "1.0.0",
		Entry:     "main.go",
		Author:    "test",
		Size:      0,
		Checksums: map[string]string{},
	}
	err := Validate(m)
	require.Error(t, err)
	require.Contains(t, err.Error(), "name")
}

func TestValidateMissingVersion(t *testing.T) {
	m := validManifest()
	m.Version = ""
	err := Validate(m)
	require.Error(t, err)
	require.Contains(t, err.Error(), "version")
}

func TestValidateMissingEntry(t *testing.T) {
	m := validManifest()
	m.Entry = ""
	err := Validate(m)
	require.Error(t, err)
	require.Contains(t, err.Error(), "entry")
}

func TestValidateMissingAuthor(t *testing.T) {
	m := validManifest()
	m.Author = ""
	err := Validate(m)
	require.Error(t, err)
	require.Contains(t, err.Error(), "author")
}

func TestValidateNegativeSize(t *testing.T) {
	m := validManifest()
	m.Size = -1
	err := Validate(m)
	require.Error(t, err)
	require.Contains(t, err.Error(), "size")
}

func TestValidateNilChecksums(t *testing.T) {
	m := validManifest()
	m.Checksums = nil
	err := Validate(m)
	require.Error(t, err)
	require.Contains(t, err.Error(), "checksums")
}

func TestValidateValidManifest(t *testing.T) {
	m := validManifest()
	require.NoError(t, Validate(m))
	require.True(t, IsValid(m))
}

func TestIsValid(t *testing.T) {
	require.False(t, IsValid(nil))
	require.False(t, IsValid(&Manifest{}))
	require.True(t, IsValid(validManifest()))
}

func TestValidateDetailed(t *testing.T) {
	m := validManifest()
	m.Name = ""
	m.Version = ""
	errs := ValidateDetailed(m)
	require.NotNil(t, errs)
	require.Len(t, errs, 2)
	require.Contains(t, errs.Error(), "name")
	require.Contains(t, errs.Error(), "version")
}

func TestValidateDetailedNil(t *testing.T) {
	errs := ValidateDetailed(nil)
	require.NotNil(t, errs)
}

func TestSetDefaults(t *testing.T) {
	m := &Manifest{
		Name:    "test",
		Version: "1.0",
		Entry:   "main",
		Author:  "a",
	}
	SetDefaults(m)
	require.NotNil(t, m.Checksums)
	require.NotNil(t, m.Platform)
	require.NotNil(t, m.Dependencies)
	require.False(t, m.Created.IsZero())
	require.Equal(t, "MIT", m.License)
}

func TestSetDefaultsPreservesValues(t *testing.T) {
	m := validManifest()
	m.Checksums = map[string]string{"x": "y"}
	m.Platform = []string{"linux"}
	m.License = "Apache-2.0"
	SetDefaults(m)
	require.Equal(t, map[string]string{"x": "y"}, m.Checksums)
	require.Equal(t, []string{"linux"}, m.Platform)
	require.Equal(t, "Apache-2.0", m.License)
}

func TestPackageID(t *testing.T) {
	id1 := PackageID("myapp", "1.0.0", "author1")
	id2 := PackageID("myapp", "1.0.0", "author1")
	id3 := PackageID("myapp", "2.0.0", "author1")
	require.Equal(t, id1, id2)
	require.NotEqual(t, id1, id3)
	require.Len(t, id1, 32)
}

func TestPackageIDDeterministic(t *testing.T) {
	ids := make(map[string]bool)
	for i := 0; i < 100; i++ {
		id := PackageID("app", "1.0", "author")
		ids[id] = true
	}
	require.Len(t, ids, 1, "package ID should be deterministic")
}

func TestIsValidPlatform(t *testing.T) {
	require.True(t, IsValidPlatform(nil, ""))
	require.True(t, IsValidPlatform([]string{"linux"}, ""))
	require.True(t, IsValidPlatform([]string{"linux"}, "linux"))
	require.True(t, IsValidPlatform([]string{"any"}, "windows"))
	require.False(t, IsValidPlatform([]string{"linux"}, "windows"))
}

func TestMarshalUnmarshalManifest(t *testing.T) {
	now := time.Now().UTC()
	m := &Manifest{
		Name:         "roundtrip-app",
		Version:      "3.0.0",
		Description:  "Test roundtrip",
		Author:       "tester",
		Homepage:     "https://example.com",
		License:      "BSD-3",
		Entry:        "start.sh",
		Platform:     []string{"linux", "darwin"},
		Dependencies: []string{"dep1"},
		Checksums:    map[string]string{"a": "b"},
		Size:         2048,
		Created:      now,
	}

	data, err := MarshalManifest(m)
	require.NoError(t, err)

	got, err := UnmarshalManifest(data)
	require.NoError(t, err)
	require.Equal(t, m.Name, got.Name)
	require.Equal(t, m.Version, got.Version)
	require.Equal(t, m.Description, got.Description)
	require.Equal(t, m.Author, got.Author)
	require.Equal(t, m.Homepage, got.Homepage)
	require.Equal(t, m.License, got.License)
	require.Equal(t, m.Entry, got.Entry)
	require.Equal(t, m.Platform, got.Platform)
	require.Equal(t, m.Dependencies, got.Dependencies)
	require.Equal(t, m.Size, got.Size)
	require.Equal(t, m.Checksums, got.Checksums)
	require.Equal(t, m.Created.UnixNano(), got.Created.UnixNano())
}

func TestUnmarshalInvalidYAML(t *testing.T) {
	_, err := UnmarshalManifest([]byte("{{invalid"))
	require.Error(t, err)
}

func TestValidationError(t *testing.T) {
	e := &ValidationError{Field: "name", Message: "empty"}
	require.Contains(t, e.Error(), "name")
	require.Contains(t, e.Error(), "empty")
}

func TestValidationErrors(t *testing.T) {
	errs := ValidationErrors{
		{Field: "name", Message: "empty"},
		{Field: "version", Message: "missing"},
	}
	require.Contains(t, errs.Error(), "name")
	require.Contains(t, errs.Error(), "version")
}

func validManifest() *Manifest {
	return &Manifest{
		Name:      "test-app",
		Version:   "1.0.0",
		Entry:     "main.go",
		Author:    "test-author",
		Size:      1024,
		Checksums: map[string]string{"main.go": "abc123"},
		Created:   timeNow(),
	}
}
