import Repositories

/// What the Shopping tab needs, built once in `RootView` and handed down.
public struct ShoppingDependencies: Sendable {
    public let shopping: ShoppingListsRepository
    public let sync: ShoppingSyncEngine
    public let partner: PartnerRepository
    public let events: ListEventStream
    public let monitor: NetworkMonitor
    /// Non-nil only when the app was launched with `-uiTesting`: the UI tests flip it to go offline.
    public let networkSwitch: NetworkSwitch?
    public let currentUserID: @Sendable @MainActor () -> String?

    public init(
        shopping: ShoppingListsRepository, sync: ShoppingSyncEngine, partner: PartnerRepository, events: ListEventStream,
        monitor: NetworkMonitor, networkSwitch: NetworkSwitch?, currentUserID: @escaping @Sendable @MainActor () -> String?
    ) {
        self.shopping = shopping
        self.sync = sync
        self.partner = partner
        self.events = events
        self.monitor = monitor
        self.networkSwitch = networkSwitch
        self.currentUserID = currentUserID
    }

    /// Drains the queue now (launch), then again each time a network path appears. Run for as long as someone is signed in.
    public func runSyncLoop() async {
        await sync.drain()
        for await online in monitor.updates() where online {
            await sync.drain()
        }
    }
}
