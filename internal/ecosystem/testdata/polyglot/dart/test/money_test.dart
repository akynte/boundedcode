import 'package:money/money.dart';
import 'package:test/test.dart';

void main() {
  test('adds', () {
    expect(centsOf(const Money(1) + const Money(2)), 3);
  });
}
