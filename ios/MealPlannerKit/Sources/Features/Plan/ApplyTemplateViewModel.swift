import API
import Foundation
import Observation
import Repositories

/// The apply sheet: pick one of my templates and a start date. A `409 plan_conflict` becomes a question
/// (`confirmingReplace`), not an error; confirming repeats the call with `overwrite: true`. Partner templates cannot
/// be applied (`404`), so only mine are listed.
@Observable
@MainActor
public final class ApplyTemplateViewModel {
    public private(set) var templates: [Components.Schemas.DietTemplateSummary] = []
    public var selectedID: String?
    /// `YYYY-MM-DD`; the sheet's date picker writes it.
    public var startDate: String
    public private(set) var isLoading = false
    public private(set) var isApplying = false
    public private(set) var errorMessage: String?
    public var confirmingReplace = false
    public private(set) var didApply = false

    @ObservationIgnored private let repository: TemplatesRepository
    @ObservationIgnored private let plan: PlanViewModel

    public init(templates: TemplatesRepository, plan: PlanViewModel, startDate: String) {
        self.repository = templates
        self.plan = plan
        self.startDate = startDate
    }

    public var selected: Components.Schemas.DietTemplateSummary? { templates.first { $0.id == selectedID } }

    public func load() async {
        isLoading = true
        defer { isLoading = false }
        templates = await repository.cachedTemplates(.mine)
        select()
        do {
            try await repository.refreshTemplates(.mine)
            templates = await repository.cachedTemplates(.mine)
            select()
        } catch {
            if templates.isEmpty { errorMessage = ErrorText.message(for: error) }
        }
    }

    public func apply() async { await run(overwrite: false) }

    public func confirmReplace() async {
        confirmingReplace = false
        await run(overwrite: true)
    }

    private func select() {
        if selected == nil { selectedID = templates.first?.id }
    }

    private func run(overwrite: Bool) async {
        guard let template = selected else {
            errorMessage = "Choose a template."
            return
        }
        isApplying = true
        errorMessage = nil
        defer { isApplying = false }
        switch await plan.apply(templateID: template.id, dayCount: template.dayCount, startDate: startDate, overwrite: overwrite) {
        case .applied: didApply = true
        case .needsConfirmation: confirmingReplace = true
        case .failed(let message): errorMessage = message
        }
    }
}
