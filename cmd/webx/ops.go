package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"github.com/go-rod/rod/lib/launcher"
	"github.com/kasyap1234/webx/client"
	"github.com/kasyap1234/webx/internal/license"
	"github.com/kasyap1234/webx/internal/mcp"
	"github.com/kasyap1234/webx/internal/serve"
	"github.com/kasyap1234/webx/internal/store"
	"github.com/kasyap1234/webx/web/eval"
	"github.com/kasyap1234/webx/web/fetch"
	"github.com/kasyap1234/webx/web/search"
	"github.com/spf13/cobra"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"
)

var mcpCmd = &cobra.Command{
	Use:   "mcp",
	Short: "Run webx as an MCP server (stdio) for coding agents",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		return mcp.Serve(cmd.Context(), cmd.InOrStdin(), cmd.OutOrStdout())
	},
}

var evalCmd = &cobra.Command{
	Use:   "eval",
	Short: "Score search quality on the bundled dev-query set (hit rate + MRR)",
	Args:  cobra.NoArgs,
	RunE:  runEval,
}

var (
	eNum       int
	eProviders string
	eSet       string
	eExtract   bool
	eCite      bool
	eVs        string
	eJSON      bool
	eGen       bool
	eGenOut    string
	eWorkers   int
)

func runEval(cmd *cobra.Command, args []string) error {
	logf := func(format string, a ...any) {
		fmt.Fprintf(cmd.ErrOrStderr(), format+"\n", a...)
	}

	if eCite {
		set, err := eval.LoadCiteSet(eSet)
		if err != nil {
			return fmt.Errorf("load cite set: %w", err)
		}
		rep := eval.RunCite(set)
		return evalOut(cmd, eJSON, rep)
	}

	if eExtract {
		set, err := eval.LoadExtractSet(eSet)
		if err != nil {
			return fmt.Errorf("load extract set: %w", err)
		}
		if eVs != "" {
			engines, skipped := eval.EnginesFor(strings.Split(eVs, ","))
			rep := eval.RunExtractCompare(cmd.Context(), set, engines, skipped, logf)
			return evalOut(cmd, eJSON, rep)
		}
		rep := eval.RunExtract(cmd.Context(), set, logf)
		fmt.Fprint(cmd.OutOrStdout(), rep.Text())
		return nil
	}

	var set eval.Set
	var err error
	if eGen {
		// Corpus from real data: ORCAS Bing click logs + Stack Exchange
		// top questions + GitHub descriptions — expected hosts come from
		// what users actually clicked, not hand-labeling.
		go_ := eval.DefaultGen()
		go_.Out = eGenOut
		set, err = eval.Generate(cmd.Context(), go_, logf)
		if err != nil {
			return err
		}
		logf("generated %d queries", len(set.Queries))
		if eGenOut != "" {
			if err := set.Write(eGenOut); err != nil {
				return err
			}
			logf("corpus → %s", eGenOut)
		}
	} else {
		set, err = eval.LoadSet(eSet)
	}
	if err != nil {
		return fmt.Errorf("load eval set: %w", err)
	}
	var providers []string
	if eProviders != "" {
		providers = strings.Split(eProviders, ",")
	}
	if eVs != "" {
		engines, skipped := eval.EnginesFor(strings.Split(eVs, ","))
		rep := eval.RunCompare(cmd.Context(), set, eNum, providers, engines, skipped, logf)
		return evalOut(cmd, eJSON, rep)
	}
	rep := eval.Run(cmd.Context(), set, eNum, providers, eWorkers, logf)
	return evalOut(cmd, eJSON, rep)
}

// evalOut prints the text report, or JSON when --json is set (agent pipelines
// and CI regression checks want the machine-readable form).
func evalOut(cmd *cobra.Command, asJSON bool, rep interface {
	Text() string
},
) error {
	if asJSON {
		enc := json.NewEncoder(cmd.OutOrStdout())
		enc.SetIndent("", "  ")
		return enc.Encode(rep)
	}
	fmt.Fprint(cmd.OutOrStdout(), rep.Text())
	return nil
}

var doctorCmd = &cobra.Command{
	Use:   "doctor",
	Short: "Probe every search provider and report liveness",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		fmt.Fprintln(cmd.ErrOrStderr(), "webx: probing providers…")
		hs := search.Diagnose(cmd.Context(), "")
		printRow := func(name string, ok bool, results int, latency time.Duration, note string) {
			mark := "ok  "
			if !ok {
				mark = "FAIL"
			}
			line := fmt.Sprintf("%s %-8s", mark, name)
			if results > 0 {
				line += fmt.Sprintf(" %3d results", results)
			}
			if latency > 0 {
				line += fmt.Sprintf(" %7s", latency)
			}
			if note != "" {
				line += "  " + note
			}
			fmt.Fprintln(cmd.OutOrStdout(), line)
		}
		for _, h := range hs {
			printRow(h.Name, h.OK, h.Results, h.Latency, h.Note)
		}
		// The LLM backend powers extract/verify/research/ask --llm — a dead
		// endpoint is why those degrade, so it belongs in the same report.
		if ok, lat, note := fetch.LLMHealth(cmd.Context()); true {
			printRow("llm", ok, 0, lat, note)
		}
		return nil
	},
}

var skillCmd = &cobra.Command{
	Use:   "skill",
	Short: "Print a SKILL.md teaching agent harnesses how to use webx",
	Long: `Emits an agent skill file. Install it so your coding agent knows when
to reach for webx:

  webx skill > .devin/skills/webx/SKILL.md   # or .claude/skills/, .agents/skills/`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		_, err := fmt.Fprint(cmd.OutOrStdout(), skillMD)
		return err
	},
}
var (
	botkeyOut  string
	botkeyDir  string
	installBin string
)

var botkeyCmd = &cobra.Command{
	Use:   "botkey",
	Short: "Generate a Web Bot Auth signing key + the JWKS to self-host",
	Long: `Creates an ed25519 keypair for draft-meunier-webbotauth-httpsig-protocol.
Host the printed JWKS at /.well-known/http-message-signatures-directory on a
domain you control, then export:

  WEBX_BOT_KEY=<key file>  WEBX_BOT_DIRECTORY=https://<your-domain>/.well-known/http-message-signatures-directory

Signed requests identify webx to verifiers (e.g. Cloudflare zones requiring
Web Bot Auth) — cryptographic identity instead of fingerprint guessing.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		jwks, kid, err := fetch.GenerateBotKey(botkeyOut, botkeyDir)
		if err != nil {
			return err
		}
		w := cmd.OutOrStdout()
		fmt.Fprintf(w, "key written → %s (mode 0600)\nkeyid: %s\n\n", botkeyOut, kid)
		fmt.Fprintf(w, "JWKS — serve this at %s:\n%s\n\n",
			orDefault(botkeyDir, "https://<your-domain>/.well-known/http-message-signatures-directory"), jwks)
		fmt.Fprintf(w, "export WEBX_BOT_KEY=%s\nexport WEBX_BOT_DIRECTORY=%s\n",
			botkeyOut, orDefault(botkeyDir, "https://<your-domain>/.well-known/http-message-signatures-directory"))
		return nil
	},
}

func orDefault(s, d string) string {
	if s == "" {
		return d
	}
	return s
}

// ── license — Ed25519-signed entitlement files (open-core tier) ──────────
//
// Trust licensing, not DRM: MIT code means the check is patchable — the
// license is what honest customers buy and what support attaches to.
// `keygen` creates the maintainer keypair (once, kept private); `gen`
// signs customer files; `check` verifies locally.

var (
	licTier  string
	licEmail string
	licDays  int
	licKey   string
	licOut   string
)

var licenseCmd = &cobra.Command{
	Use:   "license <keygen|gen|check|install|status> [file]",
	Short: "License tooling — generate signing keys, mint license files, verify them",
	Args:  cobra.RangeArgs(1, 2),
	RunE:  runLicense,
}

func runLicense(cmd *cobra.Command, args []string) error {
	switch args[0] {
	case "keygen":
		pub, priv, err := ed25519.GenerateKey(rand.Reader)
		if err != nil {
			return err
		}
		if err := os.WriteFile(licKey, priv, 0o600); err != nil {
			return err
		}
		fmt.Fprintf(cmd.OutOrStdout(),
			"maintainer keypair → %s (private — never commit)\npublic key (paste as embeddedPubKey in internal/license/license.go):\n%s\n\n",
			licKey, base64.StdEncoding.EncodeToString(pub))
		fmt.Fprintf(cmd.OutOrStdout(), "or export WEBX_LICENSE_PUBKEY=%s\n", base64.StdEncoding.EncodeToString(pub))
		return nil

	case "gen":
		priv, err := os.ReadFile(licKey)
		if err != nil {
			return fmt.Errorf("signing key: %w (make one with `webx license keygen --key maintainer.pem`)", err)
		}
		if len(priv) != ed25519.PrivateKeySize {
			return fmt.Errorf("signing key: want %d bytes, got %d", ed25519.PrivateKeySize, len(priv))
		}
		lic := &license.License{
			ID:      "lic_" + time.Now().Format("20060102") + "_" + store.NewID(),
			Email:   licEmail,
			Tier:    licTier,
			Issued:  time.Now().UTC().Format(time.RFC3339),
			Expires: time.Now().UTC().AddDate(0, 0, licDays).Format(time.RFC3339),
		}
		if lic.Tier == "" {
			lic.Tier = license.TierPro
		}
		if err := license.Sign(priv, lic); err != nil {
			return err
		}
		raw, _ := json.MarshalIndent(lic, "", "  ")
		if licOut == "" {
			fmt.Fprintln(cmd.OutOrStdout(), string(raw))
			return nil
		}
		if err := os.WriteFile(licOut, raw, 0o644); err != nil {
			return err
		}
		fmt.Fprintf(cmd.OutOrStdout(), "license → %s (tier=%s, expires=%s)\n", licOut, lic.Tier, lic.Expires)
		return nil

	case "check":
		if len(args) < 2 {
			return fmt.Errorf("usage: webx license check <file>")
		}
		l, err := license.Load(args[1])
		if err != nil {
			return err
		}
		fmt.Fprintf(cmd.OutOrStdout(), "valid — tier=%s email=%s expires=%s features=%v\n",
			l.Tier, l.Email, l.Expires, l.Features)
		return nil

	case "install":
		// Customer-side: verify the signed file, then park it at the
		// well-known path LoadEnv reads — serve/webxd pick it up with
		// no env config. `-` reads stdin (paste from the purchase email).
		if len(args) < 2 {
			return fmt.Errorf("usage: webx license install <file|->")
		}
		var raw []byte
		var err error
		if args[1] == "-" {
			raw, err = io.ReadAll(cmd.InOrStdin())
		} else {
			raw, err = os.ReadFile(args[1])
		}
		if err != nil {
			return err
		}
		l, err := license.Parse(raw)
		if err != nil {
			return err
		}
		dest := license.DefaultPath()
		if dest == "" {
			return fmt.Errorf("no home dir — export WEBX_LICENSE=%s instead", args[1])
		}
		if err := os.MkdirAll(filepath.Dir(dest), 0o700); err != nil {
			return err
		}
		if err := os.WriteFile(dest, raw, 0o600); err != nil {
			return err
		}
		fmt.Fprintf(cmd.OutOrStdout(), "installed → %s (tier=%s, expires=%s)\nserve/webxd pick it up automatically\n",
			dest, l.Tier, l.Expires)
		return nil

	case "status":
		l, err := license.LoadEnv()
		if err != nil {
			return err
		}
		fmt.Fprintf(cmd.OutOrStdout(), "tier=%s\n", l.TierName())
		if l != nil {
			fmt.Fprintf(cmd.OutOrStdout(), "email=%s expires=%s features=%v\n",
				l.Email, l.Expires, l.Features)
		}
		return nil
	}
	return fmt.Errorf("unknown subcommand %q (want keygen|gen|check|install|status)", args[0])
}

var installCmd = &cobra.Command{
	Use:   "install <chrome|lightpanda>",
	Short: "Install a render engine — Chrome for Testing or Lightpanda",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		switch args[0] {
		case "chrome", "chromium":
			path, err := launcher.NewBrowser().Get()
			if err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "chromium → %s\n(webx finds it automatically — or export WEBX_CHROME_BIN=%s)\n", path, path)
			return nil
		case "lightpanda", "light":
			return installLightpanda(cmd, installBin)
		default:
			return fmt.Errorf("unknown engine %q (want chrome|lightpanda)", args[0])
		}
	},
}

// installLightpanda downloads the nightly Lightpanda binary to
// ~/.webx/bin/lightpanda — the Zig engine behind --engine=light.
func installLightpanda(cmd *cobra.Command, binDir string) error {
	asset := map[string]string{
		"darwin/arm64": "aarch64-macos",
		"linux/amd64":  "x86_64-linux",
		"linux/arm64":  "aarch64-linux",
		"darwin/amd64": "x86_64-macos",
	}[runtime.GOOS+"/"+runtime.GOARCH]
	if asset == "" {
		return fmt.Errorf("no lightpanda build for %s/%s — see https://lightpanda.io (WSL on Windows)", runtime.GOOS, runtime.GOARCH)
	}
	u := "https://github.com/lightpanda-io/browser/releases/download/nightly/lightpanda-" + asset
	if binDir == "" {
		if h, err := os.UserHomeDir(); err == nil {
			binDir = filepath.Join(h, ".webx", "bin")
		}
	}
	dest := filepath.Join(binDir, "lightpanda")
	fmt.Fprintf(cmd.ErrOrStderr(), "webx: downloading %s → %s\n", u, dest)
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		return err
	}
	resp, err := http.Get(u) //nolint: public release URL, not user input
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return fmt.Errorf("download: status %d", resp.StatusCode)
	}
	f, err := os.OpenFile(dest, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o755)
	if err != nil {
		return err
	}
	if _, err := io.Copy(f, io.LimitReader(resp.Body, 300<<20)); err != nil {
		f.Close()
		return err
	}
	f.Close()
	fmt.Fprintf(cmd.OutOrStdout(), "lightpanda → %s\nadd to PATH (or run via webx which checks ~/.webx/bin):\n  export PATH=%s:$PATH\n", dest, binDir)
	return nil
}

var serveCmd = &cobra.Command{
	Use:   "serve",
	Short: "Run webx's HTTP API (Firecrawl-shaped routes)",
	Args:  cobra.NoArgs,
	RunE:  runServe,
}

var (
	serveAddr    string
	serveDB      string
	serveWorkers int
)

// remoteCrawl drives an async crawl job on a remote webx/webxd: POST /crawl
// → poll GET /crawl/{id} → print pages (Firecrawl's lifecycle, locally).
func remoteCrawl(cmd *cobra.Command, c *client.Client, args []string) error {
	pages, err := c.Crawl(cmd.Context(), args[0], args[1], cLimit, cDepth, cSemantic,
		func(status string, done, total int) {
			fmt.Fprintf(cmd.ErrOrStderr(), "\rwebx: %s %d/%d   ", status, done, total)
		})
	fmt.Fprint(cmd.ErrOrStderr(), "\r                          \r")
	if err != nil {
		return err
	}
	for _, p := range pages {
		fmt.Fprintf(cmd.OutOrStdout(), "%s\n      %s\n", p.URL, p.Title)
	}
	fmt.Fprintf(cmd.ErrOrStderr(), "webx: remote crawl done — %d pages\n", len(pages))
	return nil
}

var jobsCmd = &cobra.Command{
	Use:   "jobs [id]",
	Short: "List async jobs (or show one). Local jobs.db by default; --api for a remote server",
	Args:  cobra.MaximumNArgs(1),
	RunE:  runJobs,
}

var (
	jobsGC        bool
	jobsOlderThan time.Duration
)

var cancelCmd = &cobra.Command{
	Use:   "cancel <job-id>",
	Short: "Cancel a queued/running job",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if c := apiClient(); c != nil {
			return c.Cancel(cmd.Context(), args[0])
		}
		st, err := store.OpenSQLite(serveDB)
		if err != nil {
			return err
		}
		defer st.Close()
		return st.UpdateJob(cmd.Context(), args[0], func(j *store.Job) {
			j.Status = store.Cancelled
		})
	},
}

func runJobs(cmd *cobra.Command, args []string) error {
	if c := apiClient(); c != nil {
		js, err := c.Jobs(cmd.Context())
		if err != nil {
			return err
		}
		enc := json.NewEncoder(cmd.OutOrStdout())
		enc.SetIndent("", "  ")
		return enc.Encode(js)
	}
	st, err := store.OpenSQLite(serveDB)
	if err != nil {
		return err
	}
	defer st.Close()
	if jobsGC {
		n, err := st.SweepFinished(cmd.Context(), jobsOlderThan)
		if err != nil {
			return err
		}
		fmt.Fprintf(cmd.ErrOrStderr(), "gc: removed %d finished jobs older than %s\n", n, jobsOlderThan)
		return nil
	}
	if len(args) == 1 {
		j, err := st.GetJob(cmd.Context(), args[0])
		if err != nil {
			return err
		}
		pages, _ := st.Pages(cmd.Context(), j.ID, 500, 0)
		enc := json.NewEncoder(cmd.OutOrStdout())
		enc.SetIndent("", "  ")
		return enc.Encode(map[string]any{"job": j, "pages": pages})
	}
	js, err := st.ListJobs(cmd.Context(), 50)
	if err != nil {
		return err
	}
	for _, j := range js {
		fmt.Fprintf(cmd.OutOrStdout(), "%-7s %-9s %3d/%-3d %s\n",
			j.Kind, j.Status, j.Done, j.Total, j.ID)
	}
	return nil
}

var batchCmd = &cobra.Command{
	Use:   "batch <urls-file>",
	Short: "Scrape many URLs — local concurrency, or a server-side job with --api",
	Args:  cobra.ExactArgs(1),
	RunE:  runBatch,
}

var (
	bConcurrency int
	bFormat      string
)

func runBatch(cmd *cobra.Command, args []string) error {
	data, err := os.ReadFile(args[0])
	if err != nil {
		return err
	}
	var urls []string
	for _, l := range strings.Split(string(data), "\n") {
		l = strings.TrimSpace(l)
		if l != "" && !strings.HasPrefix(l, "#") {
			urls = append(urls, l)
		}
	}
	if len(urls) == 0 {
		return fmt.Errorf("no urls in %s", args[0])
	}

	if c := apiClient(); c != nil {
		pages, err := c.Batch(cmd.Context(), urls, map[string]any{
			"browser": browser, "session": session,
			"render": doRender, "auto_render": autoRender,
		}, func(status string, done, total int) {
			fmt.Fprintf(cmd.ErrOrStderr(), "\rwebx: %s %d/%d   ", status, done, total)
		})
		fmt.Fprint(cmd.ErrOrStderr(), "\r                          \r")
		if err != nil {
			return err
		}
		for _, p := range pages {
			fmt.Fprintf(cmd.OutOrStdout(), "%s\n", p.URL)
		}
		return nil
	}

	// Local batch — concurrent fetches, one JSON object per line (NDJSON for
	// agent pipelines; -f md prints each doc separated by a marker).
	type result struct {
		idx int
		doc *fetch.Document
		err error
	}
	results := make([]result, len(urls))
	var wg sync.WaitGroup
	sem := make(chan struct{}, bConcurrency)
	for i, u := range urls {
		wg.Add(1)
		go func(i int, u string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			doc, err := fetch.Fetch(cmd.Context(), fetch.FetchRequest{
				URL: u, Browser: browser, Session: session,
				Render: doRender, AutoRender: autoRender, Timeout: timeout,
			})
			results[i] = result{i, doc, err}
		}(i, u)
	}
	wg.Wait()
	enc := json.NewEncoder(cmd.OutOrStdout())
	for _, r := range results {
		if r.err != nil {
			fmt.Fprintf(cmd.ErrOrStderr(), "webx: %s: %v\n", urls[r.idx], r.err)
			if bFormat == "json" {
				enc.Encode(map[string]any{"url": urls[r.idx], "error": r.err.Error()})
			}
			continue
		}
		if bFormat == "json" {
			enc.Encode(r.doc)
		} else {
			fmt.Fprintf(cmd.OutOrStdout(), "── %s ──\n%s\n\n", r.doc.FinalURL, r.doc.Markdown)
		}
	}
	return nil
}

func runServe(cmd *cobra.Command, args []string) error {
	st, err := store.OpenSQLite(serveDB)
	if err != nil {
		return err
	}
	defer st.Close()
	srv := serve.New(st)
	srv.RunWorkers(cmd.Context(), serveWorkers)
	go srv.RunScheduler(cmd.Context())
	fmt.Fprintf(cmd.ErrOrStderr(), "webx listening on %s\n", serveAddr)
	httpSrv := &http.Server{
		Addr: serveAddr, Handler: srv.Handler(),
		ReadHeaderTimeout: 15 * time.Second,
	}
	return httpSrv.ListenAndServe()
}
