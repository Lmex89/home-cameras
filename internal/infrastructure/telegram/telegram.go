// Package telegram sends video reports and plain messages to a Telegram
// chat via the Bot API, with a 50 MB upload cap and an S3 fallback for
// oversized files.
package telegram

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"time"

	"github.com/Lmex89/home-cameras/internal/config"
	"github.com/Lmex89/home-cameras/internal/infrastructure/storage"
)

// UploadLimit is Telegram's 50 MB bot upload cap.
const UploadLimit = 50 * 1024 * 1024

// Notifier sends messages and videos to a configured chat.
type Notifier struct {
	cfg     config.Config
	storage *storage.S3
	client  *http.Client
}

// New builds a notifier from the application config, wiring the S3
// provider when storage is enabled.
func New(cfg config.Config) *Notifier {
	var s3 *storage.S3
	if cfg.StorageEnabled {
		if p, err := storage.NewFromConfig(cfg); err == nil {
			s3 = p
		}
	}
	return &Notifier{
		cfg:     cfg,
		storage: s3,
		client:  &http.Client{Timeout: 120 * time.Second},
	}
}

// Enabled reports whether Telegram is configured and turned on.
func (n *Notifier) Enabled() bool {
	return n.cfg.TelegramEnabled && n.cfg.TelegramBotToken != "" && n.cfg.TelegramChatID != ""
}

func (n *Notifier) apiURL(method string) string {
	return fmt.Sprintf("https://api.telegram.org/bot%s/%s", n.cfg.TelegramBotToken, method)
}

// SendMessage posts a plain text message; returns false when disabled
// or on failure.
func (n *Notifier) SendMessage(ctx context.Context, text string) bool {
	if !n.Enabled() {
		return false
	}
	payload := map[string]any{"chat_id": n.cfg.TelegramChatID, "text": text}
	body, _ := json.Marshal(payload)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, n.apiURL("sendMessage"), bytes.NewReader(body))
	if err != nil {
		return false
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := n.client.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	return resp.StatusCode >= 200 && resp.StatusCode < 300
}

// SendVideo sends an MP4 to the chat. Files over the 50 MB limit are
// uploaded to S3 when configured and reported as a link.
func (n *Notifier) SendVideo(ctx context.Context, path, caption, fallbackURL, publicURL string) bool {
	if !n.Enabled() {
		return false
	}
	size := int64(0)
	if info, err := os.Stat(path); err == nil {
		size = info.Size()
	}

	if size > UploadLimit {
		link := publicURL
		if link == "" && n.storage != nil {
			if url, err := n.storage.Upload(ctx, path); err == nil {
				link = url
			}
		}
		if link == "" {
			link = fallbackURL
		}
		return n.SendMessage(ctx, caption+"\n\nDownload: "+link)
	}

	if ok := n.sendVideoFile(ctx, path, caption); !ok {
		return false
	}
	link := publicURL
	if link == "" {
		link = fallbackURL
	}
	if link != "" {
		return n.SendMessage(ctx, caption+"\n\nDownload: "+link)
	}
	return true
}

func (n *Notifier) sendVideoFile(ctx context.Context, path, caption string) bool {
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()

	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	mw.WriteField("chat_id", n.cfg.TelegramChatID)
	mw.WriteField("caption", caption)
	mw.WriteField("supports_streaming", "true")
	part, err := mw.CreateFormFile("video", fileBase(path))
	if err != nil {
		return false
	}
	if _, err := io.Copy(part, f); err != nil {
		return false
	}
	mw.Close()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, n.apiURL("sendVideo"), &buf)
	if err != nil {
		return false
	}
	req.Header.Set("Content-Type", mw.FormDataContentType())
	resp, err := n.client.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	return resp.StatusCode >= 200 && resp.StatusCode < 300
}

func fileBase(p string) string {
	for i := len(p) - 1; i >= 0; i-- {
		if p[i] == '/' || p[i] == '\\' {
			return p[i+1:]
		}
	}
	return p
}
