// Package gpu reads what the card reports: total, used and free memory, and
// the processes holding it. Two probes implement Probe: NvidiaSMI shells out to
// nvidia-smi (NVML, no CUDA context) and AMDProbe reads amdgpu's sysfs figures
// plus /proc/<pid>/fdinfo. Neither opens a driver handle, so probing costs no
// VRAM. Detect picks one from the card the kernel exposes.
package gpu

import (
	"context"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

const MiB = 1 << 20

// Proc is one compute process holding memory on the card.
type Proc struct {
	PID  int
	Name string
	Used uint64 // bytes
}

// Device is one card as the driver reports it. Telemetry the driver does
// not report ("[N/A]") reads -1, never a made-up zero.
type Device struct {
	Index int
	Name  string
	Total uint64
	Used  uint64
	Free  uint64

	Util       float64 // %
	Temp       float64 // C
	Power      float64 // W
	PowerLimit float64 // W
	Fan        float64 // %
	ClockSM    float64 // MHz
	ClockMem   float64 // MHz
	PCIeGen    float64
	PCIeWidth  float64
}

// Query is the per-card field list, memory first.
const Query = "index,name,memory.total,memory.used,memory.free,utilization.gpu,temperature.gpu,power.draw,power.limit,fan.speed,clocks.sm,clocks.mem,pcie.link.gen.current,pcie.link.width.current"

// Snapshot is one reading: the scheduled card in full, every card's memory
// in Devices, so a multi-GPU box is inventoried with its real limits.
type Snapshot struct {
	Devices []Device
	Index   int
	Name    string
	Total   uint64
	Used    uint64
	Free    uint64
	Procs   []Proc
	At      time.Time
}

// Baseline is memory in use that no compute process accounts for: the
// desktop, the driver, graphics clients nvidia-smi does not list.
func (s Snapshot) Baseline() uint64 {
	var procs uint64
	for _, p := range s.Procs {
		procs += p.Used
	}
	if procs >= s.Used {
		return 0
	}
	return s.Used - procs
}

// Probe reads the card.
type Probe interface {
	Read(ctx context.Context) (Snapshot, error)
}

// NvidiaSMI probes one GPU by index through nvidia-smi.
type NvidiaSMI struct {
	Index int
	Bin   string // default "nvidia-smi"
}

func (n NvidiaSMI) bin() string {
	if n.Bin == "" {
		return "nvidia-smi"
	}
	return n.Bin
}

func (n NvidiaSMI) run(ctx context.Context, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, n.bin(), args...).Output()
	if err != nil {
		return "", fmt.Errorf("%s %s: %w", n.bin(), strings.Join(args, " "), err)
	}
	return string(out), nil
}

func (n NvidiaSMI) Read(ctx context.Context) (Snapshot, error) {
	mem, err := n.run(ctx, "--query-gpu="+Query, "--format=csv,noheader,nounits")
	if err != nil {
		return Snapshot{}, err
	}
	apps, err := n.run(ctx, "-i", strconv.Itoa(n.Index), "--query-compute-apps=pid,process_name,used_memory", "--format=csv,noheader,nounits")
	if err != nil {
		return Snapshot{}, err
	}
	return Parse(n.Index, mem, apps, time.Now())
}

// ParseDevices reads "index, name, total, used, free[, telemetry...]" lines
// (memory in MiB), one per card, in the order of Query.
func ParseDevices(mem string) ([]Device, error) {
	var out []Device
	for _, line := range strings.Split(mem, "\n") {
		if line = strings.TrimSpace(line); line == "" {
			continue
		}
		f := splitCSV(line)
		if len(f) != 5 && len(f) != 14 {
			return nil, fmt.Errorf("gpu query: want 5 or 14 fields, got %q", line)
		}
		idx, err := strconv.Atoi(f[0])
		if err != nil {
			return nil, fmt.Errorf("memory query index %q: %w", f[0], err)
		}
		d := Device{Index: idx, Name: f[1]}
		for i, dst := range []*uint64{&d.Total, &d.Used, &d.Free} {
			n, err := strconv.ParseUint(f[2+i], 10, 64)
			if err != nil {
				return nil, fmt.Errorf("memory query %q field %d: %w", line, 2+i, err)
			}
			*dst = n * MiB
		}
		tel := []*float64{&d.Util, &d.Temp, &d.Power, &d.PowerLimit, &d.Fan, &d.ClockSM, &d.ClockMem, &d.PCIeGen, &d.PCIeWidth}
		for i, dst := range tel {
			*dst = -1
			if len(f) == 14 {
				if v, err := strconv.ParseFloat(f[5+i], 64); err == nil {
					*dst = v
				}
			}
		}
		out = append(out, d)
	}
	return out, nil
}

// Parse builds the snapshot for the card at index from nvidia-smi's
// csv,noheader,nounits output: mem lists every card, apps that card's processes.
func Parse(index int, mem, apps string, at time.Time) (Snapshot, error) {
	s := Snapshot{At: at}
	devs, err := ParseDevices(mem)
	if err != nil {
		return s, err
	}
	s.Devices = devs
	found := false
	for _, d := range devs {
		if d.Index == index {
			s.Index, s.Name, s.Total, s.Used, s.Free = d.Index, d.Name, d.Total, d.Used, d.Free
			found = true
		}
	}
	if !found {
		return s, fmt.Errorf("no GPU with index %d (found %d)", index, len(devs))
	}

	for _, line := range strings.Split(apps, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "No running") {
			continue
		}
		// The name may itself contain commas: the PID is before the first
		// one and the memory figure after the last.
		i, j := strings.IndexByte(line, ','), strings.LastIndexByte(line, ',')
		if i < 0 || i == j {
			return s, fmt.Errorf("compute-apps line %q", line)
		}
		pid, err := strconv.Atoi(strings.TrimSpace(line[:i]))
		if err != nil {
			return s, fmt.Errorf("compute-apps pid in %q: %w", line, err)
		}
		used, err := strconv.ParseUint(strings.TrimSpace(line[j+1:]), 10, 64)
		if err != nil {
			// "[N/A]" on some drivers and in some containers.
			used = 0
		}
		s.Procs = append(s.Procs, Proc{PID: pid, Name: strings.TrimSpace(line[i+1 : j]), Used: used * MiB})
	}
	return s, nil
}

func splitCSV(line string) []string {
	parts := strings.Split(line, ",")
	for i := range parts {
		parts[i] = strings.TrimSpace(parts[i])
	}
	return parts
}
