package search

import "testing"

func TestClassify(t *testing.T) {
	cases := []struct {
		q    string
		want QueryKind
	}{
		{"panic: concurrent map read and map write", KindErrorish},
		{"fatal: not a git repository", KindErrorish},
		{"TypeError: cannot read property of undefined", KindErrorish},
		{"mux.HandleFunc", KindIdentifier},
		{"std::vector::push_back", KindIdentifier},
		{"os.Getenv", KindIdentifier},
		{"useEffect react", KindIdentifier},
		{"express.Router", KindIdentifier},
		{"npm ERR! code ERESOLVE unable to resolve dependency tree", KindErrorish},
		{"NullPointerException at com.example.Main", KindErrorish},
		{"how to reverse a linked list", KindHowTo},
		{"what is a goroutine", KindHowTo},
		{"best rust web frameworks 2025", KindFresh},
		{"go 1.23 release notes", KindFresh},
		{"golang websocket chat server tutorial", KindConcept},
	}
	for _, c := range cases {
		if got := Classify(c.q); got != c.want {
			t.Errorf("Classify(%q) = %v, want %v", c.q, got, c.want)
		}
	}
}

func TestWeightsForErrorishBoostsSO(t *testing.T) {
	w := weightsFor(KindErrorish)
	if w["so"] <= w["wiki"] {
		t.Errorf("errorish queries should rank SO over wiki: so=%v wiki=%v", w["so"], w["wiki"])
	}
}

func TestWeightsForIdentifierBoostsIndex(t *testing.T) {
	w := weightsFor(KindIdentifier)
	if w["index"] <= w["ddg"] {
		t.Errorf("identifier queries should rank index over ddg: index=%v ddg=%v", w["index"], w["ddg"])
	}
}
