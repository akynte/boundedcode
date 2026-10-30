#include <cstdio>

#include "ringbuffer.hpp"

static int failures;

static void expect(bool ok, const char *what) {
    if (!ok) {
        std::printf("FAIL %s\n", what);
        failures++;
    }
}

int main() {
    RingBuffer<int, 4> rb;
    expect(rb.empty(), "new buffer is empty");
    expect(rb.push(1) && rb.push(2), "push");
    expect(rb.size() == 2, "size after two pushes");
    expect(rb.pop() == 1, "pop returns the oldest");
    expect(rb.size() == 1, "size after a pop");
    return failures ? 1 : 0;
}
