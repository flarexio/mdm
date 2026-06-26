// Package checkin models the "check-in" half of the Apple MDM protocol: the
// lifecycle messages a device sends at enrollment-related moments — Authenticate,
// TokenUpdate and CheckOut.
//
// Unlike a command (whose type the server mostly just forwards), each check-in
// message has different fields AND requires different server-side handling, so its
// concrete type must be known before it can be acted on. The type is carried in a
// MessageType field INSIDE the message, which makes decoding a "discriminated
// union": we decode the discriminator first, then the full concrete message.
package checkin

import (
	"encoding/hex"
	"errors"
	"fmt"

	"github.com/micromdm/plist"
)

var (
	ErrEmptyMessage            = errors.New("empty check-in message bytes")
	ErrUnrecognizedMessageType = errors.New("unrecognized MessageType")
)

// Enrollment carries the identity fields present on every check-in (and command)
// message.
//
// IMPORTANT: these values are merely CLAIMED by the device in the request body.
// They must NOT be trusted on their own to identify the device — the authenticated
// identity comes from the client certificate (mTLS), which a later module binds to
// the enrollment. Trusting the body alone would let any device impersonate another
// by simply sending someone else's UDID.
type Enrollment struct {
	UDID             string `plist:",omitempty"`
	EnrollmentID     string `plist:",omitempty"`
	UserID           string `plist:",omitempty"`
	UserShortName    string `plist:",omitempty"`
	UserLongName     string `plist:",omitempty"`
	EnrollmentUserID string `plist:",omitempty"`
}

// MessageType is the discriminator field identifying which check-in message this
// is. It is embedded into each message so the type survives decoding.
type MessageType struct {
	MessageType string
}

// Push carries the data the server needs to later wake the device through APNs.
// Receiving this (inside a TokenUpdate) is the moment an enrollment becomes usable:
// without the push Token + PushMagic the server can never initiate contact and the
// device would only ever be reachable when it chooses to poll on its own.
type Push struct {
	Topic     string
	PushMagic string
	Token     []byte // APNs device token; binary on the wire, hex-encoded for APNs
}

// TokenHex returns the APNs device token in the hex form APNs expects.
func (p Push) TokenHex() string {
	return hex.EncodeToString(p.Token)
}

// Authenticate is the first check-in: the device announces itself at the start of
// enrollment. The server decides whether to accept it.
type Authenticate struct {
	Enrollment
	MessageType
	Topic        string
	SerialNumber string `plist:",omitempty"`
	Raw          []byte `plist:"-"`
}

// TokenUpdate provides (or refreshes) the device's APNs push credentials. The first
// TokenUpdate is the watershed that marks an enrollment active and reachable.
type TokenUpdate struct {
	Enrollment
	MessageType
	Push
	UnlockToken []byte `plist:",omitempty"`
	Raw         []byte `plist:"-"`
}

// CheckOut is the device's best-effort notice that it is removing MDM. It is NOT
// guaranteed to arrive (the device may be wiped, destroyed or offline), so the
// server must never rely on it alone to consider a device unenrolled.
type CheckOut struct {
	Enrollment
	MessageType
	Raw []byte `plist:"-"`
}

// newMessage returns a pointer to the concrete check-in struct for a MessageType.
// The check-in set is small and closed: the server must handle each type
// differently, so — unlike command RequestTypes — we deliberately enumerate them.
func newMessage(t string, raw []byte) any {
	switch t {
	case "Authenticate":
		return &Authenticate{Raw: raw}
	case "TokenUpdate":
		return &TokenUpdate{Raw: raw}
	case "CheckOut":
		return &CheckOut{Raw: raw}
	default:
		return nil
	}
}

// checkinEnvelope drives the two-pass, discriminated-union decode.
type checkinEnvelope struct {
	message any
	raw     []byte
}

// UnmarshalPlist implements the discriminated-union decode. The concrete type is
// only known from a field inside the message, so we cannot pick the target struct
// up front:
//
//  1. decode only the MessageType discriminator,
//  2. construct the matching concrete struct,
//  3. decode the full message into it.
//
// The plist library calls this with f, a function that decodes the same plist into
// whatever target we hand it — which is what lets us decode the document twice.
func (w *checkinEnvelope) UnmarshalPlist(f func(any) error) error {
	var disc MessageType
	if err := f(&disc); err != nil {
		return err
	}

	w.message = newMessage(disc.MessageType, w.raw)
	if w.message == nil {
		return fmt.Errorf("%w: %q", ErrUnrecognizedMessageType, disc.MessageType)
	}

	return f(w.message)
}

// DecodeCheckin parses a raw check-in plist and returns the concrete message:
// *Authenticate, *TokenUpdate or *CheckOut. Callers type-switch on the result.
func DecodeCheckin(raw []byte) (any, error) {
	if len(raw) == 0 {
		return nil, ErrEmptyMessage
	}

	w := &checkinEnvelope{raw: raw}
	if err := plist.Unmarshal(raw, w); err != nil {
		return nil, err
	}

	return w.message, nil
}
