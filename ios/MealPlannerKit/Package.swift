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
    targets: [
        .target(name: "AppCore")
    ]
)
