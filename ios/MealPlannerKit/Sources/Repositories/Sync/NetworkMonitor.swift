import Foundation
import Network

/// Whether the device has a network path, as a stream: `true` when one appears. The sync engine drains the queue on `true`.
/// A path is not proof the API is reachable; a drain that fails just leaves the rows for the next trigger.
public final class NetworkMonitor: @unchecked Sendable {
    public let updates: AsyncStream<Bool>
    private let monitor = NWPathMonitor()

    public init() {
        let (stream, continuation) = AsyncStream<Bool>.makeStream(bufferingPolicy: .bufferingNewest(1))
        updates = stream
        monitor.pathUpdateHandler = { continuation.yield($0.status == .satisfied) }
        continuation.onTermination = { [monitor] _ in monitor.cancel() }
        monitor.start(queue: DispatchQueue(label: "NetworkMonitor"))
    }
}
