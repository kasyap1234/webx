package eval

// Citation-verifier eval — precision/recall of VerifyCitations against labeled
// supported/unsupported claim-evidence pairs. This measures a feature none of
// the competitors expose, so the bar is our own correctness.

import (
	_ "embed"
	"fmt"
	"strings"

	"github.com/kasyap1234/webx/internal/serve"
	"gopkg.in/yaml.v3"
)

//go:embed citeset.yaml
var defaultCiteSet []byte

// CitePair is one labeled claim/evidence judgment.
type CitePair struct {
	Claim     string `yaml:"claim"`
	Evidence  string `yaml:"evidence"`
	Supported bool   `yaml:"supported"` // does the evidence actually support the claim?
}

// CiteSet is the citation eval corpus.
type CiteSet struct {
	Pairs []CitePair `yaml:"pairs"`
}

// CiteReport aggregates verifier accuracy.
type CiteReport struct {
	Pairs     int      `json:"pairs"`
	TP        int      `json:"tp"`
	FP        int      `json:"fp"`
	TN        int      `json:"tn"`
	FN        int      `json:"fn"`
	Precision float64  `json:"precision"`
	Recall    float64  `json:"recall"`
	Accuracy  float64  `json:"accuracy"`
	Errors    []string `json:"errors,omitempty"` // pairs where prediction ≠ label
}

// LoadCiteSet parses the citation eval set; path empty → bundled.
func LoadCiteSet(path string) (CiteSet, error) {
	data := defaultCiteSet
	if path != "" {
		var err error
		if data, err = readFile(path); err != nil {
			return CiteSet{}, err
		}
	}
	var s CiteSet
	return s, yaml.Unmarshal(data, &s)
}

// RunCite scores the verifier: claim "C [1]" against evidence {1: evidence}.
func RunCite(s CiteSet) *CiteReport {
	rep := &CiteReport{Pairs: len(s.Pairs)}
	for _, p := range s.Pairs {
		verified, _ := serve.VerifyCitations(p.Claim+" [1]", map[int]string{1: p.Evidence})
		predicted := len(verified) > 0
		switch {
		case p.Supported && predicted:
			rep.TP++
		case !p.Supported && predicted:
			rep.FP++
			rep.Errors = append(rep.Errors, "false-accept: "+truncate(p.Claim, 60))
		case p.Supported && !predicted:
			rep.FN++
			rep.Errors = append(rep.Errors, "false-reject: "+truncate(p.Claim, 60))
		default:
			rep.TN++
		}
	}
	if rep.TP+rep.FP > 0 {
		rep.Precision = float64(rep.TP) / float64(rep.TP+rep.FP)
	}
	if rep.TP+rep.FN > 0 {
		rep.Recall = float64(rep.TP) / float64(rep.TP+rep.FN)
	}
	if rep.Pairs > 0 {
		rep.Accuracy = float64(rep.TP+rep.TN) / float64(rep.Pairs)
	}
	return rep
}

// Text renders the citation-verifier report.
func (r *CiteReport) Text() string {
	var b strings.Builder
	fmt.Fprintf(&b, "webx eval --cite — %d labeled pairs\n", r.Pairs)
	fmt.Fprintf(&b, "precision %.0f%%  recall %.0f%%  accuracy %.0f%%  (tp %d fp %d tn %d fn %d)\n",
		r.Precision*100, r.Recall*100, r.Accuracy*100, r.TP, r.FP, r.TN, r.FN)
	for _, e := range r.Errors {
		fmt.Fprintf(&b, "  %s\n", e)
	}
	return b.String()
}
