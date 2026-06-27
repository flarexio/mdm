package identity

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
)

// Client calls identity's mTLS endpoints, authenticating with mdm-server's own
// service client certificate.
type Client struct {
	baseURL string
	http    *http.Client
}

func NewClient(baseURL string, cert tls.Certificate, roots *x509.CertPool) Challenger {
	return &Client{
		baseURL: baseURL,
		http: &http.Client{
			Transport: &http.Transport{
				TLSClientConfig: &tls.Config{
					Certificates: []tls.Certificate{cert},
					RootCAs:      roots,
					MinVersion:   tls.VersionTLS12,
				},
			},
		},
	}
}

func (c *Client) GenerateChallenge(ctx context.Context, subject string) (string, error) {
	body, err := json.Marshal(map[string]string{"subject": subject})
	if err != nil {
		return "", err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		c.baseURL+"/scep/challenge/generate", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("identity generate challenge: status %d", resp.StatusCode)
	}

	var out struct {
		Challenge string `json:"challenge"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return "", err
	}

	if out.Challenge == "" {
		return "", errors.New("identity generate challenge: empty challenge")
	}
	return out.Challenge, nil
}
