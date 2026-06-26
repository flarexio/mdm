// Package push wakes enrolled devices through APNs so they connect and poll for
// commands. An MDM push carries no command — only a wake signal and the device's
// PushMagic, which the device checks before trusting the wake.
package push

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
)

// APNs endpoints. Port 443 is implied; 2197 is also valid if 443 is blocked.
const (
	HostProduction = "https://api.push.apple.com"
	HostSandbox    = "https://api.sandbox.push.apple.com"
)

var ErrIncompleteTarget = errors.New("push target missing token, topic or push magic")

// Target is everything needed to wake one device. It is derived from an enrollment:
// the values come from the device's TokenUpdate.
type Target struct {
	Token     string // APNs device token, hex-encoded
	Topic     string // APNs topic = the MDM push certificate's subject UID
	PushMagic string // echoed in the payload so the device trusts the wake
}

func (t Target) valid() bool {
	return t.Token != "" && t.Topic != "" && t.PushMagic != ""
}

// Pusher wakes devices so they connect and poll for commands.
type Pusher interface {
	Push(ctx context.Context, target Target) error
}

// Authorizer provides the bearer token for token-based (.p8) APNs authentication.
//
// MDM's standard authentication is CERTIFICATE-based: the MDM push certificate is
// configured as the TLS client certificate on the http.Client, and there is no
// Authorization header. In that (default MDM) setup auth is nil. Token-based auth is
// the general-APNs alternative and is optional here.
type Authorizer interface {
	Bearer(ctx context.Context) (string, error)
}

// Client is an APNs HTTP/2 client. The Go standard http.Client negotiates HTTP/2
// over TLS automatically, so no special transport is required.
type Client struct {
	http *http.Client
	host string
	auth Authorizer
}

// NewClient builds an APNs client for host (HostProduction/HostSandbox).
//
// For certificate-based MDM push (the standard) pass auth=nil and supply an
// httpClient whose TLS config carries the MDM push certificate as a client cert.
// For token-based push pass an Authorizer. A nil httpClient uses http.DefaultClient.
func NewClient(host string, auth Authorizer, httpClient *http.Client) *Client {
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	return &Client{http: httpClient, host: host, auth: auth}
}

// Push sends an MDM wake notification to one device.
func (c *Client) Push(ctx context.Context, target Target) error {
	if !target.valid() {
		return ErrIncompleteTarget
	}

	// The entire MDM push payload: just the device's PushMagic.
	payload, err := json.Marshal(map[string]string{"mdm": target.PushMagic})
	if err != nil {
		return err
	}

	url := c.host + "/3/device/" + target.Token
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		return err
	}

	// Token-based auth sets a bearer header; certificate-based auth (the MDM
	// standard) leaves auth nil and authenticates at the TLS layer instead.
	if c.auth != nil {
		bearer, err := c.auth.Bearer(ctx)
		if err != nil {
			return err
		}
		req.Header.Set("authorization", "bearer "+bearer)
	}

	req.Header.Set("apns-topic", target.Topic)
	req.Header.Set("apns-push-type", "mdm") // MDM wakes use the dedicated push type

	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusOK {
		return nil
	}

	// APNs reports failures as a small JSON body with a "reason".
	var body struct {
		Reason string `json:"reason"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&body)

	return &Error{Status: resp.StatusCode, Reason: body.Reason}
}

// Error is a non-success APNs response.
type Error struct {
	Status int
	Reason string
}

func (e *Error) Error() string {
	return fmt.Sprintf("apns: %d %s", e.Status, e.Reason)
}

// Unregistered reports that the device token is no longer valid: the device was
// wiped or unenrolled. This is a reconciliation signal — the server can mark the
// enrollment gone even though no CheckOut ever arrived.
func (e *Error) Unregistered() bool {
	return e.Status == http.StatusGone || e.Reason == "Unregistered" || e.Reason == "BadDeviceToken"
}
