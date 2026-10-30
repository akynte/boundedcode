#ifndef CHECKSUM_H
#define CHECKSUM_H

/* luhn_valid reports whether a string of decimal digits passes the Luhn
 * (mod 10) check: 1 if it does, 0 if it does not or holds a non-digit. */
int luhn_valid(const char *digits);

/* luhn_check_digit returns the digit (0-9) that, appended to partial, makes
 * it pass the Luhn check, or -1 if partial holds a non-digit. */
int luhn_check_digit(const char *partial);

#endif
