import Repositories

/// What the Profile tab needs, built once in `RootView` and handed down.
public struct ProfileDependencies: Sendable {
    public let profile: ProfileRepository
    public let plan: PlanRepository
    public let partner: PartnerRepository
    public let ingredients: IngredientsRepository

    public init(profile: ProfileRepository, plan: PlanRepository, partner: PartnerRepository, ingredients: IngredientsRepository) {
        self.profile = profile
        self.plan = plan
        self.partner = partner
        self.ingredients = ingredients
    }
}
