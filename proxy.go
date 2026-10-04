package main

import (
	"bytes"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
	"time"
)

type config struct {
	cloudURL    string // cloud upstream (Anthropic or an existing gateway)
	localURL    string // local Anthropic-compatible upstream (Splash)
	localPrefix string // model prefix routed to the local upstream
	localModel  string // model name sent to the local upstream
}

// Headers that must never reach the local engine.
var credentialHeaders = []string{"Authorization", "X-Api-Key", "Cookie"}

func reverseProxy(target string) *httputil.ReverseProxy {
	u, err := url.Parse(target)
	if err != nil {
		log.Fatal(err)
	}
	return &httputil.ReverseProxy{
		Rewrite: func(r *httputil.ProxyRequest) {
			r.SetURL(u)
			r.Out.Host = u.Host
		},
		FlushInterval: -1, // stream SSE immediately
	}
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (s *statusRecorder) WriteHeader(code int) { s.status = code; s.ResponseWriter.WriteHeader(code) }
func (s *statusRecorder) Flush() {
	if f, ok := s.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

// newHandler routes /v1/messages* requests whose model starts with
// cfg.localPrefix to the local upstream; everything else goes to the cloud.
func newHandler(cfg config) http.Handler {
	cloud, local := reverseProxy(cfg.cloudURL), reverseProxy(cfg.localURL)

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w, status: 200}
		route, model := "cloud", ""

		if r.Method == http.MethodPost && strings.HasPrefix(r.URL.Path, "/v1/messages") {
			body, err := io.ReadAll(r.Body)
			if err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			var req map[string]json.RawMessage
			if json.Unmarshal(body, &req) == nil {
				json.Unmarshal(req["model"], &model)
			}
			if strings.HasPrefix(model, cfg.localPrefix) {
				route = "local"
				req["model"], _ = json.Marshal(cfg.localModel)
				body, _ = json.Marshal(req)
				for _, h := range credentialHeaders {
					r.Header.Del(h)
				}
			}
			r.Body = io.NopCloser(bytes.NewReader(body))
			r.ContentLength = int64(len(body))
			r.Header.Del("Content-Length")
		}

		if route == "local" {
			local.ServeHTTP(rec, r)
		} else {
			cloud.ServeHTTP(rec, r)
		}
		log.Printf("%-5s %3d %6.1fs %s %s model=%s class=%s",
			route, rec.status, time.Since(start).Seconds(), r.Method, r.URL.Path, model,
			r.Header.Get("X-Claude-Code-Request-Class"))
	})
}

// localReachable reports whether the local upstream answers at all.
func localReachable(localURL string) bool {
	c := http.Client{Timeout: 2 * time.Second}
	resp, err := c.Get(strings.TrimRight(localURL, "/") + "/v1/models")
	if err != nil {
		return false
	}
	resp.Body.Close()
	return resp.StatusCode < 500
}
