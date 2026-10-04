package serve

import "testing"

func TestVerifyCitations(t *testing.T) {
	evidence := "## [1] SpaceX valuation\nhttps://g.example/x\n\n> SpaceX was valued at $350 billion in the latest round.\n\n## [2] Other\nhttps://o.example/\n\n> Cats sleep fifteen hours daily.\n"
	ans := "SpaceX hit a $350 billion valuation in its latest funding round [1]. Cats sleep twenty hours [2]."
	v, u := VerifyCitations(ans, evidenceSections(evidence))
	if len(v) != 1 || v[0] != 1 {
		t.Fatalf("verified = %v", v)
	}
	if len(u) != 1 || u[0] != 2 {
		t.Fatalf("unverified = %v", u)
	}
}

func TestCiteSentence(t *testing.T) {
	s := citeSentence("First line. SpaceX is valued highly [1] per reports. Third.", 33)
	if s != " SpaceX is valued highly [1] per reports." {
		t.Fatalf("sentence = %q", s)
	}
}
