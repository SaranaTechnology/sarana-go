package sarana

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestSetAutoWithdrawal(t *testing.T) {
	ts, s := server(t, 200, `{"success":true,"data":{"auto_approve_withdrawal":true}}`)
	c := New("sk_test", WithBaseURL(ts.URL)).ForSubRef("OUTLET-01")

	if _, err := c.SetAutoWithdrawal(context.Background(), false); err != nil {
		t.Fatal(err)
	}
	if s.method != "PATCH" || s.path != "/account/auto-withdrawal" {
		t.Fatalf("request = %s %s", s.method, s.path)
	}
	// false must be sent, not dropped: the API answers 400 without "enabled".
	if v, ok := s.body["enabled"]; !ok || v != false {
		t.Fatalf("body = %v", s.body)
	}
	if s.header.Get("X-Sub-Client") != "OUTLET-01" {
		t.Fatalf("headers = %v", s.header)
	}
}

func TestUploadKYCDocument(t *testing.T) {
	var gotName, gotContent, gotPath string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		_, params, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
		if err != nil {
			t.Errorf("content type: %v", err)
			return
		}
		part, err := multipart.NewReader(r.Body, params["boundary"]).NextPart()
		if err != nil {
			t.Errorf("multipart: %v", err)
			return
		}
		b, _ := io.ReadAll(part)
		gotName, gotContent = part.FileName(), string(b)
		if part.FormName() != "file" {
			t.Errorf("field = %q, want file", part.FormName())
		}
		_, _ = w.Write([]byte(`{"success":true,"data":{"id":"file-123"}}`))
	}))
	defer ts.Close()

	got, err := New("sk_test", WithBaseURL(ts.URL)).UploadKYCDocument(context.Background(), "ktp.pdf", strings.NewReader("%PDF-1.4"))
	if err != nil {
		t.Fatal(err)
	}
	if got["id"] != "file-123" || gotPath != "/account/kyc/files" || gotName != "ktp.pdf" || gotContent != "%PDF-1.4" {
		t.Fatalf("got %v, path %s, file %s %q", got, gotPath, gotName, gotContent)
	}
}

func TestSubmitKYCRawPassthrough(t *testing.T) {
	ts, s := server(t, 200, `{"success":true,"data":{"status":"PENDING"}}`)
	c := New("sk_test", WithBaseURL(ts.URL))

	payload := json.RawMessage(`{"business_detail":{"legal_name":"PT Toko"}}`)
	if _, err := c.SubmitKYC(context.Background(), payload); err != nil {
		t.Fatal(err)
	}
	bd, _ := s.body["business_detail"].(map[string]any)
	if s.method != "POST" || s.path != "/account/kyc" || bd["legal_name"] != "PT Toko" {
		t.Fatalf("request = %s %s %v", s.method, s.path, s.body)
	}
	if s.header.Get("Content-Type") != "application/json" {
		t.Fatalf("content type = %q", s.header.Get("Content-Type"))
	}
}

func TestCheckoutConflictKeepsBill(t *testing.T) {
	ts, s := server(t, 409, `{"success":false,"message":"Tagihan sudah dibayar","data":{"external_id":"SUB-1","status":"paid"}}`)
	c := New("sk_test", WithBaseURL(ts.URL))

	_, err := c.Checkout(context.Background(), CheckoutRequest{PlanCode: "cashier-pro", ExternalID: "SUB-1"})
	var e *Error
	if !errors.As(err, &e) || e.Status != 409 || e.Message != "Tagihan sudah dibayar" {
		t.Fatalf("err = %v", err)
	}
	if !strings.Contains(string(e.Body), `"status":"paid"`) {
		t.Fatalf("body = %s", e.Body)
	}
	if s.path != "/subscriptions/checkout" || s.body["plan_code"] != "cashier-pro" {
		t.Fatalf("request = %s %v", s.path, s.body)
	}
}

func TestCheckoutStatusEscapesRef(t *testing.T) {
	ts, s := server(t, 200, `{"success":true,"data":{"status":"open"}}`)
	if _, err := New("sk_test", WithBaseURL(ts.URL)).CheckoutStatus(context.Background(), "SUB/1"); err != nil {
		t.Fatal(err)
	}
	if s.method != "GET" || s.path != "/subscriptions/checkout/SUB%2F1" {
		t.Fatalf("request = %s %s", s.method, s.path)
	}
}

// Signatures from api-go's webhookout.SignatureHeader for this exact body,
// timestamp and secrets ("whsec_new" current, "whsec_old" in rotation).
const (
	vecBody = `{"client_id":15,"data":{"external_id":"SUB-1"},"delivery_id":"d1","event":"subscription.paid","occurred_at":"2026-10-29T00:00:00Z","provider":"sarana","version":1}`
	vecTS   = "1793232000"
	vecSig  = "sha256=c26ee6e21544b1067621a055b36fbbed6368afe1310d4e5af42689413108ebbb,sha256=41d9bb24fea38a3e061b593275067a656765896dc44fd38c69c8b582b12d79a2"
)

func atVecTime(t *testing.T, offset time.Duration) {
	t.Helper()
	now = func() time.Time { return time.Unix(1793232000, 0).Add(offset) }
	t.Cleanup(func() { now = time.Now })
}

func TestVerifyWebhook(t *testing.T) {
	atVecTime(t, time.Minute)
	body := []byte(vecBody)

	for _, secret := range []string{"whsec_new", "whsec_old"} {
		if err := VerifyWebhook(body, vecTS, vecSig, secret, 0); err != nil {
			t.Fatalf("secret %s: %v", secret, err)
		}
	}
	cases := map[string]struct {
		body            []byte
		ts, sig, secret string
		want            error
	}{
		"wrong secret":  {body, vecTS, vecSig, "whsec_x", ErrInvalidSignature},
		"tampered body": {[]byte(strings.Replace(vecBody, "SUB-1", "SUB-2", 1)), vecTS, vecSig, "whsec_new", ErrInvalidSignature},
		"no signature":  {body, vecTS, "", "whsec_new", ErrInvalidSignature},
		"no secret":     {body, vecTS, vecSig, "", ErrInvalidSignature},
		"bad timestamp": {body, "abc", vecSig, "whsec_new", ErrInvalidSignature},
		"stale":         {body, "1793231000", vecSig, "whsec_new", ErrStaleWebhook},
	}
	for name, tc := range cases {
		if err := VerifyWebhook(tc.body, tc.ts, tc.sig, tc.secret, 0); !errors.Is(err, tc.want) {
			t.Errorf("%s: err = %v, want %v", name, err, tc.want)
		}
	}
}

func TestParseWebhook(t *testing.T) {
	atVecTime(t, 0)
	r := httptest.NewRequest("POST", "/hook", strings.NewReader(vecBody))
	r.Header.Set(HeaderWebhookTimestamp, vecTS)
	r.Header.Set(HeaderWebhookSignature, vecSig)
	r.Header.Set(HeaderWebhookAttempt, "2")

	w, err := ParseWebhook(r, "whsec_new")
	if err != nil {
		t.Fatal(err)
	}
	if w.Event != EventSubscriptionPaid || w.DeliveryID != "d1" || w.ClientID != 15 || w.Attempt != 2 || w.Data["external_id"] != "SUB-1" {
		t.Fatalf("webhook = %+v", w)
	}

	atVecTime(t, 10*time.Minute)
	r = httptest.NewRequest("POST", "/hook", strings.NewReader(vecBody))
	r.Header.Set(HeaderWebhookTimestamp, vecTS)
	r.Header.Set(HeaderWebhookSignature, vecSig)
	if _, err := ParseWebhook(r, "whsec_new"); !errors.Is(err, ErrStaleWebhook) {
		t.Fatalf("replay: err = %v", err)
	}
}
