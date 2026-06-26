// Package challenge issues and redeems the one-time passwords that authorize SCEP
// certificate issuance.
//
// A SCEP CSR carries a challengePassword. A STATIC shared challenge is weak: anyone
// who learns it can mint certificates forever. The correct design is a DYNAMIC,
// single-use, expiring challenge bound to the specific enrollment it was issued for
// — so even a leaked value is good for one certificate, for one identity, briefly.
//
// This package defines only the issue/redeem semantics; it knows nothing about SCEP
// itself. An adapter wires a Store into the SCEP server's challenge validator.
package challenge

import "errors"

var (
	ErrChallengeNotFound = errors.New("challenge not found")  // never issued, or already redeemed
	ErrChallengeExpired  = errors.New("challenge expired")    //
)

// Store issues and redeems one-time challenge passwords.
type Store interface {
	// Generate creates a new one-time challenge bound to subject — typically the
	// enrollment/device identifier that will appear in the CSR's subject CN.
	Generate(subject string) (string, error)

	// Redeem validates pw and consumes it (single use). On success it returns the
	// subject the challenge was bound to, letting the caller cross-check it against
	// the CSR. A not-found, already-redeemed or expired challenge returns an error.
	Redeem(pw string) (subject string, err error)
}
