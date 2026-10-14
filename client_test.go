package sarana

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type seen struct {
	method, path, query string
	header              http.Header
	body                map[string]any
}

func server(t *testing.T, status int, resp string) (*httptest.Server, *seen) {
	t.Helper()
	s := &seen{}
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.method, s.path, s.query, s.header = r.Method, r.URL.EscapedPath(), r.URL.RawQuery, r.Header.Clone()
		b, _ := io.ReadAll(r.Body)
		if len(b) > 0 {
			_ = json.Unmarshal(b, &s.body)
		}
		w.WriteHeader(status)
		_, _ = w.Write([]byte(resp))
	}))
	t.Cleanup(ts.Close)
	return ts, s
}

func TestCreateInvoice(t *testing.T) {
	ts, s := server(t, 201, `{"success":true,"code":201,"data":{"invoice_url":"https://x","transaction":{"transaction_id":"INV-1"}}}`)
	c := New("sk_test", WithBaseURL(ts.URL))

	got, err := c.CreateInvoice(context.Background(), InvoiceRequest{Amount: 15000, ExternalID: "ORDER-1", CallbackURL: "https://me/cb"})
	if err != nil {
		t.Fatal(err)
	}
	if got["invoice_url"] != "https://x" {
		t.Fatalf("data = %v", got)
	}
	if s.method != "POST" || s.path != "/payments/invoice" {
		t.Fatalf("request = %s %s", s.method, s.path)
	}
	if s.header.Get("X-API-Key") != "sk_test" || s.header.Get("for-user-id") != "" {
		t.Fatalf("headers = %v", s.header)
	}
	if s.body["external_id"] != "ORDER-1" || s.body["amount"] != 15000.0 {
		t.Fatalf("body = %v", s.body)
	}
	if _, ok := s.body["client_id"]; ok {
		t.Fatal("client_id must not be sent; the API takes it from the key")
	}
}

func TestSubClientHeaders(t *testing.T) {
	ts, s := server(t, 200, `{"success":true,"data":{"saldo":1}}`)
	c := New("k", WithBaseURL(ts.URL))

	if _, err := c.ForSubClient("sub_abc").GetBalance(context.Background()); err != nil {
		t.Fatal(err)
	}
	if s.header.Get("for-user-id") != "sub_abc" {
		t.Fatalf("for-user-id = %q", s.header.Get("for-user-id"))
	}

	if _, err := c.ForSubRef("OUTLET-01").GetAccount(context.Background()); err != nil {
		t.Fatal(err)
	}
	if s.header.Get("X-Sub-Client") != "OUTLET-01" || s.header.Get("for-user-id") != "" {
		t.Fatalf("headers = %v", s.header)
	}

	// Parent-only endpoints never carry the sub-client header.
	if _, err := c.ForSubRef("OUTLET-01").TransferSubClient(context.Background(), "OUTLET-01", TransferRequest{Arah: KeSub, Jumlah: 5}); err != nil {
		t.Fatal(err)
	}
	if s.header.Get("X-Sub-Client") != "" || s.path != "/sub-clients/OUTLET-01/transfers" {
		t.Fatalf("transfer sent %s with %v", s.path, s.header)
	}
}

func TestListAndQuery(t *testing.T) {
	ts, s := server(t, 200, `{"success":true,"data":[{"id":1}],"meta":{"total":1,"per_page":10,"current_page":1,"last_page":1}}`)
	c := New("k", WithBaseURL(ts.URL))

	l, err := c.ListTransactions(context.Background(), ListParams{Page: 2, Status: "PAID", Group: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(l.Data) != 1 || l.Meta == nil || l.Meta.Total != 1 {
		t.Fatalf("list = %+v", l)
	}
	if s.query != "group=true&page=2&status=PAID" {
		t.Fatalf("query = %s", s.query)
	}
}

func TestPathEscape(t *testing.T) {
	ts, s := server(t, 200, `{"success":true,"data":{}}`)
	c := New("k", WithBaseURL(ts.URL))
	if _, err := c.GetTransactionByExternalID(context.Background(), "A/B C"); err != nil {
		t.Fatal(err)
	}
	if s.path != "/transactions/external/A%2FB%20C" {
		t.Fatalf("path = %s", s.path)
	}
}

func TestError(t *testing.T) {
	ts, _ := server(t, 422, `{"success":false,"message":"Saldo kurang","code":422}`)
	c := New("k", WithBaseURL(ts.URL))

	_, err := c.CreateWithdrawal(context.Background(), WithdrawalRequest{Jumlah: 1})
	var e *Error
	if !errors.As(err, &e) || e.Status != 422 || e.Message != "Saldo kurang" {
		t.Fatalf("err = %v", err)
	}

	ts2, _ := server(t, 502, `<html>bad gateway</html>`)
	_, err = New("k", WithBaseURL(ts2.URL)).GetAccount(context.Background())
	if !errors.As(err, &e) || e.Status != 502 || e.Message != "Bad Gateway" {
		t.Fatalf("err = %v", err)
	}
}

func TestParsePaymentCallback(t *testing.T) {
	r := httptest.NewRequest("POST", "/cb", strings.NewReader(`{"transaction_id":"INV-1","client_external_id":"ORDER-1","status":"paid","amount":15000,"extra":1}`))
	r.Header.Set("for-user-id", "sub_abc")
	r.Header.Set("X-Sub-Client", "OUTLET-01")

	cb, err := ParsePaymentCallback(r)
	if err != nil {
		t.Fatal(err)
	}
	if !cb.IsPaid() || cb.ClientExternalID != "ORDER-1" || cb.SubRef != "OUTLET-01" || cb.SubID != "sub_abc" || cb.Raw["extra"] != 1.0 {
		t.Fatalf("cb = %+v", cb)
	}
}
