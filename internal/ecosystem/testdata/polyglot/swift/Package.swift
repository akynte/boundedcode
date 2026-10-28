// swift-tools-version:5.9
import PackageDescription

let package = Package(
    name: "Geo",
    targets: [
        .target(name: "Geo"),
        .testTarget(name: "GeoTests", dependencies: ["Geo"]),
    ]
)
