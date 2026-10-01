import SwiftUI
import Features

struct TabShellView: View {
    let appState: AppState

    var body: some View {
        TabView {
            NavigationStack { TodayView() }
                .tabItem { Label("Today", systemImage: "sun.max") }
            NavigationStack { PlanView() }
                .tabItem { Label("Plan", systemImage: "calendar") }
            NavigationStack { MealsView() }
                .tabItem { Label("Meals", systemImage: "fork.knife") }
            NavigationStack { ShoppingView() }
                .tabItem { Label("Shopping", systemImage: "cart") }
            NavigationStack {
                ProfileView(onSignOut: { Task { await appState.signOut() } })
            }
            .tabItem { Label("Profile", systemImage: "person") }
        }
    }
}
