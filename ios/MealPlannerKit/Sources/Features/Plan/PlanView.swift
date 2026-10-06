import API
import SwiftUI

/// Plan: a Monday-to-Sunday week with its total and per-day average, and each day's four slots editable in place
/// (the same rows as Today). The toolbar pushes the diet templates and opens "Apply template".
public struct PlanView: View {
    @State private var viewModel: PlanViewModel
    @State private var weekStart: String
    @State private var applying = false
    private let dependencies: PlanDependencies
    @Environment(\.scenePhase) private var scenePhase

    public init(dependencies: PlanDependencies) {
        self.dependencies = dependencies
        let localDay = LocalDay()
        let monday = localDay.startOfWeek(localDay.today())
        _weekStart = State(initialValue: monday)
        _viewModel = State(initialValue: PlanViewModel(
            plan: dependencies.plan, localDay: localDay,
            range: .init(from: monday, to: localDay.addDays(monday, 6))
        ))
    }

    private var weekDays: [String] { viewModel.localDay.weekDays(startingAt: weekStart) }
    private var onCurrentWeek: Bool { weekStart == viewModel.localDay.startOfWeek(viewModel.localDay.today()) }

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
                weekBar
                WeekSummaryView(totals: WeekTotals.make(days: viewModel.days, dates: weekDays))
                    .opacity(viewModel.isWriting ? 0.5 : 1)
            }
            ForEach(weekDays, id: \.self) { date in
                Section {
                    DaySlotsView(date: date, viewModel: viewModel, meals: dependencies.meals)
                } header: {
                    dayHeader(date)
                }
            }
        }
        .navigationTitle("Plan")
        .toolbar {
            ToolbarItemGroup(placement: .primaryAction) {
                NavigationLink {
                    TemplatesView(dependencies: dependencies)
                } label: {
                    Image(systemName: "list.bullet.rectangle")
                }
                .accessibilityLabel("Diet templates")
                .accessibilityIdentifier("dietTemplatesLink")
                Button { applying = true } label: {
                    Image(systemName: "calendar.badge.plus")
                }
                .accessibilityLabel("Apply template")
                .accessibilityIdentifier("applyTemplateButton")
            }
        }
        .refreshable { await viewModel.load() }
        .task { await moveTo(weekStart) }
        .onChange(of: scenePhase) { _, phase in
            if phase == .active { Task { await viewModel.load() } }
        }
        .sheet(isPresented: $applying) {
            ApplyTemplateView(dependencies: dependencies, plan: viewModel, startDate: weekStart)
        }
    }

    private var weekBar: some View {
        HStack {
            Button { Task { await moveTo(viewModel.localDay.addDays(weekStart, -7)) } } label: {
                Image(systemName: "chevron.left")
            }
            .accessibilityLabel("Previous week")
            Spacer()
            Text(viewModel.localDay.weekRange(startingAt: weekStart))
                .font(.headline)
                .accessibilityIdentifier("planWeekLabel")
            Spacer()
            Button { Task { await moveTo(viewModel.localDay.addDays(weekStart, 7)) } } label: {
                Image(systemName: "chevron.right")
            }
            .accessibilityLabel("Next week")
            Button("This week") {
                Task { await moveTo(viewModel.localDay.startOfWeek(viewModel.localDay.today())) }
            }
            .disabled(onCurrentWeek)
        }
        .buttonStyle(.borderless)
    }

    private func dayHeader(_ date: String) -> some View {
        VStack(alignment: .leading, spacing: 2) {
            Text(viewModel.localDay.heading(date) + (date == viewModel.localDay.today() ? " · Today" : ""))
                .font(.subheadline.weight(.semibold))
            Text(viewModel.nutrition(on: date).map(macroLine) ?? "—")
                .font(.caption)
                .monospacedDigit()
                .opacity(viewModel.isBusy(date) ? 0.5 : 1)
        }
        .textCase(nil)
    }

    private func moveTo(_ monday: String) async {
        weekStart = monday
        await viewModel.setRange(from: monday, to: viewModel.localDay.addDays(monday, 6))
    }
}
