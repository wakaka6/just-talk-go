package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/c/just-talk-go/config"
	"github.com/c/just-talk-go/engine"
	"github.com/c/just-talk-go/hotkey"
	"github.com/c/just-talk-go/internal/doctor"
	"github.com/c/just-talk-go/internal/gui"
	"github.com/c/just-talk-go/internal/tui"
	"github.com/c/just-talk-go/plugins"
	"github.com/c/just-talk-go/plugins/overlay"
	"github.com/c/just-talk-go/plugins/voice"
	tea "github.com/charmbracelet/bubbletea"
)

func main() {
	backend := flag.String("backend", "", "force backend")
	cfgPath := flag.String("config", "", "path to config file")
	debug := flag.Bool("debug", false, "enable debug plugin")
	verbose := flag.Bool("verbose", false, "verbose logging")
	useTUI := flag.Bool("tui", false, "run with the terminal UI instead of the desktop window")
	noTUI := flag.Bool("no-tui", false, "run without any UI (daemon mode)")
	useWeb := flag.Bool("web", false, "serve the settings page to a browser on 127.0.0.1 instead of the desktop window")
	doctorOnly := flag.Bool("doctor", false, "run startup doctor and exit")
	installOnly := flag.Bool("install", false, "install just-talk for the current user")
	overlayHelper := flag.Bool("overlay-helper", false, "run macOS overlay helper")
	overlayPosition := flag.String("overlay-position", "top-right", "overlay helper position")
	overlayScale := flag.Float64("overlay-scale", 1.0, "overlay helper scale")
	flag.Parse()
	if *installOnly {
		if err := installSelf(); err != nil {
			fmt.Fprintf(os.Stderr, "install failed: %v\n", err)
			os.Exit(1)
		}
		return
	}
	if *overlayHelper {
		if err := overlay.RunHelper(*overlayPosition, *overlayScale, os.Stdin); err != nil {
			fmt.Fprintf(os.Stderr, "overlay helper error: %v\n", err)
			os.Exit(1)
		}
		return
	}
	// Without a flag Just Talk runs as a tray app with a settings window.
	mode := "app"
	switch {
	case *noTUI:
		mode = "daemon"
	case *useWeb:
		mode = "web"
	case *useTUI:
		mode = "tui"
	}

	logLevel := slog.LevelInfo
	if *verbose {
		logLevel = slog.LevelDebug
	}

	// Daemon and web modes: log to stderr + file. TUI and app modes: file
	// only (stderr corrupts the TUI, which is also the app's fallback).
	logPath := defaultLogPath()
	_ = os.MkdirAll(filepath.Dir(logPath), 0755)
	var logWriter io.Writer
	if mode == "tui" || mode == "app" {
		lf, _ := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
		if lf != nil {
			logWriter = lf
		} else {
			logWriter = io.Discard
		}
	} else {
		lf, _ := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
		if lf != nil {
			logWriter = io.MultiWriter(os.Stderr, lf)
		} else {
			logWriter = os.Stderr
		}
	}
	logger := slog.New(slog.NewTextHandler(logWriter, &slog.HandlerOptions{Level: logLevel}))
	slog.SetDefault(logger)

	cfg, err := config.Load(*cfgPath)
	if err != nil {
		logger.Error("failed to load config", "error", err)
		os.Exit(1)
	}

	if *backend == "" {
		*backend = os.Getenv("JUST_TALK_BACKEND")
	}
	if *backend == "" && runtime.GOOS == "linux" {
		combos, _ := config.ParseHotkeyList(cfg.Voice.PushToTalk)
		for _, combo := range combos {
			if combo.Mods&hotkey.ModFn != 0 {
				// Linux exposes Fn through evdev; X11 does not provide a portable Fn keysym.
				*backend = "wayland"
				break
			}
		}
	}
	if *backend != "" {
		os.Setenv("JUST_TALK_BACKEND", *backend)
	}

	report := doctor.Run(cfg, *backend)
	if *doctorOnly || !report.Healthy() {
		report.Print(os.Stderr)
		if report.Healthy() {
			return
		}
		os.Exit(1)
	}

	provider, err := createProvider(*backend)
	if err != nil {
		logger.Error("failed to create provider", "error", err)
		printTroubleshooting(err)
		os.Exit(1)
	}
	logger.Info("provider created", "platform", provider.Info().Platform, "backend", provider.Info().Backend)

	eng := engine.New(provider, cfg, logger)

	// The debug plugin prints to stdout, which only daemon and web modes own.
	if *debug && cfg.Debug.Enabled && (mode == "daemon" || mode == "web") {
		eng.LoadPlugin(plugins.NewDebugPlugin())
	}
	// A plugin that fails to load leaves the app running without it; saying
	// nothing makes that look like the feature simply did nothing.
	for _, p := range []engine.Plugin{voice.NewVoicePlugin(), overlay.NewOverlayPlugin()} {
		if err := eng.LoadPlugin(p); err != nil {
			logger.Error("plugin failed to load", "error", err)
			os.Exit(1)
		}
	}
	// -config 指定的文件优先；否则热加载会把另一份配置的改动套到本实例上。
	p := *cfgPath
	if p == "" {
		p = config.FindConfig()
	}
	if p != "" {
		eng.WatchConfig(p)
	}

	switch mode {
	case "tui":
		runTUI(eng, cfg, p, *debug)
	case "web":
		runWeb(eng, cfg, p, *debug)
	case "daemon":
		runDaemon(eng)
	default:
		runApp(eng, cfg, p, *debug, logger)
	}
}

func runDaemon(eng *engine.Engine) {
	slog.Info("just-talk started — press hotkeys, Ctrl+C to quit")
	if err := eng.Start(true); err != nil && err != context.Canceled {
		slog.Error("engine exited with error", "error", err)
		os.Exit(1)
	}
}

func uiOptions(eng *engine.Engine, cfg *config.Config, cfgPath string, debug bool) gui.Options {
	return gui.Options{
		Config:     cfg,
		ConfigPath: cfgPath,
		Apply:      eng.ReloadConfig,
		Provider:   eng.Provider().Info(),
		Debug:      debug,
	}
}

func runApp(eng *engine.Engine, cfg *config.Config, cfgPath string, debug bool, logger *slog.Logger) {
	voice.SetupTUILog()
	voice.SetOutput(io.Discard)
	done := make(chan struct{})
	err := gui.RunApp(gui.AppOptions{
		Options: uiOptions(eng, cfg, cfgPath, debug),
		Logger:  logger,
		Start: func(quit func()) {
			go func() {
				defer close(done)
				if err := eng.Start(false); err != nil && err != context.Canceled {
					slog.Error("engine error", "error", err)
				}
				quit()
			}()
		},
		Stop: func() {
			select {
			case <-done:
				return
			default:
			}
			eng.Stop()
			select {
			case <-done:
			case <-time.After(5 * time.Second):
				slog.Warn("engine did not stop in time")
			}
		},
	})
	if errors.Is(err, gui.ErrNoWindow) {
		fmt.Fprintf(os.Stderr, "%v\nFalling back to the terminal UI; use --web for the settings page in a browser.\n", err)
		time.Sleep(2 * time.Second)
		runTUI(eng, cfg, cfgPath, debug)
		return
	}
	if err != nil {
		slog.Error("app exited with error", "error", err)
		fmt.Fprintf(os.Stderr, "app error: %v\n", err)
		os.Exit(1)
	}
}

func runWeb(eng *engine.Engine, cfg *config.Config, cfgPath string, debug bool) {
	voice.SetupTUILog()
	voice.SetOutput(io.Discard)
	srv, err := gui.ListenWeb("127.0.0.1:0", gui.Handler(uiOptions(eng, cfg, cfgPath, debug)))
	if err != nil {
		fmt.Fprintf(os.Stderr, "web UI failed: %v\n", err)
		os.Exit(1)
	}
	fmt.Fprintf(os.Stderr, "Just Talk settings: %s\n", srv.URL())
	go func() {
		if err := srv.Serve(); err != nil {
			slog.Error("web UI stopped", "error", err)
		}
	}()
	runDaemon(eng)
	srv.Close()
}

func runTUI(eng *engine.Engine, cfg *config.Config, cfgPath string, debug bool) {
	voice.SetupTUILog()
	voice.SetOutput(io.Discard)
	model := tui.New(cfg, cfgPath)
	model.SetDebug(debug)
	model.OnSave = func(c *config.Config) error { return eng.ReloadConfig(c) }
	go func() {
		if err := eng.Start(false); err != nil && err != context.Canceled {
			slog.Error("engine error", "error", err)
		}
	}()
	go func() { model.Update(tui.SetProviderInfo(eng.Provider().Info())) }()
	p := tea.NewProgram(model, tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "TUI error: %v\n", err)
		os.Exit(1)
	}
	eng.Stop()
}

func createProvider(backend string) (hotkey.Provider, error) {
	if backend == "mock" {
		return hotkey.NewMockProvider(), nil
	}
	return hotkey.NewProvider()
}

func printTroubleshooting(err error) {
	fmt.Fprintf(os.Stderr, "Error: %v\n\nTroubleshooting:\n", err)
	fmt.Fprintf(os.Stderr, "  X11:      Ensure $DISPLAY is set\n")
	fmt.Fprintf(os.Stderr, "  Wayland:  Add user to 'input' group\n")
	fmt.Fprintf(os.Stderr, "  macOS:    Grant Accessibility permission\n")
	fmt.Fprintf(os.Stderr, "  Windows:  Check microphone privacy settings\n")
}

func installSelf() error {
	targetDir, err := installDir()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(targetDir, 0755); err != nil {
		return fmt.Errorf("create %s: %w", targetDir, err)
	}

	src, err := os.Executable()
	if err != nil {
		return fmt.Errorf("find current executable: %w", err)
	}
	if resolved, err := filepath.EvalSymlinks(src); err == nil {
		src = resolved
	}
	name := "just-talk"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	target := filepath.Join(targetDir, name)
	if samePath(src, target) {
		fmt.Fprintf(os.Stdout, "just-talk is already installed at %s\n", target)
		printInstallPathNote(targetDir)
		return nil
	}

	in, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("open current executable %s: %w", src, err)
	}
	defer in.Close()

	tmp, err := os.CreateTemp(targetDir, ".just-talk-*")
	if err != nil {
		return fmt.Errorf("create temporary installer file: %w", err)
	}
	tmpName := tmp.Name()
	ok := false
	defer func() {
		if !ok {
			_ = os.Remove(tmpName)
		}
	}()

	if _, err := io.Copy(tmp, in); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("copy executable: %w", err)
	}
	if err := tmp.Chmod(0755); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("set executable mode: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close temporary installer file: %w", err)
	}
	if err := replaceInstalledFile(tmpName, target); err != nil {
		return fmt.Errorf("install to %s: %w", target, err)
	}
	ok = true

	fmt.Fprintf(os.Stdout, "Installed just-talk to %s\n", target)
	printInstallPathNote(targetDir)
	return nil
}

func installDir() (string, error) {
	if runtime.GOOS == "windows" {
		base := strings.TrimSpace(os.Getenv("LOCALAPPDATA"))
		if base == "" {
			var err error
			base, err = os.UserCacheDir()
			if err != nil {
				return "", fmt.Errorf("find local application data directory: %w", err)
			}
		}
		return filepath.Join(base, "Programs", "Just Talk"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("find home directory: %w", err)
	}
	return filepath.Join(home, ".local", "bin"), nil
}

func replaceInstalledFile(source, target string) error {
	if runtime.GOOS != "windows" {
		return os.Rename(source, target)
	}
	backup := target + ".old"
	_ = os.Remove(backup)
	hadTarget := false
	if _, err := os.Stat(target); err == nil {
		if err := os.Rename(target, backup); err != nil {
			return err
		}
		hadTarget = true
	}
	if err := os.Rename(source, target); err != nil {
		if hadTarget {
			_ = os.Rename(backup, target)
		}
		return err
	}
	if hadTarget {
		_ = os.Remove(backup)
	}
	return nil
}

func defaultLogPath() string {
	if runtime.GOOS == "windows" {
		if dir, err := os.UserCacheDir(); err == nil {
			return filepath.Join(dir, "just-talk", "just-talk.log")
		}
	}
	return "/tmp/just-talk.log"
}

func samePath(a, b string) bool {
	absA, errA := filepath.Abs(a)
	absB, errB := filepath.Abs(b)
	if errA == nil {
		a = absA
	}
	if errB == nil {
		b = absB
	}
	if resolved, err := filepath.EvalSymlinks(b); err == nil {
		b = resolved
	}
	if runtime.GOOS == "windows" {
		return strings.EqualFold(a, b)
	}
	return a == b
}

func printInstallPathNote(dir string) {
	if !pathContains(dir) {
		fmt.Fprintf(os.Stdout, "Note: %s is not in PATH. Add it to your shell profile to run just-talk directly.\n", dir)
	}
}

func pathContains(dir string) bool {
	for _, p := range filepath.SplitList(os.Getenv("PATH")) {
		if samePath(p, dir) {
			return true
		}
		if rel, err := filepath.Rel(p, dir); err == nil && rel == "." {
			return true
		}
		if strings.TrimRight(p, string(os.PathSeparator)) == strings.TrimRight(dir, string(os.PathSeparator)) {
			return true
		}
	}
	return false
}
