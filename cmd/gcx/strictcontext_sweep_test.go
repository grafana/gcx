//go:build !windows

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/grafana/gcx/cmd/gcx/root"
	"github.com/grafana/gcx/internal/agent"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"github.com/stretchr/testify/require"
)

// The sweep checks the strict context mode security invariant over the whole
// command tree. With GCX_REQUIRE_CONTEXT on and no context named, an invocation
// sends no request to the inherited (current-context) endpoints.
//
// The sweep is gated because it starts thousands of child processes:
//
//	GCX_STRICT_SWEEP=1 go test ./cmd/gcx/ -run TestStrictContextSweep -v -timeout 30m
//
// Optional variables:
//
//	GCX_SWEEP_BINARY  drive this gcx binary instead of re-executing the test
//	                  binary (used to run pass (a) against origin/main).
//	GCX_SWEEP_OUT     directory that receives sweep.json (machine-readable).
//	GCX_SWEEP_ONLY    substring filter on command paths (debugging).
const (
	sweepGate       = "GCX_STRICT_SWEEP"
	sweepBinaryEnv  = "GCX_SWEEP_BINARY"
	sweepOutEnv     = "GCX_SWEEP_OUT"
	sweepOnlyEnv    = "GCX_SWEEP_ONLY"
	sweepTimeout    = 15 * time.Second
	sweepWorkers    = 8
	sweepSelectedID = "selected"
)

// sweepSkips lists commands that the sweep never invokes, with the reason. An
// entry matches an exact command path (without the leading "gcx").
var sweepSkips = map[string]string{ //nolint:gochecknoglobals
	"dev serve":       "long-running dev server that listens on a port",
	"resources serve": "long-running server that listens on a port",
}

// knownFailOpen lists command paths whose invariant violations the sweep
// reports but does not fail on. Each entry needs a reason. The controller
// reviews them.
var knownFailOpen = map[string]string{} //nolint:gochecknoglobals

type sweepRun struct {
	Shape     string   `json:"shape"`
	Args      []string `json:"args"`
	Inherited []string `json:"inherited,omitempty"`
	Selected  []string `json:"selected,omitempty"`
	Exit      int      `json:"exit"`
	Refused   bool     `json:"refused"`
	TimedOut  bool     `json:"timedOut"`
	Tail      string   `json:"tail,omitempty"`
}

type sweepCommand struct {
	Path     string                `json:"path"`
	Skip     string                `json:"skip,omitempty"`
	HasYes   bool                  `json:"hasYes"`
	Passes   map[string][]sweepRun `json:"passes,omitempty"` // a, b, c, c2
	reaching []string
}

type sweepRecorder struct {
	*httptest.Server

	mu   sync.Mutex
	reqs []string
}

func newSweepRecorder() *sweepRecorder {
	rec := &sweepRecorder{}
	rec.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rec.mu.Lock()
		rec.reqs = append(rec.reqs, r.Method+" "+r.URL.RequestURI())
		rec.mu.Unlock()
		http.Error(w, "sweep: always failing", http.StatusInternalServerError)
	}))
	return rec
}

func (r *sweepRecorder) requests() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.reqs...)
}

// sweepConfig points every endpoint a context can carry at the two listeners.
func sweepConfig(inherited, selected string) string {
	entry := func(name, url string) string {
		return fmt.Sprintf(`  %[1]s:
    slug: %[1]s
    grafana:
      server: %[2]s
      proxy-endpoint: %[2]s
      org-id: 1
      stack-id: 1
      auth-method: token
      token: x
    providers:
      metrics:
        metrics-tenant-id: "1"
        metrics-tenant-url: %[2]s
      logs:
        logs-tenant-id: "1"
        logs-tenant-url: %[2]s
      traces:
        traces-tenant-id: "1"
        traces-tenant-url: %[2]s
      profiles:
        profiles-tenant-id: "1"
        profiles-tenant-url: %[2]s
      synth:
        sm-url: %[2]s
        sm-token: x
      faro:
        faro-api-url: %[2]s
      k6:
        api-domain: %[2]s
`, name, url)
	}
	cloud := func(name, url string) string {
		return fmt.Sprintf(`  %s:
    token: x
    api-url: %s
    oauth-url: %s
`, name, url, url)
	}
	return "version: 1\nstacks:\n" +
		entry("inherited", inherited) + entry(sweepSelectedID, selected) +
		"cloud:\n" + cloud("inherited", inherited) + cloud(sweepSelectedID, selected) +
		"contexts:\n  inherited:\n    stack: inherited\n    cloud: inherited\n" +
		"  " + sweepSelectedID + ":\n    stack: " + sweepSelectedID + "\n    cloud: " + sweepSelectedID + "\n" +
		"current-context: inherited\n"
}

type sweepPass struct {
	name   string
	strict bool
	flag   bool // append --context selected
	env    bool // GCX_CONTEXT=selected
}

var sweepPasses = []sweepPass{ //nolint:gochecknoglobals
	{name: "a"},
	{name: "b", strict: true},
	{name: "c", strict: true, flag: true},
	{name: "c2", strict: true, env: true},
}

// sweepInvoke runs one invocation in a fully isolated sandbox. It returns what
// each listener saw.
func sweepInvoke(args []string, pass sweepPass) (sweepRun, error) {
	inherited, selected := newSweepRecorder(), newSweepRecorder()
	defer inherited.Close()
	defer selected.Close()

	dir, err := os.MkdirTemp("", "gcx-sweep-")
	if err != nil {
		return sweepRun{}, err
	}
	defer os.RemoveAll(dir)
	sub := func(name string) string {
		p := filepath.Join(dir, name)
		_ = os.MkdirAll(p, 0o700)
		return p
	}
	cfg := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(cfg, []byte(sweepConfig(inherited.URL, selected.URL)), 0o600); err != nil {
		return sweepRun{}, err
	}

	full := append([]string(nil), args...)
	if pass.flag {
		full = append(full, "--context", sweepSelectedID)
	}

	ctx, cancel := context.WithTimeout(context.Background(), sweepTimeout)
	defer cancel()

	var cmd *exec.Cmd
	env := strictBaseEnv()
	if bin := os.Getenv(sweepBinaryEnv); bin != "" {
		cmd = exec.CommandContext(ctx, bin, full...)
	} else {
		encoded, err := json.Marshal(full)
		if err != nil {
			return sweepRun{}, err
		}
		cmd = exec.CommandContext(ctx, os.Args[0], "-test.run=^TestStrictContextProcessHelper$") //nolint:gosec
		env = append(env, strictContextProcessHelper+"=1", strictContextArgsEnv+"="+string(encoded))
	}
	env = append(env,
		"GCX_AGENT_MODE=true", // no browser launch, no prompts
		"GCX_KEYCHAIN=off",
		"GCX_TELEMETRY=disabled",
		"GCX_NO_UPDATE_NOTIFIER=1",
		"NO_COLOR=1",
		"HOME="+sub("home"),
		"XDG_CONFIG_HOME="+sub("xdg-config"),
		"XDG_CONFIG_DIRS="+sub("xdg-dirs"),
		"XDG_CACHE_HOME="+sub("xdg-cache"),
		"XDG_STATE_HOME="+sub("xdg-state"),
		"GCX_CONFIG="+cfg,
		// Anything but the loopback listeners goes to a dead proxy (Go never
		// proxies 127.0.0.1), so nothing can reach the real network.
		"HTTP_PROXY=http://127.0.0.1:1", "HTTPS_PROXY=http://127.0.0.1:1", "ALL_PROXY=http://127.0.0.1:1",
	)
	if pass.strict {
		env = append(env, "GCX_REQUIRE_CONTEXT=true")
	}
	if pass.env {
		env = append(env, "GCX_CONTEXT="+sweepSelectedID)
	}
	cmd.Env = env
	cmd.Dir = sub("cwd")
	cmd.Stdin = strings.NewReader("")
	var out bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &out
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) }
	cmd.WaitDelay = 2 * time.Second

	runErr := cmd.Run()
	if cmd.Process != nil { // reap stragglers that outlived the child
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	}

	run := sweepRun{Args: full, Inherited: inherited.requests(), Selected: selected.requests()}
	run.TimedOut = ctx.Err() != nil
	var exitErr *exec.ExitError
	if cmd.ProcessState != nil {
		run.Exit = cmd.ProcessState.ExitCode()
	} else if runErr != nil && !asExitError(runErr, &exitErr) {
		return run, runErr
	}
	text := out.String()
	run.Refused = run.Exit == 2 && strings.Contains(text, strictRefusalDetail)
	if len(text) > 300 {
		text = text[:300]
	}
	run.Tail = text
	return run, nil
}

func asExitError(err error, target **exec.ExitError) bool {
	ee, ok := err.(*exec.ExitError) //nolint:errorlint
	if ok {
		*target = ee
	}
	return ok
}

func sweepHasFlag(cmd *cobra.Command, long string) bool {
	found := false
	visit := func(f *pflag.Flag) {
		if f.Name == long {
			found = true
		}
	}
	cmd.LocalFlags().VisitAll(visit)
	cmd.InheritedFlags().VisitAll(visit)
	return found
}

func sweepShapes(path []string, hasYes bool) [][]string {
	shapes := make([][]string, 0, 3)
	for _, extra := range [][]string{nil, {"x"}, {"x", "x"}} {
		args := append(append([]string(nil), path...), extra...)
		if hasYes {
			args = append(args, "--yes")
		}
		shapes = append(shapes, args)
	}
	return shapes
}

//nolint:gocyclo,maintidx // one linear sweep-and-report flow
func TestStrictContextSweep_InheritedEndpointNeverHit(t *testing.T) {
	if os.Getenv(sweepGate) != "1" {
		t.Skip("set " + sweepGate + "=1 to run the command sweep")
	}

	// Collect every runnable command (leaves and runnable groups).
	var cmds []*sweepCommand
	var total, leaves int
	agent.WalkCommands(root.Command("sweep"), func(c *cobra.Command) {
		total++
		if !c.HasSubCommands() {
			leaves++
		}
		if !c.Runnable() || c.Hidden && c.Name() == "__complete" || c.Parent() == nil {
			return
		}
		path := strings.Fields(c.CommandPath())[1:]
		sc := &sweepCommand{Path: strings.Join(path, " "), HasYes: sweepHasFlag(c, "yes"), Passes: map[string][]sweepRun{}}
		if only := os.Getenv(sweepOnlyEnv); only != "" && !strings.Contains(sc.Path, only) {
			return
		}
		if reason, ok := sweepSkips[sc.Path]; ok {
			sc.Skip = reason
		}
		cmds = append(cmds, sc)
	})
	sort.Slice(cmds, func(i, j int) bool { return cmds[i].Path < cmds[j].Path })

	type job struct {
		sc    *sweepCommand
		pass  sweepPass
		shape int
		args  []string
	}
	var jobs []job
	for _, sc := range cmds {
		if sc.Skip != "" {
			continue
		}
		shapes := sweepShapes(strings.Fields(sc.Path), sc.HasYes)
		for _, pass := range sweepPasses {
			if os.Getenv(sweepBinaryEnv) != "" && pass.name != "a" {
				continue // an external (main) binary has no strict mode
			}
			sc.Passes[pass.name] = make([]sweepRun, len(shapes))
			for i, args := range shapes {
				jobs = append(jobs, job{sc, pass, i, args})
			}
		}
	}

	var wg sync.WaitGroup
	sem := make(chan struct{}, sweepWorkers)
	var mu sync.Mutex
	var harnessErrs []string
	for _, j := range jobs {
		wg.Add(1)
		sem <- struct{}{}
		go func() {
			defer wg.Done()
			defer func() { <-sem }()
			run, err := sweepInvoke(j.args, j.pass)
			run.Shape = strconv.Itoa(j.shape)
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				harnessErrs = append(harnessErrs, fmt.Sprintf("%s [%s]: %v", j.sc.Path, j.pass.name, err))
			}
			j.sc.Passes[j.pass.name][j.shape] = run
		}()
	}
	wg.Wait()
	require.Empty(t, harnessErrs, "harness failures")

	// Analyse.
	var skipped, reaching, refusedB, timedOut, runnable int
	var violations, notRefused, selectedMissed, timeoutList []string
	var skippedList, refusedNotReaching []string
	reachSet := []string{}
	for _, sc := range cmds {
		if sc.Skip != "" {
			skipped++
			skippedList = append(skippedList, sc.Path+": "+sc.Skip)
			continue
		}
		runnable++
		reachedShapes := map[int]bool{}
		for i, r := range sc.Passes["a"] {
			if len(r.Inherited) > 0 {
				reachedShapes[i] = true
			}
		}
		if len(reachedShapes) > 0 {
			reaching++
			sc.reaching = []string{}
			for i := range reachedShapes {
				sc.reaching = append(sc.reaching, strconv.Itoa(i))
			}
			sort.Strings(sc.reaching)
			reachSet = append(reachSet, sc.Path)
		}
		anyRefused, anyTimeout := false, false
		for _, pass := range sweepPasses {
			for i, r := range sc.Passes[pass.name] {
				if r.TimedOut {
					anyTimeout = true
					timeoutList = append(timeoutList, fmt.Sprintf("%s [%s shape %d] %q", sc.Path, pass.name, i, r.Args))
				}
				if pass.strict && len(r.Inherited) > 0 {
					violations = append(violations, fmt.Sprintf("%s [%s shape %d] args=%q inherited=%v exit=%d tail=%q",
						sc.Path, pass.name, i, r.Args, r.Inherited, r.Exit, r.Tail))
				}
				if pass.name == "b" {
					if r.Refused {
						anyRefused = true
					}
					if len(r.Selected) > 0 {
						violations = append(violations, fmt.Sprintf("%s [b shape %d] args=%q selected listener hit with no context named: %v",
							sc.Path, i, r.Args, r.Selected))
					}
					if reachedShapes[i] && !r.Refused {
						notRefused = append(notRefused, fmt.Sprintf("%s [shape %d] args=%q exit=%d tail=%q",
							sc.Path, i, r.Args, r.Exit, r.Tail))
					}
				}
				if (pass.name == "c" || pass.name == "c2") && reachedShapes[i] && len(r.Selected) == 0 {
					selectedMissed = append(selectedMissed, fmt.Sprintf("%s [%s shape %d] args=%q exit=%d tail=%q",
						sc.Path, pass.name, i, r.Args, r.Exit, r.Tail))
				}
			}
		}
		var extra []string
		for i, r := range sc.Passes["b"] {
			if r.Refused && !reachedShapes[i] {
				a := sc.Passes["a"][i]
				extra = append(extra, fmt.Sprintf("%d(a: exit=%d timedOut=%v)", i, a.Exit, a.TimedOut))
			}
		}
		if len(extra) > 0 {
			refusedNotReaching = append(refusedNotReaching, sc.Path+" shapes="+strings.Join(extra, ","))
		}
		if anyRefused {
			refusedB++
		}
		if anyTimeout {
			timedOut++
		}
	}

	var hard, known []string
	for _, v := range append(append([]string(nil), violations...), notRefused...) {
		path := v
		for p := range knownFailOpen {
			if strings.HasPrefix(v, p+" [") {
				path = p
			}
		}
		if reason, ok := knownFailOpen[path]; ok {
			known = append(known, "KNOWN "+v+" -- "+reason)
		} else {
			hard = append(hard, v)
		}
	}

	report := func(title string, lines []string) {
		sort.Strings(lines)
		t.Logf("%s (%d)", title, len(lines))
		for _, l := range lines {
			t.Logf("  %s", l)
		}
	}
	t.Logf("SWEEP TOTALS: tree commands=%d leaves=%d runnable-commands=%d invoked=%d reaching(a)=%d refused(b)=%d skipped=%d timed-out=%d invocations=%d",
		total, leaves, len(cmds), runnable, reaching, refusedB, skipped, timedOut, len(jobs))
	report("SKIPPED", skippedList)
	report("TIMED OUT", timeoutList)
	sort.Strings(reachSet)
	report("REACHING (pass a)", func() []string {
		var l []string
		for _, sc := range cmds {
			if len(sc.reaching) > 0 {
				l = append(l, fmt.Sprintf("%s shapes=%v", sc.Path, sc.reaching))
			}
		}
		return l
	}())
	report("REFUSED BUT NOT REACHING (pass b \\ pass a)", refusedNotReaching)
	report("SELECTED LISTENER NOT HIT IN (c)/(c2) for a reaching shape", selectedMissed)
	report("KNOWN FAIL-OPEN (reported, not failing)", known)
	report("VIOLATIONS", hard)

	if dir := os.Getenv(sweepOutEnv); dir != "" {
		data, err := json.MarshalIndent(cmds, "", " ")
		require.NoError(t, err)
		require.NoError(t, os.WriteFile(filepath.Join(dir, "sweep.json"), data, 0o600))
		require.NoError(t, os.WriteFile(filepath.Join(dir, "reaching.txt"), []byte(strings.Join(reachSet, "\n")+"\n"), 0o600))
	}

	require.Empty(t, hard, "strict-mode invariant violations (fail-open paths)")
}
