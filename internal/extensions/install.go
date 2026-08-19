package extensions

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/Masterminds/semver/v3"
)

// maxDownloadBytes caps a single extension artifact download.
const maxDownloadBytes = 256 << 20 // 256 MiB

// InstallOptions controls a single install.
type InstallOptions struct {
	// Source is a local path, an https URL to a manifest, or a git URL.
	Source string
	// GCXVersion is the running gcx version, checked against minGCXVersion.
	GCXVersion string
	// Progress receives human-readable narration. May be nil.
	Progress io.Writer
}

// Install resolves a source, fetches the entrypoint for this platform, and
// records the result in the index.
func (s *Store) Install(ctx context.Context, opts InstallOptions) (*Installed, error) {
	manifestDir, manifest, cleanup, err := resolveSource(ctx, opts)
	if cleanup != nil {
		defer cleanup()
	}
	if err != nil {
		return nil, err
	}

	if err := checkGCXVersion(manifest, opts.GCXVersion); err != nil {
		return nil, err
	}

	var platform *Platform
	if manifest.HasPlatforms() {
		if platform, err = manifest.SelectPlatform(); err != nil {
			return nil, err
		}
	}

	dest := s.dir(manifest.Metadata.Name, manifest.Spec.Version)
	if err := os.RemoveAll(dest); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(dest, 0o755); err != nil {
		return nil, err
	}

	entrypoint := filepath.Join(dest, entrypointName(manifest, platform))
	switch {
	case platform == nil:
		src := filepath.Join(manifestDir, filepath.FromSlash(manifest.Spec.Script))
		if err := copyExecutable(src, entrypoint); err != nil {
			return nil, err
		}
	case platform.Path != "":
		src := filepath.Join(manifestDir, filepath.FromSlash(platform.Path))
		if err := copyExecutable(src, entrypoint); err != nil {
			return nil, err
		}
	default:
		progressf(opts.Progress, "Downloading %s...", platform.URL)
		if err := fetchEntrypoint(ctx, *platform, entrypoint); err != nil {
			return nil, err
		}
	}

	// Record the manifest alongside the entrypoint so `ext list` and `update`
	// can read what was installed without re-fetching the source.
	if data, err := os.ReadFile(filepath.Join(manifestDir, ManifestFilename)); err == nil {
		_ = os.WriteFile(filepath.Join(dest, ManifestFilename), data, 0o600)
	}

	installed := Installed{
		Name:        manifest.Metadata.Name,
		Version:     manifest.Spec.Version,
		Description: manifest.Spec.Description,
		Source:      opts.Source,
		Entrypoint:  entrypoint,
		Interpreted: platform == nil,
		ReportUsage: manifest.Spec.ReportUsage(),
		InstalledAt: time.Now().UTC(),
	}
	if err := s.record(installed); err != nil {
		return nil, err
	}
	return &installed, nil
}

// resolveSource returns the directory holding the manifest, the parsed
// manifest, and a cleanup for any temporary directory it created.
func resolveSource(ctx context.Context, opts InstallOptions) (string, *Manifest, func(), error) {
	src := opts.Source
	switch {
	case isGitSource(src):
		dir, err := os.MkdirTemp("", "gcx-ext-git-")
		if err != nil {
			return "", nil, nil, err
		}
		cleanup := func() { _ = os.RemoveAll(dir) }
		progressf(opts.Progress, "Cloning %s...", src)
		// Shelling out to the user's own git keeps this host-agnostic: no
		// GitHub API call, no vendored git implementation.
		cmd := exec.CommandContext(ctx, "git", "clone", "--depth", "1", "--quiet", src, dir)
		cmd.Stderr = opts.Progress
		if err := cmd.Run(); err != nil {
			return "", nil, cleanup, fmt.Errorf("cloning %s: %w", src, err)
		}
		m, err := LoadManifest(filepath.Join(dir, ManifestFilename))
		return dir, m, cleanup, err

	case isHTTPSource(src):
		dir, err := os.MkdirTemp("", "gcx-ext-url-")
		if err != nil {
			return "", nil, nil, err
		}
		cleanup := func() { _ = os.RemoveAll(dir) }
		progressf(opts.Progress, "Fetching manifest %s...", src)
		data, err := httpGet(ctx, src)
		if err != nil {
			return "", nil, cleanup, err
		}
		if err := os.WriteFile(filepath.Join(dir, ManifestFilename), data, 0o600); err != nil {
			return "", nil, cleanup, err
		}
		m, err := ParseManifest(data)
		return dir, m, cleanup, err

	default:
		path := src
		info, err := os.Stat(path)
		if err != nil {
			return "", nil, nil, fmt.Errorf("reading source %q: %w", src, err)
		}
		if info.IsDir() {
			path = filepath.Join(path, ManifestFilename)
		}
		m, err := LoadManifest(path)
		return filepath.Dir(path), m, nil, err
	}
}

func isGitSource(src string) bool {
	if strings.HasPrefix(src, "git@") || strings.HasPrefix(src, "ssh://") {
		return true
	}
	return isHTTPSource(src) && strings.HasSuffix(src, ".git")
}

func isHTTPSource(src string) bool {
	u, err := url.Parse(src)
	if err != nil {
		return false
	}
	return u.Scheme == "https" || u.Scheme == "http"
}

// checkGCXVersion enforces spec.minGCXVersion. A gcx built without version
// information (a dev build) is never blocked.
func checkGCXVersion(m *Manifest, gcxVersion string) error {
	if m.Spec.MinGCXVersion == "" {
		return nil
	}
	want, err := semver.NewVersion(m.Spec.MinGCXVersion)
	if err != nil {
		return fmt.Errorf("invalid spec.minGCXVersion %q: %w", m.Spec.MinGCXVersion, err)
	}
	// A build with no parseable version (a local dev build) is never blocked.
	have, err := semver.NewVersion(strings.TrimPrefix(gcxVersion, "v"))
	if err != nil {
		return nil //nolint:nilerr // an unparseable gcx version must not block installs
	}
	if have.LessThan(want) {
		return fmt.Errorf("extension %q requires gcx %s or newer (running %s)", m.Metadata.Name, want, have)
	}
	return nil
}

func fetchEntrypoint(ctx context.Context, p Platform, dest string) error {
	body, err := httpGet(ctx, p.URL)
	if err != nil {
		return err
	}
	sum := sha256.Sum256(body)
	if got := hex.EncodeToString(sum[:]); !strings.EqualFold(got, p.SHA256) {
		return fmt.Errorf("checksum mismatch for %s: manifest declares %s, downloaded %s", p.URL, p.SHA256, got)
	}
	if strings.HasSuffix(p.URL, ".tar.gz") || strings.HasSuffix(p.URL, ".tgz") {
		return extractFromTarGz(body, filepath.Base(dest), dest)
	}
	return os.WriteFile(dest, body, 0o755) //nolint:gosec // an extension entrypoint must be executable
}

func httpGet(ctx context.Context, rawURL string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("fetching %s: %s", rawURL, resp.Status)
	}
	return io.ReadAll(io.LimitReader(resp.Body, maxDownloadBytes))
}

func extractFromTarGz(archive []byte, binName, dest string) error {
	gz, err := gzip.NewReader(bytes.NewReader(archive))
	if err != nil {
		return err
	}
	defer func() { _ = gz.Close() }()
	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return fmt.Errorf("archive does not contain %q", binName)
		}
		if err != nil {
			return err
		}
		if hdr.Typeflag != tar.TypeReg || filepath.Base(hdr.Name) != binName {
			continue
		}
		out, err := os.OpenFile(dest, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o755)
		if err != nil {
			return err
		}
		defer func() { _ = out.Close() }()
		_, err = io.Copy(out, io.LimitReader(tr, maxDownloadBytes))
		return err
	}
}

func copyExecutable(src, dest string) error {
	data, err := os.ReadFile(src)
	if err != nil {
		return fmt.Errorf("reading entrypoint %s: %w", src, err)
	}
	return os.WriteFile(dest, data, 0o755) //nolint:gosec // an extension entrypoint must be executable
}

func progressf(w io.Writer, format string, args ...any) {
	if w == nil {
		return
	}
	fmt.Fprintf(w, format+"\n", args...)
}
