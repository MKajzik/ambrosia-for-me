import SwiftUI
import Features

struct TabShellView: View {
    let appState: AppState
    let mealsDependencies: MealsDependencies
    let planDependencies: PlanDependencies
    let shoppingDependencies: ShoppingDependencies

    var body: some View {
        TabView {
            NavigationStack { TodayView(dependencies: planDependencies) }
                .tabItem { Label("Today", systemImage: "sun.max").accessibilityIdentifier("todayTab") }
            NavigationStack { PlanView(dependencies: planDependencies) }
                .tabItem { Label("Plan", systemImage: "calendar").accessibilityIdentifier("planTab") }
            NavigationStack { MealsView(dependencies: mealsDependencies) }
                .tabItem { Label("Meals", systemImage: "fork.knife").accessibilityIdentifier("mealsTab") }
            NavigationStack { ShoppingView(dependencies: shoppingDependencies) }
                .tabItem { Label("Shopping", systemImage: "cart").accessibilityIdentifier("shoppingTab") }
            NavigationStack {
                ProfileView(onSignOut: { Task { await appState.signOut() } })
            }
            .tabItem { Label("Profile", systemImage: "person").accessibilityIdentifier("profileTab") }
        }
    }
}
