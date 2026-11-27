package extensions

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"strings"
)

// maxArtifactSize bounds downloads and extracted executables.
const maxArtifactSize = 1 << 30

// fetch downloads the platform's artifact to artifact, verifies its checksum
// and writes the executable into dir. It returns the executable's file name.
func (s *Store) fetch(ctx context.Context, m *Manifest, p Platform, artifact, dir string) (string, error) {
	if err := s.download(ctx, p.URL, p.SHA256, artifact); err != nil {
		return "", err
	}

	name := m.Name
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	if p.Bin != "" {
		name = path.Base(p.Bin)
	}
	exe := filepath.Join(dir, name)

	var err error
	switch archiveFormat(p.URL) {
	case ".tar.gz":
		err = extractTarGz(artifact, p.Bin, exe)
	case ".zip":
		err = extractZip(artifact, p.Bin, exe)
	default:
		err = os.Rename(artifact, exe)
	}
	if err != nil {
		return "", err
	}
	return name, os.Chmod(exe, 0o755)
}

// archiveFormat returns ".tar.gz" or ".zip" for an archive URL, else "".
func archiveFormat(url string) string {
	switch u := strings.ToLower(url); {
	case strings.HasSuffix(u, ".tar.gz"), strings.HasSuffix(u, ".tgz"):
		return ".tar.gz"
	case strings.HasSuffix(u, ".zip"):
		return ".zip"
	}
	return ""
}

func (s *Store) download(ctx context.Context, url, sum, dst string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	resp, err := s.HTTPClient.Do(req)
	if err != nil {
		return fmt.Errorf("download %s: %w", url, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download %s: %s", url, resp.Status)
	}

	f, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(io.MultiWriter(f, h), io.LimitReader(resp.Body, maxArtifactSize)); err != nil {
		return fmt.Errorf("download %s: %w", url, err)
	}
	if got := hex.EncodeToString(h.Sum(nil)); !strings.EqualFold(got, sum) {
		return fmt.Errorf("checksum mismatch for %s: manifest has %s, downloaded %s", url, sum, got)
	}
	return f.Close()
}

func extractTarGz(archive, bin, dst string) error {
	f, err := os.Open(archive)
	if err != nil {
		return err
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return err
	}
	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return fmt.Errorf("%s not found in archive", bin)
		}
		if err != nil {
			return err
		}
		if hdr.Typeflag == tar.TypeReg && path.Clean(hdr.Name) == path.Clean(bin) {
			return writeFile(dst, tr)
		}
	}
}

func extractZip(archive, bin, dst string) error {
	zr, err := zip.OpenReader(archive)
	if err != nil {
		return err
	}
	defer zr.Close()
	for _, f := range zr.File {
		if f.Mode().IsRegular() && path.Clean(f.Name) == path.Clean(bin) {
			rc, err := f.Open()
			if err != nil {
				return err
			}
			defer rc.Close()
			return writeFile(dst, rc)
		}
	}
	return fmt.Errorf("%s not found in archive", bin)
}

func writeFile(dst string, r io.Reader) error {
	f, err := os.Create(dst)
	if err != nil {
		return err
	}
	if _, err := io.Copy(f, io.LimitReader(r, maxArtifactSize)); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}
