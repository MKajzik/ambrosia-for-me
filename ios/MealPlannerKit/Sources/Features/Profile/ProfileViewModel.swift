import API
import Foundation
import Observation
import Repositories

/// The Profile tab's account and targets. Reads render the cache, await the refresh, re-read; a failed refresh keeps
/// what is shown and marks it stale. Saving and deleting are online-only. The draft follows the saved targets unless
/// the person has typed something, so a refresh never overwrites their typing.
@Observable
@MainActor
public final class ProfileViewModel {
    public private(set) var user: Components.Schemas.User?
    public var draft = TargetsDraft()
    public private(set) var fieldErrors: [TargetsDraft.Field: String] = [:]
    public private(set) var banner: String?
    public private(set) var isSaving = false
    public private(set) var isStale = false
    public private(set) var loadError: String?
    /// Loads can overlap (appear, refresh, foreground), so this counts them.
    public private(set) var loadsInFlight = 0
    public var isLoading: Bool { loadsInFlight > 0 }
    /// Save is offered only when the draft differs from what the server has.
    public var canSave: Bool { user != nil && !isSaving && draft != savedDraft }

    private var savedDraft = TargetsDraft()
    /// Bumped when a save starts and again when it ends. A refresh whose answer was read before a save may be older
    /// than that save, so it is not trusted if this changed while it ran.
    @ObservationIgnored private var saveEpoch = 0
    @ObservationIgnored private let profile: ProfileRepository
    @ObservationIgnored private let plan: PlanRepository
    @ObservationIgnored private let signOut: @MainActor () async -> Void

    /// `signOut` runs once an account has been deleted (the app's normal sign-out, which clears every cache).
    public init(profile: ProfileRepository, plan: PlanRepository, signOut: @escaping @MainActor () async -> Void) {
        self.profile = profile
        self.plan = plan
        self.signOut = signOut
    }

    /// The tab appears or the app returns to the foreground.
    public func appear() async {
        loadsInFlight += 1
        defer { loadsInFlight -= 1 }
        if let cached = await profile.cachedUser() { adopt(cached, keepEdits: true) }
        let epoch = saveEpoch
        do {
            var fresh = try await profile.refreshUser()
            if saveEpoch != epoch {
                // A save began or ended while this was in flight: the answer may predate it. While the save is still
                // running its own answer wins; otherwise ask again so the screen and the cache end on the saved values.
                if isSaving { return }
                fresh = try await profile.refreshUser()
            }
            adopt(fresh, keepEdits: true)
            isStale = false
            loadError = nil
        } catch {
            isStale = true
            loadError = user == nil ? ErrorText.message(for: error) : nil
        }
    }

    /// Validates, saves all four targets, and on success stores the answer in the profile cache and gives the plan
    /// cache the new targets. Returns whether it saved.
    @discardableResult
    public func saveTargets() async -> Bool {
        guard user != nil, !isSaving else { return false }
        fieldErrors = [:]
        banner = nil
        let update: TargetsUpdate
        switch draft.validate() {
        case .invalid(let errors):
            fieldErrors = errors
            return false
        case .valid(let valid):
            update = valid
        }
        isSaving = true
        saveEpoch += 1
        defer {
            isSaving = false
            saveEpoch += 1
        }
        do {
            let saved = try await profile.updateTargets(update)
            await plan.storeTargets(from: saved)
            adopt(saved, keepEdits: false)
            return true
        } catch let ProfileError.validationFailed(fields, message) {
            var inline: [TargetsDraft.Field: String] = [:]
            for (path, text) in fields {
                if let field = TargetsDraft.Field(apiPath: path) { inline[field] = text }
            }
            if inline.isEmpty { banner = message } else { fieldErrors = inline }
            return false
        } catch {
            banner = ErrorText.message(for: error)
            return false
        }
    }

    public static func emailMatches(_ typed: String, _ email: String) -> Bool {
        typed.trimmingCharacters(in: .whitespacesAndNewlines).lowercased() == email.lowercased()
    }

    /// Deletes the account once the typed email matches, then signs out. Returns the text to show, or `nil` when it is
    /// gone. Nothing is sent while the email does not match; a failure leaves the account and the session alone.
    public func deleteAccount(typedEmail: String) async -> String? {
        guard let email = user?.email, Self.emailMatches(typedEmail, email) else {
            return "Type your email address exactly to confirm."
        }
        do {
            try await profile.deleteAccount()
        } catch {
            return ErrorText.message(for: error)
        }
        await signOut()
        return nil
    }

    /// Takes in a user from the cache, the server or a save. The draft follows it unless the person has typed
    /// something (`keepEdits`).
    private func adopt(_ next: Components.Schemas.User, keepEdits: Bool) {
        let edited = keepEdits && draft != savedDraft
        user = next
        savedDraft = TargetsDraft(user: next)
        if !edited { draft = savedDraft }
    }
}
