import API
import Foundation
import Persistence
import Repositories
import Testing
@testable import Features

@Suite
@MainActor
struct ApplyTemplateViewModelTests {
    @MainActor
    struct Harness {
        let vm: ApplyTemplateViewModel
        let server: PlanServer
        let transport: RoutingTransport

        init(server: PlanServer, summaries: [Components.Schemas.DietTemplateSummary], startDate: String = "2026-10-05") throws {
            self.server = server
            transport = RoutingTransport { call in
                call.route == "GET /diet-templates" ? (200, Fixtures.templateList(summaries)) : try await server.route(call)
            }
            let client = makeAuthlessClient(transport: transport)
            let planRepository = PlanRepository(client: client, cache: CacheStore.makePlanCache(try CacheStore.inMemoryContainer()))
            let templates = TemplatesRepository(client: client, cache: CacheStore.makeTemplateCache(try CacheStore.inMemoryContainer()))
            let plan = PlanViewModel(plan: planRepository, localDay: LocalDay(timeZone: TimeZone(identifier: "UTC")!))
            vm = ApplyTemplateViewModel(templates: templates, plan: plan, startDate: startDate)
        }
    }

    private static let template = PlanServer.Template(dayCount: 3, slots: [.init(dayIndex: 0, slot: .breakfast, mealID: "m1", portion: 1)])

    @Test("Lists my templates only and selects the first by default")
    func loadsMineAndSelectsFirst() async throws {
        let h = try Harness(server: PlanServer(templates: ["t1": Self.template]), summaries: [Fixtures.templateSummary(id: "t1", name: "Cut", dayCount: 3)])
        await h.vm.load()
        #expect(h.vm.templates.map(\.id) == ["t1"])
        #expect(h.vm.selectedID == "t1")
        #expect(await h.transport.calls.map(\.route) == ["GET /diet-templates"])
    }

    @Test("A choice the person already made is kept when the list reloads")
    func keepsSelection() async throws {
        let h = try Harness(server: PlanServer(), summaries: [Fixtures.templateSummary(id: "a", name: "A"), Fixtures.templateSummary(id: "b", name: "B")])
        await h.vm.load()
        h.vm.selectedID = "b"
        await h.vm.load()
        #expect(h.vm.selectedID == "b")
    }

    @Test("Applying with no template asks for one")
    func noTemplate() async throws {
        let h = try Harness(server: PlanServer(), summaries: [])
        await h.vm.load()
        await h.vm.apply()
        #expect(h.vm.errorMessage == "Choose a template.")
        #expect(h.vm.didApply == false)
    }

    @Test("A clean apply uses the chosen start date and marks done")
    func applies() async throws {
        let server = PlanServer(templates: ["t1": Self.template])
        let h = try Harness(server: server, summaries: [Fixtures.templateSummary(id: "t1", dayCount: 3)], startDate: "2026-10-12")
        await h.vm.load()
        await h.vm.apply()
        #expect(h.vm.didApply)
        #expect(h.vm.errorMessage == nil)
        #expect(server.entries(on: "2026-10-12").map(\.mealName) == ["Oats"])
    }

    @Test("A conflict asks to replace; confirming replaces and finishes; the question is not an error")
    func conflictThenConfirm() async throws {
        let server = PlanServer(entries: [Fixtures.entry(date: "2026-10-05", slot: .breakfast, mealName: "Old")], templates: ["t1": Self.template])
        let h = try Harness(server: server, summaries: [Fixtures.templateSummary(id: "t1", dayCount: 3)])
        await h.vm.load()
        await h.vm.apply()
        #expect(h.vm.confirmingReplace)
        #expect(h.vm.didApply == false)
        #expect(h.vm.errorMessage == nil)
        await h.vm.confirmReplace()
        #expect(h.vm.confirmingReplace == false)
        #expect(h.vm.didApply)
        #expect(server.entries(on: "2026-10-05").map(\.mealName) == ["Oats"])
    }

    @Test("Declining the replacement leaves the plan alone")
    func declining() async throws {
        let server = PlanServer(entries: [Fixtures.entry(date: "2026-10-05", slot: .breakfast, mealName: "Old")], templates: ["t1": Self.template])
        let h = try Harness(server: server, summaries: [Fixtures.templateSummary(id: "t1", dayCount: 3)])
        await h.vm.load()
        await h.vm.apply()
        h.vm.confirmingReplace = false
        #expect(server.entries(on: "2026-10-05").map(\.mealName) == ["Old"])
        #expect(h.vm.didApply == false)
    }

    @Test("A template that is gone shows its message")
    func gone() async throws {
        let h = try Harness(server: PlanServer(), summaries: [Fixtures.templateSummary(id: "ghost")])
        await h.vm.load()
        await h.vm.apply()
        #expect(h.vm.errorMessage == "That template isn't available anymore.")
    }
}
