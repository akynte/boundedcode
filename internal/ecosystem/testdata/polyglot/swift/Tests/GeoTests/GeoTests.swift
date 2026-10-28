import XCTest
@testable import Geo

final class GeoTests: XCTestCase {
    func testDistance() {
        XCTAssertEqual(distance(Point(x: 0, y: 0), Point(x: 3, y: 4)), 5)
    }
}
