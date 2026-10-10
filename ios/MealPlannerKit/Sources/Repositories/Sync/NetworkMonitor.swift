import Foundation
import Network

/// Whether the device has a network path, as a stream: `true` when one appears. The sync engine drains the queue on `true`.
/// A path is not proof the API is reachable; a drain that fails just leaves the rows for the next trigger.
/// Every call to `updates()` owns its own `NWPathMonitor`, cancelled when that stream ends, so the sync loop can be
/// cancelled on sign-out and started again on the next sign-in.
public struct NetworkMonitor: Sendable {
    public init() {}

    public func updates() -> AsyncStream<Bool> {
        let (stream, continuation) = AsyncStream<Bool>.makeStream(bufferingPolicy: .bufferingNewest(1))
        let monitor = NWPathMonitor()
        monitor.pathUpdateHandler = { continuation.yield($0.status == .satisfied) }
        continuation.onTermination = { _ in monitor.cancel() }
        monitor.start(queue: DispatchQueue(label: "NetworkMonitor"))
        return stream
    }
}
