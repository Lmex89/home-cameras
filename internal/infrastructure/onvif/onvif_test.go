package onvif

import (
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/Lmex89/home-cameras/internal/config"
)

// TestNewClientAndHelpers covers construction and pure helpers.
func TestNewClientAndHelpers(t *testing.T) {
	c := NewFromConfig(config.Config{})
	if c.timeout != 15*1e9 {
		t.Errorf("timeout = %v", c.timeout)
	}
	if got := truncate("hello world", 5); got != "hello..." {
		t.Errorf("truncate = %q", got)
	}
	if got := truncate("hi", 5); got != "hi" {
		t.Errorf("truncate short = %q", got)
	}
}

// TestBuildAuthURL covers credential embedding in URIs.
func TestBuildAuthURL(t *testing.T) {
	c := NewClient(5)
	tests := []struct {
		raw, user, pass, want string
	}{
		{"http://cam:8080/stream", "u", "p", "http://u:p@cam:8080/stream"},
		{"rtsp://cam/stream", "admin", "secret", "rtsp://admin:secret@cam/stream"},
		{"http://cam/x", "", "p", "http://cam/x"},
		{"http://cam/x", "u", "", "http://cam/x"},
		{"", "u", "p", ""},
		{"no-scheme-uri", "u", "p", "no-scheme-uri"},
	}
	for _, tt := range tests {
		if got := c.BuildAuthURL(tt.raw, tt.user, tt.pass); got != tt.want {
			t.Errorf("BuildAuthURL(%q) = %q want %q", tt.raw, got, tt.want)
		}
	}
}

// TestTestConnectionUnreachable verifies fast failure on dead hosts.
func TestTestConnectionUnreachable(t *testing.T) {
	c := NewClient(2)
	res := c.TestConnection("127.0.0.1", 1, "", "")
	if res.Reachable || res.Error == "" {
		t.Fatalf("expected unreachable error: %+v", res)
	}
}

// TestURIResolutionErrors verifies error paths against dead hosts.
func TestURIResolutionErrors(t *testing.T) {
	c := NewClient(2)
	if _, err := c.GetProfiles("127.0.0.1", 1, "", ""); err == nil {
		t.Fatal("expected profiles error")
	}
	if _, err := c.GetSnapshotURI("127.0.0.1", 1, "", "", ""); err == nil {
		t.Fatal("expected snapshot error")
	}
	if _, err := c.GetStreamURI("127.0.0.1", 1, "", "", "x"); err == nil {
		t.Fatal("expected stream error")
	}
	if _, _, err := c.GetJPEGStreamURI("127.0.0.1", 1, "", ""); err == nil {
		t.Fatal("expected jpeg error")
	}
	if _, err := c.GetFirstStreamURI("127.0.0.1", 1, "", ""); err == nil {
		t.Fatal("expected first error")
	}
	if _, _, err := c.GetBestStreamURI("127.0.0.1", 1, "", ""); err == nil {
		t.Fatal("expected best error")
	}
}

// fakeSoapCaller answers SOAP calls with a canned HTTP response.
type fakeSoapCaller struct {
	status  int
	body    string
	err     error
	invoked bool
}

func (f *fakeSoapCaller) CallMethod(method any) (*http.Response, error) {
	f.invoked = true
	if f.err != nil {
		return nil, f.err
	}
	return &http.Response{
		StatusCode: f.status,
		Body:       io.NopCloser(strings.NewReader(f.body)),
		Header:     http.Header{},
	}, nil
}

const profilesBody = `<Envelope><Body><GetProfilesResponse>
  <Profiles token="HDMain">
    <VideoEncoderConfiguration>
      <Encoding>JPEG</Encoding>
      <Resolution><Width>1920</Width><Height>1080</Height></Resolution>
    </VideoEncoderConfiguration>
  </Profiles>
  <Profiles token="Sub">
    <VideoEncoderConfiguration>
      <Encoding>H264</Encoding>
      <Resolution><Width>640</Width><Height>360</Height></Resolution>
    </VideoEncoderConfiguration>
  </Profiles>
</GetProfilesResponse></Body></Envelope>`

// TestCallAndGetProfiles covers SOAP decoding, HTTP errors and the
// profile extraction logic against a fake caller.
func TestCallAndGetProfiles(t *testing.T) {
	c := NewClient(5)

	// Success: decode + extract both profiles.
	fake := &fakeSoapCaller{status: http.StatusOK, body: profilesBody}
	profiles, err := c.getProfiles(fake)
	if err != nil || len(profiles) != 2 {
		t.Fatalf("profiles: %v %v", profiles, err)
	}
	if profiles[0].Token != "HDMain" || profiles[0].Width != 1920 || profiles[0].Encoding != "JPEG" {
		t.Fatalf("profile 0: %+v", profiles[0])
	}
	if profiles[1].Token != "Sub" || profiles[1].Height != 360 {
		t.Fatalf("profile 1: %+v", profiles[1])
	}

	// HTTP error status surfaces the status line.
	fake = &fakeSoapCaller{status: http.StatusInternalServerError, body: "<x/>"}
	if _, err := c.getProfiles(fake); err == nil || !strings.Contains(err.Error(), "SOAP HTTP 500") {
		t.Fatalf("http error: %v", err)
	}

	// Malformed XML surfaces a decode error.
	fake = &fakeSoapCaller{status: http.StatusOK, body: "not xml at all"}
	if _, err := c.getProfiles(fake); err == nil {
		t.Fatal("expected decode error")
	}

	// Transport error surfaces as-is.
	fake = &fakeSoapCaller{err: errors.New("dial timeout")}
	if _, err := c.getProfiles(fake); err == nil {
		t.Fatal("expected transport error")
	}

	// Empty profiles list decodes to an empty result.
	fake = &fakeSoapCaller{status: http.StatusOK, body: `<Envelope><Body><GetProfilesResponse></GetProfilesResponse></Body></Envelope>`}
	profiles, err = c.getProfiles(fake)
	if err != nil || len(profiles) != 0 {
		t.Fatalf("empty profiles: %v %v", profiles, err)
	}
}
