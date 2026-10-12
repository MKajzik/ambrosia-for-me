/// The four daily targets as the repository sends them. `nil` means "clear this target" (the repository turns it into
/// the null sentinel). All four are always sent, so a save never leaves the server and the screen disagreeing.
public struct TargetsUpdate: Equatable, Sendable {
    public var kcal: Double?
    public var protein: Double?
    public var carbs: Double?
    public var fat: Double?

    public init(kcal: Double?, protein: Double?, carbs: Double?, fat: Double?) {
        self.kcal = kcal
        self.protein = protein
        self.carbs = carbs
        self.fat = fat
    }
}
