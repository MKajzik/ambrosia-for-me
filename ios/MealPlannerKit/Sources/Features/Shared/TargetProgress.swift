/// How far `value` is towards `target`, ported from web's `targetProgress`.
public struct TargetProgress: Equatable, Sendable {
    /// How much of the ring to fill, 0 to 1 (a value over target fills it).
    public let fraction: Double
    /// The real ratio as a rounded percent, so 125 means 25% over.
    public let percent: Int
    public let over: Bool
}

/// `nil` when either side is missing, not finite, or the target is not a positive number: never NaN or infinity.
public func targetProgress(value: Double?, target: Double?) -> TargetProgress? {
    guard let value, let target, value.isFinite, target.isFinite, target > 0, value >= 0 else { return nil }
    let ratio = value / target
    return TargetProgress(fraction: min(ratio, 1), percent: Int((ratio * 100).rounded()), over: ratio > 1)
}
