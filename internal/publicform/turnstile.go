package publicform

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const turnstileVerifyEndpoint = "https://challenges.cloudflare.com/turnstile/v0/siteverify"

var (
	ErrTurnstileRequired    = errors.New("تأیید ضدربات انجام نشد؛ دوباره تلاش کنید")
	ErrTurnstileUnavailable = errors.New("تأیید ضدربات این فرم به‌درستی پیکربندی نشده است")
)

type TurnstileVerifier interface {
	Verify(context.Context, string, string) (bool, error)
}

type CloudflareTurnstileVerifier struct {
	secret string
	client *http.Client
}

func NewCloudflareTurnstileVerifier(secret string) (*CloudflareTurnstileVerifier, error) {
	secret = strings.TrimSpace(secret)
	if len(secret) < 8 {
		return nil, errors.New("turnstile secret is invalid")
	}
	return &CloudflareTurnstileVerifier{
		secret: secret,
		client: &http.Client{Timeout: 5 * time.Second},
	}, nil
}

func (v *CloudflareTurnstileVerifier) Verify(ctx context.Context, token, remoteIP string) (bool, error) {
	values := url.Values{"secret": {v.secret}, "response": {strings.TrimSpace(token)}}
	if remoteIP = strings.TrimSpace(remoteIP); remoteIP != "" {
		values.Set("remoteip", remoteIP)
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, turnstileVerifyEndpoint, strings.NewReader(values.Encode()))
	if err != nil {
		return false, fmt.Errorf("create turnstile verification request: %w", err)
	}
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.Header.Set("Accept", "application/json")
	response, err := v.client.Do(request)
	if err != nil {
		return false, fmt.Errorf("verify turnstile response: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return false, fmt.Errorf("verify turnstile response: unexpected status %d", response.StatusCode)
	}
	var result struct {
		Success bool `json:"success"`
	}
	decoder := json.NewDecoder(response.Body)
	if err := decoder.Decode(&result); err != nil {
		return false, fmt.Errorf("decode turnstile response: %w", err)
	}
	return result.Success, nil
}
