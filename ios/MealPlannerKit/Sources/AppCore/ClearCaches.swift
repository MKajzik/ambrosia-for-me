import Repositories

/// Everything that must be emptied whenever the session ends, by any path (`AppState.clearCaches`), so a second user
/// on this device never sees the first user's meals, plan, templates, shopping lists or profile, never sends their
/// queued changes, and never sees a sync notice meant for them.
func clearAllCaches(
    meals: MealsRepository, plan: PlanRepository, templates: TemplatesRepository, shopping: ShoppingListsRepository,
    profile: ProfileRepository, sync: ShoppingSyncEngine
) async {
    await meals.clearCaches()
    await plan.clearCaches()
    await templates.clearCaches()
    await shopping.clearCaches()
    await profile.clearCaches()
    await sync.reset()
}
