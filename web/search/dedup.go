package search

import (
	"hash/fnv"
	"math/bits"
	"regexp"
	"strings"
)

// Near-duplicate detection — syndicated copies and mirror pages share
// content under different URLs, which URL-keyed dedup can't catch.
// Standard approach (Charikar simhash over word shingles, Manku et al.
// WWW'07): a 64-bit fingerprint whose Hamming distance ≤3 flags near-dups.

// simhash64 fingerprints text via 3-word shingles: each shingle's FNV hash
// votes ±1 on each bit position; the sign pattern is the fingerprint.
func simhash64(text string) uint64 {
	words := normWords(text)
	if len(words) == 0 {
		return 0
	}
	var v [64]int
	h := fnv.New64a()
	const w = 3
	for i := 0; i+w <= len(words); i++ {
		h.Reset()
		for j := i; j < i+w; j++ {
			h.Write([]byte(words[j]))
			h.Write([]byte{0})
		}
		s := h.Sum64()
		for b := 0; b < 64; b++ {
			if s>>uint(b)&1 == 1 {
				v[b]++
			} else {
				v[b]--
			}
		}
	}
	var fp uint64
	for b := 0; b < 64; b++ {
		if v[b] > 0 {
			fp |= 1 << uint(b)
		}
	}
	return fp
}

func hamming(a, b uint64) int { return bits.OnesCount64(a ^ b) }

// nearDupThreshold — Google used ≤3/64 for web-scale corpora, but short
// documents have few shingles so a small edit flips several bits. ≤8 stays
// far below the ~32-bit expected distance of unrelated docs while tolerating
// minor edits on short scraped pages. Under-dedup beats over-dedup: a kept
// duplicate costs a result slot, a dropped distinct page loses information.
const nearDupThreshold = 8

var nonWord = regexp.MustCompile(`[^\p{L}\p{N}]+`)

// normWords lowercases and splits text into words — shared normalization
// for simhash shingles and title dedup.
func normWords(s string) []string {
	s = nonWord.ReplaceAllString(strings.ToLower(s), " ")
	return strings.Fields(s)
}

// titleKey normalizes a title for exact-dup detection — syndicated copies
// usually keep the headline verbatim even across domains.
func titleKey(title string) string {
	return strings.Join(normWords(title), " ")
}

// dedupResults drops near-duplicates, keeping the higher-scored copy.
// titles catches syndication without a fetch; hashes catches near-identical
// scraped bodies. Returns the kept list, the content map re-keyed to kept
// positions, and the number dropped.
func dedupResults(results []Result, contents map[int]string) ([]Result, map[int]string, int) {
	seenTitle := map[string]bool{}
	var hashes []uint64
	kept := make([]Result, 0, len(results))
	rekeyed := make(map[int]string, len(contents))
	dropped := 0
	for i, r := range results {
		if tk := titleKey(r.Title); tk != "" && len(tk) >= 15 {
			if seenTitle[tk] {
				dropped++
				continue
			}
			seenTitle[tk] = true
		}
		if md := contents[i]; md != "" {
			fp := simhash64(md)
			dup := false
			for _, h := range hashes {
				if hamming(fp, h) <= nearDupThreshold {
					dup = true
					break
				}
			}
			if dup {
				dropped++
				continue
			}
			hashes = append(hashes, fp)
			rekeyed[len(kept)] = md
		}
		kept = append(kept, r)
	}
	return kept, rekeyed, dropped
}
