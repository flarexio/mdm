// Package profile builds Apple Configuration Profiles (.mobileconfig): the plist
// documents that carry settings to a device — Wi-Fi, VPN, certificates,
// restrictions, and the SCEP + MDM payloads that enroll a device into management.
//
// A profile is a plist whose top-level "Configuration" payload holds a
// PayloadContent array of child payloads. Every payload — top-level or child —
// shares the same header keys (type, version, identifier, UUID).
package profile

import (
	"reflect"

	"github.com/google/uuid"
	"github.com/micromdm/plist"
)

// PayloadHeader holds the keys every payload must carry. It is embedded into the
// profile and into each concrete payload so they all share one identity shape.
type PayloadHeader struct {
	PayloadType        string
	PayloadVersion     int
	PayloadIdentifier  string
	PayloadUUID        string
	PayloadDisplayName string `plist:",omitempty"`
}

func newHeader(payloadType, identifier, displayName string) PayloadHeader {
	return PayloadHeader{
		PayloadType:        payloadType,
		PayloadVersion:     1,
		PayloadIdentifier:  identifier,
		PayloadUUID:        uuid.NewString(),
		PayloadDisplayName: displayName,
	}
}

// UUID and Type promote to every payload that embeds PayloadHeader, which is what
// lets concrete payloads satisfy the Payload interface.
func (h PayloadHeader) UUID() string { return h.PayloadUUID }
func (h PayloadHeader) Type() string { return h.PayloadType }

// Payload is any child payload that can be placed in a profile's PayloadContent.
type Payload interface {
	UUID() string
	Type() string
}

// Profile is a top-level Configuration Profile. Its PayloadType is always
// "Configuration"; the real settings live in the child payloads.
//
// PayloadContent is []any (not []Payload) so the plist marshaller reflects on each
// element's concrete type. Add still accepts the Payload interface, keeping the
// public API type-safe while the storage stays marshal-friendly.
type Profile struct {
	PayloadHeader
	PayloadContent []any
}

// New creates an empty profile with a fresh UUID.
func New(identifier, displayName string) *Profile {
	return &Profile{
		PayloadHeader: newHeader("Configuration", identifier, displayName),
	}
}

// Add appends child payloads to the profile.
//
// Payloads are dereferenced to their struct values before storage: micromdm/plist
// marshals struct values inside an interface slice but not pointers, so storing a
// *SCEPPayload would fail at Marshal time. Dereferencing here keeps callers free to
// pass the pointers the constructors return.
func (p *Profile) Add(payloads ...Payload) {
	for _, payload := range payloads {
		v := reflect.ValueOf(payload)
		if v.Kind() == reflect.Pointer {
			v = v.Elem()
		}
		p.PayloadContent = append(p.PayloadContent, v.Interface())
	}
}

// Marshal renders the profile as an indented XML plist — the .mobileconfig bytes.
func (p *Profile) Marshal() ([]byte, error) {
	return plist.MarshalIndent(p, "\t")
}
