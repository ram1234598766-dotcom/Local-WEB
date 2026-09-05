package registry

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"fmt"
	"testing"

	"github.com/mrityunjay/LocalWEB/pkg/crypto"
	"github.com/stretchr/testify/require"
)

func TestEncodeDecodeLWPKG(t *testing.T) {
	priv, _, err := crypto.GenerateKeyPair()
	require.NoError(t, err)

	manifest := &Manifest{
		Name:        "test-app",
		Version:     "1.0.0",
		Description: "A test application",
		Author:      "test-author",
		Entry:       "main.go",
		Size:        1024,
		Checksums: map[string]string{
			"main.go": sha256hex([]byte("package main")),
		},
		Created: timeNow(),
	}
	SetDefaults(manifest)

	pkg := &LWPKG{
		Manifest: manifest,
		Files: map[string][]byte{
			"main.go": []byte("package main"),
		},
	}

	sig, err := crypto.Sign(priv, []byte("test-signature-data"))
	require.NoError(t, err)
	pkg.Signature = sig

	data, err := EncodeLWPKG(pkg)
	require.NoError(t, err)
	require.True(t, len(data) > 100)

	decoded, sigOut, err := DecodeLWPKG(data)
	require.NoError(t, err)
	require.Equal(t, sig, sigOut)
	require.Equal(t, "test-app", decoded.Manifest.Name)
	require.Equal(t, "1.0.0", decoded.Manifest.Version)
	require.Equal(t, "main.go", decoded.Manifest.Entry)
	require.Equal(t, []byte("package main"), decoded.Files["main.go"])
}

func TestDecodeLWPKGTooShort(t *testing.T) {
	_, _, err := DecodeLWPKG([]byte("short"))
	require.Error(t, err)
	require.Contains(t, err.Error(), "too short")
}

func TestDecodeLWPKGNoManifest(t *testing.T) {
	var tarBuf bytes.Buffer
	gw := gzip.NewWriter(&tarBuf)
	tw := tar.NewWriter(gw)
	_ = writeTarFile(tw, "files/readme.txt", []byte("hello"))
	_ = tw.Close()
	_ = gw.Close()

	sig := make([]byte, Ed25519SignatureSize)
	data := append(tarBuf.Bytes(), sig...)

	_, _, err := DecodeLWPKG(data)
	require.Error(t, err)
	require.Contains(t, err.Error(), "manifest")
}

func TestEncodeLWPKGNoManifest(t *testing.T) {
	_, err := EncodeLWPKG(&LWPKG{})
	require.Error(t, err)
	require.Contains(t, err.Error(), "manifest is required")
}

func TestVerifyIntegrity(t *testing.T) {
	content := []byte("hello world")
	checksum := sha256hex(content)

	pkg := &LWPKG{
		Manifest: &Manifest{
			Name:      "app",
			Version:   "1.0.0",
			Entry:     "file.txt",
			Author:    "a",
			Size:      int64(len(content)),
			Checksums: map[string]string{"file.txt": checksum},
			Created:   timeNow(),
		},
		Files: map[string][]byte{"file.txt": content},
	}
	require.NoError(t, VerifyIntegrity(pkg))

	pkg2 := &LWPKG{
		Manifest: &Manifest{
			Name:      "app",
			Version:   "1.0.0",
			Entry:     "file.txt",
			Author:    "a",
			Size:      int64(len(content)),
			Checksums: map[string]string{"file.txt": "badchecksum"},
			Created:   timeNow(),
		},
		Files: map[string][]byte{"file.txt": content},
	}
	require.Error(t, VerifyIntegrity(pkg2))

	pkg3 := &LWPKG{Manifest: nil}
	require.Error(t, VerifyIntegrity(pkg3))

	pkg4 := &LWPKG{
		Manifest: &Manifest{
			Name:      "app",
			Version:   "1.0.0",
			Entry:     "file.txt",
			Author:    "a",
			Size:      0,
			Checksums: nil,
			Created:   timeNow(),
		},
		Files: map[string][]byte{"file.txt": content},
	}
	require.Error(t, VerifyIntegrity(pkg4))
}

func TestComputeArchiveHash(t *testing.T) {
	priv, _, err := crypto.GenerateKeyPair()
	require.NoError(t, err)

	pkg := &LWPKG{
		Manifest: &Manifest{
			Name:      "app",
			Version:   "1.0.0",
			Entry:     "main",
			Author:    "a",
			Size:      0,
			Checksums: make(map[string]string),
			Created:   timeNow(),
		},
		Signature: make([]byte, Ed25519SignatureSize),
	}
	sig, _ := crypto.Sign(priv, []byte("payload"))
	pkg.Signature = sig

	data, err := EncodeLWPKG(pkg)
	require.NoError(t, err)

	h1, err := ComputeArchiveHash(data)
	require.NoError(t, err)
	require.NotEqual(t, [32]byte{}, h1)

	h2, err := ComputeArchiveHash(data)
	require.NoError(t, err)
	require.Equal(t, h1, h2)

	_, err = ComputeArchiveHash([]byte("too short"))
	require.Error(t, err)
}

func TestLWPKGRoundTripMultipleFiles(t *testing.T) {
	priv, _, err := crypto.GenerateKeyPair()
	require.NoError(t, err)

	files := map[string][]byte{
		"main.go":     []byte("package main\n"),
		"README.md":   []byte("# Test App\n"),
		"config.yaml": []byte("port: 8080\n"),
	}
	checksums := make(map[string]string)
	for name, data := range files {
		checksums[name] = sha256hex(data)
	}

	pkg := &LWPKG{
		Manifest: &Manifest{
			Name:         "multi-file-app",
			Version:      "2.0.0",
			Description:  "Multi-file test",
			Author:       "author",
			Entry:        "main.go",
			Platform:     []string{"linux", "darwin"},
			Dependencies: []string{"libc"},
			Size:         300,
			Checksums:    checksums,
			Created:      timeNow(),
		},
		Files: files,
	}
	SetDefaults(pkg.Manifest)

	sig, _ := crypto.Sign(priv, []byte("roundtrip"))
	pkg.Signature = sig

	data, err := EncodeLWPKG(pkg)
	require.NoError(t, err)

	decoded, _, err := DecodeLWPKG(data)
	require.NoError(t, err)
	require.Equal(t, "multi-file-app", decoded.Manifest.Name)
	require.Equal(t, "2.0.0", decoded.Manifest.Version)
	require.Len(t, decoded.Manifest.Platform, 2)
	require.Len(t, decoded.Manifest.Dependencies, 1)
	require.Len(t, decoded.Files, 3)
	require.Equal(t, []byte("package main\n"), decoded.Files["main.go"])
	require.Equal(t, []byte("# Test App\n"), decoded.Files["README.md"])
	require.Equal(t, []byte("port: 8080\n"), decoded.Files["config.yaml"])
}

func sha256hex(data []byte) string {
	return fmt.Sprintf("%x", sha256.Sum256(data))
}
