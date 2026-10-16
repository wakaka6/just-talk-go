// Copyright (C) 2026 just-talk-go contributors
// SPDX-License-Identifier: GPL-3.0-only
//
// This file is part of Just Talk.

//go:build nogui

package gui

import (
	"errors"
	"log/slog"
)

// ErrNoWindow is returned by RunApp in builds without the desktop window.
var ErrNoWindow = errors.New("this build has no desktop window (built with -tags nogui)")

type AppOptions struct {
	Options
	Logger *slog.Logger
	Start  func(quit func())
	Stop   func()
}

func RunApp(AppOptions) error { return ErrNoWindow }
