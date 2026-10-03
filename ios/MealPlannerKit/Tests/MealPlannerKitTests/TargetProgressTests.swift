import Testing
@testable import Features

@Suite
struct TargetProgressTests {
    @Test("The share of the target reached")
    func share() {
        #expect(targetProgress(value: 1200, target: 2000) == TargetProgress(fraction: 0.6, percent: 60, over: false))
        #expect(targetProgress(value: 0, target: 2000) == TargetProgress(fraction: 0, percent: 0, over: false))
        #expect(targetProgress(value: 2000, target: 2000) == TargetProgress(fraction: 1, percent: 100, over: false))
    }

    @Test("Over target fills the ring but keeps the real percent")
    func over() {
        #expect(targetProgress(value: 2500, target: 2000) == TargetProgress(fraction: 1, percent: 125, over: true))
    }

    @Test("No progress without a usable target or amount: never NaN or infinite")
    func noProgress() {
        #expect(targetProgress(value: 1200, target: nil) == nil)
        #expect(targetProgress(value: 1200, target: 0) == nil)
        #expect(targetProgress(value: 1200, target: -5) == nil)
        #expect(targetProgress(value: nil, target: 2000) == nil)
        #expect(targetProgress(value: nil, target: nil) == nil)
        #expect(targetProgress(value: .nan, target: 2000) == nil)
        #expect(targetProgress(value: 1200, target: .nan) == nil)
        #expect(targetProgress(value: 1200, target: .infinity) == nil)
        #expect(targetProgress(value: -1, target: 2000) == nil)
    }
}
