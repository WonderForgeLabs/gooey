package main

// Real numbers for the slide that claims top is a real program.
//
// Beat 1.2 says "a live table of everything your computer is doing,
// updating in place". Rendering a screenshot of that would be a lie
// told inside a sentence about honesty, so the slide samples /proc and
// the gauges move. The sampler is deliberately small: it is a slide, not
// a monitoring tool, and cmd/sysmon already exists for the real thing.
//
// Confinement: Sample is called from the deck's one-second Timer, which
// runs on the UI goroutine. Reading three files under /proc costs tens
// of microseconds and buys not having a second goroutine to join.

import (
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
)

type Sample struct {
	CPU   int // busy percent since the previous sample
	Mem   int // used percent
	Procs []Proc
	Head  string
}

// Proc is one row of the table, as VALUES. No padding, no column widths,
// no %-22s: the row template lays those out with Width and HAlign,
// because that is what a layout engine is for.
type Proc struct {
	PID  int
	Name string
	RSS  uint64
}

type Sampler struct {
	prevIdle, prevTotal uint64
	history             []float64
}

// Sample reads the current state. The first call has no previous total to
// difference against, so its CPU figure is the machine's average since
// boot rather than a spike — accurate, just not interesting.
func (s *Sampler) Sample() Sample {
	cpu := s.cpu()
	s.history = append(s.history, float64(cpu))
	if len(s.history) > 40 {
		s.history = s.history[len(s.history)-40:]
	}
	total, avail := meminfo()
	mem := 0
	if total > 0 {
		mem = int((total - avail) * 100 / total)
	}
	return Sample{
		CPU:   cpu,
		Mem:   mem,
		Procs: topProcs(6),
		Head: fmt.Sprintf("%d processes   %s used of %s",
			countProcs(), gib(total-avail), gib(total)),
	}
}

func (s *Sampler) History() []float64 {
	out := make([]float64, len(s.history))
	copy(out, s.history)
	return out
}

// cpu differences /proc/stat's aggregate line against the previous read.
func (s *Sampler) cpu() int {
	b, err := os.ReadFile("/proc/stat")
	if err != nil {
		return 0
	}
	line, _, _ := strings.Cut(string(b), "\n")
	fields := strings.Fields(line)
	if len(fields) < 5 || fields[0] != "cpu" {
		return 0
	}
	var total, idle uint64
	for i, f := range fields[1:] {
		v, err := strconv.ParseUint(f, 10, 64)
		if err != nil {
			continue
		}
		total += v
		if i == 3 || i == 4 { // idle, iowait
			idle += v
		}
	}
	dt, di := total-s.prevTotal, idle-s.prevIdle
	s.prevTotal, s.prevIdle = total, idle
	if dt == 0 {
		return 0
	}
	return clamp(int((dt-di)*100/dt), 0, 100)
}

func meminfo() (total, avail uint64) {
	b, err := os.ReadFile("/proc/meminfo")
	if err != nil {
		return 0, 0
	}
	for _, line := range strings.Split(string(b), "\n") {
		key, rest, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		fields := strings.Fields(rest)
		if len(fields) == 0 {
			continue
		}
		v, err := strconv.ParseUint(fields[0], 10, 64)
		if err != nil {
			continue
		}
		switch key {
		case "MemTotal":
			total = v * 1024
		case "MemAvailable":
			avail = v * 1024
		}
	}
	return total, avail
}

func countProcs() int {
	ents, err := os.ReadDir("/proc")
	if err != nil {
		return 0
	}
	n := 0
	for _, e := range ents {
		if _, err := strconv.Atoi(e.Name()); err == nil {
			n++
		}
	}
	return n
}

// topProcs returns the n largest processes by resident memory, formatted
// as top formats them. Memory rather than CPU because CPU share needs two
// samples per process and the slide is on screen for fifty seconds.
func topProcs(n int) []Proc {
	ents, err := os.ReadDir("/proc")
	if err != nil {
		return nil
	}
	var rows []Proc
	for _, e := range ents {
		pid, err := strconv.Atoi(e.Name())
		if err != nil {
			continue
		}
		b, err := os.ReadFile("/proc/" + e.Name() + "/stat")
		if err != nil {
			continue
		}
		// The comm field is parenthesised and may itself contain spaces
		// and parens, so split on the LAST ')' rather than by fields.
		open := strings.IndexByte(string(b), '(')
		close := strings.LastIndexByte(string(b), ')')
		if open < 0 || close < open {
			continue
		}
		name := string(b[open+1 : close])
		fields := strings.Fields(string(b[close+1:]))
		if len(fields) < 22 {
			continue
		}
		pages, err := strconv.ParseUint(fields[21], 10, 64)
		if err != nil {
			continue
		}
		rows = append(rows, Proc{PID: pid, Name: name, RSS: pages * 4096})
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].RSS > rows[j].RSS })
	if len(rows) > n {
		rows = rows[:n]
	}
	return rows
}

func gib(b uint64) string {
	const unit = 1024
	if b < unit*unit {
		return fmt.Sprintf("%dK", b/unit)
	}
	if b < unit*unit*unit {
		return fmt.Sprintf("%dM", b/unit/unit)
	}
	return fmt.Sprintf("%.1fG", float64(b)/float64(unit*unit*unit))
}
