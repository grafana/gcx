package extensions

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strings"

	"github.com/Masterminds/semver/v3"
)

// scpLikeURL matches git's user@host:path form, as used for SSH remotes.
var scpLikeURL = regexp.MustCompile(`^[\w.-]+@[\w.-]+:`)

// isGitURL accepts encrypted transports and local file URLs only.
func isGitURL(source string) bool {
	for _, scheme := range []string{"https://", "ssh://", "file://"} {
		if strings.HasPrefix(source, scheme) {
			return true
		}
	}
	return scpLikeURL.MatchString(source)
}

// latestTag returns the highest release tag, ignoring pre-releases and tags
// that are not semantic versions.
func latestTag(ctx context.Context, url string) (string, error) {
	out, err := git(ctx, "ls-remote", "--tags", "--refs", "--", url)
	if err != nil {
		return "", err
	}
	var latest *semver.Version
	var tag string
	for line := range strings.Lines(out) {
		_, ref, ok := strings.Cut(strings.TrimSpace(line), "\trefs/tags/")
		if !ok {
			continue
		}
		v, err := semver.NewVersion(ref)
		if err != nil || v.Prerelease() != "" {
			continue
		}
		if latest == nil || v.GreaterThan(latest) {
			latest, tag = v, ref
		}
	}
	if latest == nil {
		return "", fmt.Errorf("%s has no release tags; tag a semantic version such as v0.1.0", url)
	}
	return tag, nil
}

// cloneTag checks out tag into dir and returns its commit.
func cloneTag(ctx context.Context, url, tag, dir string) (string, error) {
	if _, err := git(ctx, "-c", "advice.detachedHead=false", "clone", "--quiet", "--depth", "1", "--branch", tag, "--", url, dir); err != nil {
		return "", err
	}
	return git(ctx, "-C", dir, "rev-parse", "HEAD")
}

func git(ctx context.Context, args ...string) (string, error) {
	if _, err := exec.LookPath("git"); err != nil {
		return "", errors.New("git is required to install extensions from a repository")
	}
	cmd := exec.CommandContext(ctx, "git", args...)
	// Fail instead of waiting for credentials nobody can type; credential
	// helpers still work.
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("git: %w: %s", err, strings.TrimSpace(stderr.String()))
	}
	return strings.TrimSpace(string(out)), nil
}
