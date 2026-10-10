import Foundation
import Repositories

/// The user-facing text for a failed call. Transport failures reach here as `URLError` (the repositories
/// unwrap the generated client's `ClientError`); anything unmapped falls back to a generic line.
public enum ErrorText {
    public static let rateLimited = "Too many requests. Please wait a moment and try again."

    public static func message(for error: Error) -> String {
        switch error {
        case let error as MealsError:
            switch error {
            case .notFound: return "This meal isn't available anymore."
            case .partnerNotLinked: return "You're not linked with a partner."
            case .inUse: return "This meal is used in your plan or a diet template. Remove it there first."
            case .validationFailed(let message): return message
            case .unauthorized: return "Please sign in again."
            case .rateLimited: return rateLimited
            case .server(let message): return message
            }
        case let error as IngredientError:
            switch error {
            case .validationFailed(_, let message): return message
            case .notFound: return "This ingredient isn't available anymore."
            case .inUse: return "A meal still uses this ingredient. Remove it from those meals first."
            case .unitInUse: return "A meal uses this ingredient by piece or by volume, so its weight per piece or density can't be cleared."
            case .unauthorized: return "Please sign in again."
            case .rateLimited: return rateLimited
            case .server(let message): return message
            }
        case let error as PlanError:
            switch error {
            case .notFound: return "That template isn't available anymore."
            case .conflict: return "Meals are already planned for some of those days."
            case .validationFailed(let message): return message
            case .unauthorized: return "Please sign in again."
            case .rateLimited: return rateLimited
            case .server(let message): return message
            }
        case let error as TemplatesError:
            switch error {
            case .notFound: return "This template isn't available anymore."
            case .partnerNotLinked: return "You're not linked with a partner."
            case .validationFailed(let message): return message
            case .unauthorized: return "Please sign in again."
            case .rateLimited: return rateLimited
            case .server(let message): return message
            }
        case let error as ShoppingError:
            switch error {
            case .notFound: return "This list isn't available anymore."
            case .partnerNotLinked: return "You're not linked with a partner."
            case .versionConflict: return "Someone else changed this item."
            case .validationFailed(let message): return message
            case .unauthorized: return "Please sign in again."
            case .rateLimited: return rateLimited
            case .server(let message): return message
            }
        case let error as ProfileError:
            switch error {
            case .validationFailed(_, let message): return message
            case .unauthorized: return "Please sign in again."
            case .rateLimited: return rateLimited
            case .server(let message): return message
            }
        case let error as PartnerError:
            switch error {
            case .inviteInvalid: return "That code didn't work. Check it, or ask your partner for a new one."
            case .alreadyLinked: return "You're already linked with a partner."
            case .validationFailed(let message): return message
            case .unauthorized: return "Please sign in again."
            case .rateLimited: return "Too many tries. Wait a minute and try again."
            case .server(let message): return message
            }
        case is URLError:
            return "Can't reach the server. Check your connection and try again."
        default:
            return "Something went wrong. Please try again."
        }
    }
}
