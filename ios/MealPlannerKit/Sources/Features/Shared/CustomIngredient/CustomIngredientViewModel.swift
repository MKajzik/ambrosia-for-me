import API
import Observation
import Repositories

@Observable
@MainActor
public final class CustomIngredientViewModel {
    public var form: CustomIngredientForm
    public private(set) var fieldErrors: [CustomIngredientForm.Field: String] = [:]
    public private(set) var bannerError: String?
    public private(set) var isSaving = false

    @ObservationIgnored private let repository: IngredientsRepository

    /// `name` is the search text the person was looking for, prefilled.
    public init(name: String, repository: IngredientsRepository) {
        self.form = CustomIngredientForm(name: name)
        self.repository = repository
    }

    /// Returns the created ingredient, or `nil` with `fieldErrors` / `bannerError` set. A `400` shows inline on
    /// the field it names, else as a banner.
    public func save() async -> Components.Schemas.Ingredient? {
        fieldErrors = [:]
        bannerError = nil
        switch form.validate() {
        case .invalid(let errors):
            fieldErrors = errors
            return nil
        case .valid(let request):
            isSaving = true
            defer { isSaving = false }
            do {
                return try await repository.create(request)
            } catch let IngredientError.validationFailed(fields, message) {
                var inline: [CustomIngredientForm.Field: String] = [:]
                for (path, text) in fields {
                    if let field = CustomIngredientForm.Field(apiPath: path) { inline[field] = text }
                }
                if inline.isEmpty { bannerError = message } else { fieldErrors = inline }
                return nil
            } catch {
                bannerError = ErrorText.message(for: error)
                return nil
            }
        }
    }
}
