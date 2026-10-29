package sarana

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"strconv"
)

// ---------- Payments ----------

// VARequest creates a Virtual Account payment.
type VARequest struct {
	Amount       float64 `json:"amount"`
	BankCode     string  `json:"bank_code"` // BCA, BNI, BRI, MANDIRI, ...
	CustomerName string  `json:"customer_name,omitempty"`
	AppID        int     `json:"app_id,omitempty"` // required when the account has more than one app
}

// EWalletRequest creates an e-wallet charge.
type EWalletRequest struct {
	Amount        float64 `json:"amount"`
	ChannelCode   string  `json:"channel_code"` // OVO, DANA, SHOPEEPAY, LINKAJA
	CustomerPhone string  `json:"customer_phone,omitempty"`
	SuccessURL    string  `json:"success_url,omitempty"`
	AppID         int     `json:"app_id,omitempty"`
}

// QRISRequest creates a QRIS payment.
type QRISRequest struct {
	Amount float64 `json:"amount"`
	AppID  int     `json:"app_id,omitempty"`
}

// InvoiceRequest creates a hosted payment page. CallbackURL receives the
// terminal status (PAID/SETTLED/EXPIRED); see ParsePaymentCallback.
type InvoiceRequest struct {
	Amount         float64        `json:"amount"` // minimum 1000
	ExternalID     string         `json:"external_id,omitempty"`
	Description    string         `json:"description,omitempty"`
	CustomerName   string         `json:"customer_name,omitempty"`
	CustomerEmail  string         `json:"customer_email,omitempty"`
	PaymentMethods []string       `json:"payment_methods,omitempty"`
	ExpiryDuration int            `json:"expiry_duration,omitempty"` // seconds
	SuccessURL     string         `json:"success_url,omitempty"`
	FailureURL     string         `json:"failure_url,omitempty"`
	CallbackURL    string         `json:"callback_url,omitempty"`
	Metadata       map[string]any `json:"metadata,omitempty"`
	AppID          int            `json:"app_id,omitempty"`
}

// PaymentMethods lists the payment methods available to the account.
func (c *Client) PaymentMethods(ctx context.Context) (any, error) {
	return c.raw(ctx, "/payments/methods", true)
}

// CreateVA creates a Virtual Account payment.
func (c *Client) CreateVA(ctx context.Context, req VARequest) (Object, error) {
	return c.object(ctx, http.MethodPost, "/payments/va", nil, req, true)
}

// CreateEWallet creates an e-wallet charge.
func (c *Client) CreateEWallet(ctx context.Context, req EWalletRequest) (Object, error) {
	return c.object(ctx, http.MethodPost, "/payments/ewallet", nil, req, true)
}

// CreateQRIS creates a QRIS payment.
func (c *Client) CreateQRIS(ctx context.Context, req QRISRequest) (Object, error) {
	return c.object(ctx, http.MethodPost, "/payments/qris", nil, req, true)
}

// CreateInvoice creates a hosted invoice (payment page).
func (c *Client) CreateInvoice(ctx context.Context, req InvoiceRequest) (Object, error) {
	return c.object(ctx, http.MethodPost, "/payments/invoice", nil, req, true)
}

// PaymentStatus returns the status of a payment request.
func (c *Client) PaymentStatus(ctx context.Context, id string) (Object, error) {
	return c.object(ctx, http.MethodGet, "/payments/"+seg(id)+"/status", nil, nil, true)
}

// ---------- Fees ----------

// FeeRequest is the body for CalculateFee.
type FeeRequest struct {
	Amount      float64 `json:"amount"`
	ChannelCode string  `json:"channel_code"`
	IncludeVAT  bool    `json:"include_vat,omitempty"`
}

// BulkFeeRequest is the body for CalculateFeeBulk.
type BulkFeeRequest struct {
	Amount     float64  `json:"amount"`
	Channels   []string `json:"channels"`
	IncludeVAT bool     `json:"include_vat,omitempty"`
}

// VATRequest is the body for CalculateVAT.
type VATRequest struct {
	Amount      float64 `json:"amount"`
	IsInclusive bool    `json:"is_inclusive,omitempty"`
}

// WithdrawalFeeRequest is the body for CalculateWithdrawalFee.
type WithdrawalFeeRequest struct {
	Amount   float64 `json:"amount"`
	BankCode string  `json:"bank_code"`
}

// AmountToPayRequest is the body for CalculateAmountToPay.
type AmountToPayRequest struct {
	NetAmount   float64 `json:"net_amount"`
	ChannelCode string  `json:"channel_code"`
	IncludeVAT  bool    `json:"include_vat,omitempty"`
}

// FeeRates lists the fee per channel.
func (c *Client) FeeRates(ctx context.Context) (any, error) {
	return c.raw(ctx, "/fees/rates", true)
}

// SettlementSchedule tells when paid money becomes withdrawable, per channel.
func (c *Client) SettlementSchedule(ctx context.Context) (Object, error) {
	return c.object(ctx, http.MethodGet, "/fees/settlement", nil, nil, true)
}

// CalculateFee calculates the fee for one channel.
func (c *Client) CalculateFee(ctx context.Context, req FeeRequest) (Object, error) {
	return c.object(ctx, http.MethodPost, "/fees/calculate", nil, req, true)
}

// CalculateFeeBulk calculates the fee for several channels.
func (c *Client) CalculateFeeBulk(ctx context.Context, req BulkFeeRequest) (any, error) {
	env, err := c.do(ctx, http.MethodPost, "/fees/calculate/bulk", nil, req, true)
	if err != nil {
		return nil, err
	}
	return decodeAny(env.Data)
}

// CalculateVAT calculates VAT.
func (c *Client) CalculateVAT(ctx context.Context, req VATRequest) (Object, error) {
	return c.object(ctx, http.MethodPost, "/fees/calculate/vat", nil, req, true)
}

// CalculateWithdrawalFee calculates the withdrawal fee.
func (c *Client) CalculateWithdrawalFee(ctx context.Context, req WithdrawalFeeRequest) (Object, error) {
	return c.object(ctx, http.MethodPost, "/fees/calculate/withdrawal", nil, req, true)
}

// CalculateAmountToPay returns what the customer must pay so the merchant
// receives NetAmount.
func (c *Client) CalculateAmountToPay(ctx context.Context, req AmountToPayRequest) (Object, error) {
	return c.object(ctx, http.MethodPost, "/fees/calculate/amount-to-pay", nil, req, true)
}

// ---------- Transactions ----------

// ListParams filters list endpoints. Group=true lists the parent account and
// every sub-client together; it cannot be combined with ForSubClient.
type ListParams struct {
	Page    int
	PerPage int // max 100
	Status  string
	Group   bool
}

func (p ListParams) values() url.Values {
	q := url.Values{}
	if p.Page > 0 {
		q.Set("page", strconv.Itoa(p.Page))
	}
	if p.PerPage > 0 {
		q.Set("per_page", strconv.Itoa(p.PerPage))
	}
	if p.Status != "" {
		q.Set("status", p.Status)
	}
	if p.Group {
		q.Set("group", "true")
	}
	return q
}

// ListTransactions lists transactions of the account (or sub-client).
func (c *Client) ListTransactions(ctx context.Context, p ListParams) (*List, error) {
	return c.list(ctx, "/transactions", p.values(), true)
}

// GetTransaction returns one transaction by numeric id.
func (c *Client) GetTransaction(ctx context.Context, id int) (Object, error) {
	return c.object(ctx, http.MethodGet, "/transactions/"+strconv.Itoa(id), nil, nil, true)
}

// GetTransactionByExternalID looks a transaction up by your external_id.
// A 409 means the id exists under several sub-clients: use ForSubClient.
func (c *Client) GetTransactionByExternalID(ctx context.Context, externalID string) (Object, error) {
	return c.object(ctx, http.MethodGet, "/transactions/external/"+seg(externalID), nil, nil, true)
}

// ---------- Withdrawals ----------

// WithdrawalRequest asks for a withdrawal to the registered bank account.
// URL receives the status callback.
type WithdrawalRequest struct {
	Jumlah         float64 `json:"jumlah"`
	BiayaPenarikan float64 `json:"biaya_penarikan,omitempty"`
	By             string  `json:"by,omitempty"`
	URL            string  `json:"url,omitempty"`
}

// ListWithdrawals lists withdrawals.
func (c *Client) ListWithdrawals(ctx context.Context, p ListParams) (*List, error) {
	return c.list(ctx, "/withdrawals", p.values(), true)
}

// CreateWithdrawal asks for a withdrawal.
func (c *Client) CreateWithdrawal(ctx context.Context, req WithdrawalRequest) (Object, error) {
	return c.object(ctx, http.MethodPost, "/withdrawals", nil, req, true)
}

// GetWithdrawal returns one withdrawal by numeric id.
func (c *Client) GetWithdrawal(ctx context.Context, id int) (Object, error) {
	return c.object(ctx, http.MethodGet, "/withdrawals/"+strconv.Itoa(id), nil, nil, true)
}

// GetWithdrawalByExternalID looks a withdrawal up by external_id.
func (c *Client) GetWithdrawalByExternalID(ctx context.Context, externalID string) (Object, error) {
	return c.object(ctx, http.MethodGet, "/withdrawals/external/"+seg(externalID), nil, nil, true)
}

// CancelWithdrawal cancels a pending withdrawal.
func (c *Client) CancelWithdrawal(ctx context.Context, id int) (Object, error) {
	return c.object(ctx, http.MethodPost, "/withdrawals/"+strconv.Itoa(id)+"/cancel", nil, nil, true)
}

// ---------- Settlements ----------

// SettlementRequest creates a settlement (disbursement).
type SettlementRequest struct {
	Jumlah float64 `json:"jumlah"`
	Tujuan string  `json:"tujuan"`
	By     string  `json:"by,omitempty"`
	Norek  string  `json:"norek,omitempty"`
	Bank   string  `json:"bank,omitempty"`
}

// CreateSettlement creates a settlement.
func (c *Client) CreateSettlement(ctx context.Context, req SettlementRequest) (Object, error) {
	return c.object(ctx, http.MethodPost, "/settlements", nil, req, true)
}

// ---------- Bank channels ----------

// ActiveBankChannels lists active banks/channels.
func (c *Client) ActiveBankChannels(ctx context.Context) (any, error) {
	return c.raw(ctx, "/bank-channels/active", true)
}

// GetBankChannel returns one channel by code.
func (c *Client) GetBankChannel(ctx context.Context, code string) (Object, error) {
	return c.object(ctx, http.MethodGet, "/bank-channels/code/"+seg(code), nil, nil, true)
}

// ---------- Account ----------

// AccountUpdate holds the profile fields a client may change. Sub-clients
// cannot change Norek/Bank (403).
type AccountUpdate struct {
	Nama      string `json:"nama,omitempty"`
	Email     string `json:"email,omitempty"`
	Alamat    string `json:"alamat,omitempty"`
	PIC       string `json:"pic,omitempty"`
	NoTelpPIC string `json:"no_telp_pic,omitempty"`
	EmailPIC  string `json:"email_pic,omitempty"`
	Norek     string `json:"norek,omitempty"`
	Bank      string `json:"bank,omitempty"`
}

// GetAccount returns the profile behind the API key (or sub-client).
func (c *Client) GetAccount(ctx context.Context) (Object, error) {
	return c.object(ctx, http.MethodGet, "/account", nil, nil, true)
}

// UpdateAccount updates the whitelisted profile fields.
func (c *Client) UpdateAccount(ctx context.Context, req AccountUpdate) (Object, error) {
	return c.object(ctx, http.MethodPut, "/account", nil, req, true)
}

// GetBalance returns the balance of the account (or sub-client).
func (c *Client) GetBalance(ctx context.Context) (Object, error) {
	return c.object(ctx, http.MethodGet, "/account/balance", nil, nil, true)
}

// ListApps lists the apps of the account; pass an id as AppID when there is
// more than one.
func (c *Client) ListApps(ctx context.Context) (any, error) {
	return c.raw(ctx, "/apps", true)
}

// ---------- Sub-clients (always as the parent account) ----------

// TransferRequest moves balance between the parent and one sub-client.
// Arah is "ke_sub" (parent → sub) or "ke_induk" (sub → parent).
type TransferRequest struct {
	Arah    string  `json:"arah"`
	Jumlah  float64 `json:"jumlah"`
	Catatan string  `json:"catatan,omitempty"`
}

const (
	KeSub   = "ke_sub"
	KeInduk = "ke_induk"
)

// BalanceSummary returns the parent balance, each sub-client and the total.
func (c *Client) BalanceSummary(ctx context.Context) (Object, error) {
	return c.object(ctx, http.MethodGet, "/account/balance-summary", nil, nil, false)
}

// ListSubClients lists the sub-clients of the account.
func (c *Client) ListSubClients(ctx context.Context, p ListParams) (*List, error) {
	return c.list(ctx, "/sub-clients", p.values(), false)
}

// GetSubClient returns one sub-client by sub_id or sub_ref.
func (c *Client) GetSubClient(ctx context.Context, ref string) (Object, error) {
	return c.object(ctx, http.MethodGet, "/sub-clients/"+seg(ref), nil, nil, false)
}

// TransferSubClient moves balance between the parent and a sub-client
// (sub_id or sub_ref). Check the returned "status": HTTP 202 comes back as
// success with status "unknown", meaning Xendit gave no clear answer. Do NOT
// retry that one; a retry can move the money twice.
func (c *Client) TransferSubClient(ctx context.Context, key string, req TransferRequest) (Object, error) {
	return c.object(ctx, http.MethodPost, "/sub-clients/"+seg(key)+"/transfers", nil, req, false)
}

// ListSubClientTransfers lists the transfers of one sub-client.
func (c *Client) ListSubClientTransfers(ctx context.Context, key string, p ListParams) (*List, error) {
	return c.list(ctx, "/sub-clients/"+seg(key)+"/transfers", p.values(), false)
}

// ListGroupTransfers lists every parent ↔ sub-client transfer, newest first.
func (c *Client) ListGroupTransfers(ctx context.Context, p ListParams) (*List, error) {
	return c.list(ctx, "/group-transfers", p.values(), false)
}

// SetAutoWithdrawal turns auto-approved withdrawals on or off for the account
// (or sub-client). Turning it on needs complete bank info on file (400 if not).
func (c *Client) SetAutoWithdrawal(ctx context.Context, enabled bool) (Object, error) {
	return c.object(ctx, http.MethodPatch, "/account/auto-withdrawal", nil, map[string]bool{"enabled": enabled}, true)
}

// ---------- KYC (Xendit sub-account verification) ----------

// GetKYC returns the sub-account status and its verification ("verification"
// is null until something is submitted). A sub-client gets its parent's, with
// "ikut_induk": true.
func (c *Client) GetKYC(ctx context.Context) (Object, error) {
	return c.object(ctx, http.MethodGet, "/account/kyc", nil, nil, true)
}

// UploadKYCDocument uploads one document (PDF, PNG or JPG, max 10MB) and
// returns Xendit's file id, which SubmitKYC's payload then refers to.
// A sub-client cannot upload its own (422): its verification is its parent's.
func (c *Client) UploadKYCDocument(ctx context.Context, filename string, file io.Reader) (Object, error) {
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	part, err := w.CreateFormFile("file", filename)
	if err != nil {
		return nil, err
	}
	if _, err := io.Copy(part, file); err != nil {
		return nil, err
	}
	if err := w.Close(); err != nil {
		return nil, err
	}
	return c.object(ctx, http.MethodPost, "/account/kyc/files", nil, rawBody{buf.Bytes(), w.FormDataContentType()}, true)
}

// SubmitKYC sends the verification. payload follows Xendit's "Submit account
// verification" schema and is forwarded unchanged, so pass a struct, a map, or
// a json.RawMessage. Xendit's validation errors come back as *Error with its
// status (4xx) and message.
func (c *Client) SubmitKYC(ctx context.Context, payload any) (Object, error) {
	if raw, ok := payload.(json.RawMessage); ok {
		return c.object(ctx, http.MethodPost, "/account/kyc", nil, rawBody{raw, "application/json"}, true)
	}
	return c.object(ctx, http.MethodPost, "/account/kyc", nil, payload, true)
}

// ---------- Subscriptions (sell Sarana plans from your product) ----------

// CheckoutRequest opens a payment page for one plan. ExternalID is your
// reference: calling again with the same one while the bill is open returns
// that bill instead of a new one.
type CheckoutRequest struct {
	PlanCode   string `json:"plan_code"`
	ExternalID string `json:"external_id"`
	PayerName  string `json:"payer_name,omitempty"`
	PayerEmail string `json:"payer_email,omitempty"`
	SuccessURL string `json:"success_url,omitempty"`
	FailureURL string `json:"failure_url,omitempty"`
}

// Checkout creates (201) or returns the still-open (200) bill for
// req.ExternalID. Errors: 409 when that external_id is already paid or closed
// (the bill is in Error.Body; start over with a new external_id only if it was
// not paid), 422 plan not purchasable, 502 gateway refused (safe to retry).
func (c *Client) Checkout(ctx context.Context, req CheckoutRequest) (Object, error) {
	return c.object(ctx, http.MethodPost, "/subscriptions/checkout", nil, req, true)
}

// CheckoutStatus reads a bill by your external_id. Another account's
// external_id is a 404.
func (c *Client) CheckoutStatus(ctx context.Context, externalID string) (Object, error) {
	return c.object(ctx, http.MethodGet, "/subscriptions/checkout/"+seg(externalID), nil, nil, true)
}
