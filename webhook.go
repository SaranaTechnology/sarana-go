package sarana

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// Events sent to the webhook URLs registered for the account.
const (
	EventSubscriptionPaid    = "subscription.paid"
	EventSubscriptionExpired = "subscription.expired"
	EventPing                = "ping"
)

// Headers on every signed webhook.
const (
	HeaderWebhookEvent     = "X-Sarana-Event"
	HeaderWebhookDelivery  = "X-Sarana-Delivery"
	HeaderWebhookTimestamp = "X-Sarana-Timestamp"
	HeaderWebhookSignature = "X-Sarana-Signature"
	HeaderWebhookAttempt   = "X-Sarana-Attempt"
)

// DefaultWebhookTolerance is how old (or how far in the future) a webhook's
// timestamp may be before ParseWebhook rejects it as a replay.
const DefaultWebhookTolerance = 5 * time.Minute

var (
	ErrInvalidSignature = errors.New("sarana: invalid webhook signature")
	ErrStaleWebhook     = errors.New("sarana: webhook timestamp outside tolerance")
)

// Webhook is one signed event. Unlike PaymentCallback it is signed with the
// webhook's secret, so a verified body can be trusted as is.
//
// The same event can arrive more than once (retries); dedupe on DeliveryID.
type Webhook struct {
	Event      string         `json:"event"`
	Version    int            `json:"version"`
	DeliveryID string         `json:"delivery_id"`
	OccurredAt string         `json:"occurred_at"`
	Provider   string         `json:"provider"`
	ClientID   int            `json:"client_id"`
	Data       map[string]any `json:"data"`
	// Attempt counts from 1 (X-Sarana-Attempt).
	Attempt int `json:"-"`
}

// now is replaced in tests.
var now = time.Now

// VerifyWebhook checks signature (the X-Sarana-Signature header) against the
// raw body and timestamp (X-Sarana-Timestamp, unix seconds). The header may
// carry two signatures while Sarana rotates the secret; either one matching is
// enough. tolerance <= 0 means DefaultWebhookTolerance.
//
// body must be the bytes as received: re-encoding a decoded struct changes
// them and the signature no longer matches.
func VerifyWebhook(body []byte, timestamp, signature, secret string, tolerance time.Duration) error {
	if secret == "" || signature == "" || timestamp == "" {
		return ErrInvalidSignature
	}
	unix, err := strconv.ParseInt(timestamp, 10, 64)
	if err != nil {
		return ErrInvalidSignature
	}
	if tolerance <= 0 {
		tolerance = DefaultWebhookTolerance
	}
	if d := now().Sub(time.Unix(unix, 0)); d > tolerance || d < -tolerance {
		return ErrStaleWebhook
	}

	m := hmac.New(sha256.New, []byte(secret))
	m.Write([]byte(timestamp))
	m.Write([]byte("."))
	m.Write(body)
	want := hex.EncodeToString(m.Sum(nil))
	for _, part := range strings.Split(signature, ",") {
		got := strings.TrimPrefix(strings.TrimSpace(part), "sha256=")
		if hmac.Equal([]byte(got), []byte(want)) {
			return nil
		}
	}
	return ErrInvalidSignature
}

// ParseWebhook reads, verifies and decodes a webhook from an incoming request,
// with DefaultWebhookTolerance. Answer 2xx quickly once it returns nil; a
// non-2xx makes Sarana retry.
func ParseWebhook(r *http.Request, secret string) (*Webhook, error) {
	body, err := io.ReadAll(io.LimitReader(r.Body, maxCallbackBody))
	if err != nil {
		return nil, err
	}
	if err := VerifyWebhook(body, r.Header.Get(HeaderWebhookTimestamp), r.Header.Get(HeaderWebhookSignature), secret, 0); err != nil {
		return nil, err
	}
	var w Webhook
	if err := json.Unmarshal(body, &w); err != nil {
		return nil, fmt.Errorf("sarana: invalid webhook body: %w", err)
	}
	w.Attempt, _ = strconv.Atoi(r.Header.Get(HeaderWebhookAttempt))
	return &w, nil
}
