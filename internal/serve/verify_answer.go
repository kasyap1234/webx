package serve

import (
	"regexp"
	"strings"
)

// Citation verification — the ALCE-style groundedness check. An [n] marker
// is only honest if the claim it tags is actually supported by source n's
// excerpt; LLMs routinely cite sources that say something adjacent. The
// cheap deterministic proxy: the cited sentence's content words must
// substantially appear in the cited source's evidence text.

var (
	citeMarker = regexp.MustCompile(`\[(\d+)\]`)
	stopWords  = map[string]bool{
		"the": true, "and": true, "for": true, "that": true, "this": true,
		"with": true, "from": true, "are": true, "was": true, "were": true,
		"is": true, "be": true, "been": true, "has": true, "have": true,
		"had": true, "not": true, "but": true, "they": true, "their": true,
		"its": true, "also": true, "into": true, "than": true, "then": true,
		"them": true, "when": true, "which": true, "will": true, "would": true,
		"can": true, "could": true, "should": true, "may": true, "might": true,
		"about": true, "over": true, "such": true, "each": true, "other": true,
		"more": true, "most": true, "some": true, "any": true, "all": true,
		"very": true, "just": true, "only": true, "same": true, "how": true,
		"what": true, "who": true, "there": true, "here": true, "where": true,
		"these": true, "those": true, "does": true, "did": true, "done": true,
	}
	// numWords are spelled-out quantities — like digits, they carry the
	// claim and must all appear in the cited source.
	numWords = map[string]bool{
		"one": true, "two": true, "three": true, "four": true, "five": true,
		"six": true, "seven": true, "eight": true, "nine": true, "ten": true,
		"eleven": true, "twelve": true, "thirteen": true, "fourteen": true,
		"fifteen": true, "sixteen": true, "seventeen": true, "eighteen": true,
		"nineteen": true, "twenty": true, "thirty": true, "forty": true,
		"fifty": true, "sixty": true, "seventy": true, "eighty": true,
		"ninety": true, "hundred": true, "thousand": true, "million": true,
		"billion": true, "trillion": true, "half": true, "quarter": true,
	}
)

// verifyCitations checks each [n] marker in the answer: the sentence it
// tags must be supported by source n's evidence. Returns verified and
// unverified marker numbers.
func VerifyCitations(answer string, evidenceByN map[int]string) (verified, unverified []int) {
	seen := map[int]bool{}
	for _, loc := range citeMarker.FindAllStringSubmatchIndex(answer, -1) {
		ns := answer[loc[2]:loc[3]]
		var n int
		for _, c := range ns {
			n = n*10 + int(c-'0')
		}
		if seen[n] {
			continue
		}
		seen[n] = true
		sentence := citeSentence(answer, loc[0])
		if evidenceByN[n] == "" || !supported(sentence, evidenceByN[n]) {
			unverified = append(unverified, n)
			continue
		}
		verified = append(verified, n)
	}
	return
}

// citeSentence returns the sentence containing the marker at byte offset.
func citeSentence(answer string, markerAt int) string {
	start := 0
	for i := markerAt; i > 0; i-- {
		if c := answer[i-1]; c == '.' || c == '\n' || c == '!' || c == '?' {
			start = i
			break
		}
	}
	end := len(answer)
	for i := markerAt; i < len(answer); i++ {
		if c := answer[i]; c == '.' || c == '\n' || c == '!' || c == '?' {
			end = i + 1
			break
		}
	}
	return answer[start:end]
}

// supported checks whether the sentence's content words appear in the
// evidence — ≥60% of distinctive terms plus three hard requirements:
// EVERY numeric token, EVERY spelled-out number word, and EVERY mid-sentence
// capitalized word (entities — "invented by Google" must cite evidence that
// actually mentions Google; word-overlap alone passes it otherwise).
func supported(sentence, evidence string) bool {
	ev := strings.ToLower(evidence)
	// Strip [n] markers — the cite index is not claim content and would
	// be checked as a numeric token otherwise.
	sentence = citeMarker.ReplaceAllString(sentence, "")
	isSep := func(r rune) bool {
		return !(r >= 'a' && r <= 'z') && !(r >= 'A' && r <= 'Z') &&
			!(r >= '0' && r <= '9') && r != '\''
	}
	raw := strings.FieldsFunc(sentence, isSep)
	lower := strings.FieldsFunc(strings.ToLower(sentence), isSep)
	var hits, total int
	for i, w := range lower {
		if w == "" || stopWords[w] {
			continue
		}
		numeric := w[0] >= '0' && w[0] <= '9'
		// Mid-sentence capitalized tokens are entity mentions (Go, Google,
		// SQLite) — the first token is exempt since sentence-initial case
		// is just grammar, not an entity signal. Checked before the length
		// filter so short entities ("Go") still count.
		if i > 0 && i < len(raw) && len(raw[i]) >= 2 &&
			raw[i][0] >= 'A' && raw[i][0] <= 'Z' && !strings.Contains(ev, w) &&
			!acronymInEvidence(ev, raw[i]) {
			return false
		}
		if !numeric && len(w) < 4 {
			continue
		}
		// $350-style figures normalize to digits — "350" and "$350bn"
		// share the meaningful token.
		if numeric {
			// Any digit run in the source that equals the number counts —
			// handles "$350 billion" vs "350bn" formatting differences.
			if !digitPresent(ev, strings.Trim(w, "$€£.,%")) {
				return false
			}
			continue
		}
		if numWords[w] && !strings.Contains(ev, w) {
			return false
		}
		total++
		if strings.Contains(ev, w) {
			hits++
		}
	}
	if total == 0 {
		return false
	}
	return float64(hits)/float64(total) >= 0.6
}

// acronymInEvidence handles "MCP" ↔ "Model Context Protocol": an all-caps
// claim token is also satisfied by a run of evidence words whose initials
// spell it. Mixed-case entities (Google) don't reach here — they require
// the literal mention.
func acronymInEvidence(ev, acronym string) bool {
	for _, r := range acronym {
		if r < 'A' || r > 'Z' {
			return false // not all-caps → not an acronym
		}
	}
	words := strings.FieldsFunc(ev, func(r rune) bool {
		return !(r >= 'a' && r <= 'z')
	})
	n := len(acronym)
	if n < 2 || len(words) < n {
		return false
	}
	for i := 0; i+n <= len(words); i++ {
		match := true
		for j := 0; j < n; j++ {
			w := words[i+j]
			if w == "" || w[0] != byte(acronym[j]-'A'+'a') {
				match = false
				break
			}
		}
		if match {
			return true
		}
	}
	return false
}

// digitPresent reports whether the numeric token appears in the evidence
// as a standalone number — "20" must not match inside "2024" or "200".
func digitPresent(ev, num string) bool {
	i := 0
	for {
		j := strings.Index(ev[i:], num)
		if j < 0 {
			return false
		}
		j += i
		before := j > 0 && (ev[j-1] >= '0' && ev[j-1] <= '9')
		after := j+len(num) < len(ev) && (ev[j+len(num)] >= '0' && ev[j+len(num)] <= '9')
		if !before && !after {
			return true
		}
		i = j + 1
	}
}

// evidenceSections splits the "## [n] title" evidence block per source so
// each citation is checked against its own source only.
func evidenceSections(evidence string) map[int]string {
	out := map[int]string{}
	re := regexp.MustCompile(`## \[(\d+)\] `)
	idx := re.FindAllStringSubmatchIndex(evidence, -1)
	for i, loc := range idx {
		var n int
		for _, c := range evidence[loc[2]:loc[3]] {
			n = n*10 + int(c-'0')
		}
		end := len(evidence)
		if i+1 < len(idx) {
			end = idx[i+1][0]
		}
		out[n] = evidence[loc[1]:end]
	}
	return out
}
