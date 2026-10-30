#include <stdio.h>

#include "checksum.h"

static int failures;

static void expect(int got, int want, const char *what) {
    if (got != want) {
        printf("FAIL %s: got %d, want %d\n", what, got, want);
        failures++;
    }
}

int main(void) {
    expect(luhn_valid("4539148803436467"), 1, "16-digit Visa");
    expect(luhn_valid("4539148803436468"), 0, "16-digit Visa, wrong last digit");
    expect(luhn_valid("12a4"), 0, "non-digit");
    expect(luhn_valid(""), 0, "empty");
    return failures ? 1 : 0;
}
