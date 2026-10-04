package fetch

// youtube.go — YouTube transcript extraction through the InnerTube player
// API: POST /youtubei/v1/player (iOS client) → captionTracks[] → GET the
// track's baseUrl with fmt=json3 → events[].segs[].utf8. No API key, no
// browser, no timedtext-scraping of the watch page. Language selection
// prefers manual captions over auto-generated (kind="asr") in the
// requested language, then any manual track, then whatever exists.
import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"
)

// VideoTranscript is a YouTube video's caption track as plain text +
// timed segments — Firecrawl formats:["transcript"] parity.
type VideoTranscript struct {
	VideoID       string              `json:"video_id"`
	Title         string              `json:"title,omitempty"`
	Language      string              `json:"language"`
	IsAutoCaption bool                `json:"is_auto"`
	DurationMs    int                 `json:"duration_ms,omitempty"`
	Segments      []TranscriptSegment `json:"segments"`
	Text          string              `json:"text"`
}

// TranscriptSegment is one caption cue.
type TranscriptSegment struct {
	StartMs int    `json:"start_ms"`
	DurMs   int    `json:"dur_ms"`
	Text    string `json:"text"`
}

var (
	ytHostRe = regexp.MustCompile(`(?i)^(?:www\.|m\.)?(youtube\.com|youtu\.be|youtube-nocookie\.com)$`)
	ytIDRe   = regexp.MustCompile(`^[a-zA-Z0-9_-]{11}$`)
)

// YouTubeID extracts the video ID from any YouTube URL shape — watch?v=,
// youtu.be/, /shorts/, /embed/, /live/ — or accepts a bare 11-char ID.
// "" means "not a YouTube video URL" — an honest refusal, not a guess.
func YouTubeID(raw string) string {
	if ytIDRe.MatchString(raw) {
		return raw
	}
	u := strings.TrimSpace(raw)
	for _, pre := range []string{"https://", "http://"} {
		u = strings.TrimPrefix(u, pre)
	}
	host := u
	if i := strings.IndexByte(u, '/'); i >= 0 {
		host = u[:i]
	}
	if i := strings.IndexByte(host, '?'); i >= 0 {
		host = host[:i]
	}
	if i := strings.IndexByte(host, ':'); i >= 0 {
		host = host[:i]
	}
	if !ytHostRe.MatchString(host) {
		return ""
	}
	path := u[strings.IndexByte(u, '/')+1:]
	// watch?v=ID and youtu.be/ID differ — query vs path.
	if strings.HasPrefix(path, "watch") {
		for _, kv := range strings.Split(u[strings.IndexByte(u, '?')+1:], "&") {
			if k, v, ok := strings.Cut(kv, "="); ok && k == "v" && ytIDRe.MatchString(v) {
				return v
			}
		}
		return ""
	}
	// youtu.be/ID, youtube.com/shorts|embed|live/ID
	seg := strings.TrimPrefix(path, "shorts/")
	seg = strings.TrimPrefix(seg, "embed/")
	seg = strings.TrimPrefix(seg, "live/")
	if i := strings.IndexAny(seg, "?&/"); i >= 0 {
		seg = seg[:i]
	}
	if ytIDRe.MatchString(seg) {
		return seg
	}
	return ""
}

// playerRequest is the minimal InnerTube body the captions listing needs.
// The iOS client surface is the one that returns captionTracks without
// a PO token.
func playerRequest(videoID string) []byte {
	body, _ := json.Marshal(map[string]any{
		"videoId": videoID,
		"context": map[string]any{
			"client": map[string]any{
				"clientName":    "IOS",
				"clientVersion": "20.10.38",
				"deviceModel":   "iPhone16,2",
				"hl":            "en",
			},
		},
	})
	return body
}

// captionTrack is the slice of captionTracks we care about.
type captionTrack struct {
	BaseURL      string `json:"baseUrl"`
	LanguageCode string `json:"languageCode"`
	Kind         string `json:"kind"` // "asr" = auto-generated
}

// pickTrack prefers: manual in lang → auto in lang → first manual → first.
func pickTrack(tracks []captionTrack, lang string) *captionTrack {
	if len(tracks) == 0 {
		return nil
	}
	var autoLang, manual *captionTrack
	for i := range tracks {
		t := &tracks[i]
		if lang != "" && strings.EqualFold(t.LanguageCode, lang) {
			if t.Kind == "asr" {
				if autoLang == nil {
					autoLang = t
				}
			} else {
				return t // manual captions in the asked language — best case
			}
			continue
		}
		if t.Kind != "asr" && manual == nil {
			manual = t
		}
	}
	for _, t := range []*captionTrack{autoLang, manual, &tracks[0]} {
		if t != nil {
			return t
		}
	}
	return nil
}

// json3 is YouTube's timed JSON caption format.
type json3Events struct {
	Events []struct {
		StartMs int `json:"tStartMs"`
		DurMs   int `json:"dDurationMs"`
		Segs    []struct {
			UTF8 string `json:"utf8"`
		} `json:"segs"`
	} `json:"events"`
}

// YouTubeTranscript fetches a video's captions — vid is an 11-char ID.
// hc is the caller's client so session/proxy settings carry through.
func YouTubeTranscript(ctx context.Context, vid, lang string, hc *http.Client) (*VideoTranscript, error) {
	if hc == nil {
		hc = &http.Client{Timeout: 20 * time.Second}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		"https://www.youtube.com/youtubei/v1/player?prettyPrint=false",
		bytes.NewReader(playerRequest(vid)))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "com.google.ios.youtube/20.10.38 (iPhone16,2; U; CPU iOS 17_4 like Mac OS X)")
	resp, err := hc.Do(req)
	if err != nil {
		return nil, fmt.Errorf("youtube player: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("youtube player: status %d", resp.StatusCode)
	}
	var player struct {
		VideoDetails struct {
			Title string `json:"title"`
		} `json:"videoDetails"`
		Captions struct {
			Rend struct {
				Tracks []captionTrack `json:"captionTracks"`
			} `json:"playerCaptionsTracklistRenderer"`
		} `json:"captions"`
	}
	if err := json.Unmarshal(body, &player); err != nil {
		return nil, fmt.Errorf("youtube player: bad response")
	}
	track := pickTrack(player.Captions.Rend.Tracks, lang)
	if track == nil {
		return nil, fmt.Errorf("no captions available for %s", vid)
	}

	// fmt=json3 gives timed segments as JSON — no XML caption parsing.
	treq, err := http.NewRequestWithContext(ctx, http.MethodGet, track.BaseURL+"&fmt=json3", nil)
	if err != nil {
		return nil, err
	}
	treq.Header.Set("Referer", "https://www.youtube.com/watch?v="+vid)
	tresp, err := hc.Do(treq)
	if err != nil {
		return nil, fmt.Errorf("timedtext: %w", err)
	}
	defer tresp.Body.Close()
	tbody, err := io.ReadAll(io.LimitReader(tresp.Body, 8<<20))
	if err != nil {
		return nil, err
	}
	var j3 json3Events
	if err := json.Unmarshal(tbody, &j3); err != nil {
		return nil, fmt.Errorf("timedtext: bad json3")
	}

	out := &VideoTranscript{
		VideoID: vid, Title: player.VideoDetails.Title,
		Language: track.LanguageCode, IsAutoCaption: track.Kind == "asr",
	}
	var sb strings.Builder
	for _, ev := range j3.Events {
		var text string
		for _, s := range ev.Segs {
			text += s.UTF8
		}
		text = strings.TrimSpace(text)
		if text == "" {
			continue
		}
		out.Segments = append(out.Segments, TranscriptSegment{
			StartMs: ev.StartMs, DurMs: ev.DurMs, Text: text,
		})
		if sb.Len() > 0 {
			sb.WriteByte(' ')
		}
		sb.WriteString(text)
		out.DurationMs = ev.StartMs + ev.DurMs
	}
	out.Text = sb.String()
	if out.Text == "" {
		return nil, fmt.Errorf("captions for %s came back empty (region/IP restrictions can zero timedtext bodies)", vid)
	}
	return out, nil
}
