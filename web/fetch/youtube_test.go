package fetch

import (
	"encoding/json"
	"testing"
)

func TestYouTubeID(t *testing.T) {
	cases := map[string]string{
		"https://www.youtube.com/watch?v=dQw4w9WgXcQ":        "dQw4w9WgXcQ",
		"https://youtu.be/dQw4w9WgXcQ":                       "dQw4w9WgXcQ",
		"https://youtu.be/dQw4w9WgXcQ?t=42":                  "dQw4w9WgXcQ",
		"https://www.youtube.com/shorts/dQw4w9WgXcQ":         "dQw4w9WgXcQ",
		"https://www.youtube.com/embed/dQw4w9WgXcQ":          "dQw4w9WgXcQ",
		"https://www.youtube.com/live/dQw4w9WgXcQ":           "dQw4w9WgXcQ",
		"https://www.youtube.com/watch?v=dQw4w9WgXcQ&list=x": "dQw4w9WgXcQ",
		"https://m.youtube.com/watch?v=dQw4w9WgXcQ":          "dQw4w9WgXcQ",
		"dQw4w9WgXcQ": "dQw4w9WgXcQ",
		"https://example.com/watch?v=dQw4w9WgXcQ": "",
		"https://www.youtube.com/user/someone":    "",
		"https://www.youtube.com/watch":           "",
	}
	for in, want := range cases {
		if got := YouTubeID(in); got != want {
			t.Errorf("YouTubeID(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestPickTrack(t *testing.T) {
	tracks := []captionTrack{
		{BaseURL: "u1", LanguageCode: "en", Kind: "asr"},
		{BaseURL: "u2", LanguageCode: "fr"},
		{BaseURL: "u3", LanguageCode: "en"},
	}
	// Manual caption in lang beats auto in lang.
	if got := pickTrack(tracks, "en"); got == nil || got.BaseURL != "u3" {
		t.Fatalf("en pick = %+v", got)
	}
	// A lang nobody speaks falls back to the first manual track.
	if got := pickTrack(tracks, "es"); got == nil || got.BaseURL != "u2" {
		t.Fatalf("es fallback should pick first manual (fr u2), got %+v", got)
	}
	if got := pickTrack(nil, "en"); got != nil {
		t.Fatal("empty tracks should pick nil")
	}
}

func TestJSON3ParseShape(t *testing.T) {
	// Verify the json3 struct tags line up with YouTube's real payload.
	var j json3Events
	in := `{"events":[{"tStartMs":100,"dDurationMs":200,"segs":[{"utf8":"hello "},{"utf8":"world"}]}]}`
	if err := json.Unmarshal([]byte(in), &j); err != nil {
		t.Fatal(err)
	}
	if len(j.Events) != 1 || j.Events[0].StartMs != 100 || len(j.Events[0].Segs) != 2 {
		t.Fatalf("json3 parse = %+v", j)
	}
}
