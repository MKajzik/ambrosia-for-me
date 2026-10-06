import API
import Repositories
import SwiftUI

/// Today: the day's calories and macros as rings against the targets `GET /plan` returns, and the day's four slots
/// editable in place. The date follows the device's local day (on foreground and on a significant time change).
public struct TodayView: View {
    @State private var viewModel: PlanViewModel
    private let dependencies: PlanDependencies
    @Environment(\.scenePhase) private var scenePhase

    public init(dependencies: PlanDependencies) {
        self.dependencies = dependencies
        _viewModel = State(initialValue: PlanViewModel(plan: dependencies.plan))
    }

    private var date: String { viewModel.range.from }

    public var body: some View {
        List {
            if viewModel.isStale {
                Label("Offline: showing saved plan", systemImage: "wifi.slash")
                    .font(.footnote)
                    .foregroundStyle(.secondary)
            }
            if let notice = viewModel.notice {
                Text(notice).font(.footnote).foregroundStyle(.secondary)
            }
            if let error = viewModel.loadError {
                VStack(alignment: .leading, spacing: 8) {
                    Text(error)
                    Button("Retry") { Task { await viewModel.load() } }
                }
            }
            Section {
                Text(viewModel.localDay.longDate(date))
                    .font(.headline)
                    .accessibilityIdentifier("todayDateLabel")
                HStack(spacing: 12) {
                    ForEach(NutrientCatalog.macroKeys) { key in
                        RingView(
                            key: key,
                            amount: viewModel.nutrition(on: date).flatMap { key.amount(in: $0) },
                            target: viewModel.targets?.target(for: key),
                            isDimmed: viewModel.isBusy(date)
                        )
                        .frame(maxWidth: .infinity)
                    }
                }
                if NutrientCatalog.macroKeys.contains(where: { viewModel.targets?.target(for: $0) == nil }) {
                    Text("Set daily targets in Profile.").font(.footnote).foregroundStyle(.secondary)
                }
            }
            Section {
                DaySlotsView(date: date, viewModel: viewModel, meals: dependencies.meals)
            }
        }
        .navigationTitle("Today")
        .refreshable { await viewModel.followToday() }
        .task { await viewModel.followToday() }
        .onChange(of: scenePhase) { _, phase in
            if phase == .active { Task { await viewModel.followToday() } }
        }
        .onSignificantTimeChange { Task { await viewModel.followToday() } }
    }
}
