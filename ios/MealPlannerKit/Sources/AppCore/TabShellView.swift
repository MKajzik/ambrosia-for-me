import SwiftUI
import Features

struct TabShellView: View {
    let appState: AppState
    let mealsDependencies: MealsDependencies

    var body: some View {
        TabView {
            NavigationStack { TodayView() }
                .tabItem { Label("Today", systemImage: "sun.max").accessibilityIdentifier("todayTab") }
            NavigationStack { PlanView() }
                .tabItem { Label("Plan", systemImage: "calendar").accessibilityIdentifier("planTab") }
            NavigationStack { MealsView(dependencies: mealsDependencies) }
                .tabItem { Label("Meals", systemImage: "fork.knife").accessibilityIdentifier("mealsTab") }
            NavigationStack { ShoppingView() }
                .tabItem { Label("Shopping", systemImage: "cart").accessibilityIdentifier("shoppingTab") }
            NavigationStack {
                ProfileView(onSignOut: { Task { await appState.signOut() } })
            }
            .tabItem { Label("Profile", systemImage: "person").accessibilityIdentifier("profileTab") }
        }
    }
}
