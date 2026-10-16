// Copyright (C) 2026 just-talk-go contributors
// SPDX-License-Identifier: GPL-3.0-only
//
// This file is part of Just Talk.

package gui

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"net"
	"net/http"
	"time"
)

const keyCookie = "just_talk_key"

// WebServer serves the page to a browser on the loopback interface. The page
// holds API keys, so every request must carry this run's key: the printed
// link passes it once in ?key= and it is kept in a cookie from then on.
type WebServer struct {
	ln  net.Listener
	srv *http.Server
	key string
}

// ListenWeb binds addr; callers pass a loopback address such as 127.0.0.1:0.
func ListenWeb(addr string, h http.Handler) (*WebServer, error) {
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, err
	}
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		ln.Close()
		return nil, err
	}
	w := &WebServer{ln: ln, key: hex.EncodeToString(b)}
	w.srv = &http.Server{Handler: w.guard(h), ReadHeaderTimeout: 10 * time.Second}
	return w, nil
}

// URL is the link that signs a browser in.
func (w *WebServer) URL() string {
	return "http://" + w.ln.Addr().String() + "/?key=" + w.key
}

// Serve blocks until Close.
func (w *WebServer) Serve() error {
	if err := w.srv.Serve(w.ln); err != http.ErrServerClosed {
		return err
	}
	return nil
}

func (w *WebServer) Close() error {
	return w.srv.Close()
}

func (w *WebServer) guard(next http.Handler) http.Handler {
	host := w.ln.Addr().String()
	return http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		// A page on another site can resolve its own name to 127.0.0.1;
		// only this address is ours.
		if r.Host != host {
			http.Error(rw, "forbidden", http.StatusForbidden)
			return
		}
		if r.Method != http.MethodGet && r.Header.Get("Origin") != "http://"+host {
			http.Error(rw, "forbidden", http.StatusForbidden)
			return
		}
		if k := r.URL.Query().Get("key"); k != "" {
			if !w.valid(k) {
				http.Error(rw, "链接已失效，请使用本次启动时输出的链接。", http.StatusUnauthorized)
				return
			}
			http.SetCookie(rw, &http.Cookie{Name: keyCookie, Value: w.key, Path: "/", HttpOnly: true, SameSite: http.SameSiteStrictMode})
			http.Redirect(rw, r, "/", http.StatusSeeOther)
			return
		}
		c, err := r.Cookie(keyCookie)
		if err != nil || !w.valid(c.Value) {
			http.Error(rw, "请使用本次启动时输出的链接打开。", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(rw, r)
	})
}

func (w *WebServer) valid(k string) bool {
	return subtle.ConstantTimeCompare([]byte(k), []byte(w.key)) == 1
}
