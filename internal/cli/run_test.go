package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/T-Crypt/berth/internal/config"
	"github.com/T-Crypt/berth/internal/runner"
)

// --keep-comfyui-up without --drain-comfyui is a silent no-op downstream
// (KeepComfyUI never fires without DrainComfyUI), so the CLI rejects it
// like the berth.toml validation does.
func TestCheckRunFlagsKeepNeedsDrain(t *testing.T) {
	if err := checkRunFlags(true, ""); err == nil {
		t.Fatal("keep without drain should error")
	}
	if err := checkRunFlags(true, "http://127.0.0.1:8188"); err != nil {
		t.Fatalf("keep with drain should pass, got %v", err)
	}
	if err := checkRunFlags(false, ""); err != nil {
		t.Fatalf("plain run should pass, got %v", err)
	}
}

func TestRunNeedsAnEngineAndACommand(t *testing.T) {
	isolate(t)
	code, _, errOut := runCLI(t, "run", "--", "true")
	if code != ExitSystem {
		t.Fatalf("exit = %d, want %d", code, ExitSystem)
	}
	if !strings.Contains(errOut, "--as NAME and the engine's command are required") {
		t.Errorf("stderr = %q", errOut)
	}
	if !strings.Contains(errOut, "usage: berth run --as NAME") {
		t.Errorf("stderr %q lacks the usage line", errOut)
	}
}

func TestRunRejectsABadPriority(t *testing.T) {
	isolate(t)
	code, _, errOut := runCLI(t, "run", "--as", "x", "--priority", "urgent", "--", "true")
	if code != ExitSystem {
		t.Fatalf("exit = %d, want %d", code, ExitSystem)
	}
	if !strings.Contains(errOut, "urgent") {
		t.Errorf("stderr = %q", errOut)
	}
}

// --fail-closed with berthd down refuses to start the engine: EX_TEMPFAIL, so
// a service manager can retry instead of running unprotected.
func TestRunFailClosedRefusesWithoutADaemon(t *testing.T) {
	isolate(t)
	missing := filepath.Join(t.TempDir(), "nope.sock")
	code, out, _ := runCLI(t, "run", "--as", "x", "--fail-closed", "--socket", missing, "--", "true")
	if code != runner.ExitRefused {
		t.Fatalf("exit = %d, want %d", code, runner.ExitRefused)
	}
	if out != "" {
		t.Errorf("stdout = %q, want empty: the engine owns stdout", out)
	}
}

// Fail-open is the invariant: with berthd down the engine still starts, the run
// is recorded as unprotected, and --quiet never hides that warning.
func TestRunFailOpenStartsWithAWarningEvenQuiet(t *testing.T) {
	isolate(t)
	missing := filepath.Join(t.TempDir(), "nope.sock")
	code, _, errOut := runCLI(t, "-q", "run", "--as", "x", "--socket", missing, "--", "true")
	if code != ExitOK {
		t.Fatalf("exit = %d, want 0 (stderr %q)", code, errOut)
	}
	if !strings.Contains(errOut, "UNPROTECTED: berthd unreachable") {
		t.Errorf("stderr = %q, want the unprotected warning", errOut)
	}
}

// A setting no flag sets comes from the engine's entry; a set flag wins, and
// each override is named with both values. A flag equal to the entry is not
// an override.
func TestApplyEngineEntry(t *testing.T) {
	e := &config.Engine{Name: "comfyui", Priority: "batch", NeedsMiB: 8192,
		DrainPost: "http://127.0.0.1:1/free", DrainSlots: "http://127.0.0.1:2",
		DrainComfyUI: "http://127.0.0.1:3"}

	var errBuf bytes.Buffer
	c := &Context{setFlags: map[string]bool{}, Err: &errBuf}
	prio, keep := "interactive", false
	needs, dp, ds, dc, dt := uint64(0), "", "", "", ""
	applyEngineEntry(c, e, &prio, &needs, &dp, &ds, &dc, &dt, &keep)
	if prio != "batch" || needs != 8192 || dp != "http://127.0.0.1:1/free" ||
		ds != "http://127.0.0.1:2" || dc != "http://127.0.0.1:3" {
		t.Errorf("defaults from the entry = %q %d %q %q %q", prio, needs, dp, ds, dc)
	}
	if errBuf.String() != "" {
		t.Errorf("no flag set, but a notice was printed: %q", errBuf.String())
	}

	errBuf.Reset()
	c = &Context{setFlags: map[string]bool{"priority": true, "needs-mib": true, "drain-post": true,
		"drain-slots": true, "drain-comfyui": true}, Err: &errBuf}
	prio, needs, dp, ds, dc = "foreground", 4096, "http://127.0.0.1:9", "http://127.0.0.1:9", "http://127.0.0.1:9"
	applyEngineEntry(c, e, &prio, &needs, &dp, &ds, &dc, &dt, &keep)
	if prio != "foreground" || needs != 4096 || dp != "http://127.0.0.1:9" {
		t.Errorf("explicit flags lost to the entry: %q %d %q", prio, needs, dp)
	}
	for _, want := range []string{
		"berth: --priority overrides berth.toml priority = batch for comfyui",
		"berth: --needs-mib overrides berth.toml needs_mib = 8192 for comfyui",
		"berth: --drain-post overrides berth.toml drain_post = http://127.0.0.1:1/free for comfyui",
		"berth: --drain-slots overrides berth.toml drain_slots = http://127.0.0.1:2 for comfyui",
		"berth: --drain-comfyui overrides berth.toml drain_comfyui = http://127.0.0.1:3 for comfyui",
	} {
		if !strings.Contains(errBuf.String(), want) {
			t.Errorf("stderr lacks %q:\n%s", want, errBuf.String())
		}
	}

	errBuf.Reset()
	c = &Context{setFlags: map[string]bool{"priority": true}, Err: &errBuf}
	prio = "batch"
	applyEngineEntry(c, e, &prio, &needs, &dp, &ds, &dc, &dt, &keep)
	if errBuf.String() != "" {
		t.Errorf("a flag equal to the entry printed a notice: %q", errBuf.String())
	}

	on := &config.Engine{Name: "comfyui", KeepComfyUI: true, DrainComfyUI: "http://127.0.0.1:3"}
	errBuf.Reset()
	c = &Context{setFlags: map[string]bool{}, Err: &errBuf}
	keep = false
	applyEngineEntry(c, on, &prio, &needs, &dp, &ds, &dc, &dt, &keep)
	if !keep {
		t.Error("the entry's keep_comfyui_up = true was not applied")
	}

	off := &config.Engine{Name: "comfyui"}
	errBuf.Reset()
	c = &Context{setFlags: map[string]bool{"keep-comfyui-up": true}, Err: &errBuf}
	keep = false
	applyEngineEntry(c, off, &prio, &needs, &dp, &ds, &dc, &dt, &keep)
	if !strings.Contains(errBuf.String(), "berth: --keep-comfyui-up overrides berth.toml keep_comfyui_up = false for comfyui") {
		t.Errorf("stderr = %q, want the keep-comfyui-up notice", errBuf.String())
	}
}

func TestRunOverrideNoticeIsPrinted(t *testing.T) {
	isolate(t)
	home := t.TempDir()
	toml := filepath.Join(home, "berth.toml")
	file := "" +
		"[[engine]]\n" +
		"name = \"comfyui\"\n" +
		"type = \"comfyui\"\n" +
		"url = \"http://127.0.0.1:8188\"\n" +
		"drain_comfyui = \"http://127.0.0.1:8188\"\n" +
		"keep_comfyui_up = false\n"
	if err := os.WriteFile(toml, []byte(file), 0o644); err != nil {
		t.Fatal(err)
	}
	missing := filepath.Join(home, "nope.sock")
	code, _, errOut := runCLI(t, "run", "--as", "comfyui", "--keep-comfyui-up",
		"--socket", missing, "--config", toml, "--", "true")
	if code != ExitOK {
		t.Fatalf("exit = %d, want 0 (stderr %q)", code, errOut)
	}
	if !strings.Contains(errOut, "berth: --keep-comfyui-up overrides berth.toml keep_comfyui_up = false for comfyui") {
		t.Errorf("stderr = %q, want the override notice", errOut)
	}
	if !strings.Contains(errOut, "UNPROTECTED: berthd unreachable") {
		t.Errorf("stderr = %q, want the unprotected warning", errOut)
	}
}

// A run that names a model reads the settings of the entry its --group names.
func TestRunMatchesTheGroupEntry(t *testing.T) {
	isolate(t)
	home := t.TempDir()
	toml := filepath.Join(home, "berth.toml")
	file := "" +
		"[[engine]]\n" +
		"name = \"llama-swap\"\n" +
		"type = \"llamacpp\"\n" +
		"url = \"http://127.0.0.1:1234\"\n" +
		"priority = \"batch\"\n"
	if err := os.WriteFile(toml, []byte(file), 0o644); err != nil {
		t.Fatal(err)
	}
	missing := filepath.Join(home, "nope.sock")
	code, _, errOut := runCLI(t, "run", "--as", "NInfer-27B", "--group", "llama-swap",
		"--priority", "foreground", "--socket", missing, "--config", toml, "--", "true")
	if code != ExitOK {
		t.Fatalf("exit = %d, want 0 (stderr %q)", code, errOut)
	}
	if !strings.Contains(errOut, "berth: --priority overrides berth.toml priority = batch for llama-swap") {
		t.Errorf("stderr = %q, want the override notice for the group's entry", errOut)
	}
}

// A config that cannot be read fails the run instead of ignoring it.
func TestRunFailsOnAnInvalidConfig(t *testing.T) {
	isolate(t)
	home := t.TempDir()
	toml := filepath.Join(home, "berth.toml")
	if err := os.WriteFile(toml, []byte("priority = \"urgent\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	code, _, errOut := runCLI(t, "run", "--as", "x", "--config", toml, "--", "true")
	if code != ExitFail {
		t.Fatalf("exit = %d, want %d", code, ExitFail)
	}
	if !strings.Contains(errOut, "config invalid") {
		t.Errorf("stderr = %q, want the config failure", errOut)
	}
}
