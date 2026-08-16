// sysmon is a live system monitor over real /proc data — the "better
// than logs" component-model demo. Every value flows through dependency
// properties, so a sample tick repaints only the components whose
// (rounded) values actually changed — watch the "painted" counter sit
// near zero on an idle system and spike under load.
//
//	c / m   sort process table by CPU / memory
//	q       quit
//
// The screen itself is sysmon.gooey, embedded: the layout, the styles,
// the four key gestures and the 700ms sample clock are all declared
// there. What is left in this file is what markup has no way to say —
// /proc parsing, the viewmodel those samples Set, the row projection,
// and one registered element for the per-core gauges, whose COUNT is
// only known once /proc/stat has been read.
package main

import (
	"context"
	"embed"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"

	"github.com/WonderForgeLabs/gooey"
	"github.com/WonderForgeLabs/gooey/components"
	"github.com/WonderForgeLabs/gooey/markup"
	"github.com/WonderForgeLabs/gooey/prop"
	"github.com/WonderForgeLabs/gooey/render"
)

// The page ships in the binary: sysmon has no hot-reload story, and an
// embed.FS is the same fs.FS seam markuplog fills with os.DirFS — so the
// demo runs from any working directory.
//
//go:embed sysmon.gooey
var pageFS embed.FS

// warn is the one style left in Go: it is picked per ROW, from the
// sampled CPU number, inside the projection. Markup has no style
// triggers yet, so a threshold cannot be declared.
var warn = render.Style{Fg: render.RGB(230, 190, 80)}

func main() {
	var app *gooey.App
	ctx, statsP := newContext(gooey.Command(func() { app.Quit() }))

	// No graphics probe — sysmon is cell-only — and no mouse: the page
	// has no focus stop and nothing to click. The color depth still comes
	// from the environment (App's default), so the gauges downsample
	// correctly on a 16-color terminal for the cost of one getenv.
	app = gooey.NewApp(markup.Page(pageFS, "sysmon.gooey", ctx), gooey.WithoutMouse())
	app.BeforeFrame(func() {
		statsP.Set(fmt.Sprintf("frames=%d  components painted last frame=%d", app.Frames(), app.PaintedLastFrame()))
	})
	if err := app.Run(context.Background()); err != nil {
		gooey.Exit(err)
	}
}

// newContext builds the viewmodel and the binding registry sysmon.gooey
// resolves against, and hands back the stats handle the run loop writes.
//
// It is a function rather than a block inside main so that the page can
// be LOADED in a test: every binding in the file, every style name, every
// gesture and the <CoreGauges> builder resolve here, and nothing else in
// the repo compiles that file.
func newContext(quit gooey.Action) (*markup.Context, *prop.Property[string]) {
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
	// The table's rows, projected for an ItemsView. Built inside a
	// computed because a row's LOOK depends on its rank (the top process
	// is bold), and rank is a property of the sorted slice, not of the
	// item — the ItemsOf pattern. The payoff over the old hand-rolled
	// table is damage: a tick that reorders nothing and changes two
	// numbers repaints two rows, not the whole table.
	tableRows := prop.NewComputed(func() components.ItemSource {
		ps := sorted.Get()
		rows := make([]procRow, len(ps))
		for i, p := range ps {
			style := render.Style{}
			if p.cpu >= 50 {
				style = warn
			}
			if i == 0 {
				style.Bold = true
			}
			rows[i] = procRow{
				text:  fmt.Sprintf("%7d  %-24s %6.1f %9d", p.pid, clip(p.comm, 24), p.cpu, p.memMB),
				style: style,
			}
		}
		return components.ItemsOf(rows, func(r procRow) map[string]any {
			return map[string]any{"Row": r.text, "Style": r.style}
		})
	})

	// --- the sample tick, the one command with real work behind it.
	// <Timer Tick="{{.Sample}}"/> resolves to this at load time, and the
	// Composer runs it on the UI goroutine, so it may Set freely.
	prev := first
	prevProcs := sampleProcs()
	sample := gooey.Command(func() {
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
	})

	// --- markup context: the binding registry the page resolves against ---
	host, _ := os.Hostname()
	return &markup.Context{
		Values: map[string]any{
			"Host":     host,
			"CorePct":  corePct,
			"MemPct":   memPct,
			"MemLabel": memLabel,
			"Hist":     hist,
			"LoadAvg":  loadavg,
			"Stats":    statsP,

			"TableTitle": tableTitle,
			// A literal in the page would lose its four leading spaces —
			// <Text>'s body is trimmed — so the column header comes
			// through the binding registry as a plain string.
			"TableHeader": fmt.Sprintf("%7s  %-24s %6s %9s", "PID", "COMMAND", "CPU%", "MEM MB"),
			"TableRows":   tableRows,

			"Sample":    sample,
			"SortByCPU": gooey.Command(func() { sortKey.Set("cpu") }),
			"SortByMem": gooey.Command(func() { sortKey.Set("mem") }),
			"Quit":      quit,
		},
		Styles: map[string]render.Style{
			"panel":  {Fg: render.RGB(120, 90, 220)},
			"accent": {Fg: render.RGB(255, 170, 60), Bold: true},
			"dim":    {Fg: render.RGB(140, 140, 150)},
			"header": {Bold: true, Underline: true},
		},
		Components: map[string]markup.Builder{
			// The gauge column. Its length is len(/proc/stat's cpuN
			// lines), which no markup element can range over, so the page
			// declares WHERE the column goes and this builder fills it —
			// the same hand-off markuplog's <LogPane Lines="…"/> makes.
			"CoreGauges": func(e markup.Element, c *markup.Context) (gooey.Component, error) {
				v, err := c.BindingValue(e.Attrs["Values"])
				if err != nil {
					return nil, fmt.Errorf("CoreGauges Values: %w", err)
				}
				vals, ok := v.([]*prop.Property[int])
				if !ok {
					return nil, fmt.Errorf("CoreGauges Values: got %T, want []*prop.Property[int]", v)
				}
				gauges := make([]gooey.Component, len(vals))
				for i, p := range vals {
					gauges[i] = &components.Gauge{Label: components.Str(fmt.Sprintf("cpu%-2d", i)), Value: p}
				}
				return &components.VStack{Children: gauges}, nil
			},
		},
	}, statsP
}

// ---- what a row is ----
//
// The Gauge and Sparkline that used to live here are now framework
// built-ins (components.Gauge, components.Sparkline) — they were written here,
// proved here, and promoted once their shape stopped moving. The process
// table followed: its rows ride components.ItemsView now, and the row
// TEMPLATE is <ItemsView.ItemTemplate> in the page. Only the projection
// above — what a process row SAYS, and what colour that makes it — is
// still this demo's own. Full column machinery (sortable headers,
// per-column widths) is the DataGrid epic, not this table.

type procInfo struct {
	pid   int
	comm  string
	cpu   float64
	memMB int
}

// procRow is one projected table row: text and rank-aware style.
type procRow struct {
	text  string
	style render.Style
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
