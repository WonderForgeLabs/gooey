// sysmon is a live system monitor over real /proc data — the "better
// than logs" component-model demo. Three custom widgets (Gauge,
// Sparkline, ProcTable) compose with the builtins; every value flows
// through dependency properties, so a sample tick repaints only the
// widgets whose (rounded) values actually changed — watch the
// "painted" counter sit near zero on an idle system and spike under
// load.
//
//	c / m   sort process table by CPU / memory
//	q       quit
package main

import (
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/WonderForgeLabs/gooey"
	"github.com/WonderForgeLabs/gooey/prop"
	"github.com/WonderForgeLabs/gooey/render"
	"github.com/WonderForgeLabs/gooey/term"
)

var (
	accent = render.Style{Fg: render.RGB(255, 170, 60), Bold: true}
	dim    = render.Style{Fg: render.RGB(140, 140, 150)}
	good   = render.Style{Fg: render.RGB(110, 220, 130)}
	warn   = render.Style{Fg: render.RGB(230, 190, 80)}
	crit   = render.Style{Fg: render.RGB(240, 90, 90), Bold: true}
)

func main() {
	first := sampleCPU()
	ncores := len(first.perCore)

	// --- viewmodel: one property per displayed value ---
	corePct := make([]*prop.Property[int], ncores)
	for i := range corePct {
		corePct[i] = prop.NewSource(0)
	}
	memPct := prop.NewSource(0)
	memLabel := prop.NewSource("")
	hist := prop.NewSource([]float64{})
	loadavg := prop.NewSource("")
	procs := prop.NewSource([]procInfo{})
	sortKey := prop.NewSource("cpu")
	statsP := prop.NewSource("")

	sorted := prop.NewComputed(func() []procInfo {
		ps := append([]procInfo(nil), procs.Get()...)
		if sortKey.Get() == "mem" {
			sort.Slice(ps, func(i, j int) bool { return ps[i].memMB > ps[j].memMB })
		} else {
			sort.Slice(ps, func(i, j int) bool { return ps[i].cpu > ps[j].cpu })
		}
		if len(ps) > 30 {
			ps = ps[:30]
		}
		return ps
	})
	tableTitle := prop.NewComputed(func() string {
		return fmt.Sprintf("processes by %s", sortKey.Get())
	})

	// --- tree ---
	host, _ := os.Hostname()
	gauges := make([]gooey.Widget, ncores)
	for i := range gauges {
		gauges[i] = &gauge{label: fmt.Sprintf("cpu%-2d", i), val: corePct[i]}
	}
	left := &gooey.VStack{Children: append(gauges,
		&gooey.Text{Content: gooey.Str("")},
		&gauge{label: "mem  ", val: memPct},
		&gooey.Text{Content: memLabel, Style: gooey.Sty(dim)},
	)}
	right := &gooey.VStack{Children: []gooey.Widget{
		&gooey.Text{Content: gooey.Str("total cpu"), Style: gooey.Sty(dim)},
		&sparkline{vals: hist, rows: 4},
		&gooey.Text{Content: gooey.Str("")},
		&gooey.Text{Content: loadavg, Style: gooey.Sty(dim)},
		&gooey.Text{Content: statsP, Style: gooey.Sty(dim)},
	}}
	root := &gooey.Border{Title: gooey.Str("sysmon — " + host), Style: gooey.Sty(render.Style{Fg: render.RGB(120, 90, 220)}), Child: &gooey.VStack{Children: []gooey.Widget{
		&gooey.HStack{Gap: 3, Children: []gooey.Widget{left, right}},
		&gooey.Text{Content: gooey.Str("")},
		&gooey.Text{Content: tableTitle, Style: gooey.Sty(accent)},
		&procTable{src: sorted},
	}}}

	screen, err := term.Open()
	if err != nil {
		fmt.Fprintln(os.Stderr, "no tty:", err)
		os.Exit(1)
	}
	cols, rows := screen.Size()

	needsFrame := true
	comp := gooey.NewComposer(root, cols, rows)
	comp.OnInvalidate(func() { needsFrame = true })

	if err := screen.Raw(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	defer screen.Restore()

	keys := make(chan byte, 8)
	go func() {
		buf := make([]byte, 1)
		for {
			if n, err := screen.File().Read(buf); err != nil {
				return
			} else if n > 0 {
				keys <- buf[0]
			}
		}
	}()

	tick := time.NewTicker(700 * time.Millisecond)
	defer tick.Stop()
	prev := first
	prevProcs := sampleProcs()
	frames, lastPainted := 0, 0

	for {
		if needsFrame {
			frames++
			statsP.Set(fmt.Sprintf("frames=%d  widgets painted last frame=%d", frames, lastPainted))
			_, lastPainted = comp.Frame()
			comp.Flush(screen.File())
			needsFrame = false
		}
		select {
		case <-tick.C:
			cur := sampleCPU()
			// Viewmodel-side dedup: only Set what actually changed, so
			// unchanged gauges never even dirty.
			for i := 0; i < ncores && i < len(cur.perCore); i++ {
				if p := int(pct(cur.perCore[i], prev.perCore[i])); p != corePct[i].Get() {
					corePct[i].Set(p)
				}
			}
			total := pct(cur.total, prev.total)
			h := append(append([]float64(nil), hist.Get()...), total)
			if len(h) > 60 {
				h = h[len(h)-60:]
			}
			hist.Set(h)
			usedMB, totalMB := sampleMem()
			if p := int(100 * usedMB / max(totalMB, 1)); p != memPct.Get() {
				memPct.Set(p)
			}
			memLabel.Set(fmt.Sprintf("%d / %d MB", usedMB, totalMB))
			loadavg.Set("load " + sampleLoad())
			curProcs := sampleProcs()
			procs.Set(diffProcs(prevProcs, curProcs, cur.total.totalJiffies()-prev.total.totalJiffies()))
			prev, prevProcs = cur, curProcs
		case k := <-keys:
			switch k {
			case 'q', 3:
				return
			case 'c':
				sortKey.Set("cpu")
			case 'm':
				sortKey.Set("mem")
			}
		}
	}
}

// ---- custom widgets ----

type gauge struct {
	label  string
	val    *prop.Property[int]
	bounds gooey.Rect
}

func (g *gauge) Measure(avail gooey.Size) gooey.Size { return gooey.Size{W: min(34, avail.W), H: 1} }
func (g *gauge) Arrange(b gooey.Rect)                { g.bounds = b }
func (g *gauge) Bounds() gooey.Rect                  { return g.bounds }

func (g *gauge) Render(f *gooey.Frame) {
	v := g.val.Get()
	style := good
	if v >= 80 {
		style = crit
	} else if v >= 50 {
		style = warn
	}
	w := g.bounds.W - len(g.label) - 6
	fill := v * w / 100
	var sb strings.Builder
	for i := 0; i < w; i++ {
		if i < fill {
			sb.WriteRune('█')
		} else {
			sb.WriteRune('░')
		}
	}
	f.Cells.SetString(g.bounds.X, g.bounds.Y, g.label, dim)
	f.Cells.SetString(g.bounds.X+len(g.label), g.bounds.Y, sb.String(), style)
	f.Cells.SetString(g.bounds.X+len(g.label)+w, g.bounds.Y, fmt.Sprintf(" %3d%%", v), style)
}

type sparkline struct {
	vals   *prop.Property[[]float64]
	rows   int
	bounds gooey.Rect
}

var sparks = []rune(" ▁▂▃▄▅▆▇█")

func (s *sparkline) Measure(avail gooey.Size) gooey.Size {
	return gooey.Size{W: min(40, avail.W), H: s.rows}
}
func (s *sparkline) Arrange(b gooey.Rect) { s.bounds = b }
func (s *sparkline) Bounds() gooey.Rect   { return s.bounds }

func (s *sparkline) Render(f *gooey.Frame) {
	vs := s.vals.Get()
	if len(vs) > s.bounds.W {
		vs = vs[len(vs)-s.bounds.W:]
	}
	for i, v := range vs {
		// v is 0..100; split across rows, bottom-up.
		level := v / 100 * float64(s.rows) // 0..rows
		style := good
		if v >= 80 {
			style = crit
		} else if v >= 50 {
			style = warn
		}
		for r := 0; r < s.rows; r++ {
			frac := level - float64(s.rows-1-r)
			ch := sparks[0]
			if frac >= 1 {
				ch = sparks[8]
			} else if frac > 0 {
				ch = sparks[int(frac*8)]
			}
			f.Cells.Set(s.bounds.X+i, s.bounds.Y+r, ch, style)
		}
	}
}

type procInfo struct {
	pid   int
	comm  string
	cpu   float64
	memMB int
}

type procTable struct {
	src    *prop.Property[[]procInfo]
	bounds gooey.Rect
}

func (t *procTable) Measure(avail gooey.Size) gooey.Size { return avail }
func (t *procTable) Arrange(b gooey.Rect)                { t.bounds = b }
func (t *procTable) Bounds() gooey.Rect                  { return t.bounds }

func (t *procTable) Render(f *gooey.Frame) {
	header := fmt.Sprintf("%7s  %-24s %6s %9s", "PID", "COMMAND", "CPU%", "MEM MB")
	f.Cells.SetString(t.bounds.X, t.bounds.Y, header, render.Style{Bold: true, Underline: true})
	ps := t.src.Get()
	for i, p := range ps {
		if i+1 >= t.bounds.H {
			break
		}
		style := render.Style{}
		if p.cpu >= 50 {
			style = warn
		}
		if i == 0 {
			style.Bold = true
		}
		row := fmt.Sprintf("%7d  %-24s %6.1f %9d", p.pid, clip(p.comm, 24), p.cpu, p.memMB)
		f.Cells.SetString(t.bounds.X, t.bounds.Y+1+i, clip(row, t.bounds.W), style)
	}
}

func clip(s string, w int) string {
	if len(s) > w {
		return s[:w]
	}
	return s
}

// ---- /proc sampling ----

type cpuTimes struct{ user, nice, system, idle, iowait, irq, softirq, steal float64 }

func (c cpuTimes) totalJiffies() float64 {
	return c.user + c.nice + c.system + c.idle + c.iowait + c.irq + c.softirq + c.steal
}
func (c cpuTimes) busy() float64 { return c.totalJiffies() - c.idle - c.iowait }

type cpuSample struct {
	total   cpuTimes
	perCore []cpuTimes
}

func pct(cur, prev cpuTimes) float64 {
	dt := cur.totalJiffies() - prev.totalJiffies()
	if dt <= 0 {
		return 0
	}
	p := 100 * (cur.busy() - prev.busy()) / dt
	return max(0, min(100, p))
}

func sampleCPU() cpuSample {
	data, _ := os.ReadFile("/proc/stat")
	var s cpuSample
	for _, ln := range strings.Split(string(data), "\n") {
		if !strings.HasPrefix(ln, "cpu") {
			continue
		}
		fs := strings.Fields(ln)
		var c cpuTimes
		vals := []*float64{&c.user, &c.nice, &c.system, &c.idle, &c.iowait, &c.irq, &c.softirq, &c.steal}
		for i, v := range vals {
			if i+1 < len(fs) {
				*v, _ = strconv.ParseFloat(fs[i+1], 64)
			}
		}
		if fs[0] == "cpu" {
			s.total = c
		} else {
			s.perCore = append(s.perCore, c)
		}
	}
	return s
}

func sampleMem() (usedMB, totalMB int) {
	data, _ := os.ReadFile("/proc/meminfo")
	var total, avail int
	for _, ln := range strings.Split(string(data), "\n") {
		fs := strings.Fields(ln)
		if len(fs) < 2 {
			continue
		}
		kb, _ := strconv.Atoi(fs[1])
		switch fs[0] {
		case "MemTotal:":
			total = kb
		case "MemAvailable:":
			avail = kb
		}
	}
	return (total - avail) / 1024, total / 1024
}

func sampleLoad() string {
	data, _ := os.ReadFile("/proc/loadavg")
	fs := strings.Fields(string(data))
	if len(fs) >= 3 {
		return strings.Join(fs[:3], " ")
	}
	return "?"
}

func sampleProcs() map[int][2]float64 { // pid → {jiffies, rssMB}
	out := map[int][2]float64{}
	entries, _ := os.ReadDir("/proc")
	for _, e := range entries {
		pid, err := strconv.Atoi(e.Name())
		if err != nil {
			continue
		}
		data, err := os.ReadFile("/proc/" + e.Name() + "/stat")
		if err != nil {
			continue
		}
		// comm is in parens and may contain spaces: parse after last ')'.
		s := string(data)
		i := strings.LastIndexByte(s, ')')
		if i < 0 {
			continue
		}
		fs := strings.Fields(s[i+1:])
		if len(fs) < 13 {
			continue
		}
		utime, _ := strconv.ParseFloat(fs[11], 64)
		stime, _ := strconv.ParseFloat(fs[12], 64)
		rss := 0.0
		if len(fs) >= 22 {
			r, _ := strconv.ParseFloat(fs[21], 64)
			rss = r * 4096 / 1024 / 1024
		}
		out[pid] = [2]float64{utime + stime, rss}
	}
	return out
}

var commCache = map[int]string{}

func commOf(pid int) string {
	if c, ok := commCache[pid]; ok {
		return c
	}
	data, _ := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/comm")
	c := strings.TrimSpace(string(data))
	if c == "" {
		c = "?"
	}
	commCache[pid] = c
	return c
}

func diffProcs(prev, cur map[int][2]float64, dtJiffies float64) []procInfo {
	if dtJiffies <= 0 {
		return nil
	}
	var ps []procInfo
	ncores := float64(len(sampleCPU().perCore))
	for pid, c := range cur {
		p, ok := prev[pid]
		if !ok {
			continue
		}
		cpu := 100 * (c[0] - p[0]) / dtJiffies * ncores
		if cpu < 0.05 && c[1] < 10 {
			continue // noise
		}
		ps = append(ps, procInfo{pid: pid, comm: commOf(pid), cpu: cpu, memMB: int(c[1])})
	}
	return ps
}
