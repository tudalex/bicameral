package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/term"
)

// knownEngines are local servers with an Anthropic-compatible /v1/messages
// endpoint, probed at their default addresses. Order is the preference used
// when nobody is around to choose.
var knownEngines = []engine{
	{name: "Splash", url: "http://127.0.0.1:8010"},
	{name: "Ollama", url: "http://127.0.0.1:11434"},
	{name: "LM Studio", url: "http://127.0.0.1:1234"},
}

type engine struct {
	name   string
	url    string
	models []string // chat models it serves, from /v1/models
}

// listModels returns the chat model ids served at baseURL, or an error if
// nothing usable answers there.
func listModels(baseURL string) ([]string, error) {
	c := http.Client{Timeout: 2 * time.Second}
	resp, err := c.Get(strings.TrimRight(baseURL, "/") + "/v1/models")
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GET /v1/models: %s", resp.Status)
	}
	var list struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&list); err != nil {
		return nil, err
	}
	var models []string
	for _, m := range list.Data {
		// LM Studio lists embedding models alongside chat models.
		if m.ID != "" && !strings.Contains(strings.ToLower(m.ID), "embed") {
			models = append(models, m.ID)
		}
	}
	return models, nil
}

// detectEngines probes every known engine concurrently and returns the ones
// that are up and serve at least one model, in knownEngines order.
func detectEngines() []engine {
	found := make([]engine, len(knownEngines))
	var wg sync.WaitGroup
	for i, e := range knownEngines {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if models, err := listModels(e.url); err == nil && len(models) > 0 {
				e.models = models
				found[i] = e
			}
		}()
	}
	wg.Wait()
	var up []engine
	for _, e := range found {
		if e.url != "" {
			up = append(up, e)
		}
	}
	return up
}

// chooseLocal fills in cfg.localURL and cfg.localModel for wrap mode.
// An explicit BICAMERAL_LOCAL_URL skips detection entirely; otherwise the
// user picks among the running engines (and their models) on the terminal.
func chooseLocal(cfg *config) {
	if os.Getenv("BICAMERAL_LOCAL_URL") != "" {
		if !localReachable(cfg.localURL) {
			fmt.Fprintf(os.Stderr, "bicameral: warning: local model not reachable at %s; local-worker will fail until it is up\n", cfg.localURL)
		}
		return
	}

	engines := detectEngines()
	if len(engines) == 0 {
		fmt.Fprintf(os.Stderr, "bicameral: warning: no local engine found (looked for %s); local-worker will fail until one is up\n", engineNames())
		return
	}

	in := bufio.NewReader(os.Stdin)
	interactive := isTerminal(os.Stdin)

	names := make([]string, len(engines))
	for i, e := range engines {
		n := fmt.Sprintf("%d models", len(e.models))
		if len(e.models) == 1 {
			n = "1 model"
		}
		names[i] = fmt.Sprintf("%-9s  %s  (%s)", e.name, e.url, n)
	}
	e := engines[pick(in, interactive, "Local engine", names)]
	cfg.localURL = e.url

	if model := os.Getenv("BICAMERAL_LOCAL_MODEL"); model != "" {
		cfg.localModel = model
		return
	}
	cfg.localModel = e.models[pick(in, interactive, e.name+" model", e.models)]
}

// pick asks the user to choose one of options and returns its index. With a
// single option, or no terminal to ask on, it takes the first and says so.
func pick(in *bufio.Reader, interactive bool, what string, options []string) int {
	if len(options) == 1 || !interactive {
		fmt.Fprintf(os.Stderr, "bicameral: %s: %s\n", what, strings.TrimSpace(options[0]))
		return 0
	}
	fmt.Fprintf(os.Stderr, "bicameral: %s:\n", what)
	for i, o := range options {
		fmt.Fprintf(os.Stderr, "  %2d) %s\n", i+1, o)
	}
	for {
		fmt.Fprintf(os.Stderr, "Choose [1-%d, default 1]: ", len(options))
		line, err := in.ReadString('\n')
		line = strings.TrimSpace(line)
		if line == "" {
			if err != nil { // EOF: nobody is typing
				fmt.Fprintln(os.Stderr)
			}
			return 0
		}
		if n, convErr := strconv.Atoi(line); convErr == nil && n >= 1 && n <= len(options) {
			return n - 1
		}
	}
}

func isTerminal(f *os.File) bool { return term.IsTerminal(int(f.Fd())) }

func engineNames() string {
	names := make([]string, len(knownEngines))
	for i, e := range knownEngines {
		names[i] = fmt.Sprintf("%s at %s", e.name, e.url)
	}
	return strings.Join(names, ", ")
}
