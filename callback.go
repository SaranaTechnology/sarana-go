package sarana

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// PaymentCallback is the body Sarana POSTs to your callback_url when an
// invoice reaches a terminal status.
//
// Callbacks are not signed. Treat the body as a hint: confirm it with
// GetTransactionByExternalID (or PaymentStatus) before releasing goods. The
// same invoice can be sent more than once (for example again once the final
// fee is known), so handle it idempotently.
type PaymentCallback struct {
	ID                     string         `json:"id"`                 // Xendit invoice id
	ExternalID             string         `json:"external_id"`        // = TransactionID
	TransactionID          string         `json:"transaction_id"`     // INV-<client>-<nano>
	ClientExternalID       string         `json:"client_external_id"` // your external_id
	Status                 string         `json:"status"`             // PAID, SETTLED, EXPIRED
	Amount                 float64        `json:"amount"`
	PaidAmount             float64        `json:"paid_amount"`
	AdjustedReceivedAmount float64        `json:"adjusted_received_amount"`
	Biayavat               float64        `json:"biayavat"`
	PaidAt                 string         `json:"paid_at"`
	PaymentMethod          string         `json:"payment_method"`
	PaymentChannel         string         `json:"payment_channel"`
	Metadata               map[string]any `json:"metadata"`

	// SubID / SubRef are set when the transaction belongs to a sub-client
	// (from the for-user-id / X-Sub-Client headers).
	SubID  string `json:"-"`
	SubRef string `json:"-"`
	// Raw keeps every field of the body, including ones not mapped above.
	Raw map[string]any `json:"-"`
}

// IsPaid reports whether the status is PAID or SETTLED.
func (p *PaymentCallback) IsPaid() bool {
	s := strings.ToUpper(p.Status)
	return s == "PAID" || s == "SETTLED"
}

// WithdrawalCallback is the body POSTed to the withdrawal url.
type WithdrawalCallback struct {
	Status         string `json:"status"`
	DiTransferOleh string `json:"di_transfer_oleh"`
	TransferAt     string `json:"transfer_at"`
	SubID          string `json:"-"`
	SubRef         string `json:"-"`
}

const maxCallbackBody = 1 << 20

// ParsePaymentCallback reads a payment callback from an incoming request.
func ParsePaymentCallback(r *http.Request) (*PaymentCallback, error) {
	body, err := io.ReadAll(io.LimitReader(r.Body, maxCallbackBody))
	if err != nil {
		return nil, err
	}
	var cb PaymentCallback
	if err := json.Unmarshal(body, &cb); err != nil {
		return nil, fmt.Errorf("sarana: invalid callback body: %w", err)
	}
	_ = json.Unmarshal(body, &cb.Raw)
	cb.Status = strings.ToUpper(cb.Status)
	cb.SubID = r.Header.Get("for-user-id")
	cb.SubRef = r.Header.Get("X-Sub-Client")
	return &cb, nil
}

// ParseWithdrawalCallback reads a withdrawal callback from an incoming request.
func ParseWithdrawalCallback(r *http.Request) (*WithdrawalCallback, error) {
	body, err := io.ReadAll(io.LimitReader(r.Body, maxCallbackBody))
	if err != nil {
		return nil, err
	}
	var cb WithdrawalCallback
	if err := json.Unmarshal(body, &cb); err != nil {
		return nil, fmt.Errorf("sarana: invalid callback body: %w", err)
	}
	cb.SubID = r.Header.Get("for-user-id")
	cb.SubRef = r.Header.Get("X-Sub-Client")
	return &cb, nil
}
