package render

import "testing"

func TestParseActions(t *testing.T) {
	acts, err := ParseActions("click:.accept | wait:.quote | type:#q=golang | press:enter | sleep:400 | scroll | screenshot")
	if err != nil {
		t.Fatal(err)
	}
	if len(acts) != 7 {
		t.Fatalf("got %d actions", len(acts))
	}
	if acts[0].Op != "click" || acts[0].Arg != ".accept" {
		t.Fatalf("action0 = %+v", acts[0])
	}
	if acts[2].Op != "type" || acts[2].Arg != "#q" || acts[2].Arg2 != "golang" {
		t.Fatalf("type = %+v", acts[2])
	}
	if acts[3].Arg != "enter" || acts[4].Arg != "400" {
		t.Fatalf("press/sleep = %+v %+v", acts[3], acts[4])
	}
}

func TestParseActionsErrors(t *testing.T) {
	for _, bad := range []string{"frobnicate:x", "wait:", "type:#sel", "sleep:abc", "click:", "get:", "fill:#x"} {
		if _, err := ParseActions(bad); err == nil {
			t.Fatalf("%q should fail", bad)
		}
	}
}

func TestParseActionsRefOps(t *testing.T) {
	acts, err := ParseActions("fill:@e3=me@x.com | get:@e1 | hover:.menu | type:@e2=hi")
	if err != nil {
		t.Fatal(err)
	}
	if acts[0].Op != "fill" || acts[0].Arg != "@e3" || acts[0].Arg2 != "me@x.com" {
		t.Fatalf("fill parse: %+v", acts[0])
	}
	if acts[1].Op != "get" || acts[1].Arg != "@e1" {
		t.Fatalf("get parse: %+v", acts[1])
	}
	if acts[3].Op != "type" || acts[3].Arg != "@e2" {
		t.Fatalf("type-ref parse: %+v", acts[3])
	}
}

func TestParseActionsEmpty(t *testing.T) {
	acts, err := ParseActions("  |  ")
	if err != nil || len(acts) != 0 {
		t.Fatalf("empty spec = %v %v", acts, err)
	}
}

func TestParseActionsActAndRef(t *testing.T) {
	acts, err := ParseActions("act:accept the cookies | click:@e3 | eval:document.title")
	if err != nil {
		t.Fatal(err)
	}
	if acts[0].Op != "act" || acts[0].Arg != "accept the cookies" {
		t.Fatalf("act parse: %+v", acts[0])
	}
	if acts[1].Arg != "@e3" {
		t.Fatalf("ref click parse: %+v", acts[1])
	}
	if _, err := ParseActions("act:"); err == nil {
		t.Fatal("empty act should error")
	}
}

// \| inside an argument must not end the action — JS in eval: and CSS
// attribute values like [data-x="a|b"] legitimately contain pipes.
func TestParseActionsEscapedPipe(t *testing.T) {
	acts, err := ParseActions(`eval:x==='a\|b' | click:#go`)
	if err != nil {
		t.Fatal(err)
	}
	if len(acts) != 2 {
		t.Fatalf("want 2 actions, got %d: %+v", len(acts), acts)
	}
	if acts[0].Arg != `x==='a|b'` {
		t.Fatalf("eval arg = %q", acts[0].Arg)
	}
	if acts[1].Op != "click" || acts[1].Arg != "#go" {
		t.Fatalf("click = %+v", acts[1])
	}
}
