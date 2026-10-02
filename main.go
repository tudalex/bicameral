// bicameral is an ANTHROPIC_BASE_URL proxy for Claude Code. Requests whose
// model starts with the local prefix (default "claude-local") go to a local
// Anthropic-compatible engine (Splash); everything else passes through to
// api.anthropic.com untouched.
package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"io"
	"log"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
	"time"
)

var (
	listen      = flag.String("listen", "127.0.0.1:8787", "listen address")
	cloudURL    = flag.String("cloud", "https://api.anthropic.com", "cloud upstream")
	localURL    = flag.String("local", "http://127.0.0.1:8010", "local Anthropic-compatible upstream")
	localPrefix = flag.String("prefix", "claude-local", "model prefix routed to the local upstream")
	localModel  = flag.String("local-model", "incoai/Qwen3.8-27B-Splash", "model name sent to the local upstream")
)

// Headers that must never reach the local engine.
var credentialHeaders = []string{"Authorization", "X-Api-Key", "Cookie"}

func proxy(target string) *httputil.ReverseProxy {
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

func main() {
	flag.Parse()
	cloud, local := proxy(*cloudURL), proxy(*localURL)

	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
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
			if strings.HasPrefix(model, *localPrefix) {
				route = "local"
				req["model"], _ = json.Marshal(*localModel)
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

	log.Printf("bicameral on %s: %s* -> %s (%s), rest -> %s", *listen, *localPrefix, *localURL, *localModel, *cloudURL)
	log.Fatal(http.ListenAndServe(*listen, nil))
}
