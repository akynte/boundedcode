package xservice

import (
	"errors"
	"strings"
	"testing"
)

const payProto = `syntax = "proto3";
package shop.v1;

service Pay {
  rpc Charge(ChargeRequest) returns (ChargeResponse);
  rpc Refund(RefundRequest) returns (RefundResponse);
  rpc Watch(WatchRequest) returns (stream Event);
}

// A comment.
message ChargeRequest {
  string order_id = 1;
  Money amount = 2;
  repeated string tags = 3;
  map<string, string> labels = 4;
  oneof method {
    string card = 5;
    string iban = 6;
  }
  reserved 9;
  message Line { string sku = 1; int32 qty = 2; }
  repeated Line lines = 7 [deprecated = true];
}
message Money { int64 units = 1; string currency = 2; }
message ChargeResponse { string id = 1; Status status = 2; }
enum Status { STATUS_UNSPECIFIED = 0; CAPTURED = 1; DECLINED = 2; }
message RefundRequest { string id = 1; }
message RefundResponse { bool ok = 1; }
message WatchRequest {}
message Event { string id = 1; }
`

func TestParseProtoSchema(t *testing.T) {
	s := ParseProtoSchema(payProto)
	if s.Package != "shop.v1" || len(s.Services["shop.v1.Pay"].RPCs) != 3 || s.Services["shop.v1.Pay"].RPCs["Watch"].Resp != "stream Event" {
		t.Fatalf("services %+v", s.Services)
	}
	req := s.Messages["shop.v1.ChargeRequest"]
	want := map[int]ProtoField{1: {"order_id", "string", "", 1}, 2: {"amount", "Money", "", 2}, 3: {"tags", "string", "repeated", 3},
		4: {"labels", "map<string,string>", "", 4}, 5: {"card", "string", "", 5}, 6: {"iban", "string", "", 6}, 7: {"lines", "Line", "repeated", 7}}
	if len(req.Fields) != len(want) {
		t.Fatalf("fields %+v", req.Fields)
	}
	for n, f := range want {
		if req.Fields[n] != f {
			t.Errorf("field %d = %+v, want %+v", n, req.Fields[n], f)
		}
	}
	if _, ok := s.Messages["shop.v1.ChargeRequest.Line"]; !ok || len(s.Enums["shop.v1.Status"]) != 3 {
		t.Fatalf("nested %v enums %v", s.Messages, s.Enums)
	}
}

func TestCompareRPC(t *testing.T) {
	base := ParseProtoSchema(payProto)
	for _, c := range []struct {
		name, old, new, rpc string
		changed, removed    bool
		breaking            string // substring of a breaking change; "" = none
		note                string
	}{
		{"comment only", "// A comment.", "// Another comment.", "Charge", false, false, "", ""},
		{"another rpc's message", "message RefundRequest { string id = 1; }", "message RefundRequest { string id = 1; string why = 2; }", "Charge", false, false, "", ""},
		{"field added", "string currency = 2; }", "string currency = 2; int32 nanos = 3; }", "Charge", true, false, "", "field 3 (int32 nanos) added to shop.v1.Money"},
		{"nested field retyped", "int64 units = 1;", "string units = 1;", "Charge", true, false, "field 1 (units) of shop.v1.Money changed type int64 -> string", ""},
		{"field renamed", "string order_id = 1;", "string order_ref = 1;", "Charge", true, false, "renamed order_id -> order_ref", ""},
		{"field removed", "  repeated string tags = 3;\n", "", "Charge", true, false, "field 3 (tags) of shop.v1.ChargeRequest removed", ""},
		{"label changed", "repeated string tags = 3;", "string tags = 3;", "Charge", true, false, "changed label", ""},
		{"enum value removed", "DECLINED = 2; }", "}", "Charge", true, false, "enum shop.v1.Status value 2 (DECLINED) removed", ""},
		{"nested message field", "int32 qty = 2;", "int64 qty = 2;", "Charge", true, false, "ChargeRequest.Line changed type int32 -> int64", ""},
		{"streaming changed", "returns (stream Event)", "returns (Event)", "Watch", true, false, "signature changed", ""},
		{"rpc removed", "  rpc Refund(RefundRequest) returns (RefundResponse);\n", "", "Refund", true, true, "rpc Refund removed", ""},
		{"rpc name normalized", "message RefundResponse { bool ok = 1; }", "message RefundResponse { bool ok = 1; int32 code = 2; }", "refund", true, false, "", "code"},
	} {
		head := ParseProtoSchema(strings.Replace(payProto, c.old, c.new, 1))
		got := CompareRPC(base, head, "shop.v1.Pay", c.rpc)
		joined := strings.Join(got.Breaking, "; ")
		if got.Changed != c.changed || got.Removed != c.removed || (c.breaking == "") != (len(got.Breaking) == 0) || !strings.Contains(joined, c.breaking) ||
			!strings.Contains(strings.Join(got.Notes, "; "), c.note) {
			t.Errorf("%s: %+v", c.name, got)
		}
	}
	// The whole file: any service, message or enum.
	head := ParseProtoSchema(strings.Replace(payProto, "message RefundRequest { string id = 1; }", "message RefundRequest { int64 id = 1; }", 1))
	if f := CompareFile(base, head); !f.Changed || len(f.Breaking) != 1 {
		t.Fatalf("file %+v", f)
	}
	if f := CompareFile(base, ParseProtoSchema(strings.ReplaceAll(payProto, "\n", "\n\n"))); f.Changed {
		t.Fatalf("formatting %+v", f)
	}
}

const ordersSpec = `openapi: 3.0.3
servers: [{url: "https://api.example.com/v1"}]
paths:
  /orders/{id}:
    parameters: [{name: id, in: path, required: true, schema: {type: string}}]
    get:
      operationId: getOrder
      responses:
        "200": {description: ok, content: {application/json: {schema: {$ref: "#/components/schemas/Order"}}}}
    delete:
      responses: {"204": {description: gone}}
  /health:
    get: {responses: {"200": {description: ok}}}
components:
  schemas:
    Order: {type: object, properties: {id: {type: string}, total: {$ref: "#/components/schemas/Money"}}}
    Money: {type: integer}
    Unused: {type: string}
`

func TestOpenAPIOperationDigest(t *testing.T) {
	digest := func(src, method, path string) string {
		for _, o := range OpenAPIOperations([]byte(src)) {
			if o.Method == method && o.Path == path {
				return o.Digest
			}
		}
		return ""
	}
	get := digest(ordersSpec, "GET", "/v1/orders/{}")
	if get == "" || digest(ordersSpec, "GET", "/v1/health") == "" {
		t.Fatal("operations not found")
	}
	for _, c := range []struct {
		name, old, new string
		same           bool
	}{
		{"unrelated schema", "Unused: {type: string}", "Unused: {type: integer}", true},
		{"another operation", `"204": {description: gone}`, `"204": {description: deleted}`, true},
		{"formatting", "Money: {type: integer}", "Money:\n      type: integer", true},
		{"referenced schema, transitively", "Money: {type: integer}", "Money: {type: string}", false},
		{"path-level parameter", "required: true, schema: {type: string}", "required: true, schema: {type: integer}", false},
	} {
		if got := digest(strings.Replace(ordersSpec, c.old, c.new, 1), "GET", "/v1/orders/{}"); (got == get) != c.same {
			t.Errorf("%s: same=%v", c.name, got == get)
		}
	}
}

func TestOpenAPIKnockOut(t *testing.T) {
	out, err := OpenAPIKnockOut([]byte(ordersSpec), "openapi.yaml", "GET", "/v1/orders/{}")
	if err != nil {
		t.Fatal(err)
	}
	ops := OpenAPIOperations(out)
	if len(ops) != 2 || ops[0].Method != "DELETE" {
		t.Fatalf("after knocking out GET: %+v", ops)
	}
	out, err = OpenAPIKnockOut([]byte(ordersSpec), "openapi.yaml", "GET", "/v1/health")
	if err != nil || strings.Contains(string(out), "/health") {
		t.Fatalf("a path item left without operations is removed: %v\n%s", err, out)
	}
	js := `{"openapi": "3.0.0", "paths": {"/a": {"get": {}, "post": {}}}}`
	out, err = OpenAPIKnockOut([]byte(js), "spec.json", "POST", "/a")
	if err != nil || !strings.HasPrefix(string(out), "{") || len(OpenAPIOperations(out)) != 1 {
		t.Fatalf("json: %v %s", err, out)
	}
	if _, err := OpenAPIKnockOut([]byte(ordersSpec), "openapi.yaml", "PUT", "/v1/orders/{}"); !errors.Is(err, ErrNoOperation) {
		t.Fatalf("missing operation: %v", err)
	}
}
