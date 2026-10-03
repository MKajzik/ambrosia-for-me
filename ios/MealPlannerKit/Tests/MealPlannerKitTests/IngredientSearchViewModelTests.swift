import API
import Foundation
import Repositories
import Testing
@testable import Features

@Suite
@MainActor
struct IngredientSearchViewModelTests {
    @MainActor
    struct Harness {
        let vm: IngredientSearchViewModel
        let transport: RoutingTransport
        let sleeper = TestSleeper()

        init(_ route: @escaping RoutingTransport.Route) {
            transport = RoutingTransport(route)
            let sleeper = self.sleeper
            vm = IngredientSearchViewModel(
                repository: IngredientsRepository(client: makeAuthlessClient(transport: transport)),
                sleep: { _ in try await sleeper.sleep() }
            )
        }
    }

    private nonisolated static func found(_ names: [String]) -> String {
        Fixtures.ingredientList(names.map { Fixtures.ingredient(id: $0, name: $0) })
    }

    @Test("Opening the sheet browses: the first page, no q")
    func startBrowses() async throws {
        let h = Harness { _ in (200, Self.found(["Apple"])) }
        h.vm.start()
        #expect(await waitUntil { h.vm.results.map(\.name) == ["Apple"] })
        let path = try #require(await h.transport.calls.first?.path)
        #expect(!path.contains("q="))
        #expect(path.contains("limit=20"))
    }

    @Test("Typing is debounced: quick keystrokes make one search for the last text")
    func debounce() async throws {
        let h = Harness { _ in (200, Self.found(["Rice"])) }
        h.vm.text = "r"
        h.vm.text = "ri"
        h.vm.text = "rice"
        await settle()
        #expect(await h.transport.calls.isEmpty)
        await h.sleeper.fire()
        #expect(await waitUntil { h.vm.results.map(\.name) == ["Rice"] })
        let calls = await h.transport.calls
        #expect(calls.count == 1)
        #expect(calls[0].path.contains("q=rice"))
    }

    @Test("Previous results stay on screen while the next search runs")
    func keepsPreviousResults() async throws {
        let gate = Gate()
        let h = Harness { call in
            if call.path.contains("q=oats") { await gate.wait() }
            return (200, Self.found(call.path.contains("q=oats") ? ["Oats"] : ["Rice"]))
        }
        h.vm.start()
        #expect(await waitUntil { h.vm.results.map(\.name) == ["Rice"] })
        h.vm.text = "oats"
        await h.sleeper.fire()
        #expect(await waitUntil { h.vm.isSearching })
        #expect(h.vm.results.map(\.name) == ["Rice"])
        await gate.release()
        #expect(await waitUntil { h.vm.results.map(\.name) == ["Oats"] })
        #expect(h.vm.isSearching == false)
    }

    @Test("Choosing a category searches at once, without the typing pause")
    func categoryIsImmediate() async throws {
        let h = Harness { _ in (200, Self.found(["Apple"])) }
        h.vm.category = .produce
        #expect(await waitUntil { !h.vm.results.isEmpty })
        #expect(await h.transport.calls.first?.path.contains("category=produce") == true)
    }

    @Test("The query is trimmed and capped at 100 characters")
    func queryIsTrimmedAndCapped() async throws {
        let h = Harness { _ in (200, Self.found([])) }
        h.vm.text = "  " + String(repeating: "a", count: 120) + "  "
        await h.sleeper.fire()
        for _ in 0..<400 where await h.transport.calls.isEmpty { try await Task.sleep(for: .milliseconds(5)) }
        let path = try #require(await h.transport.calls.first?.path)
        #expect(path.contains("q=" + String(repeating: "a", count: 100)))
        #expect(!path.contains(String(repeating: "a", count: 101)))
    }

    @Test("A failed search shows the message and keeps the previous results")
    func failure() async throws {
        let failing = Locked(false)
        let h = Harness { _ in
            failing.value ? (500, Fixtures.problem(500, code: "internal")) : (200, Self.found(["Rice"]))
        }
        h.vm.start()
        #expect(await waitUntil { !h.vm.results.isEmpty })
        failing.set(true)
        h.vm.text = "x"
        await h.sleeper.fire()
        #expect(await waitUntil { h.vm.errorMessage != nil })
        #expect(h.vm.errorMessage == "The server had a problem searching ingredients.")
        #expect(h.vm.results.map(\.name) == ["Rice"])
    }
}
