package xservice

import (
	"fmt"
	"slices"
	"sort"
	"strings"
	"testing"
)

// TestContractsGroundTruth checks every link of the multi-language
// testdata/contracts workspace, both ways: each expected link is found
// (recall) and nothing else is (precision). The workspace has a shared
// protos repository (two services named PaymentService in different
// packages; grpc-gateway annotations), a Go server with migrations and an
// OpenAPI spec, and Python, Java, TypeScript and Rust services using them
// over gRPC, HTTP and a shared database. Its traps: a gRPC call in a
// comment, a server name in a docstring, prose that starts like SQL, a
// table the Java service owns itself, and a Rust raw string after a
// lifetime.
func TestContractsGroundTruth(t *testing.T) {
	var all []Endpoint
	for _, r := range []string{"protos", "payments-go", "checkout-py", "ledger-java", "web-ts", "rust-refunds"} {
		all = append(all, mustScan(t, r, "testdata/contracts/"+r)...)
	}
	var got []string
	for _, l := range LinkAll(all, LinkOptions{}) {
		got = append(got, fmt.Sprintf("%s|%s|%s:%d|%s:%d", l.Kind, l.Contract, l.From.Repo+"/"+l.From.File, l.From.Line, l.To.Repo+"/"+l.To.File, l.To.Line))
	}
	want := []string{
		// gRPC: client -> server, resolved through the .proto definitions.
		"grpc|grpc payments.v1.PaymentService/CreatePayment|checkout-py/checkout/client.py:16|payments-go/main.go:38",
		"grpc|grpc payments.v1.PaymentService/CreatePayment|web-ts/src/api.ts:7|payments-go/main.go:38",
		"grpc|grpc payments.v1.PaymentService/GetPayment|ledger-java/src/main/java/com/acme/ledger/LedgerService.java:25|payments-go/main.go:38",
		"grpc|grpc payments.v1.RefundService/IssueRefund|payments-go/main.go:30|rust-refunds/src/main.rs:11",
		// gRPC: implementation and use -> definition.
		"grpc_def|grpc payments.v1.PaymentService|payments-go/main.go:38|protos/proto/payments/v1/payment.proto:12",
		"grpc_def|grpc payments.v1.PaymentService/CreatePayment|checkout-py/checkout/client.py:16|protos/proto/payments/v1/payment.proto:13",
		"grpc_def|grpc payments.v1.PaymentService/CreatePayment|web-ts/src/api.ts:7|protos/proto/payments/v1/payment.proto:13",
		"grpc_def|grpc payments.v1.PaymentService/GetPayment|ledger-java/src/main/java/com/acme/ledger/LedgerService.java:25|protos/proto/payments/v1/payment.proto:20",
		"grpc_def|grpc payments.v1.RefundService|rust-refunds/src/main.rs:11|protos/proto/payments/v1/payment.proto:26",
		"grpc_def|grpc payments.v1.RefundService/IssueRefund|payments-go/main.go:30|protos/proto/payments/v1/payment.proto:27",
		// HTTP, including a call served by a grpc-gateway annotation.
		"http|GET /api/v1/refunds/{}|web-ts/src/api.ts:11|payments-go/main.go:39",
		"http|POST /v1/payments|web-ts/src/api.ts:15|protos/proto/payments/v1/payment.proto:15",
		// OpenAPI: call -> spec, route -> spec.
		"openapi|GET /api/v1/refunds/{}|web-ts/src/api.ts:11|payments-go/api/openapi.yaml:9",
		"openapi_impl|GET /api/v1/refunds/{}|payments-go/main.go:39|payments-go/api/openapi.yaml:9",
		// Protobuf: generated-code imports (Go go_package, Java package,
		// Python _pb2 and TS _connect modules) -> package.
		"proto|proto payments.v1|checkout-py/checkout/client.py:6|protos/proto/payments/v1/payment.proto:4",
		"proto|proto payments.v1|ledger-java/src/main/java/com/acme/ledger/LedgerService.java:2|protos/proto/payments/v1/payment.proto:4",
		"proto|proto payments.v1|payments-go/main.go:9|protos/proto/payments/v1/payment.proto:4",
		"proto|proto payments.v1|web-ts/src/api.ts:2|protos/proto/payments/v1/payment.proto:4",
		// SQL: queries in three languages -> both migrations of the table.
		"sql|table payments|checkout-py/checkout/client.py:21|payments-go/migrations/001_payments.sql:2",
		"sql|table payments|checkout-py/checkout/client.py:21|payments-go/migrations/002_payments_status.sql:1",
		"sql|table payments|ledger-java/src/main/java/com/acme/ledger/LedgerService.java:28|payments-go/migrations/001_payments.sql:2",
		"sql|table payments|ledger-java/src/main/java/com/acme/ledger/LedgerService.java:28|payments-go/migrations/002_payments_status.sql:1",
		"sql|table payments|rust-refunds/src/main.rs:7|payments-go/migrations/001_payments.sql:2",
		"sql|table payments|rust-refunds/src/main.rs:7|payments-go/migrations/002_payments_status.sql:1",
	}
	sort.Strings(got)
	sort.Strings(want)
	for _, w := range want {
		if !slices.Contains(got, w) {
			t.Errorf("missing link %s", w)
		}
	}
	for _, g := range got {
		if !slices.Contains(want, g) {
			t.Errorf("unexpected link %s", g)
		}
	}
	for _, e := range all {
		if e.Kind == GRPCServe && e.Service == "Fake" || strings.Contains(e.Table, "cart") || e.RPC == "DeletePayment" {
			t.Errorf("false positive from a comment, docstring or prose: %+v", e)
		}
	}
}
