// swift-tools-version:6.2
import PackageDescription

let package = Package(
    name: "MealPlannerKit",
    // iOS 26+ per the parent spec's locked decisions. macOS is also declared, only so
    // `swift test` can run natively on the build machine without a simulator (spec §4).
    platforms: [.iOS(.v26), .macOS(.v15)],
    products: [
        .library(name: "MealPlannerKit", targets: ["AppCore"])
    ],
    dependencies: [
        .package(url: "https://github.com/apple/swift-openapi-generator", from: "1.6.0"),
        .package(url: "https://github.com/apple/swift-openapi-runtime", from: "1.7.0"),
        .package(url: "https://github.com/apple/swift-openapi-urlsession", from: "1.0.0"),
        .package(url: "https://github.com/apple/swift-http-types", from: "1.0.0"),
    ],
    targets: [
        .target(
            name: "API",
            dependencies: [
                .product(name: "OpenAPIRuntime", package: "swift-openapi-runtime"),
                .product(name: "OpenAPIURLSession", package: "swift-openapi-urlsession"),
            ]
        ),
        .target(
            name: "Auth",
            dependencies: [
                "API",
                .product(name: "OpenAPIRuntime", package: "swift-openapi-runtime"),
                .product(name: "HTTPTypes", package: "swift-http-types"),
            ]
        ),
        .target(name: "Features", dependencies: ["Auth"]),
        .target(name: "AppCore", dependencies: ["API", "Auth", "Features"]),
        .testTarget(
            name: "MealPlannerKitTests",
            dependencies: ["API", "Auth", "Features"]
        )
    ]
)
