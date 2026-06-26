package push_test

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/flarexio/mdm-server/push"
)

// fakeTransport captures the outgoing request and returns a canned response,
// letting us assert the APNs request is built correctly without reaching Apple.
type fakeTransport struct {
	req  *http.Request
	body string

	status int
	resp   string
}

func (f *fakeTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	f.req = r
	if r.Body != nil {
		b, _ := io.ReadAll(r.Body)
		f.body = string(b)
	}
	return &http.Response{
		StatusCode: f.status,
		Body:       io.NopCloser(strings.NewReader(f.resp)),
		Header:     make(http.Header),
	}, nil
}

// staticAuth is a stub Authorizer; the real .p8 JWT provider is tested separately.
type staticAuth string

func (s staticAuth) Bearer(context.Context) (string, error) { return string(s), nil }

func newClient(status int, resp string) (*push.Client, *fakeTransport) {
	ft := &fakeTransport{status: status, resp: resp}
	client := push.NewClient(push.HostProduction, staticAuth("TESTTOKEN"), &http.Client{Transport: ft})
	return client, ft
}

var target = push.Target{
	Token:     "abcd1234",
	Topic:     "com.apple.mgmt.External.0001",
	PushMagic: "MAGIC-123",
}

func TestPush_BuildsCorrectAPNsRequest(t *testing.T) {
	client, ft := newClient(http.StatusOK, "")

	require.NoError(t, client.Push(context.Background(), target))

	assert.Equal(t, http.MethodPost, ft.req.Method)
	assert.Equal(t, "https://api.push.apple.com/3/device/abcd1234", ft.req.URL.String())
	assert.Equal(t, "bearer TESTTOKEN", ft.req.Header.Get("authorization"))
	assert.Equal(t, "com.apple.mgmt.External.0001", ft.req.Header.Get("apns-topic"))
	assert.Equal(t, "mdm", ft.req.Header.Get("apns-push-type"))
	assert.JSONEq(t, `{"mdm":"MAGIC-123"}`, ft.body, "payload is only the PushMagic")
}

func TestPush_CertificateBasedSendsNoAuthHeader(t *testing.T) {
	// MDM's standard auth is the push certificate at the TLS layer: auth is nil,
	// so no Authorization header is sent.
	ft := &fakeTransport{status: http.StatusOK}
	client := push.NewClient(push.HostProduction, nil, &http.Client{Transport: ft})

	require.NoError(t, client.Push(context.Background(), target))
	assert.Empty(t, ft.req.Header.Get("authorization"), "cert-based push carries no bearer header")
	assert.Equal(t, "mdm", ft.req.Header.Get("apns-push-type"))
}

func TestPush_IncompleteTarget(t *testing.T) {
	client, _ := newClient(http.StatusOK, "")

	err := client.Push(context.Background(), push.Target{Token: "abcd"}) // missing topic/magic
	assert.ErrorIs(t, err, push.ErrIncompleteTarget)
}

func TestPush_UnregisteredIsReconciliationSignal(t *testing.T) {
	client, _ := newClient(http.StatusGone, `{"reason":"Unregistered"}`)

	err := client.Push(context.Background(), target)
	require.Error(t, err)

	var apnsErr *push.Error
	require.ErrorAs(t, err, &apnsErr)
	assert.True(t, apnsErr.Unregistered(), "410/Unregistered means the device is gone")
}

func TestPush_OtherErrorIsNotUnregistered(t *testing.T) {
	client, _ := newClient(http.StatusBadRequest, `{"reason":"PayloadTooLarge"}`)

	err := client.Push(context.Background(), target)
	require.Error(t, err)

	var apnsErr *push.Error
	require.ErrorAs(t, err, &apnsErr)
	assert.False(t, apnsErr.Unregistered())
}
