import Observation

/// The autosave engine shared by the meal and template editors (a port of web's `useAutosave`).
/// Saves once the value from `pending` has stopped changing for 700 ms; one save at a time; an edit made during
/// a save is saved after it; a value that was tried (saved or failed) is not tried again by itself, so a failing
/// server is not hammered: the next edit, `retry()` or leaving the editor tries again.
///
/// The client owns what "the server holds": `pending` returns the valid draft value when it differs from that
/// (or `nil`), and `perform` writes it and records the new baseline.
@Observable
@MainActor
public final class Autosaver<Value: Equatable & Sendable> {
    public private(set) var isSaving = false
    public private(set) var saveError: String?

    @ObservationIgnored private var settled: Value?
    @ObservationIgnored private var debounceTask: Task<Void, Never>?
    @ObservationIgnored private var isRunning = false
    @ObservationIgnored private let pending: @MainActor () -> Value?
    @ObservationIgnored private let perform: @MainActor (Value) async throws -> Void
    @ObservationIgnored private let sleep: @Sendable (Duration) async throws -> Void

    public init(
        sleep: @escaping @Sendable (Duration) async throws -> Void = { try await Task.sleep(for: $0) },
        pending: @escaping @MainActor () -> Value?,
        perform: @escaping @MainActor (Value) async throws -> Void
    ) {
        self.sleep = sleep
        self.pending = pending
        self.perform = perform
    }

    /// Call after every edit: restarts the pause, unless a save is running or the value was already tried.
    public func schedule() {
        debounceTask?.cancel()
        debounceTask = nil
        guard !isSaving, let value = pending(), value != settled else { return }
        let sleep = self.sleep
        debounceTask = Task { [weak self] in
            do { try await sleep(.milliseconds(700)) } catch { return }
            guard !Task.isCancelled else { return }
            await self?.run()
        }
    }

    /// "Try again": forget that the value was tried, and save now.
    public func retry() async {
        settled = nil
        await run()
    }

    /// Leaving the editor (sheet dismissed, or the scene leaving the foreground) saves a pending edit in an
    /// unstructured task, so dismissing the view does not cancel it. A flush that fails after the sheet is gone
    /// is not surfaced; web has the same limit.
    public func flush() {
        debounceTask?.cancel()
        debounceTask = nil
        Task { await self.run() }
    }

    /// A new baseline was adopted (the editor loaded or reloaded the server's copy).
    public func forget() {
        settled = nil
    }

    private func run() async {
        guard !isRunning, let value = pending() else { return }
        isRunning = true
        isSaving = true
        saveError = nil
        do {
            try await perform(value)
        } catch {
            saveError = ErrorText.message(for: error)
        }
        settled = value
        isRunning = false
        isSaving = false
        schedule()
    }
}
