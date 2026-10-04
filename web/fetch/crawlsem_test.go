package fetch

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// With an embedder wired, a page about "cats" should outscore one about
// "cars" for the query "feline pets" even with zero keyword overlap —
// that's the whole point of semantic crawl scoring.
func TestCrawlSemanticScoring(t *testing.T) {
	var body string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/":
			body = `<html><body><a href="` + "/cats" + `">c</a><a href="/cars">d</a></body></html>`
		case "/cats":
			body = `<html><head><title>cats</title></head><body><p>Felines sleep sixteen hours daily and knead soft blankets</p></body></html>`
		case "/cars":
			body = `<html><head><title>cars</title></head><body><p>Engines horsepower torque transmission brake specs</p></body></html>`
		}
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, body)
	}))
	defer srv.Close()

	// Fake embedder: 2-dim vectors — query "pets" → [1,0], cat-ish text →
	// high x, car-ish → high y. No keyword overlap with the query.
	embed := func(_ context.Context, text string) ([]float32, error) {
		low := strings.ToLower(text)
		if strings.Contains(low, "pets") {
			return []float32{1, 0}, nil
		}
		if strings.Contains(low, "felines") || strings.Contains(low, "knead") {
			return []float32{0.95, 0.05}, nil
		}
		return []float32{0.05, 0.95}, nil
	}

	res, err := Crawl(context.Background(), CrawlRequest{
		Start: srv.URL, Query: "domestic pets care",
		Limit: 3, Depth: 1, Threshold: 0.01, RatePerSec: 100,
		Embedder: embed,
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	var catScore, carScore float64
	for _, p := range res.Pages {
		if strings.Contains(p.FinalURL, "cats") {
			catScore = p.Relevance
		}
		if strings.Contains(p.FinalURL, "cars") {
			carScore = p.Relevance
		}
	}
	if catScore <= carScore {
		t.Fatalf("semantic scoring: cats=%.3f cars=%.3f — embeddings should rank meaning over keywords", catScore, carScore)
	}
}

// No embedder → pure lexical path still works (regression guard).
func TestCrawlLexicalStillWorks(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, `<html><body><p>kubernetes auth tokens certificates</p></body></html>`)
	}))
	defer srv.Close()
	res, err := Crawl(context.Background(), CrawlRequest{
		Start: srv.URL, Query: "kubernetes auth",
		Limit: 1, Threshold: 0.1, RatePerSec: 100,
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Pages) != 1 || res.Pages[0].Relevance < 0.9 {
		t.Fatalf("lexical relevance broken: %+v", res.Pages)
	}
}
