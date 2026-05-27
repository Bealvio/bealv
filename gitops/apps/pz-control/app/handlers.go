package main

import (
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"html/template"
	"io/fs"
	"net/http"
	"time"
)

//go:embed web/*
var webFS embed.FS

type Server struct {
	Kube         KubeClient
	Auth         *Auth
	Cooldown     *Cooldown
	SecureCookie bool
	indexTpl     *template.Template
	loginTpl     *template.Template
}

func (s *Server) Router() http.Handler {
	if s.indexTpl == nil {
		s.indexTpl = template.Must(template.ParseFS(webFS, "web/index.html"))
		s.loginTpl = template.Must(template.ParseFS(webFS, "web/login.html"))
	}
	mux := http.NewServeMux()

	// Public
	mux.HandleFunc("GET /login", s.getLogin)
	mux.HandleFunc("POST /login", s.postLogin)
	mux.Handle("GET /static/", http.StripPrefix("/static/", http.FileServerFS(mustSubFS(webFS, "web"))))
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("ok")) })

	// Protected
	protected := http.NewServeMux()
	protected.HandleFunc("GET /", s.getIndex)
	protected.HandleFunc("GET /api/status", s.getStatus)
	protected.HandleFunc("GET /api/logs", s.getLogs)
	protected.HandleFunc("POST /api/restart", s.postRestart)
	mux.Handle("/", s.Auth.Middleware(protected))

	return mux
}

func (s *Server) getIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := s.indexTpl.Execute(w, nil); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func (s *Server) getLogin(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := s.loginTpl.Execute(w, map[string]any{"Error": r.URL.Query().Get("error")}); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func (s *Server) postLogin(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	if !s.Auth.Verify(r.FormValue("password")) {
		http.Error(w, "wrong password", http.StatusUnauthorized)
		return
	}
	s.Auth.SetSessionCookie(w, time.Now(), s.SecureCookie)
	http.Redirect(w, r, "/", http.StatusFound)
}

func (s *Server) getStatus(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	st, err := s.Kube.PodStatus(ctx)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(st)
}

func (s *Server) getLogs(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	out, err := s.Kube.PodLogs(ctx, 500)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Write([]byte(out))
}

func (s *Server) postRestart(w http.ResponseWriter, r *http.Request) {
	now := time.Now()
	if !s.Cooldown.Try(now) {
		remaining := s.Cooldown.Remaining(now)
		w.Header().Set("Retry-After", fmt.Sprintf("%d", int(remaining.Seconds())+1))
		http.Error(w, fmt.Sprintf("cooldown: %s remaining", remaining.Truncate(time.Second)), http.StatusTooManyRequests)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	if err := s.Kube.RestartDeployment(ctx); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("restart triggered"))
}

func mustSubFS(efs embed.FS, dir string) fs.FS {
	sub, err := fs.Sub(efs, dir)
	if err != nil {
		panic(err)
	}
	return sub
}
