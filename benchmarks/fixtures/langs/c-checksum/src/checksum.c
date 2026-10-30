#include "checksum.h"

#include <string.h>

/* sum_digits adds the digits of s, doubling every second digit as the Luhn
 * algorithm does (a doubled digit above 9 has 9 taken off). double_first
 * says whether the first digit of s is one of the doubled ones. Returns -1
 * on a non-digit. */
static int sum_digits(const char *s, int double_first) {
    int sum = 0;
    size_t n = strlen(s);
    for (size_t i = 0; i < n; i++) {
        if (s[i] < '0' || s[i] > '9') {
            return -1;
        }
        int d = s[i] - '0';
        if ((i % 2 == 0) == double_first) {
            d *= 2;
            if (d > 9) {
                d -= 9;
            }
        }
        sum += d;
    }
    return sum;
}

int luhn_valid(const char *digits) {
    if (digits == NULL || digits[0] == '\0') {
        return 0;
    }
    int sum = sum_digits(digits, 1);
    return sum >= 0 && sum % 10 == 0;
}

int luhn_check_digit(const char *partial) {
    if (partial == NULL) {
        return -1;
    }
    int sum = sum_digits(partial, 1);
    if (sum < 0) {
        return -1;
    }
    return (10 - sum % 10) % 10;
}
