package xservice

import (
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"testing"

	"go.yaml.in/yaml/v3"
)

func sqlTables(s string) []string {
	var out []string
	for _, r := range analyzeSQL(s) {
		k := "access"
		if r.kind == SQLSchema {
			k = "schema"
		}
		out = append(out, k+" "+r.table)
	}
	sort.Strings(out)
	return slices.Compact(out)
}

func TestSQLStatements(t *testing.T) {
	for _, c := range []struct {
		sql  string
		want []string
	}{
		{"SELECT a FROM payments p JOIN accounts a ON a.id = p.account_id", []string{"access accounts", "access payments"}},
		{"select * from billing.invoices, public.\"Lines\" l where 1=1", []string{"access billing.invoices", "access public.lines"}},
		{"WITH recent AS (SELECT * FROM payments WHERE ts > now()) SELECT * FROM recent", []string{"access payments"}},
		{"SELECT EXTRACT(YEAR FROM created_at), x IS DISTINCT FROM y FROM orders", []string{"access orders"}},
		{"SELECT * FROM (SELECT id FROM inner_t) sub", []string{"access inner_t"}},
		{"SELECT * FROM generate_series(1, 10) g, unnest($1::int[]) u", nil},
		{"INSERT INTO ledger (a) SELECT a FROM staging ON CONFLICT (a) DO UPDATE SET a = excluded.a", []string{"access ledger", "access staging"}},
		{"INSERT INTO t (a) VALUES (1) ON DUPLICATE KEY UPDATE a = a + 1", []string{"access t"}},
		{"UPDATE payments p SET status = 'x' FROM accounts a WHERE a.id = p.account_id", []string{"access accounts", "access payments"}},
		{"DELETE FROM sessions USING users WHERE sessions.user_id = users.id", []string{"access sessions", "access users"}},
		{"SELECT 1 FROM t FOR UPDATE", []string{"access t"}},
		{"CREATE TABLE IF NOT EXISTS `shop`.`orders` (id int); CREATE UNIQUE INDEX CONCURRENTLY ix ON ONLY orders (id)", []string{"schema orders", "schema shop.orders"}},
		{"CREATE TEMP TABLE scratch (id int)", nil},
		{"ALTER TABLE IF EXISTS ONLY orders ADD COLUMN x int; DROP TABLE a, b CASCADE; DROP VIEW v", []string{"schema a", "schema b", "schema orders", "schema v"}},
		{"CREATE OR REPLACE VIEW active AS SELECT * FROM users WHERE active", []string{"schema active"}},
		{"CREATE FUNCTION f() RETURNS void AS $$ BEGIN DELETE FROM audit; END $$ LANGUAGE plpgsql", nil},
		{"-- SELECT * FROM commented\n/* DELETE FROM also_commented */ SELECT 'FROM not_a_table' FROM real_table", []string{"access real_table"}},
		{"SELECT * FROM information_schema.tables; SELECT * FROM pg_class", nil},
		{"TRUNCATE TABLE ONLY events; COPY imports FROM STDIN", []string{"access events", "access imports"}},
		{"SELECT * FROM {table} WHERE id = :id", nil},
	} {
		if got := sqlTables(c.sql); !slices.Equal(got, c.want) {
			t.Errorf("%q:\n got %q\nwant %q", c.sql, got, c.want)
		}
	}
}

func TestLooksLikeSQL(t *testing.T) {
	for s, want := range map[string]bool{
		"SELECT id FROM payments":                   true,
		"\n  select id\n  from payments\n":          true,
		"-- name: GetPayment :one\nSELECT * FROM p": true,
		"INSERT INTO t (a) VALUES (?)":              true,
		"UPDATE t SET a = 1":                        true,
		"delete from cart failed":                   false, // prose (lower case, one line)
		"Select a plan from the list":               false,
		"Update your profile":                       false,
		"SELECT":                                    false,
		"CREATE TABLE x (id int)":                   true,
		"Created the table":                         false,
	} {
		if got := looksLikeSQL(s); got != want {
			t.Errorf("looksLikeSQL(%q) = %v, want %v", s, got, want)
		}
	}
}

func TestProtoParse(t *testing.T) {
	src := `
edition = "2023";
package acme.shop.v2;
option go_package = "example.com/gen/shop/v2;shopv2";
option csharp_namespace = "Acme.Shop.V2";
/* service Fake { rpc Nope(A) returns (B); } */
message Order {
  option (custom) = { service: "not a service" };
  message Line { string sku = 1; }
}
enum Status { STATUS_UNSPECIFIED = 0; }
service OrderService {
  option (svc_opt) = true;
  rpc Get(GetRequest) returns (Order) {
    option (google.api.http) = {
      get: "/v2/{name=shops/*/orders/*}"
      additional_bindings { custom: { kind: "HEAD" path: "/v2/orders/{id}" } }
    };
  }
  rpc Stream(stream Order) returns (stream Order);
  rpc Cancel(CancelRequest) returns (Order) { option (google.api.http) = { post: "/v2/orders/{id}:cancel" body: "*" }; }
}
`
	f := parseProto(src)
	if f.pkg != "acme.shop.v2" || f.goPackage != "example.com/gen/shop/v2;shopv2" || len(f.services) != 1 {
		t.Fatalf("parsed %+v", f)
	}
	s := f.services[0]
	var rpcs []string
	for _, r := range s.rpcs {
		rpcs = append(rpcs, r.name+"("+r.req+")"+r.resp)
	}
	if want := []string{"Get(GetRequest)Order", "Stream(stream Order)stream Order", "Cancel(CancelRequest)Order"}; !slices.Equal(rpcs, want) {
		t.Fatalf("rpcs %q", rpcs)
	}
	var routes []string
	for _, r := range s.rpcs {
		for _, h := range r.http {
			routes = append(routes, h.method+" "+NormalizePath(h.path))
		}
	}
	if want := []string{"GET /v2/shops/{}/orders/{}", "HEAD /v2/orders/{}", "POST /v2/orders/{}"}; !slices.Equal(routes, want) {
		t.Fatalf("routes %q", routes)
	}
	refs := protoRefs("proto/acme/shop/v2/orders.proto", f)
	for _, want := range []string{"file:orders", "go:example.com/gen/shop/v2", "java:acme.shop.v2", "cs:Acme.Shop.V2"} {
		if !slices.Contains(refs, want) {
			t.Errorf("refs %q lack %s", refs, want)
		}
	}
}

func TestOpenAPIVersions(t *testing.T) {
	docs := map[string]string{
		"swagger.yaml": "swagger: '2.0'\nbasePath: /api/v1\npaths:\n  /users/{id}:\n    get: {operationId: getUser}\n    parameters: []\n",
		"v3.yaml": "openapi: 3.1.0\nservers:\n  - url: https://x.example.com/svc\n  - url: /svc\n  - url: '{scheme}://h/{base}'\n    variables: {scheme: {default: https}, base: {default: alt}}\n" +
			"paths:\n  /items:\n    post: {}\n  /legacy:\n    servers: [{url: /old}]\n    delete: {}\n  /shared:\n    $ref: './shared.yaml'\n",
		"helm-values.yaml": "paths:\n  /x:\n    get: {}\n", // no openapi/swagger key: not a spec
	}
	var got []string
	for name, src := range docs {
		var doc yaml.Node
		if err := yaml.Unmarshal([]byte(src), &doc); err != nil {
			t.Fatal(err)
		}
		eps, diags := openAPIOperations("r", name, &doc)
		for _, e := range eps {
			got = append(got, name+" "+e.Key()+" "+e.Symbol)
		}
		if name == "v3.yaml" && len(diags) != 1 {
			t.Errorf("$ref path item: diags %v", diags)
		}
	}
	sort.Strings(got)
	want := []string{
		"swagger.yaml GET /api/v1/users/{} getUser",
		"v3.yaml DELETE /old/legacy ",
		"v3.yaml POST /alt/items ",
		"v3.yaml POST /svc/items ",
	}
	if !slices.Equal(got, want) {
		t.Fatalf("got %q\nwant %q", got, want)
	}
	// JSON specs are found by their head; other JSON is not parsed.
	dir := t.TempDir()
	_ = os.WriteFile(filepath.Join(dir, "spec.json"), []byte(`{"openapi":"3.0.0","paths":{"/ping":{"get":{}}}}`), 0o644)
	_ = os.WriteFile(filepath.Join(dir, "package.json"), []byte(`{"name":"x","paths":{"/ping":{"get":{}}}}`), 0o644)
	eps, _ := analyzeOpenAPIJSON("r", dir, []string{"spec.json", "package.json"})
	if len(eps) != 1 || eps[0].Key() != "GET /ping" {
		t.Fatalf("json: %+v", eps)
	}
}

func TestSourceLexer(t *testing.T) {
	for _, c := range []struct {
		lang srcLang
		src  string
		want []string // string literal values
	}{
		{langPython, "x = ('SELECT a '\n     \"FROM t\")  # 'comment'\ny = r'\\d+' f\"{v}\"\n'''doc\nstring'''", []string{"SELECT a FROM t", `\d+`, "{v}", "doc\nstring"}},
		{langJVM, "String s = \"a\" +\n \"b\"; char c = '\"'; // \"x\"\nvar q = \"\"\"\n  text \"block\"\n  \"\"\";", []string{"ab", "\n  text \"block\"\n  "}},
		{langRust, "fn f<'a>(x: &'a str) { let q = r#\"say \"hi\"\"#; let c = '\\''; }", []string{`say "hi"`}},
		{langCSharp, "var s = @\"C:\\dir \"\"quoted\"\"\"; /* \"no\" */", []string{`C:\dir "quoted"`}},
		{langRuby, "=begin\n\"no\"\n=end\nsql = <<~SQL\n  SELECT 1\n  FROM t\nSQL\nx = 'y' # 'z'", []string{"  SELECT 1\n  FROM t\n", "y"}},
		{langPHP, "#[Attr] $a = 'p' . \"q\"; # \"c\"\n$b = <<<SQL\nDELETE FROM t\nSQL;", []string{"pq", "DELETE FROM t\n"}},
	} {
		f := lexSource(c.lang, c.src)
		var got []string
		for _, s := range f.strs {
			got = append(got, s.val)
		}
		if !slices.Equal(got, c.want) {
			t.Errorf("%s %q:\n got %q\nwant %q", c.lang, c.src, got, c.want)
		}
		if len(f.code) != len(c.src) || len(f.bare) != len(c.src) || strings.Count(f.code, "\n") != strings.Count(c.src, "\n") {
			t.Errorf("%s: views must keep offsets and lines", c.lang)
		}
	}
}

func epKeys(eps []Endpoint) []string {
	var out []string
	for _, e := range eps {
		out = append(out, string(e.Kind)+" "+e.Key())
	}
	sort.Strings(out)
	return slices.Compact(out)
}

func TestSourceIdioms(t *testing.T) {
	for _, c := range []struct {
		name, src string
		want      []string
	}{
		{"server.py", "import grpc\nfrom gen import orders_pb2_grpc\nclass Orders(orders_pb2_grpc.OrderServiceServicer):\n    def Get(self, request, context):\n        pass\n    def _helper(self, a, b):\n        pass\n" +
			"orders_pb2_grpc.add_OrderServiceServicer_to_server(Orders(), server)\n",
			[]string{"grpc_serve grpc OrderService", "grpc_serve grpc OrderService/Get", "proto_use proto file:orders"}},
		{"models.py", "class Payment(Base):\n    __tablename__ = 'payments'\n\ndef upgrade():\n    op.add_column('invoices', sa.Column('x'))\n    op.create_index('ix_a', 'accounts', ['id'])\n",
			[]string{"sql_access table payments", "sql_schema table accounts", "sql_schema table invoices"}},
		{"Server.java", "import io.grpc.stub.StreamObserver;\nclass S extends OrderServiceGrpc.OrderServiceImplBase {\n  @Override public void get(GetRequest req, StreamObserver<Order> obs) {}\n}\n",
			[]string{"grpc_serve grpc OrderService", "grpc_serve grpc OrderService/get", "proto_use proto java:io.grpc.stub"}},
		{"Client.kt", "val stub = OrderServiceGrpcKt.OrderServiceCoroutineStub(channel)\nsuspend fun f() = stub.get(req)\n",
			[]string{"grpc_call grpc OrderService", "grpc_call grpc OrderService/get"}},
		{"Program.cs", "using Acme.Shop.V2;\nclass Svc : OrderService.OrderServiceBase {}\nvar client = new OrderService.OrderServiceClient(channel);\nawait client.GetAsync(req);\n[Table(\"orders\")] class O {}\n",
			[]string{"grpc_call grpc OrderService", "grpc_call grpc OrderService/Get", "grpc_serve grpc OrderService", "proto_use proto cs:Acme.Shop.V2", "sql_access table orders"}},
		{"main.rs", "use tonic::Request;\nlet mut client = OrderServiceClient::connect(\"http://x\").await?;\nclient.get_order(Request::new(r)).await?;\ntable! { shop.orders (id) { id -> Int4, } }\n",
			[]string{"grpc_call grpc OrderService", "grpc_call grpc OrderService/get_order", "sql_access table orders"}},
		{"server.rb", "class Svc < Shop::OrderService::Service\nend\n@stub = Shop::OrderService::Stub.new(addr, creds)\n@stub.get_order(req)\nclass M < ApplicationRecord\n  self.table_name = :orders\nend\n",
			[]string{"grpc_call grpc OrderService", "grpc_call grpc OrderService/get_order", "grpc_serve grpc OrderService", "sql_access table orders"}},
		{"migrate.rb", "class AddX < ActiveRecord::Migration[7.1]\n  def change\n    add_column :orders, :x, :int\n  end\nend\n",
			[]string{"sql_schema table orders"}},
		{"client.php", "<?php\nuse Grpc\\ChannelCredentials;\n$client = new \\Shop\\OrderServiceClient('h', []);\n$client->GetOrder($req)->wait();\nSchema::create('orders', function ($t) {});\n",
			[]string{"grpc_call grpc OrderService", "grpc_call grpc OrderService/GetOrder", "sql_schema table orders"}},
	} {
		got := epKeys(analyzeSource("r", c.name, sourceLang(c.name), c.src))
		if !slices.Equal(got, c.want) {
			t.Errorf("%s:\n got %q\nwant %q", c.name, got, c.want)
		}
	}
}

func TestJSIdioms(t *testing.T) {
	src := `import * as grpc from "@grpc/grpc-js";
import { OrderServiceClient } from "./gen/orders_grpc_pb";
const proto = grpc.loadPackageDefinition(def);
server.addService(proto.acme.shop.v2.OrderService.service, impl);
server.addService(services.InvoiceServiceService, impl);
const client = new OrderServiceClient(addr, grpc.credentials.createInsecure());
client.getOrder(req, cb);
@Entity({ name: "orders" }) class Order {}
const rows = await knex("orders").where({ id });
await knex.schema.alterTable("orders", t => {});
const n = await prisma.orderLine.count();
const q = ` + "`SELECT * FROM invoices WHERE id = ${id}`" + `;
`
	eps := mustScanSource(t, "app.ts", src)
	got := epKeys(eps)
	want := []string{
		"grpc_call grpc OrderService", "grpc_call grpc OrderService/getOrder",
		"grpc_serve grpc InvoiceService", "grpc_serve grpc acme.shop.v2.OrderService",
		"proto_use proto file:orders",
		"sql_access table invoices", "sql_access table orderline", "sql_access table orders", "sql_schema table orders",
	}
	if !slices.Equal(got, want) {
		t.Fatalf("got %q\nwant %q", got, want)
	}
}

// mustScanSource scans one JS/TS file through Scan (both JS analyzers, and
// Prisma resolution).
func mustScanSource(t *testing.T, name, src string) []Endpoint {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	return mustScan(t, "r", dir)
}

func TestGoIdioms(t *testing.T) {
	dir := t.TempDir()
	files := map[string]string{
		"go.mod": "module example.com/svc\n",
		"db.go": `package svc

import (
	"fmt"

	sq "github.com/Masterminds/squirrel"
	ordersv1 "example.com/protos/gen/orders/v1"
	ordersconnect "example.com/protos/gen/orders/v1/ordersv1connect"
	gw "example.com/protos/gen/orders/v1/gw"
	"example.com/svc/internal/x"
)

type Order struct{}

func (Order) TableName() string { return "orders" }

type S struct{ orders ordersv1.OrderServiceClient }

func New(conn any) *S { return &S{orders: ordersv1.NewOrderServiceClient(conn)} }

func (s *S) Get(ctx any) {
	s.orders.GetOrder(ctx, nil)
	q := "SELECT id " +
		"FROM order_lines WHERE id = $1"
	_ = fmt.Sprintf("DELETE FROM %s WHERE id = $1", "archived_orders")
	_ = sq.Select("*").From("invoices")
	_ = q
	_, h := ordersconnect.NewOrderServiceHandler(nil)
	_ = gw.RegisterOrderServiceHandlerFromEndpoint(ctx, nil, "", nil)
	_ = x.Y
	_ = h
}
`,
		"gen.pb.go": "// Code generated by protoc-gen-go-grpc. DO NOT EDIT.\n\npackage svc\n\nfunc RegisterOrderServiceServer(s any, srv any) { s.RegisterService(nil, srv) }\n",
	}
	for n, c := range files {
		_ = os.WriteFile(filepath.Join(dir, n), []byte(c), 0o644)
	}
	got := epKeys(mustScan(t, "r", dir))
	want := []string{
		"grpc_call grpc OrderService", "grpc_call grpc OrderService/GetOrder", "grpc_serve grpc OrderService",
		"proto_use proto go:example.com/protos/gen/orders/v1", "proto_use proto go:example.com/protos/gen/orders/v1/gw",
		"proto_use proto go:example.com/protos/gen/orders/v1/ordersv1connect", "proto_use proto go:github.com/Masterminds/squirrel",
		"sql_access table archived_orders", "sql_access table invoices", "sql_access table order_lines", "sql_access table orders",
	}
	if !slices.Equal(got, want) {
		t.Fatalf("got %q\nwant %q", got, want)
	}
}

func TestSchemaSources(t *testing.T) {
	dir := t.TempDir()
	files := map[string]string{
		"db/changelog.xml": `<databaseChangeLog><changeSet id="1"><createTable tableName="orders" schemaName="shop"/>
<renameTable oldTableName="a" newTableName="b"/><sql>ALTER TABLE lines ADD x int</sql></changeSet></databaseChangeLog>`,
		"mapper/OrderMapper.xml": `<mapper namespace="x"><select id="get">SELECT * FROM orders <where><if test="id != null">id = #{id}</if></where></select>
<update id="u">UPDATE ${table} SET a = 1</update></mapper>`,
		"pom.xml":       "<project><dependencies/></project>",
		"schema.prisma": "model OrderLine {\n  id Int @id\n  @@map(\"order_lines\")\n}\nmodel Invoice {\n  id Int @id\n}\n",
	}
	for n, c := range files {
		_ = os.MkdirAll(filepath.Dir(filepath.Join(dir, n)), 0o755)
		_ = os.WriteFile(filepath.Join(dir, n), []byte(c), 0o644)
	}
	got := epKeys(mustScan(t, "r", dir))
	want := []string{"sql_access table orders", "sql_schema table a", "sql_schema table b", "sql_schema table invoice", "sql_schema table lines",
		"sql_schema table order_lines", "sql_schema table shop.orders"}
	if !slices.Equal(got, want) {
		t.Fatalf("got %q\nwant %q", got, want)
	}
}

// TestSQLOwnership: a repository that defines a table itself is not linked
// to another repository's table of the same name; an unqualified name
// matches a qualified one.
func TestSQLOwnership(t *testing.T) {
	eps := []Endpoint{
		{Kind: SQLSchema, Repo: "users-svc", File: "m.sql", Table: "users"},
		{Kind: SQLSchema, Repo: "auth-svc", File: "m.sql", Table: "users"},
		{Kind: SQLAccess, Repo: "auth-svc", File: "q.go", Table: "users"},
		{Kind: SQLAccess, Repo: "report-svc", File: "r.py", Table: "public.users"},
		{Kind: SQLAccess, Repo: "report-svc", File: "r.py", Table: "billing.users"},
	}
	var got []string
	for _, l := range LinkAll(eps, LinkOptions{}) {
		got = append(got, l.From.Repo+":"+l.From.Table+"->"+l.To.Repo)
	}
	sort.Strings(got)
	if want := []string{"report-svc:public.users->auth-svc", "report-svc:public.users->users-svc"}; !slices.Equal(got, want) {
		t.Fatalf("links %q, want %q", got, want)
	}
}
