package gpu

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestPCIeGen(t *testing.T) {
	cases := []struct {
		in   string
		want float64
	}{
		{"2.5 GT/s PCIe", 1}, {"5.0 GT/s PCIe", 2}, {"8.0 GT/s PCIe", 3},
		{"16.0 GT/s PCIe", 4}, {"32.0 GT/s PCIe", 5}, {"64.0 GT/s PCIe", 6},
		{"", -1}, {"unknown", -1},
	}
	for _, c := range cases {
		if got := pcieGen(c.in); got != c.want {
			t.Errorf("pcieGen(%q) = %v, want %v", c.in, got, c.want)
		}
	}
}

// writeFile creates a sysfs- or procfs-like file with its parent directories.
func writeFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// TestAMDProbeFixture reads a synthetic amdgpu card: no GPU and no root needed,
// which is how the AMD path stays testable on CI.
func TestAMDProbeFixture(t *testing.T) {
	root := t.TempDir()
	proc := t.TempDir()
	const pdev = "0000:00:08.0"

	dev := filepath.Join(root, "devices", "pci0000:00", pdev)
	writeFile(t, filepath.Join(dev, "vendor"), "0x1002\n")
	writeFile(t, filepath.Join(dev, "device"), "0x744c\n")
	writeFile(t, filepath.Join(dev, "mem_info_vram_total"), "25753026560\n")
	writeFile(t, filepath.Join(dev, "mem_info_vram_used"), "10000000000\n")
	writeFile(t, filepath.Join(dev, "gpu_busy_percent"), "37\n")
	writeFile(t, filepath.Join(dev, "current_link_speed"), "16.0 GT/s PCIe\n")
	writeFile(t, filepath.Join(dev, "current_link_width"), "16\n")
	h := filepath.Join(dev, "hwmon", "hwmon3")
	writeFile(t, filepath.Join(h, "name"), "amdgpu\n")
	writeFile(t, filepath.Join(h, "temp1_input"), "62000\n")
	writeFile(t, filepath.Join(h, "power1_average"), "76000000\n")
	writeFile(t, filepath.Join(h, "power1_cap"), "339000000\n")
	writeFile(t, filepath.Join(h, "fan1_input"), "800\n")
	writeFile(t, filepath.Join(h, "fan1_max"), "3200\n")
	writeFile(t, filepath.Join(h, "freq1_input"), "1306000000\n")
	writeFile(t, filepath.Join(h, "freq2_input"), "1249000000\n")

	// card0/device is a symlink to the PCI device, as in a real sysfs tree: the
	// probe reads the PCI address from its resolved basename.
	if err := os.MkdirAll(filepath.Join(root, "class", "drm", "card0"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(dev, filepath.Join(root, "class", "drm", "card0", "device")); err != nil {
		t.Fatal(err)
	}

	// One process with two fds carrying the same DRM client: counted once.
	writeFile(t, filepath.Join(proc, "123", "comm"), "strata\n")
	fdinfo := "drm-pdev:\t" + pdev + "\ndrm-client-id:\t5\ndrm-memory-vram:\t1024 KiB\n"
	writeFile(t, filepath.Join(proc, "123", "fdinfo", "7"), fdinfo)
	writeFile(t, filepath.Join(proc, "123", "fdinfo", "8"), fdinfo)
	// A second process holding 2 MiB.
	writeFile(t, filepath.Join(proc, "456", "comm"), "chrome\n")
	writeFile(t, filepath.Join(proc, "456", "fdinfo", "9"),
		"drm-pdev:\t"+pdev+"\ndrm-client-id:\t9\ndrm-memory-vram:\t2048 KiB\n")
	// A client on another card: ignored.
	writeFile(t, filepath.Join(proc, "789", "fdinfo", "1"),
		"drm-pdev:\t0000:00:09.0\ndrm-client-id:\t1\ndrm-memory-vram:\t9999 KiB\n")

	snap, err := AMDProbe{Index: 0, Root: root, Proc: proc}.Read(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	d := snap.Devices[0]
	if snap.Total != 25753026560 || snap.Used != 10000000000 {
		t.Errorf("memory: total %d used %d", snap.Total, snap.Used)
	}
	if snap.Free != snap.Total-snap.Used {
		t.Errorf("free %d, want %d", snap.Free, snap.Total-snap.Used)
	}
	if d.Util != 37 {
		t.Errorf("util %v, want 37", d.Util)
	}
	if d.Temp != 62 || d.Power != 76 || d.PowerLimit != 339 {
		t.Errorf("temp/power: %v %v %v", d.Temp, d.Power, d.PowerLimit)
	}
	if d.Fan != 25 {
		t.Errorf("fan %v%%, want 25", d.Fan)
	}
	if d.ClockSM != 1306 || d.ClockMem != 1249 {
		t.Errorf("clocks: %v %v", d.ClockSM, d.ClockMem)
	}
	if d.PCIeGen != 4 || d.PCIeWidth != 16 {
		t.Errorf("pcie: gen %v width %v", d.PCIeGen, d.PCIeWidth)
	}
	if len(snap.Procs) != 2 {
		t.Fatalf("procs: got %d, want 2: %+v", len(snap.Procs), snap.Procs)
	}
	// sorted by memory, descending
	if snap.Procs[0].PID != 456 || snap.Procs[0].Used != 2*1024*1024 {
		t.Errorf("first proc: %+v", snap.Procs[0])
	}
	if snap.Procs[1].PID != 123 || snap.Procs[1].Used != 1*1024*1024 {
		t.Errorf("second proc (same client on two fds must count once): %+v", snap.Procs[1])
	}
	if snap.Procs[1].Name != "strata" {
		t.Errorf("proc name: %q", snap.Procs[1].Name)
	}
}

func TestAMDProbeNoCard(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "class", "drm"), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := (AMDProbe{Index: 0, Root: root, Proc: t.TempDir()}).Read(context.Background()); err == nil {
		t.Fatal("want an error when no AMD card exists")
	}
}

func TestDetect(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "class", "drm", "card0", "device", "vendor"), "0x1002\n")
	if got := Detect(root); got != "amd" {
		t.Errorf("Detect = %q, want amd", got)
	}
	writeFile(t, filepath.Join(root, "class", "drm", "card1", "device", "vendor"), "0x10de\n")
	if got := Detect(root); got != "nvidia" {
		t.Errorf("Detect with both = %q, want nvidia", got)
	}
	if got := Detect(t.TempDir()); got != "" {
		t.Errorf("Detect on an empty root = %q, want empty", got)
	}
}

// TestAMDProbeLive reads the machine's real card when there is one. It skips on
// CI and on NVIDIA boxes, so `go test ./...` still passes everywhere.
func TestAMDProbeLive(t *testing.T) {
	if Detect("") != "amd" {
		t.Skip("no AMD card on this machine")
	}
	snap, err := (AMDProbe{Index: 0}).Read(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if snap.Total == 0 {
		t.Fatal("card reports no VRAM")
	}
	var dev Device
	for _, x := range snap.Devices {
		if x.Index == snap.Index {
			dev = x
		}
	}
	t.Logf("%s: %.1f/%.1f GiB used, util %.0f%%, %.0f C, %.0f W, fan %.0f%%, %d procs",
		snap.Name, float64(snap.Used)/(1<<30), float64(snap.Total)/(1<<30),
		dev.Util, dev.Temp, dev.Power, dev.Fan, len(snap.Procs))
	for _, p := range snap.Procs {
		t.Logf("  pid %d %-20s %.0f MiB", p.PID, p.Name, float64(p.Used)/(1<<20))
	}
}
