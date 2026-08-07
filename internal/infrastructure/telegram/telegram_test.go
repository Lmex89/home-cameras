package telegram

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Lmex89/home-cameras/internal/config"
)

// fakeBot records requests to a local Telegram Bot API stand-in.
type fakeBot struct {
	server  *httptest.Server
	calls   []string
	status  int
	lastURL string
}

func newFakeBot(t *testing.T) *fakeBot {
	t.Helper()
	fb := &fakeBot{status: http.StatusOK}
	fb.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fb.lastURL = r.URL.Path
		fb.calls = append(fb.calls, r.URL.Path)
		io.Copy(io.Discard, r.Body) // drain to avoid client stalls
		w.WriteHeader(fb.status)
	}))
	t.Cleanup(fb.server.Close)
	return fb
}

// notifierTo redirects every Bot API call to the fake bot.
func notifierTo(t *testing.T, fb *fakeBot) *Notifier {
	t.Helper()
	n := New(config.Config{
		TelegramEnabled: true, TelegramBotToken: "tok", TelegramChatID: "123",
	})
	n.client.Transport = rewriteBot{target: fb.server.URL}
	return n
}

// rewriteBot points api.telegram.org requests at the fake bot.
type rewriteBot struct {
	target string
}

func (rb rewriteBot) RoundTrip(r *http.Request) (*http.Response, error) {
	clone := r.Clone(r.Context())
	clone.URL.Scheme = "http"
	clone.URL.Host = strings.TrimPrefix(rb.target, "http://")
	clone.RequestURI = ""
	return http.DefaultTransport.RoundTrip(clone)
}

// TestSendMessage verifies the JSON message path and failures.
func TestSendMessage(t *testing.T) {
	fb := newFakeBot(t)
	n := notifierTo(t, fb)

	if !n.SendMessage(context.Background(), "hello") {
		t.Fatal("expected success")
	}
	if len(fb.calls) != 1 || !strings.HasSuffix(fb.calls[0], "/sendMessage") {
		t.Fatalf("calls = %v", fb.calls)
	}

	fb.status = http.StatusInternalServerError
	if n.SendMessage(context.Background(), "boom") {
		t.Fatal("expected failure on 500")
	}
}

// TestSendVideoSmall verifies the multipart upload path.
func TestSendVideoSmall(t *testing.T) {
	fb := newFakeBot(t)
	n := notifierTo(t, fb)

	path := filepath.Join(t.TempDir(), "small.mp4")
	if err := os.WriteFile(path, []byte("mp4"), 0o644); err != nil {
		t.Fatal(err)
	}

	if !n.SendVideo(context.Background(), path, "cap", "http://fallback", "") {
		t.Fatal("expected success")
	}
	if len(fb.calls) < 1 || !strings.HasSuffix(fb.calls[0], "/sendVideo") {
		t.Fatalf("calls = %v", fb.calls)
	}
	if len(fb.calls) < 2 || !strings.HasSuffix(fb.calls[1], "/sendMessage") {
		t.Fatalf("expected download-link message: %v", fb.calls)
	}
}

// TestSendVideoLarge verifies the >50MB fallback path sends a link
// message instead of the file.
func TestSendVideoLarge(t *testing.T) {
	fb := newFakeBot(t)
	n := notifierTo(t, fb)

	dir := t.TempDir()
	big := filepath.Join(dir, "big.mp4")
	f, err := os.Create(big)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.Truncate(UploadLimit + 1024); err != nil {
		f.Close()
		t.Fatal(err)
	}
	f.Close()

	if !n.SendVideo(context.Background(), big, "cap", "http://fallback", "http://public") {
		t.Fatal("expected success via fallback")
	}
	if len(fb.calls) != 1 || !strings.HasSuffix(fb.calls[0], "/sendMessage") {
		t.Fatalf("calls = %v", fb.calls)
	}
}

// TestSendVideoMissingFile verifies a missing file reports failure.
func TestSendVideoMissingFile(t *testing.T) {
	fb := newFakeBot(t)
	n := notifierTo(t, fb)
	if n.SendVideo(context.Background(), filepath.Join(t.TempDir(), "nope.mp4"), "c", "u", "") {
		t.Fatal("expected failure for missing file")
	}
}

// TestNewWithStorage verifies the storage branch of the constructor.
func TestNewWithStorage(t *testing.T) {
	n := New(config.Config{
		TelegramEnabled:    true,
		TelegramBotToken:   "tok",
		TelegramChatID:     "1",
		StorageEnabled:     true,
		StorageEndpointURL: "localhost:9000",
		StorageBucketName:  "bkt",
		StorageAccessKey:   "ak",
		StorageSecretKey:   "sk",
	})
	if n.storage == nil {
		t.Fatal("expected storage client when enabled")
	}
	// Misconfigured storage yields a nil client without error.
	n2 := New(config.Config{TelegramEnabled: true, TelegramBotToken: "t", TelegramChatID: "1", StorageEnabled: true})
	if n2.storage != nil {
		t.Fatal("expected nil storage for bad config")
	}
}

// TestSendFailurePaths verifies HTTP error paths return false.
func TestSendFailurePaths(t *testing.T) {
	fb := newFakeBot(t)
	fb.status = http.StatusInternalServerError
	n := notifierTo(t, fb)

	path := filepath.Join(t.TempDir(), "s.mp4")
	if err := os.WriteFile(path, []byte("mp4"), 0o644); err != nil {
		t.Fatal(err)
	}
	if n.SendVideo(context.Background(), path, "c", "u", "") {
		t.Fatal("expected failure on 500")
	}
}
