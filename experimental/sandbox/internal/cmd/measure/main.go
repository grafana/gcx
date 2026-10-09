//go:build linux

// Command measure reports the memory a process embedding the sandbox uses
// while it loads gcx from a compilation cache, runs `gcx version` once, then
// runs two rounds of 10 concurrent `gcx commands`. measure.sh builds one per
// checkout being compared and runs each in its own cgroup (see the README);
// the method is sd2k's, from https://github.com/grafana/gcx/pull/1498.
//
// Usage:
//
//	measure precompile <gcx.wasm> <cache dir>
//	measure run <gcx.wasm> <cache dir>
//	measure evict <file or dir>...
//	measure summarize <results dir> <build>...
//
// run prints a line per stage, and the same as JSON for summarize. With
// FORCE_GC=1 it runs runtime.GC and debug.FreeOSMemory before each report,
// which suits fixed costs: otherwise the heap after loading still holds the
// garbage of decoding the module. Without it, the reports show what a server
// holds between bursts.
package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/grafana/gcx/experimental/sandbox"
	"golang.org/x/sys/unix"
)

const (
	concurrent = 10 // runs per round
	rounds     = 2
	mib        = 1 << 20
)

func main() {
	if err := dispatch(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "measure:", err)
		os.Exit(1)
	}
}

func dispatch(args []string) error {
	switch {
	case len(args) == 3 && args[0] == "precompile":
		_, _, err := load(args[1], args[2])
		return err
	case len(args) == 3 && args[0] == "run":
		return run(args[1], args[2])
	case len(args) >= 2 && args[0] == "evict":
		return evict(args[1:])
	case len(args) >= 3 && args[0] == "summarize":
		return summarize(args[1], args[2:])
	}
	return errors.New("usage: measure precompile|run <gcx.wasm> <cache dir> | evict <path...> | summarize <results dir> <build...>")
}

// load reads the module and loads it with sandbox.New, which it times.
func load(wasmPath, cache string) (*sandbox.Runtime, time.Duration, error) {
	wasm, err := os.ReadFile(wasmPath)
	if err != nil {
		return nil, 0, err
	}
	start := time.Now()
	rt, err := sandbox.New(context.Background(), wasm, sandbox.Config{CacheDir: cache, MemoryLimitBytes: 512 << 20})
	if err != nil {
		return nil, 0, err
	}
	took := time.Since(start)
	fmt.Fprintf(os.Stdout, "loaded in %v\n", took.Round(time.Millisecond))
	return rt, took, nil
}

func run(wasmPath, cache string) error {
	rt, took, err := load(wasmPath, cache)
	if err != nil {
		return err
	}
	report(sample{Stage: "after load", LoadSeconds: took.Seconds()})
	gcx := func(args ...string) error {
		var out, errb bytes.Buffer
		res, err := rt.Run(context.Background(), sandbox.Invocation{Args: args, Stdout: &out, Stderr: &errb})
		if err == nil && res.ExitCode != 0 {
			err = fmt.Errorf("exit status %d: %s", res.ExitCode, errb.String())
		}
		if err != nil {
			return fmt.Errorf("gcx %s: %w", strings.Join(args, " "), err)
		}
		return nil
	}
	if err := gcx("version"); err != nil {
		return err
	}
	report(sample{Stage: "after version"})
	for round := 1; round <= rounds; round++ {
		errs := make([]error, concurrent)
		var wg sync.WaitGroup
		for i := range concurrent {
			wg.Go(func() { errs[i] = gcx("commands") })
		}
		wg.Wait()
		if err := errors.Join(errs...); err != nil {
			return err
		}
		report(sample{Stage: fmt.Sprintf("after %dx commands #%d", concurrent, round)})
	}
	// Keep the runtime, and the memory image it holds, alive through the
	// reports: once unreachable, a forced GC can collect it.
	runtime.KeepAlive(rt)
	return nil
}

// sample is one report, in MiB unless noted.
type sample struct {
	Stage       string  `json:"stage"`
	LoadSeconds float64 `json:"load_seconds,omitempty"`
	Private     float64 `json:"private"` // RssAnon: the process's own pages
	Shmem       float64 `json:"shmem"`   // RssShmem
	File        float64 `json:"file"`    // RssFile: mapped code, if the cache is on disk
	PSS         float64 `json:"pss"`     // shared pages split between processes mapping them
	Cgroup      float64 `json:"cgroup"`  // memory.current, closest to what a pod is charged
	CgroupAnon  float64 `json:"cgroup_anon"`
	CgroupShmem float64 `json:"cgroup_shmem"` // includes the memory image
	CgroupFile  float64 `json:"cgroup_file"`  // page cache, excluding shmem
	Peak        float64 `json:"peak"`         // memory.peak
	GoHeap      float64 `json:"go_heap"`      // runtime.MemStats.HeapInuse
}

func report(s sample) {
	if os.Getenv("FORCE_GC") != "" {
		runtime.GC()
		debug.FreeOSMemory()
	}
	st, sm := keyValues("/proc/self/status"), keyValues("/proc/self/smaps_rollup")
	cg := cgroupDir()
	cs := keyValues(cg + "/memory.stat")
	var ms runtime.MemStats
	runtime.ReadMemStats(&ms)
	s.Private, s.Shmem, s.File, s.PSS = st["RssAnon"]/1024, st["RssShmem"]/1024, st["RssFile"]/1024, sm["Pss"]/1024
	s.Cgroup, s.Peak = number(cg+"/memory.current")/mib, number(cg+"/memory.peak")/mib
	s.CgroupAnon, s.CgroupShmem, s.CgroupFile = cs["anon"]/mib, cs["shmem"]/mib, (cs["file"]-cs["shmem"])/mib
	s.GoHeap = float64(ms.HeapInuse) / mib
	fmt.Fprintf(os.Stdout, "%-24s private %6.0f  shmem %5.0f  file %5.0f  PSS %6.0f | cgroup %6.0f (anon %5.0f shmem %4.0f file %5.0f) peak %6.0f | goheap %4.0f  MiB\n",
		s.Stage, s.Private, s.Shmem, s.File, s.PSS, s.Cgroup, s.CgroupAnon, s.CgroupShmem, s.CgroupFile, s.Peak, s.GoHeap)
	if line, err := json.Marshal(s); err == nil {
		fmt.Fprintf(os.Stdout, "%s\n", line)
	}
}

// keyValues reads "key value" or "key: value ..." lines.
func keyValues(path string) map[string]float64 {
	out := map[string]float64{}
	f, err := os.Open(path)
	if err != nil {
		return out
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		fields := strings.Fields(strings.Replace(sc.Text(), ":", " ", 1))
		if len(fields) >= 2 {
			if v, err := strconv.ParseFloat(fields[1], 64); err == nil {
				out[fields[0]] = v
			}
		}
	}
	return out
}

func number(path string) float64 {
	b, _ := os.ReadFile(path)
	v, _ := strconv.ParseFloat(strings.TrimSpace(string(b)), 64)
	return v
}

// cgroupDir is this process's cgroup v2 directory.
func cgroupDir() string {
	b, _ := os.ReadFile("/proc/self/cgroup")
	return "/sys/fs/cgroup" + strings.TrimSpace(strings.TrimPrefix(string(b), "0::"))
}

// evict drops files from page cache, so a run is charged for the cache and
// module pages it reads, as a freshly started pod would be. It only drops
// clean pages nothing maps, so it needs no privileges and leaves other
// processes alone.
func evict(paths []string) error {
	for _, p := range paths {
		info, err := os.Stat(p)
		if err != nil {
			return err
		}
		if !info.IsDir() {
			f, err := os.Open(p)
			if err != nil {
				return err
			}
			err = errors.Join(dropCache(f), f.Close())
			if err != nil {
				return err
			}
			continue
		}
		root, err := os.OpenRoot(p)
		if err != nil {
			return err
		}
		err = fs.WalkDir(root.FS(), ".", func(path string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return err
			}
			f, err := root.Open(path)
			if err != nil {
				return err
			}
			return errors.Join(dropCache(f), f.Close())
		})
		if err := errors.Join(err, root.Close()); err != nil {
			return err
		}
	}
	return nil
}

func dropCache(f *os.File) error {
	return unix.Fadvise(int(f.Fd()), 0, 0, unix.FADV_DONTNEED)
}

// metrics are one build's medians.
type metrics struct {
	runs                                     int
	peak, perRun, peakGC, perRunGC, kept     float64
	load                                     float64 // seconds
	private, image, goHeap, pss, total, file float64 // fixed, after version with FORCE_GC=1
}

// summarize reads <dir>/<build>-gc<0|1>-<n>.txt for each build and prints
// markdown tables, with rows comparing each build with the one before it,
// and the last with the first.
func summarize(dir string, builds []string) error {
	var ms []metrics
	for _, b := range builds {
		m, err := measureBuild(dir, b)
		if err != nil {
			return err
		}
		ms = append(ms, m)
	}
	w := bufio.NewWriter(os.Stdout)
	defer w.Flush()
	fmt.Fprintf(w, "Medians of %d runs per build, with and without `FORCE_GC=1`, in MiB. Change rows compare those medians; negative is less.\n\n", ms[0].runs)
	fmt.Fprintf(w, "**%d concurrent `gcx commands`, %d rounds**\n\n", concurrent, rounds)
	table(w, builds, ms, []column{
		{"cgroup peak", func(m metrics) float64 { return m.peak }, "%.0f"},
		{"per concurrent run", func(m metrics) float64 { return m.perRun }, "%.0f"},
		{"cgroup peak, `FORCE_GC=1`", func(m metrics) float64 { return m.peakGC }, "%.0f"},
		{"per concurrent run, `FORCE_GC=1`", func(m metrics) float64 { return m.perRunGC }, "%.0f"},
		{"private after both rounds", func(m metrics) float64 { return m.kept }, "%.0f"},
		{"load from cache", func(m metrics) float64 { return m.load }, "%.2f s"},
	})
	fmt.Fprintf(w, "\n**Fixed cost**, after `gcx version` with `FORCE_GC=1`\n\n")
	table(w, builds, ms, []column{
		{"private", func(m metrics) float64 { return m.private }, "%.0f"},
		{"memory image (cgroup shmem)", func(m metrics) float64 { return m.image }, "%.0f"},
		{"Go heap", func(m metrics) float64 { return m.goHeap }, "%.0f"},
		{"PSS", func(m metrics) float64 { return m.pss }, "%.0f"},
		{"cgroup total", func(m metrics) float64 { return m.total }, "%.0f"},
		{"cgroup file", func(m metrics) float64 { return m.file }, "%.0f"},
	})
	fmt.Fprintf(w, "\n\"Per concurrent run\" is (`memory.peak` − `memory.current` after `gcx version`) / %d.\n", concurrent)
	return nil
}

type column struct {
	name   string
	value  func(metrics) float64
	format string
}

func table(w *bufio.Writer, builds []string, ms []metrics, cols []column) {
	fmt.Fprintf(w, "| |")
	for _, c := range cols {
		fmt.Fprintf(w, " %s |", c.name)
	}
	fmt.Fprintf(w, "\n|---|%s\n", strings.Repeat("---|", len(cols)))
	for i, b := range builds {
		fmt.Fprintf(w, "| %s |", b)
		for _, c := range cols {
			fmt.Fprintf(w, " "+c.format+" |", c.value(ms[i]))
		}
		fmt.Fprintf(w, "\n")
	}
	change := func(from, to int) {
		fmt.Fprintf(w, "| change, %s vs %s |", builds[to], builds[from])
		for _, c := range cols {
			switch base := c.value(ms[from]); {
			case base == 0:
				fmt.Fprintf(w, " n/a |")
			case math.Round((c.value(ms[to])-base)/base*100) == 0:
				fmt.Fprintf(w, " 0%% |")
			default:
				fmt.Fprintf(w, " %+.0f%% |", (c.value(ms[to])-base)/base*100)
			}
		}
		fmt.Fprintf(w, "\n")
	}
	for i := 1; i < len(builds); i++ {
		change(i-1, i)
	}
	if len(builds) > 2 {
		change(0, len(builds)-1)
	}
}

func measureBuild(dir, build string) (metrics, error) {
	var peak, perRun, peakGC, perRunGC, kept, load []float64
	var private, image, goHeap, pss, total, file []float64
	runs := 0
	for _, gc := range []string{"0", "1"} {
		paths, err := filepath.Glob(filepath.Join(dir, build+"-gc"+gc+"-*.txt"))
		if err != nil {
			return metrics{}, err
		}
		if len(paths) == 0 {
			return metrics{}, fmt.Errorf("no reports for %s with FORCE_GC=%s in %s", build, gc, dir)
		}
		runs = len(paths)
		for _, p := range paths {
			samples, err := readSamples(p)
			if err != nil {
				return metrics{}, err
			}
			first, version, last := samples[0], samples[1], samples[len(samples)-1]
			load = append(load, first.LoadSeconds)
			if gc == "0" {
				peak = append(peak, last.Peak)
				perRun = append(perRun, (last.Peak-version.Cgroup)/concurrent)
				kept = append(kept, last.Private)
				continue
			}
			peakGC = append(peakGC, last.Peak)
			perRunGC = append(perRunGC, (last.Peak-version.Cgroup)/concurrent)
			private = append(private, version.Private)
			image = append(image, version.CgroupShmem)
			goHeap = append(goHeap, version.GoHeap)
			pss = append(pss, version.PSS)
			total = append(total, version.Cgroup)
			file = append(file, version.CgroupFile)
		}
	}
	return metrics{
		runs: runs, peak: median(peak), perRun: median(perRun), peakGC: median(peakGC), perRunGC: median(perRunGC),
		kept: median(kept), load: median(load), private: median(private), image: median(image),
		goHeap: median(goHeap), pss: median(pss), total: median(total), file: median(file),
	}, nil
}

// readSamples reads a run's JSON reports: after load, after version, and
// after each round.
func readSamples(path string) ([]sample, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var samples []sample
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		if line := sc.Bytes(); len(line) > 0 && line[0] == '{' {
			var s sample
			if err := json.Unmarshal(line, &s); err != nil {
				return nil, fmt.Errorf("%s: %w", path, err)
			}
			samples = append(samples, s)
		}
	}
	if len(samples) != 2+rounds {
		return nil, fmt.Errorf("%s: %d reports, want %d; did the run fail?", path, len(samples), 2+rounds)
	}
	return samples, sc.Err()
}

func median(vs []float64) float64 {
	if len(vs) == 0 {
		return 0
	}
	vs = slices.Clone(vs)
	slices.Sort(vs)
	if n := len(vs); n%2 == 0 {
		return (vs[n/2-1] + vs[n/2]) / 2
	}
	return vs[len(vs)/2]
}
