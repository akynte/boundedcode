#!/bin/bash
# The held-out pruning corpus: other projects, tools and failures than the
# development corpus (../generate.sh), written and frozen with its cases
# before internal/prune was run on it. Run inside the sandbox image:
#
#   docker run --rm --entrypoint bash -v "$PWD/benchmarks/prune:/prune" \
#     boundedcode-openhands:local /prune/heldout/generate.sh
set -u
OUT=/prune/heldout/corpus
mkdir -p $OUT
W=/work
rm -rf $W && mkdir -p $W
export TERM=dumb CI=1 GOFLAGS=-modcacherw CARGO_NET_OFFLINE=false
cap() { local id=$1; shift; echo "== $id"; ( "$@" ) > $OUT/$id.txt 2>&1; echo "exit status $?" >> $OUT/$id.txt; }

# ---------- C ----------
mkdir -p $W/cstats && cd $W/cstats
cat > stats.h <<'EOF'
#ifndef STATS_H
#define STATS_H
double mean(const double *xs, int n);
double median(double *xs, int n);
#endif
EOF
cat > stats.c <<'EOF'
#include "stats.h"
#include <stdlib.h>
double mean(const double *xs, int n) {
    double s = 0;
    for (int i = 0; i <= n; i++) s += xs[i];
    return s / n;
}
static int cmp(const void *a, const void *b) { return *(double *)a - *(double *)b; }
double median(double *xs, int n) {
    qsort(xs, n, sizeof(double), cmp);
    return n % 2 ? xs[n / 2] : (xs[n / 2 - 1] + xs[n / 2]) / 2
}
int unused_helper(int x) { int y; return x + y; }
EOF
cat > main.c <<'EOF'
#include <stdio.h>
#include "stats.h"
int main(void) {
    double xs[] = {3, 1, 2};
    printf("%d\n", mean(xs, 3));
    printf("%f\n", variance(xs, 3));
    return 0;
}
EOF
cat > Makefile <<'EOF'
CFLAGS=-Wall -Wextra -O2
all: stats
stats: main.o stats.o
	$(CC) -o $@ $^
EOF
cap make-c-errors make -k

# ---------- Go ----------
mkdir -p $W/gorace && cd $W/gorace
go mod init example.com/counter >/dev/null 2>&1
cat > counter.go <<'EOF'
package counter

type Counter struct{ n int }

func (c *Counter) Inc() { c.n++ }
func (c *Counter) N() int { return c.n }
EOF
cat > counter_test.go <<'EOF'
package counter

import (
	"sync"
	"testing"
)

func TestConcurrentInc(t *testing.T) {
	var c Counter
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); c.Inc() }()
	}
	wg.Wait()
	if c.N() != 50 {
		t.Errorf("N() = %d, want 50", c.N())
	}
}
EOF
cap go-test-race go test -race -count=1 ./...

mkdir -p $W/gotestify && cd $W/gotestify
go mod init example.com/orders >/dev/null 2>&1
cat > orders.go <<'EOF'
package orders

type Order struct {
	ID     string
	Items  []string
	Total  int
	Status string
}

func Normalize(o Order) Order {
	o.Status = "open"
	return o
}
EOF
{
echo 'package orders'; echo; echo 'import ('; echo '	"testing"'; echo; echo '	"github.com/stretchr/testify/require"'; echo ')'; echo
for i in $(seq 1 30); do echo "func TestKeep$i(t *testing.T) { require.Equal(t, $i, $i) }"; done
cat <<'EOF'
func TestNormalize(t *testing.T) {
	got := Normalize(Order{ID: "o-1", Items: []string{"a", "b"}, Total: 300, Status: "NEW"})
	require.Equal(t, Order{ID: "o-1", Items: []string{"a", "b"}, Total: 300, Status: "new"}, got)
}
EOF
} > orders_test.go
GOPROXY=https://proxy.golang.org GOFLAGS=-mod=mod go get github.com/stretchr/testify@v1.9.0 >/dev/null 2>&1; GOPROXY=https://proxy.golang.org go mod tidy >/dev/null 2>&1
cap go-testify-diff go test -v -count=1 ./...

# ---------- Python ----------
python3 -m venv $W/venv >/dev/null 2>&1
cap pip-install-mypy-ruff $W/venv/bin/pip install --no-cache-dir mypy==1.13.0 ruff==0.7.4
mkdir -p $W/pybilling && cd $W/pybilling
cat > billing.py <<'EOF'
import os, sys, json
from typing import Optional


def invoice_total(lines: list[dict], discount: Optional[float]) -> int:
    total = 0
    for l in lines:
        total += l["qty"] * l["price"]
    return total * (1 - discount)


def tax(amount: int, rate: str) -> int:
    return amount * rate


def unused():
    x = 1
    return None
EOF
cap mypy-errors $W/venv/bin/mypy --strict billing.py
cap ruff-check $W/venv/bin/ruff check --select ALL --no-cache billing.py
cat > test_billing.py <<'EOF'
import unittest
from billing import invoice_total


class TestKeep(unittest.TestCase):
EOF
for i in $(seq 1 40); do echo "    def test_keep_$i(self):
        self.assertEqual($i, $i)" >> test_billing.py; done
cat >> test_billing.py <<'EOF'


class TestInvoice(unittest.TestCase):
    def test_discount(self):
        lines = [{"qty": 2, "price": 100}, {"qty": 1, "price": 50}]
        self.assertEqual(invoice_total(lines, 0.1), 225)

    def test_no_discount(self):
        self.assertEqual(invoice_total([{"qty": 1, "price": 10}], None), 10)
EOF
cap unittest-verbose $W/venv/bin/python -m unittest -v test_billing
cat > worker.py <<'EOF'
import logging, sys
logging.basicConfig(format="%(asctime)s %(levelname)s %(name)s: %(message)s", level=logging.INFO)
log = logging.getLogger("worker")
for i in range(600):
    log.info("processing job %d of 600", i + 1)
    if i % 50 == 49:
        log.error("job %d: upstream returned 503, retrying", i + 1)
raise SystemExit(sys.exit("fatal: queue 'billing' does not exist"))
EOF
cap python-log-flood $W/venv/bin/python worker.py

# ---------- Node ----------
mkdir -p $W/nodejest && cd $W/nodejest
npm init -y >/dev/null 2>&1
cap npm-install-jest npm install --no-audit --no-fund jest@29.7.0
cat > price.js <<'EOF'
function applyCoupon(price, coupon) {
  if (coupon.type === 'percent') return price - price * coupon.value;
  return price - coupon.value;
}
function format(cents) { return '$' + (cents / 100).toFixed(1); }
module.exports = { applyCoupon, format };
EOF
{
echo "const { applyCoupon, format } = require('./price');"
for i in $(seq 1 40); do echo "test('keeps $i', () => { expect($i).toBe($i); });"; done
echo "test('percent coupon', () => { expect(applyCoupon(1000, { type: 'percent', value: 10 })).toBe(900); });"
echo "test('format', () => { expect(format(1234)).toEqual('\$12.34'); });"
} > price.test.js
cap jest-failures npx jest --verbose --ci
cap npm-install-missing npm install --no-audit --no-fund left-padd-not-a-real-package-xyz@1.0.0
mkdir -p $W/tsproj/src && cd $W/tsproj
npm init -y >/dev/null 2>&1 && npm install --no-audit --no-fund typescript@5.6.3 >/dev/null 2>&1
cat > tsconfig.json <<'EOF'
{ "compilerOptions": { "strict": true, "noEmit": true, "target": "es2022", "module": "nodenext" }, "include": ["src"] }
EOF
cat > src/user.ts <<'EOF'
export interface User { id: number; email: string; roles: string[] }
export function isAdmin(u: User): boolean { return u.roles.includes(1); }
export function byEmail(us: User[], e: string): User { return us.find(u => u.email === e); }
EOF
for i in $(seq 1 15); do echo "export const h$i = (u: User) => u.name;" >> src/user.ts; done
cat > src/app.ts <<'EOF'
import { isAdmin } from "./user";
console.log(isAdmin({ id: "1", email: "a@b", roles: [] }));
EOF
cap tsc-project npx tsc -p .

# ---------- Rust ----------
cd $W && cargo new --lib rclip >/dev/null 2>&1 && cd rclip
cat > src/lib.rs <<'EOF'
pub fn total(xs: &Vec<i64>) -> i64 {
    let mut t = 0;
    for i in 0..xs.len() {
        t = t + xs[i];
    }
    return t;
}

pub fn first_even(xs: &[i64]) -> Option<i64> {
    xs.iter().filter(|x| *x % 2 == 0).next().map(|x| *x)
}
EOF
rustup component add clippy >/dev/null 2>&1
cap cargo-clippy cargo clippy --config net.offline=false -- -W clippy::pedantic
cat > src/lib.rs <<'EOF'
pub fn split(total: u32, parts: u32) -> Vec<u32> {
    let each = total / parts;
    vec![each; parts as usize]
}

#[cfg(test)]
mod tests {
    use super::*;
    #[test] fn keeps_total() { assert_eq!(split(10, 3).iter().sum::<u32>(), 10); }
    #[test] fn by_zero() { split(1, 0); }
    #[test] fn ok_case() { assert_eq!(split(9, 3), vec![3, 3, 3]); }
}
EOF
cap cargo-test-two-failures env RUST_BACKTRACE=1 cargo test --config net.offline=false

# ---------- Java ----------
mkdir -p $W/gtest/src/main/java/inv $W/gtest/src/test/java/inv && cd $W/gtest
echo "rootProject.name = 'inv'" > settings.gradle
cat > build.gradle <<'EOF'
plugins { id 'java' }
repositories { mavenCentral() }
dependencies { testImplementation 'org.junit.jupiter:junit-jupiter:5.10.2'; testRuntimeOnly 'org.junit.platform:junit-platform-launcher' }
test { useJUnitPlatform(); testLogging { events "failed"; exceptionFormat "full" } }
EOF
cat > src/main/java/inv/Stock.java <<'EOF'
package inv;
import java.util.*;
public class Stock {
    private final Map<String, Integer> q = new HashMap<>();
    public void add(String sku, int n) { q.merge(sku, n, Integer::sum); }
    public int take(String sku, int n) {
        int have = q.get(sku);
        if (have < n) throw new IllegalArgumentException("not enough " + sku);
        q.put(sku, have - n);
        return have - n;
    }
}
EOF
cat > src/test/java/inv/StockTest.java <<'EOF'
package inv;
import static org.junit.jupiter.api.Assertions.*;
import org.junit.jupiter.api.Test;
class StockTest {
    @Test void takeUnknownSkuIsRejected() {
        Stock s = new Stock();
        assertThrows(IllegalArgumentException.class, () -> s.take("nope", 1));
    }
    @Test void takeReducesStock() { Stock s = new Stock(); s.add("a", 5); assertEquals(3, s.take("a", 2)); }
}
EOF
cap gradle-test-failure gradle --no-daemon test
mkdir -p $W/mcomp/src/main/java/inv && cd $W/mcomp
cat > pom.xml <<'EOF'
<project xmlns="http://maven.apache.org/POM/4.0.0">
  <modelVersion>4.0.0</modelVersion>
  <groupId>inv</groupId><artifactId>inv</artifactId><version>1.0</version>
  <properties><maven.compiler.release>17</maven.compiler.release></properties>
</project>
EOF
cat > src/main/java/inv/Report.java <<'EOF'
package inv;
import java.util.List;
public class Report {
    public String render(List<String> rows) {
        StringBuilder b = new StringBuilder()
        for (String r : rows) b.append(r).append('\n');
        return b.toString();
    }
    public int count(List<String> rows) { return rows.length; }
}
EOF
cap mvn-compile-error mvn -B -Dmaven.repo.local=$W/m2 compile

# ---------- Search and large output ----------
cap search-node-modules sh -c "cd $W/nodejest && grep -rn 'function ' node_modules/jest-* | head -2500"
cap rg-python-stdlib sh -c "grep -rn 'def __repr__' /usr/local/lib/python3*/ | head -1500"
cap npm-ls-tree sh -c "cd $W/nodejest && npm ls --all"
echo "corpus written to $OUT"
