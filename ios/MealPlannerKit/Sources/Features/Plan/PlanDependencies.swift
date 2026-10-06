import Repositories

/// What the Today and Plan tabs need, built once in `RootView` and handed down.
public struct PlanDependencies: Sendable {
    public let plan: PlanRepository
    public let templates: TemplatesRepository
    public let meals: MealsRepository
    public let partner: PartnerRepository

    public init(plan: PlanRepository, templates: TemplatesRepository, meals: MealsRepository, partner: PartnerRepository) {
        self.plan = plan
        self.templates = templates
        self.meals = meals
        self.partner = partner
    }
}
