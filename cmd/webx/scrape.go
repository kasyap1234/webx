package main

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"github.com/kasyap1234/webx/web/fetch"
	"github.com/kasyap1234/webx/web/render"
	"github.com/spf13/cobra"
	"os"
	"time"
)

var scrapeCmd = &cobra.Command{
	Use:     "scrape <url>",
	Aliases: []string{"fetch"},
	Short:   "Scrape a URL and print it as clean markdown",
	Args:    cobra.ExactArgs(1),
	RunE:    runScrape,
}

var (
	format     string
	maxChars   int
	raw        bool
	timeout    time.Duration
	userAgent  string
	browser    bool
	session    string
	doRender   bool
	autoRender bool
	waitFor    string
	scrolls    int
	actions    string
	screenshot string
	proxy      string
	fit        string
	incSel     []string
	excSel     []string
	wantSum    bool
	wantImgs   bool
	wantRefSum bool
	wantBrand  bool
	stealth    bool
	profile    string
	blockAds   bool
	textMode   bool
	mobile     bool
	locale     string
	tz         string
	netCap     bool
	conCap     bool
	pdfOut     string
	mhtmlOut   string
	insecure   bool
	pageSess   string
	pierce     bool
	scrollSel  string
	scrollBy   float64
	maxTokens  int
	wantLinks  bool
	wantChunks bool
	wantA11y   bool
	wantAgentR bool
	engine     string
	cookieFile string
	retryAfter bool
	transcript bool
	redactPII  bool
	apiURL     string
)

func runScrape(cmd *cobra.Command, args []string) error {
	if actions != "" {
		if _, err := render.ParseActions(actions); err != nil {
			return fmt.Errorf("--actions: %w", err)
		}
	}
	req := fetch.FetchRequest{
		URL:              args[0],
		Raw:              raw,
		Timeout:          timeout,
		UserAgent:        userAgent,
		Browser:          browser,
		Session:          session,
		Render:           doRender || actions != "" || screenshot != "" || pdfOut != "" || mhtmlOut != "" || netCap || conCap || wantA11y,
		AutoRender:       autoRender,
		WaitFor:          waitFor,
		Scrolls:          scrolls,
		Actions:          actions,
		Screenshot:       screenshot != "",
		Proxy:            proxy,
		Stealth:          stealth,
		Profile:          profile,
		BlockTrackers:    blockAds,
		BlockMedia:       textMode,
		Mobile:           mobile,
		Locale:           locale,
		Timezone:         tz,
		NetworkCapture:   netCap,
		ConsoleCapture:   conCap,
		WantPDF:          pdfOut != "",
		WantMHTML:        mhtmlOut != "",
		SkipTLS:          insecure,
		PageSession:      pageSess,
		PierceDOM:        pierce,
		ScrollSelector:   scrollSel,
		ScrollBy:         scrollBy,
		Fit:              fit,
		IncludeSelectors: incSel, ExcludeSelectors: excSel,
		Summary: wantSum, WantLinks: wantLinks || wantRefSum,
		WantImages: wantImgs || wantRefSum, LinkSummary: wantRefSum,
		WantBrand: wantBrand, WantChunks: wantChunks,
		WantA11y: wantA11y, WantAgentReady: wantAgentR,
		RenderEngine: engine, CookieFile: cookieFile, RetryAfter: retryAfter,
		Transcript: transcript, TranscriptLang: locale, RedactPII: redactPII,
	}
	var doc *fetch.Document
	var err error
	if c := apiClient(); c != nil {
		doc, err = c.Scrape(cmd.Context(), req)
	} else {
		doc, err = fetch.Fetch(cmd.Context(), req)
	}
	if err != nil {
		return err
	}
	// --api mode redacts client-side too — the server applies the same
	// masking when it sees the format, but a remote doc arrives unmasked.
	if redactPII {
		doc.Redact()
	}
	if maxTokens > 0 && (maxChars == 0 || maxTokens*4 < maxChars) {
		maxChars = maxTokens * 4
	}
	if maxChars > 0 && len(doc.Markdown) > maxChars {
		doc.Markdown = doc.Markdown[:maxChars]
		doc.Truncated = true
	}
	// --screenshot <path> writes the PNG file; JSON consumers get
	// screenshot_b64 inline instead.
	if screenshot != "" && doc.ScreenshotB64 != "" && format != "json" {
		if png, derr := base64.StdEncoding.DecodeString(doc.ScreenshotB64); derr == nil {
			if werr := os.WriteFile(screenshot, png, 0o644); werr != nil {
				fmt.Fprintf(cmd.ErrOrStderr(), "webx: screenshot: %v\n", werr)
			} else {
				fmt.Fprintf(cmd.ErrOrStderr(), "webx: screenshot → %s\n", screenshot)
			}
			doc.ScreenshotB64 = ""
		}
	}
	if pdfOut != "" && doc.PDFB64 != "" {
		if pdf, derr := base64.StdEncoding.DecodeString(doc.PDFB64); derr == nil {
			if werr := os.WriteFile(pdfOut, pdf, 0o644); werr != nil {
				fmt.Fprintf(cmd.ErrOrStderr(), "webx: pdf: %v\n", werr)
			} else {
				fmt.Fprintf(cmd.ErrOrStderr(), "webx: pdf → %s\n", pdfOut)
			}
			doc.PDFB64 = ""
		}
	}
	if mhtmlOut != "" && doc.MHTML != "" {
		if werr := os.WriteFile(mhtmlOut, []byte(doc.MHTML), 0o644); werr != nil {
			fmt.Fprintf(cmd.ErrOrStderr(), "webx: mhtml: %v\n", werr)
		} else {
			fmt.Fprintf(cmd.ErrOrStderr(), "webx: mhtml → %s\n", mhtmlOut)
		}
		doc.MHTML = ""
	}
	switch format {
	case "json":
		enc := json.NewEncoder(cmd.OutOrStdout())
		enc.SetIndent("", "  ")
		return enc.Encode(doc)
	case "md", "markdown":
		if doc.Truncated {
			fmt.Fprintf(cmd.ErrOrStderr(), "webx: output truncated to %d chars\n", maxChars)
		}
		if fit != "" && doc.FitMarkdown != "" {
			fmt.Fprintf(cmd.ErrOrStderr(), "webx: fit → %d chars of %d\n", len(doc.FitMarkdown), doc.TextLength)
			_, err = fmt.Fprintln(cmd.OutOrStdout(), doc.FitMarkdown)
			return err
		}
		_, err = fmt.Fprintln(cmd.OutOrStdout(), doc.Markdown)
		return err
	default:
		return fmt.Errorf("unknown --format %q (want md|json)", format)
	}
}
