import API
import Observation
import Repositories

/// The custom-ingredient form's state, for creating an ingredient or editing one of mine.
@Observable
@MainActor
public final class CustomIngredientViewModel {
    public var form: CustomIngredientForm
    public private(set) var fieldErrors: [CustomIngredientForm.Field: String] = [:]
    public private(set) var bannerError: String?
    public private(set) var isSaving = false

    @ObservationIgnored private let repository: IngredientsRepository
    @ObservationIgnored private let editing: Components.Schemas.Ingredient?

    /// `name` is the search text the person was looking for, prefilled.
    public init(name: String, repository: IngredientsRepository) {
        self.form = CustomIngredientForm(name: name)
        self.repository = repository
        self.editing = nil
    }

    /// Editing an existing ingredient: the form is prefilled, and a save keeps every nutrient the form does not show.
    public init(editing ingredient: Components.Schemas.Ingredient, repository: IngredientsRepository) {
        self.form = CustomIngredientForm(editing: ingredient)
        self.repository = repository
        self.editing = ingredient
    }

    /// Returns the created or updated ingredient, or `nil` with `fieldErrors` / `bannerError` set. A `400` shows
    /// inline on the field it names, else as a banner.
    public func save() async -> Components.Schemas.Ingredient? {
        fieldErrors = [:]
        bannerError = nil
        if let editing {
            switch form.validateUpdate(preserving: editing) {
            case .invalid(let errors):
                fieldErrors = errors
                return nil
            case .valid(let update):
                return await send { try await self.repository.update(id: editing.id, update) }
            }
        }
        switch form.validate() {
        case .invalid(let errors):
            fieldErrors = errors
            return nil
        case .valid(let request):
            return await send { try await self.repository.create(request) }
        }
    }

    private func send(_ call: () async throws -> Components.Schemas.Ingredient) async -> Components.Schemas.Ingredient? {
        isSaving = true
        defer { isSaving = false }
        do {
            return try await call()
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
