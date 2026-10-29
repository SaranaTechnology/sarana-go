# sarana-go

Client Go untuk API pembayaran Sarana (`/api/v2`). Tanpa dependency di luar standard library.

```bash
go get github.com/SaranaTechnology/sarana-go
```

## Pakai

```go
import sarana "github.com/SaranaTechnology/sarana-go"

c := sarana.New(os.Getenv("SARANA_API_KEY"))
// Untuk tes ke Xendit sandbox:
// c := sarana.New(key, sarana.WithBaseURL(sarana.SandboxURL))

inv, err := c.CreateInvoice(ctx, sarana.InvoiceRequest{
	Amount:      150000,
	ExternalID:  "ORDER-1001",
	CallbackURL: "https://tokoku.id/sarana/callback",
})
if err != nil {
	var e *sarana.Error
	if errors.As(err, &e) {
		log.Printf("HTTP %d: %s", e.Status, e.Message)
	}
	return err
}
fmt.Println(inv["invoice_url"])
```

Tiap method mengembalikan isi field `data` dari response API (`sarana.Object` = `map[string]any`). Method list (`ListTransactions`, `ListWithdrawals`, dan lainnya) mengembalikan `*sarana.List` berisi `Data` dan `Meta` untuk paginasi.

`client_id` tidak perlu dikirim. API mengambilnya dari API key.

## Sub-client

```go
outlet := c.ForSubRef("OUTLET-01")   // header X-Sub-Client
// atau c.ForSubClient("sub_3f9a...") // header for-user-id

outlet.CreateQRIS(ctx, sarana.QRISRequest{Amount: 25000})
outlet.GetBalance(ctx)

// Pindah saldo selalu dari akun induk:
c.TransferSubClient(ctx, "OUTLET-01", sarana.TransferRequest{Arah: sarana.KeSub, Jumlah: 500000})
```

Kalau `TransferSubClient` mengembalikan `status: "unknown"` (HTTP 202), jangan diulang. Mengulang bisa memindahkan uang dua kali. Admin Sarana yang mencocokkan transfer itu.

## Callback

```go
http.HandleFunc("/sarana/callback", func(w http.ResponseWriter, r *http.Request) {
	cb, err := sarana.ParsePaymentCallback(r)
	if err != nil {
		http.Error(w, "bad request", 400)
		return
	}
	w.WriteHeader(200) // balas cepat, proses di belakang

	go func() {
		// Callback tidak ditandatangani: cek ulang ke API sebelum menandai lunas.
		tx, err := c.GetTransactionByExternalID(context.Background(), cb.ClientExternalID)
		if err == nil && tx["status"] == "paid" {
			// tandai order lunas (idempoten: callback yang sama bisa datang lagi)
		}
	}()
})
```

Callback invoice dikirim saat status `PAID`, `SETTLED`, atau `EXPIRED`. Status pending tidak dikirim, jadi pakai `PaymentStatus` untuk polling. Transaksi milik sub-client membawa `cb.SubID` dan `cb.SubRef`.

## Langganan (jual paket Sarana dari produkmu)

```go
bill, err := c.Checkout(ctx, sarana.CheckoutRequest{
	PlanCode:   "cashier-pro",
	ExternalID: "SUB-TOKO-42-2026-11", // referensimu; dipanggil ulang = tagihan yang sama
	PayerEmail: "pemilik@tokoku.id",
})
// arahkan pembeli ke bill["checkout_url"]
```

`409` berarti `external_id` itu sudah dibayar atau ditutup. Isi tagihannya ada di `Error.Body`: kalau sudah `paid`, aktifkan paketnya. Kalau tidak, buat `external_id` baru. Status bisa dibaca kapan saja dengan `CheckoutStatus`.

## Webhook (bertanda tangan)

Webhook yang didaftarkan untuk akunmu (`subscription.paid`, `subscription.expired`) ditandatangani dengan secret webhook. Beda dengan callback invoice di atas, isinya bisa dipercaya setelah lolos verifikasi.

```go
http.HandleFunc("/sarana/webhook", func(w http.ResponseWriter, r *http.Request) {
	ev, err := sarana.ParseWebhook(r, os.Getenv("SARANA_WEBHOOK_SECRET"))
	if err != nil {
		http.Error(w, "unauthorized", 401) // signature salah atau timestamp > 5 menit
		return
	}
	w.WriteHeader(200)
	// ev.DeliveryID untuk dedupe: webhook yang sama bisa dikirim ulang.
	if ev.Event == sarana.EventSubscriptionPaid {
		// aktifkan paket ev.Data["external_id"]
	}
})
```

Selama rotasi secret, header `X-Sarana-Signature` membawa dua signature. Secret lama dan baru sama-sama diterima.

## KYC

```go
f, _ := os.Open("ktp.pdf")
file, err := c.UploadKYCDocument(ctx, "ktp.pdf", f) // file["id"] dipakai di payload
v, err := c.SubmitKYC(ctx, payload)                // skema account_verification Xendit
st, err := c.GetKYC(ctx)
```

Sub-client tidak mengajukan KYC sendiri (`422`). Verifikasinya ikut induk, dan `GetKYC` mengembalikan `ikut_induk: true`.

## gRPC (opsional)

Selain REST, API yang sama tersedia lewat gRPC (`saranapb/sarana.proto`, package `sarana.v1`). Stub-nya ada di modul terpisah supaya `sarana-go` tetap tanpa dependency:

```bash
go get github.com/SaranaTechnology/sarana-go/saranapb
```

```go
conn, err := grpc.NewClient(addr, grpc.WithTransportCredentials(credentials.NewTLS(nil)))
ctx = metadata.AppendToOutgoingContext(ctx, "x-api-key", key /*, "x-sub-client", "OUTLET-01" */)

res, err := saranapb.NewPaymentsClient(conn).CreateInvoice(ctx, &saranapb.InvoiceRequest{
	Amount: 150000, ExternalId: "ORDER-1001",
})
url := res.GetData().GetStructValue().AsMap()["invoice_url"]
```

Tiap RPC menjalankan route REST kembarannya, jadi aturan dan pesan error-nya sama. Status HTTP dipetakan ke kode gRPC: 400 `InvalidArgument`, 401 `Unauthenticated`, 404 `NotFound`, 409 `AlreadyExists`, 422 `FailedPrecondition`, 5xx `Unavailable`/`Internal`. Alamat endpoint gRPC diumumkan terpisah. Selama belum ada, pakai REST.

## Daftar method

| Grup | Method |
|---|---|
| Payments | `PaymentMethods`, `CreateVA`, `CreateEWallet`, `CreateQRIS`, `CreateInvoice`, `PaymentStatus` |
| Fees | `FeeRates`, `SettlementSchedule`, `CalculateFee`, `CalculateFeeBulk`, `CalculateVAT`, `CalculateWithdrawalFee`, `CalculateAmountToPay` |
| Transactions | `ListTransactions`, `GetTransaction`, `GetTransactionByExternalID` |
| Withdrawals | `ListWithdrawals`, `CreateWithdrawal`, `GetWithdrawal`, `GetWithdrawalByExternalID`, `CancelWithdrawal` |
| Settlements | `CreateSettlement` |
| Bank channels | `ActiveBankChannels`, `GetBankChannel` |
| Account | `GetAccount`, `UpdateAccount`, `GetBalance`, `SetAutoWithdrawal`, `ListApps` |
| KYC | `GetKYC`, `UploadKYCDocument`, `SubmitKYC` |
| Langganan | `Checkout`, `CheckoutStatus` |
| Webhook | `ParseWebhook`, `VerifyWebhook` |
| Sub-clients | `BalanceSummary`, `ListSubClients`, `GetSubClient`, `TransferSubClient`, `ListSubClientTransfers`, `ListGroupTransfers` |

## Lisensi

MIT
