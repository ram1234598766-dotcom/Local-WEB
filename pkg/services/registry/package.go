package registry

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"fmt"
	"io"
)

// LWPKG represents a LocalWEB package file (.lwpkg).
type LWPKG struct {
	Manifest  *Manifest
	Files     map[string][]byte // filename -> content
	Signature []byte
}

const (
	// ManifestPath is the path of the manifest inside the tar archive.
	ManifestPath = "manifest.yaml"
	// FilesDir is the directory containing app files inside the tar archive.
	FilesDir = "files/"
	// SignatureSuffix is appended to the archive to form the .lwpkg file.
	SignatureSuffix = ".sig"
)

// EncodeLWPKG creates a .lwpkg binary: tar.gz(manifest.yaml + files/) || Ed25519 signature.
func EncodeLWPKG(pkg *LWPKG) ([]byte, error) {
	if pkg.Manifest == nil {
		return nil, fmt.Errorf("manifest is required")
	}

	var tarBuf bytes.Buffer
	gw := gzip.NewWriter(&tarBuf)
	tw := tar.NewWriter(gw)

	manifestData, err := marshalManifest(pkg.Manifest)
	if err != nil {
		return nil, fmt.Errorf("marshal manifest: %w", err)
	}

	if err := writeTarFile(tw, ManifestPath, manifestData); err != nil {
		return nil, fmt.Errorf("write manifest: %w", err)
	}

	for name, data := range pkg.Files {
		path := FilesDir + name
		if err := writeTarFile(tw, path, data); err != nil {
			return nil, fmt.Errorf("write file %s: %w", name, err)
		}
	}

	if err := tw.Close(); err != nil {
		return nil, fmt.Errorf("close tar: %w", err)
	}
	if err := gw.Close(); err != nil {
		return nil, fmt.Errorf("close gzip: %w", err)
	}

	archive := tarBuf.Bytes()
	out := make([]byte, len(archive)+len(pkg.Signature))
	copy(out, archive)
	copy(out[len(archive):], pkg.Signature)

	return out, nil
}

// DecodeLWPKG parses a .lwpkg binary into its components.
// The signature is returned separately; verification is the caller's responsibility.
func DecodeLWPKG(data []byte) (*LWPKG, []byte, error) {
	if len(data) < Ed25519SignatureSize {
		return nil, nil, fmt.Errorf("data too short for .lwpkg format")
	}

	sigStart := len(data) - Ed25519SignatureSize
	archive := data[:sigStart]
	signature := data[sigStart:]

	gz, err := gzip.NewReader(bytes.NewReader(archive))
	if err != nil {
		return nil, nil, fmt.Errorf("gzip reader: %v (archive_len=%d)", err, len(archive))
	}
	defer gz.Close()

	tr := tar.NewReader(gz)
	pkg := &LWPKG{
		Files:     make(map[string][]byte),
		Signature: signature,
	}

	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, nil, fmt.Errorf("tar read: %w", err)
		}

		if hdr.Typeflag == tar.TypeDir {
			continue
		}

		data, err := io.ReadAll(tr)
		if err != nil {
			return nil, nil, fmt.Errorf("read tar entry %s: %w", hdr.Name, err)
		}

		switch hdr.Name {
		case ManifestPath:
			manifest, err := unmarshalManifest(data)
			if err != nil {
				return nil, nil, fmt.Errorf("manifest parse: %w", err)
			}
			pkg.Manifest = manifest
		default:
			if len(hdr.Name) > len(FilesDir) && hdr.Name[:len(FilesDir)] == FilesDir {
				fname := hdr.Name[len(FilesDir):]
				if fname != "" && fname != "/" {
					pkg.Files[fname] = data
				}
			}
		}
	}

	if pkg.Manifest == nil {
		return nil, nil, fmt.Errorf("manifest.yaml missing from archive")
	}

	return pkg, signature, nil
}

// ComputeArchiveHash returns SHA3-256 of the archive portion (without signature).
func ComputeArchiveHash(data []byte) ([32]byte, error) {
	if len(data) < Ed25519SignatureSize {
		return [32]byte{}, fmt.Errorf("data too short")
	}
	archive := data[:len(data)-Ed25519SignatureSize]
	h := sha256.New()
	if _, err := h.Write(archive); err != nil {
		return [32]byte{}, err
	}
	var out [32]byte
	copy(out[:], h.Sum(nil))
	return out, nil
}

// VerifyIntegrity checks that all manifest checksums match the file contents.
func VerifyIntegrity(pkg *LWPKG) error {
	if pkg.Manifest == nil {
		return fmt.Errorf("no manifest")
	}
	if pkg.Manifest.Checksums == nil {
		return fmt.Errorf("no checksums in manifest")
	}
	for name, expectedHex := range pkg.Manifest.Checksums {
		data, ok := pkg.Files[name]
		if !ok {
			return fmt.Errorf("missing file for checksum: %s", name)
		}
		h := sha256.New()
		if _, err := h.Write(data); err != nil {
			return err
		}
		actualHex := fmt.Sprintf("%x", h.Sum(nil))
		if actualHex != expectedHex {
			return fmt.Errorf("checksum mismatch for %s", name)
		}
	}
	return nil
}

// writeTarFile adds a single file entry to a tar writer.
func writeTarFile(tw *tar.Writer, name string, data []byte) error {
	hdr := &tar.Header{
		Name:     name,
		Size:     int64(len(data)),
		Mode:     0644,
		Typeflag: tar.TypeReg,
	}
	if err := tw.WriteHeader(hdr); err != nil {
		return err
	}
	_, err := tw.Write(data)
	return err
}
