#!/bin/bash
# Generates the pruning corpus: real output of real toolchains on small
# projects with planted failures, plus dependency logs from the public
# registries. Run inside the sandbox image (it has every toolchain):
#
#   docker run --rm --entrypoint bash -v "$PWD/benchmarks/prune:/prune" \
#     boundedcode-openhands:local /prune/generate.sh
#
# Each case writes /prune/corpus/<id>.txt: the command's stdout and stderr
# as the bash tool captures them. cases.yaml says what each must keep.
set -u
OUT=/prune/corpus
mkdir -p $OUT
W=/work
rm -rf $W && mkdir -p $W
export NO_COLOR= TERM=dumb CI=1 GOFLAGS=-modcacherw
cap() { local id=$1; shift; echo "== $id"; ( "$@" ) > $OUT/$id.txt 2>&1; echo "exit status $?" >> $OUT/$id.txt; }

# ---------- Go ----------
mkdir -p $W/gomoney && cd $W/gomoney
go mod init example.com/money >/dev/null 2>&1
cat > round.go <<'EOF'
package money

// Round rounds cents half to even.
func Round(c float64) int {
	n := int(c)
	if c-float64(n) >= 0.5 {
		n++
	}
	return n
}
EOF
{
echo 'package money'; echo; echo 'import "testing"'; echo
for i in $(seq 1 80); do
cat <<EOF
func TestCase$i(t *testing.T) {
	t.Logf("checking case %d with input %d", $i, $i*7)
	if got := Round(float64($i)); got != $i {
		t.Fatalf("Round(%d) = %d", $i, got)
	}
}
EOF
done
cat <<'EOF'
func TestRoundHalfEven(t *testing.T) {
	for _, c := range []struct {
		in   float64
		want int
	}{{100.5, 100}, {101.5, 102}} {
		t.Run("", func(t *testing.T) {
			t.Logf("rounding %v", c.in)
			if got := Round(c.in); got != c.want {
				t.Errorf("Round(%v) = %d, want %d", c.in, got, c.want)
			}
		})
	}
}
EOF
} > round_test.go
cap go-test-verbose go test -v -count=1 ./...

mkdir -p $W/gobuild && cd $W/gobuild
go mod init example.com/build >/dev/null 2>&1
{
echo 'package build'; echo
echo 'func Total(xs []int) int {'; echo '	var t int64'; echo '	for _, x := range xs {'; echo '		t += x'; echo '	}'; echo '	return t'; echo '}'
for i in $(seq 1 25); do echo "func F$i() { logf(\"step $i\") }"; done
echo 'func G() string { return 42 }'
} > build.go
cap go-build-errors go build -gcflags=-e ./...
cap go-vet sh -c 'cat > vet.go <<EOF
package build
import "fmt"
func H() { fmt.Printf("%d items\n", "three"); return; fmt.Println("never") }
EOF
sed -i "s/logf(/_ = (/; s/return 42/return \"42\"/; s/var t int64/var t int/" build.go; go vet ./...'

mkdir -p $W/gopanic && cd $W/gopanic
go mod init example.com/ledger >/dev/null 2>&1
cat > main.go <<'EOF'
package main

import (
	"fmt"
	"sync"
)

type Ledger struct{ entries []int }

func (l *Ledger) At(i int) int { return l.entries[i] }

func walk(l *Ledger, depth int) int {
	if depth == 0 {
		return l.At(len(l.entries))
	}
	return walk(l, depth-1) + 1
}

func main() {
	var wg sync.WaitGroup
	block := make(chan bool)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); <-block }()
	}
	l := &Ledger{entries: []int{1, 2, 3, 4, 5}}
	fmt.Println(walk(l, 30))
}
EOF
cap go-panic env GOTRACEBACK=all go run .

mkdir -p $W/gox && cd $W/gox
go mod init example.com/x >/dev/null 2>&1
printf 'package main\n\nimport "fmt"\n\nfunc main() {\n\tfmt.Println(undefinedName)\n}\n' > main.go
cap go-build-x go build -x -a .

mkdir -p $W/gomod && cd $W/gomod
cat > go.mod <<'EOF'
module example.com/cli

go 1.22

require (
	github.com/spf13/cobra v1.8.1
	github.com/spf13/viper v1.19.0
	example.com/does-not-exist v0.1.0
)
EOF
printf 'package main\n\nimport _ "github.com/spf13/cobra"\n\nfunc main() {}\n' > main.go
cap go-mod-download env GOMODCACHE=$W/modcache GOPROXY=https://proxy.golang.org go mod download -x

# ---------- Python ----------
python3 -m venv $W/venv >/dev/null 2>&1
cap pip-install $W/venv/bin/pip install --no-cache-dir -v pytest==8.3.3
cap pip-install-error $W/venv/bin/pip install --no-cache-dir requests==0.0.999
mkdir -p $W/pyinv && cd $W/pyinv
cat > inventory.py <<'EOF'
import json
import logging

log = logging.getLogger("inventory")


def reorder_point(daily, lead_days, safety):
    return daily * lead_days + safety - 1


def load(path):
    with open(path) as f:
        return json.load(f, object_hook=lambda d: {k: d[k] for k in ("sku", "qty")})
EOF
{
echo 'import pytest'; echo 'from inventory import reorder_point'; echo
for i in $(seq 1 120); do
cat <<EOF
def test_case_$i(capsys):
    print("stock level for sku-$i is", $i * 3)
    assert $i * 2 == $i + $i
EOF
done
cat <<'EOF'
def test_reorder_point():
    print("computing reorder point")
    assert reorder_point(2, 3, 2) == 8


@pytest.mark.parametrize("daily,lead,want", [(1, 1, 1), (5, 2, 10)])
def test_reorder_zero_safety(daily, lead, want):
    assert reorder_point(daily, lead, 0) == want
EOF
} > test_inventory.py
cap pytest-verbose $W/venv/bin/python -m pytest -v -rA
printf '[{"sku": "a", "qty": 1}, {"qty": 2}]' > stock.json
cat > run.py <<'EOF'
import logging, warnings
from inventory import load
logging.basicConfig(level=logging.WARNING)
for i in range(400):
    warnings.warn("config key 'reorder.safety' is deprecated; use 'safety_stock'", DeprecationWarning, stacklevel=1)
    logging.getLogger("inventory").warning("stock file not cached, reading from disk (attempt %d)", i % 3)
print(load("stock.json"))
EOF
cap python-traceback $W/venv/bin/python -W always run.py

# ---------- Node ----------
mkdir -p $W/nodecart && cd $W/nodecart
npm init -y >/dev/null 2>&1
cap npm-install npm install --loglevel verbose --no-audit --no-fund typescript@5.6.3 eslint@9.13.0 @eslint/js@9.13.0
cat > cart.ts <<'EOF'
export interface Item { sku: string; price: number; qty: number }
export function total(items: Item[]): number {
  return items.reduce((s, i) => s + i.price * i.qty, "0");
}
export function discount(t: number, code: string): number {
  const rates: Record<string, number> = { TEN: 0.1 };
  return t * (1 - rates[code].toFixed(2));
}
EOF
for i in $(seq 1 20); do echo "export function step$i(): void { logger.info('step $i'); }" >> cart.ts; done
cap tsc-errors npx tsc --noEmit --strict cart.ts
cat > eslint.config.js <<'EOF'
import js from "@eslint/js";
export default [js.configs.recommended, { rules: { "no-unused-vars": "warn", "no-console": "warn" }, languageOptions: { sourceType: "module", globals: { console: "readonly" } } }];
EOF
echo '{"type":"module"}' > package.json.tmp && node -e 'const p=require("./package.json");p.type="module";require("fs").writeFileSync("package.json",JSON.stringify(p,null,2))'
{
for i in $(seq 1 40); do echo "export function f$i(a, b) { const unused$i = a; console.log('f$i'); return b; }"; done
echo 'export function broken() { return undefinedThing + 1; }'
echo 'export function dup(a, a2) { if (a == a2) { return } else { return; } }'
echo 'const x = 1; const x2 = x; x = 2;'
} > lint.js
cap eslint-output npx eslint lint.js
cat > cart.js <<'EOF'
export function discount(total, code) {
  const rates = { TEN: 0.1 };
  return total * (1 - rates[code].rate);
}
EOF
{
echo "import { test } from 'node:test';"; echo "import assert from 'node:assert/strict';"; echo "import { discount } from './cart.js';"
for i in $(seq 1 50); do echo "test('case $i keeps the total', () => { assert.equal(${i}0 * 1, ${i}0); });"; done
echo "test('discount applies', () => { assert.equal(discount(100, 'TEN'), 90); });"
} > cart.test.js
cap node-test node --test cart.test.js
cat > app.js <<'EOF'
import { readFileSync } from 'node:fs';
function parse(text) { return JSON.parse(text).items.map(i => i.price.amount); }
function load() { return parse(readFileSync('cart.json', 'utf8')); }
setTimeout(() => { console.log(load()); }, 0);
EOF
echo '{"items":[{"price":{"amount":1}},{}]}' > cart.json
cap node-stack node app.js

# ---------- Rust ----------
mkdir -p $W && cd $W && cargo new --lib rledger >/dev/null 2>&1 && cd rledger
cat >> Cargo.toml <<'EOF'
serde = { version = "1", features = ["derive"] }
serde_json = "1"
regex = "1"
EOF
cat > src/lib.rs <<'EOF'
use serde::Deserialize;

#[derive(Deserialize)]
pub struct Account { pub id: String, pub balance: i64 }

pub fn transfer(from: &mut Account, to: &mut Account, amount: i64) -> i64 {
    from.balance -= amount;
    to.balance += amount;
    let label: String = from.balance;
    label.len() as i64
}
EOF
cap cargo-build-deps env CARGO_NET_OFFLINE=false cargo build --config net.offline=false
cat > src/lib.rs <<'EOF'
pub struct Account { pub id: String, pub balance: i64 }

pub fn transfer(from: &mut Account, to: &mut Account, amount: i64) {
    from.balance -= amount;
    to.balance += amount - amount / 10;
}

#[cfg(test)]
mod tests {
    use super::*;
    fn acct(b: i64) -> Account { Account { id: "a".into(), balance: b } }
    #[test] fn transfer_keeps_total() {
        let (mut a, mut b) = (acct(100), acct(0));
        transfer(&mut a, &mut b, 100);
        assert_eq!(b.balance, 100);
    }
    #[test] fn nothing() { assert_eq!(1, 1); }
}
EOF
cap cargo-test-backtrace env CARGO_NET_OFFLINE=false RUST_BACKTRACE=full cargo test --config net.offline=false

# ---------- Java ----------
mkdir -p $W/jorders/src/main/java/shop $W/jorders/src/test/java/shop && cd $W/jorders
cat > pom.xml <<'EOF'
<project xmlns="http://maven.apache.org/POM/4.0.0">
  <modelVersion>4.0.0</modelVersion>
  <groupId>shop</groupId><artifactId>orders</artifactId><version>1.0</version>
  <properties><maven.compiler.release>17</maven.compiler.release><project.build.sourceEncoding>UTF-8</project.build.sourceEncoding></properties>
  <dependencies><dependency><groupId>org.junit.jupiter</groupId><artifactId>junit-jupiter</artifactId><version>5.10.2</version><scope>test</scope></dependency></dependencies>
  <build><plugins><plugin><artifactId>maven-surefire-plugin</artifactId><version>3.2.5</version><configuration><trimStackTrace>false</trimStackTrace></configuration></plugin></plugins></build>
</project>
EOF
cat > src/main/java/shop/Orders.java <<'EOF'
package shop;
public class Orders {
    public static long paidTotal(long[] amounts, boolean[] paid) {
        long t = 0;
        for (int i = 0; i < amounts.length; i++) if (!paid[i]) t += amounts[i];
        return t;
    }
}
EOF
cat > src/test/java/shop/OrdersTest.java <<'EOF'
package shop;
import static org.junit.jupiter.api.Assertions.assertEquals;
import org.junit.jupiter.api.Test;
class OrdersTest {
    @Test void paidTotalCountsPaidOrders() {
        assertEquals(100, Orders.paidTotal(new long[]{100, 90}, new boolean[]{true, false}));
    }
    @Test void empty() { assertEquals(0, Orders.paidTotal(new long[]{}, new boolean[]{})); }
}
EOF
cap mvn-test-downloads mvn -B -U -Dmaven.repo.local=$W/m2 test
mkdir -p $W/jgradle/src/main/java/shop && cd $W/jgradle
echo "rootProject.name = 'orders'" > settings.gradle
echo "plugins { id 'java' }" > build.gradle
cat > src/main/java/shop/Orders.java <<'EOF'
package shop;
import java.util.List;
public class Orders {
    public long total(List<Order> orders) { return orders.stream().mapToLong(Order::amount).sum(); }
    public String label(long cents) { return Money.format(cents); }
    public int count(List<Order> orders) { return orders.size; }
}
EOF
cap gradle-compile-errors gradle --no-daemon -q compileJava
mkdir -p $W/jtrace && cd $W/jtrace
cat > Main.java <<'EOF'
import java.lang.reflect.Method;
import java.util.*;
import java.util.stream.*;
public class Main {
    static int settle(Map<String, Integer> balances, String id) {
        if (!balances.containsKey(id)) throw new IllegalStateException("no balance for account " + id);
        return balances.get(id);
    }
    public static void main(String[] args) throws Exception {
        Method m = Main.class.getDeclaredMethod("run");
        m.invoke(null);
    }
    static void run() {
        Map<String, Integer> b = new HashMap<>(Map.of("a", 1));
        System.out.println(Stream.of("a", "b").map(id -> settle(b, id)).collect(Collectors.toList()));
    }
}
EOF
cap java-exception sh -c 'javac Main.java && java Main'

# ---------- Search and large output ----------
GOSRC=$(go env GOROOT)/src
cap search-func sh -c "grep -rn 'func (.*) String() string' $GOSRC | head -3000"
cap search-todo sh -c "grep -rn 'TODO' $GOSRC/net $GOSRC/crypto | head -2000"
cap find-listing sh -c "find $GOSRC -name '*.go' | head -4000"
echo "corpus written to $OUT"
