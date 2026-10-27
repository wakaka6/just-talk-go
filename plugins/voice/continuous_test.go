package voice

import (
	"context"
	"testing"

	"github.com/c/just-talk-go/internal/speech"
)

type resultsSession struct{ results chan speech.StreamResult }

func (s *resultsSession) Connect(context.Context) error                 { return nil }
func (s *resultsSession) SendAudio(context.Context, []byte, bool) error { return nil }
func (s *resultsSession) Results() <-chan speech.StreamResult           { return s.results }
func (s *resultsSession) Done() <-chan struct{}                         { return nil }
func (s *resultsSession) Final() <-chan struct{}                        { return nil }
func (s *resultsSession) LastText() string                              { return "" }
func (s *resultsSession) Close() error                                  { return nil }

// The transcription service is a background feed for subscribers. What it
// hears must never reach the notch or the status line, which belong to
// dictation: a meeting's words there look like the user is being recorded.
func TestServiceResultsStayOffTheNotchAndStatusLine(t *testing.T) {
	p := NewVoicePlugin()
	p.logger = discardLogger()
	before := TUIStatus()
	resetOverlaySession(42)
	client := &resultsSession{results: make(chan speech.StreamResult, 1)}
	client.results <- speech.StreamResult{
		Text:       "会议里有人说话",
		Utterances: []speech.Utterance{{Text: "会议里有人说话", Definite: true}},
		Last:       true,
	}
	if err := p.consumeContinuousResults(context.Background(), 42, client); err != errSessionEndedByServer {
		t.Fatalf("consume = %v, want the session ended by the server", err)
	}
	if got := OverlayStatus().Partial; got != "" {
		t.Fatalf("notch text = %q, want none from the service", got)
	}
	if after := TUIStatus(); after.State != before.State || after.Detail != before.Detail {
		t.Fatalf("status line went %q/%q -> %q/%q", before.State, before.Detail, after.State, after.Detail)
	}
}
