/// Which of the two meal lists a cached row belongs to. A list row carries no `is_owner`
/// (`GET /meals` returns `MealSummary`), so ownership of a row is its scope.
public enum MealScope: String, Sendable, Hashable {
    case mine
    case partner
}
