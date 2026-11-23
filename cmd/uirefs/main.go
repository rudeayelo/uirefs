// Command uirefs is the single static binary: reference store, JSON API,
// MCP server and web UI.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"syscall"
	"time"

	"github.com/uesteibar/uirefs"
	"github.com/uesteibar/uirefs/internal/api"
	"github.com/uesteibar/uirefs/internal/guide"
	"github.com/uesteibar/uirefs/internal/mcp"
	"github.com/uesteibar/uirefs/internal/store"
)

// version is set with -ldflags="-X main.version=<sha>".
var version = "dev"

const usageText = `usage: uirefs <command>

  serve --port N --data DIR   run the service on 127.0.0.1 (default port 4880, data ./data)
  reindex --data DIR          re-embed every reference (needs UIREFS_EMBED_URL)
  doctor --data DIR [--json]  check the data dir, FTS5 and the embeddings endpoint
  version                     print the version

Environment:

  UIREFS_EMBED_URL   optional OpenAI-compatible /v1/embeddings endpoint;
                      when set, search also uses semantic ranking
  UIREFS_EMBED_MODEL model name sent to that endpoint
                      (default Xenova/all-MiniLM-L6-v2)
`

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(args []string) int {
	if len(args) == 0 {
		fmt.Fprint(os.Stderr, usageText)
		return 2
	}
	var err error
	var code int
	switch args[0] {
	case "serve":
		code, err = serveCmd(args[1:])
	case "reindex":
		code, err = reindexCmd(args[1:])
	case "doctor":
		code, err = doctorCmd(args[1:])
	case "version":
		fmt.Println("uirefs " + version)
		return 0
	case "help", "-h", "--help":
		fmt.Print(usageText)
		return 0
	default:
		err = fmt.Errorf("unknown command %q", args[0])
		code = 2
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "uirefs:", err)
	}
	return code
}

func dataFlag(name string) (*flag.FlagSet, *string) {
	flags := flag.NewFlagSet(name, flag.ContinueOnError)
	dir := flags.String("data", "data", "data directory")
	return flags, dir
}

func openStore(dir string) (*store.Store, error) {
	return store.Open(dir, embedderFromEnv())
}

// embedderFromEnv returns nil when UIREFS_EMBED_URL is unset: nothing about
// embeddings is then loaded or required.
func embedderFromEnv() store.Embedder {
	url := os.Getenv("UIREFS_EMBED_URL")
	if url == "" {
		return nil
	}
	return store.NewHTTPEmbedder(url, os.Getenv("UIREFS_EMBED_MODEL"))
}

func serveCmd(args []string) (int, error) {
	flags := flag.NewFlagSet("serve", flag.ContinueOnError)
	port := flags.Int("port", 4880, "listen port")
	dir := flags.String("data", "data", "data directory")
	if err := flags.Parse(args); err != nil {
		return 2, err
	}
	if *port < 1 || *port > 65535 {
		return 2, fmt.Errorf("port must be between 1 and 65535")
	}

	st, err := openStore(*dir)
	if err != nil {
		return 1, err
	}
	defer st.Close()

	mux := http.NewServeMux()
	api.New(st).Register(mux)
	mcp.New(st, guide.Markdown, version).Register(mux)
	web, err := fs.Sub(uirefs.WebFS, "web")
	if err != nil {
		return 1, err
	}
	mux.Handle("/", http.FileServerFS(web))

	srv := &http.Server{
		Addr:              net.JoinHostPort("127.0.0.1", strconv.Itoa(*port)),
		Handler:           guard(mux, hostsFromEnv()),
		ReadHeaderTimeout: 5 * time.Second,
	}

	// Clean exit on SIGINT and SIGTERM.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	serverErr := make(chan error, 1)
	go func() { serverErr <- srv.ListenAndServe() }()

	if os.Getenv("UIREFS_EMBED_URL") == "" {
		fmt.Printf("uirefs %s listening on http://%s (embeddings off)\n", version, srv.Addr)
	} else {
		fmt.Printf("uirefs %s listening on http://%s (embeddings on)\n", version, srv.Addr)
	}

	select {
	case <-ctx.Done():
	case err := <-serverErr:
		if !errors.Is(err, http.ErrServerClosed) {
			return 1, fmt.Errorf("listen: %w", err)
		}
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		return 1, fmt.Errorf("shutdown: %w", err)
	}
	return 0, nil
}

func reindexCmd(args []string) (int, error) {
	flags, dir := dataFlag("reindex")
	if err := flags.Parse(args); err != nil {
		return 2, err
	}
	if embedderFromEnv() == nil {
		return 1, fmt.Errorf("embeddings are off: set UIREFS_EMBED_URL first")
	}
	st, err := openStore(*dir)
	if err != nil {
		return 1, err
	}
	defer st.Close()
	n, err := st.Reindex(context.Background())
	if err != nil {
		return 1, err
	}
	fmt.Printf("re-embedded %d references\n", n)
	return 0, nil
}

// doctorResult is one check outcome; Status is "ok", "off" or "fail: ...".
type doctorResult struct {
	Name   string `json:"name"`
	Status string `json:"status"`
	OK     bool   `json:"ok"`
}

func doctorCmd(args []string) (int, error) {
	flags := flag.NewFlagSet("doctor", flag.ContinueOnError)
	dir := flags.String("data", "data", "data directory")
	asJSON := flags.Bool("json", false, "print JSON")
	if err := flags.Parse(args); err != nil {
		return 2, err
	}

	var results []doctorResult
	add := func(name, status string, ok bool) {
		results = append(results, doctorResult{Name: name, Status: status, OK: ok})
	}

	// Data dir: exists (or can be created) and is writable.
	dirOK := true
	dirStatus := "ok"
	probe := filepath.Join(*dir, ".doctor-probe")
	if err := os.MkdirAll(*dir, 0o755); err != nil {
		dirOK, dirStatus = false, "fail: "+err.Error()
	} else if err := os.WriteFile(probe, []byte("ok"), 0o644); err != nil {
		dirOK, dirStatus = false, "fail: "+err.Error()
	} else {
		os.Remove(probe)
	}
	add("data_dir", dirStatus, dirOK)

	// FTS5: opening the store creates the FTS5 table; a MATCH query proves it.
	ftsOK := false
	ftsStatus := "ok"
	st, err := openStore(*dir)
	if err != nil {
		ftsStatus = "fail: " + err.Error()
	} else {
		if _, _, err := st.Search(context.Background(), store.SearchQuery{Q: "doctor probe"}); err != nil {
			ftsStatus = "fail: " + err.Error()
		} else {
			ftsOK = true
		}
		st.Close()
	}
	add("fts5", ftsStatus, ftsOK)

	// Embeddings: "off" is healthy; only a configured-but-unreachable
	// endpoint fails.
	embStatus := "off"
	embOK := true
	if e := embedderFromEnv(); e != nil {
		embStatus = "ok"
		if _, err := e.Embed(context.Background(), []string{"doctor probe"}); err != nil {
			embStatus = "fail: " + err.Error()
			embOK = false
		}
	}
	add("embeddings", embStatus, embOK)

	allOK := dirOK && ftsOK && embOK
	if *asJSON {
		type report struct {
			OK      bool           `json:"ok"`
			Results []doctorResult `json:"results"`
		}
		out, _ := json.MarshalIndent(report{OK: allOK, Results: results}, "", "  ")
		fmt.Println(string(out))
	} else {
		for _, r := range results {
			fmt.Printf("%-12s %s\n", r.Name, r.Status)
		}
		if !allOK {
			fmt.Println("doctor: failed")
		}
	}
	if !allOK {
		return 1, nil
	}
	return 0, nil
}
