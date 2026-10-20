package cli

import (
	"fmt"
	"strconv"
	"time"

	"github.com/T-Crypt/berth/internal/broker"
	"github.com/T-Crypt/berth/internal/config"
	"github.com/T-Crypt/berth/internal/gpu"
	"github.com/T-Crypt/berth/internal/runner"
	"github.com/T-Crypt/berth/internal/suite"
)

func runRun(c *Context) int {
	fs := c.FlagSet()
	sock := fs.String("socket", defaultSocket(), "berthd socket ($BERTH_SOCKET)")
	engine := fs.String("as", "", "engine name shown in status and drain messages (required)")
	group := fs.String("group", "", "engine family for juggles and the usage ranking, e.g. llama-swap for each of its models (default: --as)")
	prio := fs.String("priority", "interactive", "batch, interactive, foreground or hold")
	pinned := fs.Bool("pinned", false, "never drained: the card stays CMD's until it exits (for apps like Blender, not engines)")
	needs := fs.Uint64("needs-mib", 0, "memory this engine needs; 0 = the whole card")
	wait := fs.Duration("wait", 0, "how long to wait for a berth; 0 = berthd's default")
	drainPost := fs.String("drain-post", "", "URL to POST when asked to drain, before signalling (e.g. llama-swap's unload)")
	drainSlots := fs.String("drain-slots", "", "engine base URL whose /slots must show idle before a drain stops it (an engine with no unload URL, e.g. Strata)")
	drainComfy := fs.String("drain-comfyui", "", "ComfyUI base URL: on drain, wait until its queue is empty before stopping it (never cuts a job off)")
	drainToken := fs.String("drain-token-env", "", "environment variable holding a bearer token for the drain URLs (an engine behind an API key)")
	keepComfy := fs.Bool("keep-comfyui-up", false, "with --drain-comfyui: unload its models via POST /free and leave the server up instead of stopping it")
	grace := fs.Duration("grace", 30*time.Second, "time each drain step gets before the next")
	failClosed := fs.Bool("fail-closed", false, "do not start CMD when berthd is unreachable")
	cfgPath := fs.String("config", config.DefaultPath(), "berth.toml: the engine entry matching --as (or --group) supplies the settings a flag does not set; a set flag wins and says so; missing = no defaults ($BERTH_CONFIG)")
	if code, done := c.Parse(fs); done {
		return code
	}
	if *engine == "" || len(c.Args) == 0 {
		return c.fail(Systemf("--as NAME and the engine's command are required").
			WithHint("usage: berth run --as NAME [flags] -- CMD [ARGS...]"))
	}
	// The engine's berth.toml entry is the default for every setting a flag
	// does not set, so a run with no flags behaves like a gateway start. A
	// flag the operator set wins and says so: the file and the flags can
	// never silently disagree.
	merged, _, err := suite.Merged(*cfgPath)
	if err != nil {
		return c.fail(Failf("config invalid: %s", *cfgPath).Detail("%v", err))
	}
	if e := engineEntry(merged, *engine, *group); e != nil {
		applyEngineEntry(c, e, prio, needs, drainPost, drainSlots, drainComfy, drainToken, keepComfy)
	}
	if _, err := broker.ParsePriority(*prio); err != nil {
		return c.fail(Systemf("%v", err))
	}
	if err := checkRunFlags(*keepComfy, *drainComfy); err != nil {
		return c.fail(Systemf("%v", err))
	}
	// Nothing here goes to stdout: the engine owns it. berth's own notices are
	// diagnostics on stderr, and --quiet never hides the unprotected warning.
	return runner.Run(runner.Options{
		Socket: *sock, Engine: *engine, Group: *group, Priority: *prio, Needs: *needs * gpu.MiB, Wait: *wait, Pinned: *pinned,
		DrainPost: *drainPost, DrainSlots: *drainSlots, DrainComfyUI: *drainComfy, DrainTokenEnv: *drainToken,
		KeepComfyUI: *keepComfy, Grace: *grace,
		FailClosed: *failClosed, Argv: c.Args, Stderr: c.Err,
	})
}

// engineEntry is the [[engine]] block a run reads its defaults from: the block
// named by --as, or, for a run that names a model (llama-swap), the block
// named by --group.
func engineEntry(cfg config.Config, as, group string) *config.Engine {
	for i := range cfg.Engine {
		if cfg.Engine[i].Name == as {
			return &cfg.Engine[i]
		}
	}
	for i := range cfg.Engine {
		if group != "" && cfg.Engine[i].Name == group {
			return &cfg.Engine[i]
		}
	}
	return nil
}

// applyEngineEntry sets the run's settings the operator did not set from the
// engine's berth.toml entry, and names every flag that overrides the entry,
// in the file's key and value.
func applyEngineEntry(c *Context, e *config.Engine,
	prio *string, needs *uint64, drainPost, drainSlots, drainComfy, drainToken *string, keepComfy *bool) {
	notice := func(flag, key, toml string) {
		fmt.Fprintf(c.Err, "berth: --%s overrides berth.toml %s = %s for %s\n", flag, key, toml, e.Name)
	}
	if !c.Changed("priority") && e.Priority != "" {
		*prio = e.Priority
	}
	if c.Changed("priority") && e.Priority != "" && e.Priority != *prio {
		notice("priority", "priority", e.Priority)
	}
	if !c.Changed("needs-mib") && e.NeedsMiB > 0 {
		*needs = e.NeedsMiB
	}
	if c.Changed("needs-mib") && e.NeedsMiB > 0 && e.NeedsMiB != *needs {
		notice("needs-mib", "needs_mib", strconv.FormatUint(e.NeedsMiB, 10))
	}
	drains := []struct {
		flag, key string
		toml      string
		got       *string
	}{
		{"drain-post", "drain_post", e.DrainPost, drainPost},
		{"drain-slots", "drain_slots", e.DrainSlots, drainSlots},
		{"drain-comfyui", "drain_comfyui", e.DrainComfyUI, drainComfy},
		{"drain-token-env", "drain_token_env", e.DrainTokenEnv, drainToken},
	}
	for _, d := range drains {
		if !c.Changed(d.flag) && d.toml != "" {
			*d.got = d.toml
		}
		if c.Changed(d.flag) && d.toml != "" && d.toml != *d.got {
			notice(d.flag, d.key, d.toml)
		}
	}
	if !c.Changed("keep-comfyui-up") && e.KeepComfyUI {
		*keepComfy = true
	}
	if c.Changed("keep-comfyui-up") && !e.KeepComfyUI {
		notice("keep-comfyui-up", "keep_comfyui_up", "false")
	}
}

// checkRunFlags rejects flag combinations that can never work, mirroring
// the berth.toml validation, before anything starts.
func checkRunFlags(keepComfy bool, drainComfy string) error {
	if keepComfy && drainComfy == "" {
		return fmt.Errorf("--keep-comfyui-up needs --drain-comfyui, the ComfyUI base URL to unload on drain")
	}
	return nil
}
