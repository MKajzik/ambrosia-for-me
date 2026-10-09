import SwiftUI

public struct ShoppingView: View {
    private let dependencies: ShoppingDependencies
    public init(dependencies: ShoppingDependencies) { self.dependencies = dependencies }
    public var body: some View {
        Text("Shopping").navigationTitle("Shopping")
    }
}
