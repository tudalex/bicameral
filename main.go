// bicameral runs Claude Code behind a proxy that sends opted-in subagents
// (model "claude-local*") to a local Anthropic-compatible engine (Splash)
// while everything else passes through to the cloud.
//
//	bicameral [claude args...]   start a proxy on a random port, run claude
//	                             against it with the bicameral plugin loaded,
//	                             and stop the proxy when claude exits
//	bicameral serve [flags]      run just the proxy
//
// Configuration comes from the environment: BICAMERAL_LOCAL_URL,
// BICAMERAL_LOCAL_MODEL, BICAMERAL_PREFIX, BICAMERAL_LOG. The cloud upstream
// is an existing ANTHROPIC_BASE_URL if set, else api.anthropic.com. Without
// BICAMERAL_LOCAL_URL, the wrapper detects running local engines (Splash,
// Ollama, LM Studio) and asks which one, and which model, to use.
package main

import (
	"embed"
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"syscall"
)

//go:embed all:plugin
var pluginFS embed.FS

func env(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func configFromEnv() config {
	return config{
		cloudURL:    env("ANTHROPIC_BASE_URL", "https://api.anthropic.com"),
		localURL:    env("BICAMERAL_LOCAL_URL", "http://127.0.0.1:8010"),
		localPrefix: env("BICAMERAL_PREFIX", "claude-local"),
		localModel:  env("BICAMERAL_LOCAL_MODEL", "incoai/Qwen3.8-27B-Splash"),
	}
}

func main() {
	if len(os.Args) > 1 && os.Args[1] == "serve" {
		serve(os.Args[2:])
		return
	}
	os.Exit(wrap(os.Args[1:]))
}

func serve(args []string) {
	cfg := configFromEnv()
	fl := flag.NewFlagSet("serve", flag.ExitOnError)
	listen := fl.String("listen", "127.0.0.1:8787", "listen address")
	fl.StringVar(&cfg.cloudURL, "cloud", cfg.cloudURL, "cloud upstream")
	fl.StringVar(&cfg.localURL, "local", cfg.localURL, "local Anthropic-compatible upstream")
	fl.StringVar(&cfg.localPrefix, "prefix", cfg.localPrefix, "model prefix routed to the local upstream")
	fl.StringVar(&cfg.localModel, "local-model", cfg.localModel, "model name sent to the local upstream")
	fl.Parse(args)

	log.Printf("bicameral on %s: %s* -> %s (%s), rest -> %s",
		*listen, cfg.localPrefix, cfg.localURL, cfg.localModel, cfg.cloudURL)
	log.Fatal(http.ListenAndServe(*listen, newHandler(cfg)))
}

// wrap runs claude against a private proxy and returns claude's exit code.
func wrap(args []string) int {
	cfg := configFromEnv()
	chooseLocal(&cfg)

	// The TUI owns the terminal, so proxy logs go to a file.
	logPath := os.Getenv("BICAMERAL_LOG")
	if logPath == "" {
		dir, _ := os.UserCacheDir()
		logPath = filepath.Join(dir, "bicameral", "proxy.log")
	}
	os.MkdirAll(filepath.Dir(logPath), 0o755)
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		fmt.Fprintln(os.Stderr, "bicameral:", err)
		return 1
	}
	defer logFile.Close()
	log.SetOutput(logFile)

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		fmt.Fprintln(os.Stderr, "bicameral:", err)
		return 1
	}
	srv := &http.Server{Handler: newHandler(cfg)}
	go srv.Serve(ln)
	defer srv.Close()
	baseURL := "http://" + ln.Addr().String()
	log.Printf("bicameral on %s: %s* -> %s (%s), rest -> %s",
		baseURL, cfg.localPrefix, cfg.localURL, cfg.localModel, cfg.cloudURL)

	pluginDir, err := extractPlugin()
	if err != nil {
		fmt.Fprintln(os.Stderr, "bicameral:", err)
		return 1
	}
	defer os.RemoveAll(pluginDir)

	cmd := exec.Command("claude", append([]string{"--plugin-dir", pluginDir}, args...)...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	cmd.Env = append(os.Environ(),
		"ANTHROPIC_BASE_URL="+baseURL,
		"CLAUDE_CODE_GATEWAY_HINT_HEADERS=1",
	)

	// claude shares our process group, so the terminal delivers ^C and
	// friends to it directly; we just must not die first. Termination
	// signals sent to us alone are forwarded.
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, os.Interrupt, syscall.SIGQUIT, syscall.SIGTERM, syscall.SIGHUP)
	defer signal.Stop(sigs)

	if err := cmd.Start(); err != nil {
		fmt.Fprintln(os.Stderr, "bicameral: starting claude:", err)
		return 1
	}
	go func() {
		for s := range sigs {
			if s == syscall.SIGTERM || s == syscall.SIGHUP {
				cmd.Process.Signal(s)
			}
		}
	}()

	err = cmd.Wait()
	var exitErr *exec.ExitError
	switch {
	case err == nil:
		return 0
	case errors.As(err, &exitErr):
		if ws, ok := exitErr.Sys().(syscall.WaitStatus); ok && ws.Signaled() {
			return 128 + int(ws.Signal())
		}
		return exitErr.ExitCode()
	default:
		fmt.Fprintln(os.Stderr, "bicameral:", err)
		return 1
	}
}

// extractPlugin writes the embedded plugin to a temp dir for --plugin-dir.
func extractPlugin() (string, error) {
	dir, err := os.MkdirTemp("", "bicameral-plugin-")
	if err != nil {
		return "", err
	}
	sub, _ := fs.Sub(pluginFS, "plugin")
	if err := os.CopyFS(dir, sub); err != nil {
		os.RemoveAll(dir)
		return "", err
	}
	return dir, nil
}
