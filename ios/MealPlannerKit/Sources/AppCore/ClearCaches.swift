import Repositories

/// Everything that must be emptied whenever the session ends, by any path (`AppState.clearCaches`), so a second user
/// on this device never sees the first user's meals, plan, templates or shopping lists, and never sends their queued changes.
func clearAllCaches(meals: MealsRepository, plan: PlanRepository, templates: TemplatesRepository, shopping: ShoppingListsRepository) async {
    await meals.clearCaches()
    await plan.clearCaches()
    await templates.clearCaches()
    await shopping.clearCaches()
}
