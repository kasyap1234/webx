package main

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/kasyap1234/webx/internal/license"
	"github.com/kasyap1234/webx/internal/store"
	"github.com/spf13/cobra"
)

// bufCmd gives a RunE a cobra context with captured output.
func bufCmd() (*cobra.Command, *bytes.Buffer) {
	c := &cobra.Command{}
	c.SetContext(context.Background())
	buf := &bytes.Buffer{}
	c.SetOut(buf)
	return c, buf
}

// TestLicenseRoundTrip exercises keygen → gen → check end to end — the
// maintainer flow is the whole monetization edge and had zero coverage.
func TestLicenseRoundTrip(t *testing.T) {
	dir := t.TempDir()
	licKey = filepath.Join(dir, "maintainer.pem")
	licOut = filepath.Join(dir, "lic.json")
	licEmail, licTier, licDays = "cust@ex.com", "pro", 30
	t.Cleanup(func() { licKey, licOut, licEmail, licDays = "", "", "", 365 })

	cmd, _ := bufCmd()
	if err := runLicense(cmd, []string{"keygen"}); err != nil {
		t.Fatalf("keygen: %v", err)
	}
	priv, err := os.ReadFile(licKey)
	if err != nil || len(priv) != ed25519.PrivateKeySize {
		t.Fatalf("key file: %v len=%d", err, len(priv))
	}
	// Point the verifier at the throwaway key's public half.
	pub := ed25519.PrivateKey(priv).Public().(ed25519.PublicKey)
	t.Setenv("WEBX_LICENSE_PUBKEY", base64.StdEncoding.EncodeToString(pub))

	if err := runLicense(cmd, []string{"gen"}); err != nil {
		t.Fatalf("gen: %v", err)
	}
	l, err := license.Load(licOut)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if l.Email != "cust@ex.com" || l.Tier != license.TierPro {
		t.Fatalf("license fields: %+v", l)
	}
	cmd2, out := bufCmd()
	if err := runLicense(cmd2, []string{"check", licOut}); err != nil {
		t.Fatalf("check: %v", err)
	}
	if !strings.Contains(out.String(), "valid") {
		t.Fatalf("check output: %q", out.String())
	}
}

// TestJobsGC covers the --gc sweep path on the local jobs store.
func TestJobsGC(t *testing.T) {
	dir := t.TempDir()
	serveDB = filepath.Join(dir, "jobs.db")
	jobsGC = true
	jobsOlderThan = -time.Second // sweep everything terminal
	t.Cleanup(func() { serveDB, jobsGC = "", false })

	st, err := store.OpenSQLite(serveDB)
	if err != nil {
		t.Fatal(err)
	}
	j := &store.Job{ID: "t1", Kind: "crawl"}
	if err := st.CreateJob(context.Background(), j); err != nil {
		t.Fatal(err)
	}
	_ = st.UpdateJob(context.Background(), "t1", func(j *store.Job) { j.Status = store.Completed })
	st.Close()

	cmd, _ := bufCmd()
	if err := runJobs(cmd, nil); err != nil {
		t.Fatal(err)
	}
	st2, err := store.OpenSQLite(serveDB)
	if err != nil {
		t.Fatal(err)
	}
	defer st2.Close()
	if _, err := st2.GetJob(context.Background(), "t1"); err == nil {
		t.Fatal("gc should have removed the finished job")
	}
}

// TestSkillCommand emits the SKILL.md payload — guard against an empty body.
func TestSkillCommand(t *testing.T) {
	cmd, out := bufCmd()
	if err := skillCmd.RunE(cmd, nil); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "webx") || len(out.String()) < 200 {
		t.Fatalf("skill output looks empty: %d bytes", out.Len())
	}
}

// TestCommandsRegistered guards the split-file refactor — every command
// must still be reachable on the root.
func TestCommandsRegistered(t *testing.T) {
	want := []string{"scrape", "search", "map", "ask", "mcp", "eval", "doctor",
		"index", "query", "seed", "crawl", "research", "verify", "skill",
		"similar", "llms", "botkey", "license", "install", "wayback", "cdx",
		"archive", "extract", "diff", "watch", "serve", "jobs", "cancel", "batch"}
	have := map[string]bool{}
	for _, c := range rootCmd.Commands() {
		have[c.Name()] = true
	}
	for _, w := range want {
		if !have[w] {
			t.Errorf("command %q not registered", w)
		}
	}
}
