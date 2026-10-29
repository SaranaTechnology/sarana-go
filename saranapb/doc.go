// Package saranapb is the gRPC client for the Sarana API (sarana.v1), generated
// from sarana.proto. It is a separate module so the main sarana-go package
// stays free of dependencies outside the standard library.
//
//	conn, _ := grpc.NewClient("api.internal.saranatechnology.com:443", grpc.WithTransportCredentials(credentials.NewTLS(nil)))
//	ctx = metadata.AppendToOutgoingContext(ctx, "x-api-key", key)
//	res, err := saranapb.NewPaymentsClient(conn).CreateInvoice(ctx, &saranapb.InvoiceRequest{Amount: 150000})
//
// Every RPC returns a Result whose Data is the REST "data" field. Errors are
// gRPC statuses mapped from the REST status (see sarana.proto).
package saranapb
