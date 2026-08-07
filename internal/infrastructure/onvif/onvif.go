// Package onvif wraps the use-go/onvif SOAP client behind a narrow
// adapter exposing exactly the operations the camera services need
// (device info, profiles, snapshot URI, stream URIs). Responses are
// decoded with namespace-agnostic XML structs so the adapter survives
// camera firmware differences.
package onvif

import (
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/use-go/onvif"
	"github.com/use-go/onvif/device"
	media "github.com/use-go/onvif/media"
	onvift "github.com/use-go/onvif/xsd/onvif"

	"github.com/Lmex89/home-cameras/internal/config"
)

// Client talks to a single ONVIF camera over SOAP/HTTP.
type Client struct {
	timeout time.Duration
	// httpClient overrides the per-call client (test injection; nil
	// builds a default client with the configured timeout).
	httpClient *http.Client
}

// NewClient builds a client; the timeout applies per SOAP call.
//
// Args:
//
//	timeoutSeconds: Per-call timeout in seconds.
//
// Returns:
//
//	A ready client.
func NewClient(timeoutSeconds int) *Client {
	return &Client{timeout: time.Duration(timeoutSeconds) * time.Second}
}

// NewFromConfig builds a client from application settings (15s per-call
// timeout, parity with the legacy ONVIF client default).
//
// Args:
//
//	cfg: Application configuration.
//
// Returns:
//
//	A ready client.
func NewFromConfig(cfg config.Config) *Client {
	return NewClient(15)
}

// Profile describes one media profile discovered on a camera.
type Profile struct {
	Token    string
	Width    int
	Height   int
	Encoding string
}

// TestResult is the outcome of a connection probe.
type TestResult struct {
	Reachable bool
	Profiles  []Profile
	Error     string
}

// device builds a Device for a camera endpoint with a bounded client.
//
// Args:
//
//	host, port: Camera endpoint.
//	user, pass: ONVIF credentials (may be empty).
//
// Returns:
//
//	The device handle (which performs GetCapabilities on creation).
func (c *Client) device(host string, port int, user, pass string) (*onvif.Device, error) {
	xaddr := fmt.Sprintf("http://%s:%d/onvif/device_service", host, port)
	httpClient := c.httpClient
	if httpClient == nil {
		httpClient = &http.Client{Timeout: c.timeout}
	}
	return onvif.NewDevice(onvif.DeviceParams{
		Xaddr:      xaddr,
		Username:   user,
		Password:   pass,
		HttpClient: httpClient,
	})
}

// soapCaller is the slice of the ONVIF device the adapter needs:
// invoking a SOAP method returns the raw HTTP response.
type soapCaller interface {
	CallMethod(method any) (*http.Response, error)
}

// call invokes a SOAP method and decodes the response body into out
// (a struct with Body>... xml tags). SOAP faults and HTTP errors are
// surfaced as Go errors.
//
// Args:
//
//	dev: The device handle.
//	req: The typed request struct (e.g. media.GetProfiles{}).
//	out: Destination for the decoded response.
//
// Returns:
//
//	Any transport, HTTP, or XML error.
func call(dev soapCaller, req any, out any) error {
	resp, err := dev.CallMethod(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("SOAP HTTP %d: %s", resp.StatusCode, truncate(string(body), 300))
	}
	dec := xml.NewDecoder(resp.Body)
	if err := dec.Decode(out); err != nil {
		return fmt.Errorf("decode SOAP response: %w", err)
	}
	return nil
}

// truncate limits a string to n characters.
//
// Args:
//
//	s: The string to truncate.
//	n: Maximum length.
//
// Returns:
//
//	The truncated string.
func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}

// TestConnection verifies ONVIF reachability and lists profiles.
//
// Args:
//
//	host, port: Camera endpoint.
//	user, pass: ONVIF credentials.
//
// Returns:
//
//	A TestResult (never errors; failures set Error).
func (c *Client) TestConnection(host string, port int, user, pass string) TestResult {
	dev, err := c.device(host, port, user, pass)
	if err != nil {
		return TestResult{Error: err.Error()}
	}
	var body struct {
		Fault struct {
			Text string `xml:"Reason>Text"`
		} `xml:"Body>Fault"`
	}
	if err := call(dev, device.GetDeviceInformation{}, &body); err != nil {
		return TestResult{Error: err.Error()}
	}
	if body.Fault.Text != "" {
		return TestResult{Error: body.Fault.Text}
	}
	profiles, err := c.getProfiles(dev)
	if err != nil {
		return TestResult{Reachable: true, Error: err.Error()}
	}
	return TestResult{Reachable: true, Profiles: profiles}
}

// getProfiles lists the media profiles of a device.
//
// Args:
//
//	dev: The device handle.
//
// Returns:
//
//	The discovered profiles.
func (c *Client) getProfiles(dev soapCaller) ([]Profile, error) {
	var body struct {
		Profiles []struct {
			Token                     string `xml:"token,attr"`
			VideoEncoderConfiguration struct {
				Encoding   string `xml:"Encoding"`
				Resolution struct {
					Width  int `xml:"Width"`
					Height int `xml:"Height"`
				} `xml:"Resolution"`
			} `xml:"VideoEncoderConfiguration"`
		} `xml:"Body>GetProfilesResponse>Profiles"`
	}
	if err := call(dev, media.GetProfiles{}, &body); err != nil {
		return nil, err
	}
	out := make([]Profile, 0, len(body.Profiles))
	for _, p := range body.Profiles {
		out = append(out, Profile{
			Token:    p.Token,
			Width:    p.VideoEncoderConfiguration.Resolution.Width,
			Height:   p.VideoEncoderConfiguration.Resolution.Height,
			Encoding: p.VideoEncoderConfiguration.Encoding,
		})
	}
	return out, nil
}

// GetProfiles returns the media profiles of a camera.
//
// Args:
//
//	host, port: Camera endpoint.
//	user, pass: ONVIF credentials.
//
// Returns:
//
//	The discovered profiles.
func (c *Client) GetProfiles(host string, port int, user, pass string) ([]Profile, error) {
	dev, err := c.device(host, port, user, pass)
	if err != nil {
		return nil, err
	}
	return c.getProfiles(dev)
}

// GetSnapshotURI resolves the JPEG snapshot URI for a profile token.
// When token is empty the first available profile is used.
//
// Args:
//
//	host, port: Camera endpoint.
//	user, pass: ONVIF credentials.
//	token: Media profile token (empty = auto-select first).
//
// Returns:
//
//	The snapshot URI.
func (c *Client) GetSnapshotURI(host string, port int, user, pass, token string) (string, error) {
	dev, err := c.device(host, port, user, pass)
	if err != nil {
		return "", err
	}
	if token == "" {
		profiles, err := c.getProfiles(dev)
		if err != nil {
			return "", err
		}
		if len(profiles) == 0 {
			return "", fmt.Errorf("no media profiles found")
		}
		token = profiles[0].Token
	}
	var body struct {
		MediaUri struct {
			Uri string `xml:"Uri"`
		} `xml:"Body>GetSnapshotUriResponse>MediaUri"`
	}
	if err := call(dev, media.GetSnapshotUri{ProfileToken: onvift.ReferenceToken(token)}, &body); err != nil {
		return "", err
	}
	if body.MediaUri.Uri == "" {
		return "", fmt.Errorf("empty snapshot URI")
	}
	return body.MediaUri.Uri, nil
}

// GetStreamURI resolves the RTSP unicast stream URI for a profile.
//
// Args:
//
//	host, port: Camera endpoint.
//	user, pass: ONVIF credentials.
//	token: Media profile token.
//
// Returns:
//
//	The stream URI.
func (c *Client) GetStreamURI(host string, port int, user, pass, token string) (string, error) {
	dev, err := c.device(host, port, user, pass)
	if err != nil {
		return "", err
	}
	var body struct {
		MediaUri struct {
			Uri string `xml:"Uri"`
		} `xml:"Body>GetStreamUriResponse>MediaUri"`
	}
	req := media.GetStreamUri{
		StreamSetup: onvift.StreamSetup{
			Stream:    onvift.StreamType("RTP-Unicast"),
			Transport: onvift.Transport{Protocol: onvift.TransportProtocol("RTSP")},
		},
		ProfileToken: onvift.ReferenceToken(token),
	}
	if err := call(dev, req, &body); err != nil {
		return "", err
	}
	if body.MediaUri.Uri == "" {
		return "", fmt.Errorf("empty stream URI")
	}
	return body.MediaUri.Uri, nil
}

// GetJPEGStreamURI finds the first JPEG-encoded stream URI.
//
// Args:
//
//	host, port: Camera endpoint.
//	user, pass: ONVIF credentials.
//
// Returns:
//
//	The stream URI and its profile token.
func (c *Client) GetJPEGStreamURI(host string, port int, user, pass string) (string, string, error) {
	dev, err := c.device(host, port, user, pass)
	if err != nil {
		return "", "", err
	}
	profiles, err := c.getProfiles(dev)
	if err != nil {
		return "", "", err
	}
	for _, p := range profiles {
		if strings.EqualFold(p.Encoding, "JPEG") {
			uri, err := c.GetStreamURI(host, port, user, pass, p.Token)
			return uri, p.Token, err
		}
	}
	return "", "", fmt.Errorf("no JPEG stream profile found")
}

// GetFirstStreamURI resolves the stream for the first profile.
//
// Args:
//
//	host, port: Camera endpoint.
//	user, pass: ONVIF credentials.
//
// Returns:
//
//	The stream URI.
func (c *Client) GetFirstStreamURI(host string, port int, user, pass string) (string, error) {
	dev, err := c.device(host, port, user, pass)
	if err != nil {
		return "", err
	}
	profiles, err := c.getProfiles(dev)
	if err != nil {
		return "", err
	}
	if len(profiles) == 0 {
		return "", fmt.Errorf("no media profiles found")
	}
	return c.GetStreamURI(host, port, user, pass, profiles[0].Token)
}

// GetBestStreamURI resolves the stream of the highest-resolution profile.
//
// Args:
//
//	host, port: Camera endpoint.
//	user, pass: ONVIF credentials.
//
// Returns:
//
//	The stream URI and its profile token.
func (c *Client) GetBestStreamURI(host string, port int, user, pass string) (string, string, error) {
	dev, err := c.device(host, port, user, pass)
	if err != nil {
		return "", "", err
	}
	profiles, err := c.getProfiles(dev)
	if err != nil {
		return "", "", err
	}
	if len(profiles) == 0 {
		return "", "", fmt.Errorf("no media profiles found")
	}
	best := profiles[0]
	for _, p := range profiles[1:] {
		if p.Width*p.Height > best.Width*best.Height {
			best = p
		}
	}
	uri, err := c.GetStreamURI(host, port, user, pass, best.Token)
	return uri, best.Token, err
}

// BuildAuthURL embeds credentials into a URI's authority when possible.
//
// Args:
//
//	rawURI: The URI to amend.
//	user, pass: Credentials to embed.
//
// Returns:
//
//	The amended URI (unchanged when credentials are empty).
func (c *Client) BuildAuthURL(rawURI, user, pass string) string {
	if rawURI == "" || user == "" || pass == "" {
		return rawURI
	}
	if idx := strings.Index(rawURI, "://"); idx >= 0 {
		scheme, rest := rawURI[:idx+3], rawURI[idx+3:]
		return scheme + user + ":" + pass + "@" + rest
	}
	return rawURI
}
