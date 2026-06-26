package http_test

import (
	"bytes"
	"crypto/hmac"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/flarexio/mdm-server/persistence/inmem"
	transhttp "github.com/flarexio/mdm-server/transport/http"
)

var webhookSecret = []byte("super-secret-hmac-key")

func csrDER(t *testing.T, cn string) []byte {
	t.Helper()

	key, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)

	der, err := x509.CreateCertificateRequest(rand.Reader, &x509.CertificateRequest{
		Subject: pkix.Name{CommonName: cn},
	}, key)
	require.NoError(t, err)

	return der
}

// postSigned sends a webhook request signed the way StepCA signs it. If tamper is
// true the signature is corrupted to emulate a forged/unauthenticated caller.
func postSigned(t *testing.T, h http.Handler, challengePW, csrCN string, tamper bool) *httptest.ResponseRecorder {
	t.Helper()

	body, err := json.Marshal(map[string]any{
		"scepChallenge":     challengePW,
		"scepTransactionID": "tx-1",
		"x509CertificateRequest": map[string]any{
			"raw": csrDER(t, csrCN), // encoding/json base64-encodes []byte
		},
	})
	require.NoError(t, err)

	mac := hmac.New(sha256.New, webhookSecret)
	mac.Write(body)
	sig := hex.EncodeToString(mac.Sum(nil))
	if tamper {
		sig = "00" + sig[2:]
	}

	req := httptest.NewRequest(http.MethodPost, "/scep/challenge/verify", bytes.NewReader(body))
	req.Header.Set("X-Smallstep-Signature", sig)

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	return rec
}

func decodeAllow(t *testing.T, rec *httptest.ResponseRecorder) bool {
	t.Helper()

	var resp struct {
		Allow bool `json:"allow"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))

	return resp.Allow
}

func TestSCEPChallengeWebhook(t *testing.T) {
	challenges, err := inmem.NewChallengeStore(5 * time.Minute)
	require.NoError(t, err)

	h := transhttp.SCEPChallengeHandler(challenges, webhookSecret)

	t.Run("allows a valid, bound challenge", func(t *testing.T) {
		pw, err := challenges.Generate("device-0001")
		require.NoError(t, err)

		rec := postSigned(t, h, pw, "device-0001", false)
		require.Equal(t, http.StatusOK, rec.Code)
		assert.True(t, decodeAllow(t, rec))
	})

	t.Run("denies a reused challenge", func(t *testing.T) {
		pw, err := challenges.Generate("device-0001")
		require.NoError(t, err)

		require.True(t, decodeAllow(t, postSigned(t, h, pw, "device-0001", false)))   // first use
		assert.False(t, decodeAllow(t, postSigned(t, h, pw, "device-0001", false)))   // reuse denied
	})

	t.Run("denies a subject mismatch", func(t *testing.T) {
		pw, err := challenges.Generate("device-0001")
		require.NoError(t, err)

		rec := postSigned(t, h, pw, "device-9999", false) // CSR claims a different CN
		assert.False(t, decodeAllow(t, rec))
	})

	t.Run("denies an unknown challenge", func(t *testing.T) {
		rec := postSigned(t, h, "never-issued", "device-0001", false)
		assert.False(t, decodeAllow(t, rec))
	})

	t.Run("rejects an unauthenticated request and does not consume the challenge", func(t *testing.T) {
		pw, err := challenges.Generate("device-0001")
		require.NoError(t, err)

		rec := postSigned(t, h, pw, "device-0001", true) // forged signature
		require.Equal(t, http.StatusUnauthorized, rec.Code)

		// The challenge must have survived the rejected request.
		assert.True(t, decodeAllow(t, postSigned(t, h, pw, "device-0001", false)))
	})
}
