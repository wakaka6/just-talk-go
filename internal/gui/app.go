// Copyright (C) 2026 just-talk-go contributors
// SPDX-License-Identifier: GPL-3.0-only
//
// This file is part of Just Talk.

//go:build !nogui

package gui

import (
	_ "embed"
	"errors"
	"log/slog"
	"os"
	"os/signal"
	"runtime"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"
)

// The PNGs are rendered from icons/tray-template.svg and the site's
// website/assets/favicon.svg (rsvg-convert -w 64 / -w 256).
var (
	//go:embed icons/tray-template.png
	trayTemplateIcon []byte
	//go:embed icons/tray.png
	trayIcon []byte
	//go:embed icons/app.png
	appIcon []byte
)

// ErrNoWindow means the window cannot be shown (the WebView2 runtime is
// missing on Windows). RunApp returns it before Start is called, so the
// caller can fall back to another UI.
var ErrNoWindow = errors.New("Microsoft Edge WebView2 runtime not found; install it from https://developer.microsoft.com/microsoft-edge/webview2/")

// AppOptions configures the desktop app.
type AppOptions struct {
	Options
	Logger *slog.Logger
	// Start runs the engine. It is called once the window can be shown;
	// quit ends the app, for an engine that stops on its own.
	Start func(quit func())
	// Stop stops the engine and waits for it to finish.
	Stop func()
}

// panelBlurGrace is how long a click on the tray icon is ignored after the
// panel hid on losing focus: clicking the icon to close the panel takes
// its focus first, and the click must not open it again.
const panelBlurGrace = 300 * time.Millisecond

// RunApp runs the tray icon, its panel and the settings window until the
// user quits. It must be called from the main goroutine.
func RunApp(o AppOptions) error {
	var app *application.App
	o.OpenURL = func(url string) {
		if err := app.Browser.OpenURL(url); err != nil {
			o.Logger.Warn("open url failed", "url", url, "error", err)
		}
	}
	var window, panel *application.WebviewWindow
	show := func() {
		panel.Hide()
		window.Show()
		window.UnMinimise()
		window.Focus()
	}
	var quitOnce sync.Once
	quit := func() { quitOnce.Do(func() { application.InvokeAsync(app.Quit) }) }
	o.ShowSettings = func() { application.InvokeAsync(show) }
	o.Quit = quit
	app = application.New(application.Options{
		Name:        "Just Talk",
		Description: "Desktop voice input",
		Icon:        appIcon,
		Logger:      o.Logger,
		Assets:      application.AssetOptions{Handler: Handler(o.Options)},
		Mac: application.MacOptions{
			// A menu bar app: no Dock icon, closing the window keeps it running.
			ActivationPolicy: application.ActivationPolicyAccessory,
		},
		SingleInstance: &application.SingleInstanceOptions{
			UniqueID: "io.github.just-talk",
			// A second launch shows this window and exits.
			OnSecondInstanceLaunch: func(application.SecondInstanceData) {
				application.InvokeSync(show)
			},
		},
		OnShutdown: o.Stop,
	})
	if runtime.GOOS == "windows" {
		if v, _ := app.Env.Info().PlatformInfo["WebView2"].(string); v == "" {
			return ErrNoWindow
		}
	}

	window = app.Window.NewWithOptions(application.WebviewWindowOptions{
		Name:      "main",
		Title:     "Just Talk",
		Width:     780,
		Height:    760,
		MinWidth:  420,
		MinHeight: 480,
		URL:       "/",
		Mac: application.MacWindow{
			TitleBar:                application.MacTitleBarHiddenInset,
			InvisibleTitleBarHeight: 52,
		},
	})
	window.RegisterHook(events.Common.WindowClosing, func(e *application.WindowEvent) {
		window.Hide()
		e.Cancel()
	})

	panel = app.Window.NewWithOptions(application.WebviewWindowOptions{
		Name:          "panel",
		Title:         "Just Talk",
		Width:         340,
		Height:        596,
		URL:           "/panel.html",
		Frameless:     true,
		Hidden:        true,
		DisableResize: true,
		AlwaysOnTop:   true,
		Mac: application.MacWindow{
			CollectionBehavior: application.MacWindowCollectionBehaviorCanJoinAllSpaces |
				application.MacWindowCollectionBehaviorFullScreenAuxiliary |
				application.MacWindowCollectionBehaviorIgnoresCycle,
		},
		Windows: application.WindowsWindow{HiddenOnTaskbar: true},
	})
	var panelBlurredAt atomic.Int64 // unix ns
	panel.OnWindowEvent(events.Common.WindowLostFocus, func(*application.WindowEvent) {
		if panel.IsVisible() {
			panelBlurredAt.Store(time.Now().UnixNano())
			panel.Hide()
		}
	})

	tray := app.SystemTray.New()
	if runtime.GOOS == "darwin" {
		tray.SetTemplateIcon(trayTemplateIcon)
	} else {
		tray.SetIcon(trayIcon)
	}
	tray.SetTooltip("Just Talk")
	menu := app.NewMenu()
	menu.Add("打开 Just Talk").OnClick(func(*application.Context) { show() })
	menu.AddSeparator()
	menu.Add("退出").OnClick(func(*application.Context) { app.Quit() })
	tray.SetMenu(menu)
	// Left click toggles the panel; right click opens the menu.
	tray.AttachWindow(panel).WindowOffset(6)
	tray.OnClick(func() {
		switch {
		case panel.IsVisible():
			panel.Hide()
		case time.Since(time.Unix(0, panelBlurredAt.Load())) < panelBlurGrace:
		default:
			tray.ShowWindow()
		}
	})

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(sig)
	go func() {
		<-sig
		quit()
	}()

	o.Start(quit)
	return app.Run()
}
