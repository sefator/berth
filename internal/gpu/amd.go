package gpu

// amd.go reads an AMD card the way the nvidia-smi probe reads an NVIDIA one:
// what the card reports, used and free memory, and the processes holding it.
//
// Sources, all read-only and none requiring a driver handle, so probing costs
// no VRAM:
//
//	total/used   /sys/class/drm/cardN/device/mem_info_vram_{total,used}
//	utilization  .../gpu_busy_percent
//	temp/power   .../hwmon/hwmonM/{temp1_input,power1_average,power1_cap}
//	fan          .../hwmon/hwmonM/{fan1_input,fan1_max}   (RPM -> %)
//	clocks       .../hwmon/hwmonM/{freq1_input,freq2_input} (Hz -> MHz)
//	PCIe         .../current_link_speed, .../current_link_width
//	processes    /proc/<pid>/fdinfo/*: the amdgpu driver writes drm-pdev and
//	             drm-memory-vram per DRM client, which is per-process VRAM.

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

const amdVendor = "0x1002"

// minProcBytes drops DRM clients that hold a rounding error of memory (a
// browser renderer that opened the card but allocates nothing): they would
// otherwise be reported as stowaways with 0 MiB.
const minProcBytes = 1 << 20

// AMDProbe reads one amdgpu card. Index is the ordinal among the AMD cards
// (0 = the first), which is what --gpu means on an NVIDIA box too; the sysfs
// cardN is an internal detail because it also counts cards that are not GPUs.
type AMDProbe struct {
	Index int
	Root  string // sysfs root; default /sys
	Proc  string // procfs root; default /proc
}

func (a AMDProbe) root() string {
	if a.Root == "" {
		return "/sys"
	}
	return a.Root
}

func (a AMDProbe) procRoot() string {
	if a.Proc == "" {
		return "/proc"
	}
	return a.Proc
}

// cards lists the /sys/class/drm card numbers of the AMD GPUs, ascending. The
// probe's Index is a position in this list.
func (a AMDProbe) cards() ([]int, error) {
	entries, err := os.ReadDir(filepath.Join(a.root(), "class", "drm"))
	if err != nil {
		return nil, fmt.Errorf("read drm class: %w", err)
	}
	var out []int
	for _, e := range entries {
		n, ok := cardNumber(e.Name())
		if !ok {
			continue
		}
		if readFile(filepath.Join(a.root(), "class", "drm", e.Name(), "device", "vendor")) == amdVendor {
			out = append(out, n)
		}
	}
	sort.Ints(out)
	if len(out) == 0 {
		return nil, fmt.Errorf("no AMD (amdgpu) card found under %s", filepath.Join(a.root(), "class", "drm"))
	}
	return out, nil
}

// cardNumber accepts "card1" and rejects "card1-DP-1" and "renderD128".
func cardNumber(name string) (int, bool) {
	if !strings.HasPrefix(name, "card") {
		return 0, false
	}
	n, err := strconv.Atoi(strings.TrimPrefix(name, "card"))
	if err != nil || n < 0 {
		return 0, false
	}
	return n, true
}

func (a AMDProbe) devDir(cardN int) string {
	return filepath.Join(a.root(), "class", "drm", fmt.Sprintf("card%d", cardN), "device")
}

// Read returns the scheduled card, every AMD card's limits, and the DRM clients
// holding memory on the scheduled one.
func (a AMDProbe) Read(ctx context.Context) (Snapshot, error) {
	s := Snapshot{At: time.Now()}
	devs, err := a.devices()
	if err != nil {
		return s, err
	}
	s.Devices = devs
	if a.Index < 0 || a.Index >= len(devs) {
		return s, fmt.Errorf("no AMD GPU with index %d (found %d)", a.Index, len(devs))
	}
	d := devs[a.Index]
	s.Index, s.Name, s.Total, s.Used, s.Free = d.Index, d.Name, d.Total, d.Used, d.Free

	ns, err := a.cards()
	if err != nil {
		return s, err
	}
	if err := ctx.Err(); err != nil {
		return s, err
	}
	procs, err := a.procs(ns[a.Index])
	if err != nil {
		return s, err
	}
	s.Procs = procs
	return s, nil
}

// devices lists every AMD card, indexed by ordinal among the AMD cards.
func (a AMDProbe) devices() ([]Device, error) {
	ns, err := a.cards()
	if err != nil {
		return nil, err
	}
	out := make([]Device, 0, len(ns))
	for i, n := range ns {
		d, err := a.device(n)
		if err != nil {
			continue // a card we cannot read is not a card we can schedule
		}
		d.Index = i
		out = append(out, d)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("no readable AMD card")
	}
	return out, nil
}

func (a AMDProbe) device(cardN int) (Device, error) {
	dir := a.devDir(cardN)
	d := Device{Name: cardName(dir)}
	// Telemetry the driver does not report reads -1, never a made-up zero.
	d.Util, d.Temp, d.Power, d.PowerLimit = -1, -1, -1, -1
	d.Fan, d.ClockSM, d.ClockMem, d.PCIeGen, d.PCIeWidth = -1, -1, -1, -1, -1

	total, okT := readUint(filepath.Join(dir, "mem_info_vram_total"))
	used, okU := readUint(filepath.Join(dir, "mem_info_vram_used"))
	if !okT || !okU {
		return d, fmt.Errorf("card%d: no VRAM figures in %s", cardN, dir)
	}
	d.Total, d.Used = total, used
	if total >= used {
		d.Free = total - used
	}
	if v, ok := readUint(filepath.Join(dir, "gpu_busy_percent")); ok {
		d.Util = float64(v)
	}
	if h := hwmonDir(dir); h != "" {
		if v, ok := readUint(filepath.Join(h, "temp1_input")); ok { // millidegrees C
			d.Temp = float64(v) / 1000
		}
		if v, ok := readUint(filepath.Join(h, "power1_average")); ok { // microwatts
			d.Power = float64(v) / 1e6
		}
		if v, ok := readUint(filepath.Join(h, "power1_cap")); ok {
			d.PowerLimit = float64(v) / 1e6
		}
		if v, ok := readUint(filepath.Join(h, "fan1_input")); ok { // RPM
			if max, ok := readUint(filepath.Join(h, "fan1_max")); ok && max > 0 {
				d.Fan = 100 * float64(v) / float64(max)
			}
		}
		if v, ok := readUint(filepath.Join(h, "freq1_input")); ok { // Hz: sclk
			d.ClockSM = float64(v) / 1e6
		}
		if v, ok := readUint(filepath.Join(h, "freq2_input")); ok { // Hz: mclk
			d.ClockMem = float64(v) / 1e6
		}
	}
	if v := readFile(filepath.Join(dir, "current_link_speed")); v != "" {
		d.PCIeGen = pcieGen(v)
	}
	if v, ok := readUint(filepath.Join(dir, "current_link_width")); ok {
		d.PCIeWidth = float64(v)
	}
	return d, nil
}

// procs attributes VRAM to processes through fdinfo. One process can hold
// several DRM clients (contexts), and several file descriptors can carry the
// same client, so the figures are summed per PID and de-duplicated by
// drm-client-id.
func (a AMDProbe) procs(cardN int) ([]Proc, error) {
	pdev := pdevOf(a.devDir(cardN))
	if pdev == "" {
		return nil, fmt.Errorf("card%d: cannot resolve the PCI address", cardN)
	}
	used := map[int]uint64{}
	seen := map[int]map[string]bool{}
	ents, err := os.ReadDir(a.procRoot())
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", a.procRoot(), err)
	}
	for _, e := range ents {
		pid, err := strconv.Atoi(e.Name())
		if err != nil {
			continue
		}
		dir := filepath.Join(a.procRoot(), e.Name(), "fdinfo")
		fds, err := os.ReadDir(dir)
		if err != nil {
			continue // the process is gone, or not ours to read
		}
		for _, fd := range fds {
			data, err := os.ReadFile(filepath.Join(dir, fd.Name()))
			if err != nil {
				continue
			}
			var client, mem uint64
			var match bool
			var haveClient, haveMem bool
			for _, line := range strings.Split(string(data), "\n") {
				switch {
				case strings.HasPrefix(line, "drm-pdev:"):
					match = strings.TrimSpace(strings.TrimPrefix(line, "drm-pdev:")) == pdev
				case strings.HasPrefix(line, "drm-client-id:"):
					v := strings.TrimSpace(strings.TrimPrefix(line, "drm-client-id:"))
					client, err = strconv.ParseUint(v, 10, 64)
					haveClient = err == nil
				case strings.HasPrefix(line, "drm-memory-vram:"):
					v := strings.Fields(strings.TrimPrefix(line, "drm-memory-vram:"))
					if len(v) > 0 {
						mem, err = strconv.ParseUint(v[0], 10, 64) // KiB
						haveMem = err == nil
					}
				}
			}
			if !match || !haveMem || mem == 0 {
				continue
			}
			if haveClient {
				if seen[pid] == nil {
					seen[pid] = map[string]bool{}
				}
				key := strconv.FormatUint(client, 10)
				if seen[pid][key] {
					continue // the same client through another fd
				}
				seen[pid][key] = true
			}
			used[pid] += mem * 1024 // KiB -> bytes
		}
	}
	out := make([]Proc, 0, len(used))
	for pid, bytes := range used {
		if bytes < minProcBytes {
			continue
		}
		out = append(out, Proc{PID: pid, Name: procName(a.procRoot(), pid), Used: bytes})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Used != out[j].Used {
			return out[i].Used > out[j].Used
		}
		return out[i].PID < out[j].PID
	})
	return out, nil
}

// AMDStream caches the probe between polls. The sysfs figures are cheap, but
// the /proc walk is not, so processes are re-listed only when the card's used
// memory moves or AppsEvery passes — the rule the nvidia stream uses. There is
// no external process to follow on AMD, so Run only keeps the contract the
// daemon expects.
type AMDStream struct {
	Index     int
	Root      string
	Proc      string
	Interval  time.Duration // unused for polling; kept for the daemon's shape
	AppsEvery time.Duration // longest gap between process lists; default 10 s

	mu       sync.Mutex
	procs    []Proc
	procsAt  time.Time
	procsFor uint64
}

// Run blocks until ctx ends. Kept so the daemon can start every probe the same
// way; the AMD probe has no long-lived helper process to follow.
func (s *AMDStream) Run(ctx context.Context) { <-ctx.Done() }

func (s *AMDStream) Read(ctx context.Context) (Snapshot, error) {
	a := AMDProbe{Index: s.Index, Root: s.Root, Proc: s.Proc}
	snap := Snapshot{At: time.Now()}
	devs, err := a.devices()
	if err != nil {
		return snap, err
	}
	snap.Devices = devs
	if s.Index < 0 || s.Index >= len(devs) {
		return snap, fmt.Errorf("no AMD GPU with index %d (found %d)", s.Index, len(devs))
	}
	d := devs[s.Index]
	snap.Index, snap.Name, snap.Total, snap.Used, snap.Free = d.Index, d.Name, d.Total, d.Used, d.Free

	appsEvery := s.AppsEvery
	if appsEvery <= 0 {
		appsEvery = 10 * time.Second
	}
	s.mu.Lock()
	stale := s.procsAt.IsZero() || time.Since(s.procsAt) >= appsEvery || s.procsFor != d.Used
	procs := s.procs
	s.mu.Unlock()
	if stale {
		ns, err := a.cards()
		if err != nil {
			return snap, err
		}
		if err := ctx.Err(); err != nil {
			return snap, err
		}
		procs, err = a.procs(ns[s.Index])
		if err != nil {
			return snap, err
		}
		s.mu.Lock()
		s.procs, s.procsAt, s.procsFor = procs, time.Now(), d.Used
		s.mu.Unlock()
	}
	snap.Procs = procs
	return snap, nil
}

// Detect reports the vendor of the first card the kernel exposes as a DRM card:
// "nvidia", "amd", or "" when neither is found. The daemon picks its probe from
// this, so a box with both cards keeps nvidia-smi as before.
func Detect(root string) string {
	if root == "" {
		root = "/sys"
	}
	entries, err := os.ReadDir(filepath.Join(root, "class", "drm"))
	if err != nil {
		return ""
	}
	var amd bool
	for _, e := range entries {
		if _, ok := cardNumber(e.Name()); !ok {
			continue
		}
		switch readFile(filepath.Join(root, "class", "drm", e.Name(), "device", "vendor")) {
		case "0x10de":
			return "nvidia"
		case amdVendor:
			amd = true
		}
	}
	if amd {
		return "amd"
	}
	return ""
}

func readFile(p string) string {
	b, err := os.ReadFile(p)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}

func readUint(p string) (uint64, bool) {
	s := readFile(p)
	if s == "" {
		return 0, false
	}
	v, err := strconv.ParseUint(s, 10, 64)
	if err != nil {
		return 0, false
	}
	return v, true
}

// hwmonDir finds the hwmon node the amdgpu driver registered for this card.
func hwmonDir(devDir string) string {
	dirs, err := filepath.Glob(filepath.Join(devDir, "hwmon", "hwmon*"))
	if err != nil || len(dirs) == 0 {
		return ""
	}
	for _, d := range dirs {
		if readFile(filepath.Join(d, "name")) == "amdgpu" {
			return d
		}
	}
	return dirs[0]
}

// cardName is whatever the driver publishes; amdgpu usually publishes nothing,
// so fall back to the PCI id rather than inventing a marketing name.
func cardName(devDir string) string {
	if s := readFile(filepath.Join(devDir, "product_name")); s != "" {
		return s
	}
	vendor := strings.TrimPrefix(readFile(filepath.Join(devDir, "vendor")), "0x")
	dev := strings.TrimPrefix(readFile(filepath.Join(devDir, "device")), "0x")
	if dev == "" {
		return "AMD GPU"
	}
	return "AMD " + vendor + ":" + dev
}

// pdevOf resolves a card's PCI address (0000:08:00.0) from its device symlink.
func pdevOf(devDir string) string {
	real, err := filepath.EvalSymlinks(devDir)
	if err != nil {
		return ""
	}
	return filepath.Base(real)
}

// pcieGen turns "16.0 GT/s PCIe" into 4. Unknown speeds read -1.
func pcieGen(s string) float64 {
	f := strings.Fields(s)
	if len(f) == 0 {
		return -1
	}
	gt, err := strconv.ParseFloat(f[0], 64)
	if err != nil {
		return -1
	}
	switch {
	case gt >= 64:
		return 6
	case gt >= 32:
		return 5
	case gt >= 16:
		return 4
	case gt >= 8:
		return 3
	case gt >= 5:
		return 2
	case gt >= 2.5:
		return 1
	}
	return -1
}

// procName is the short command name, as nvidia-smi reports it.
func procName(procRoot string, pid int) string {
	if s := readFile(filepath.Join(procRoot, strconv.Itoa(pid), "comm")); s != "" {
		return s
	}
	return "?"
}
