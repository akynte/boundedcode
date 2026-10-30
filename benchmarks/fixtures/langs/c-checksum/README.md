# checksum

Luhn (mod 10) validation for card and account numbers.

Build and test:

    cmake -S . -B build -G Ninja && cmake --build build && ctest --test-dir build
