import Repositories
import Testing

@Suite
struct NetworkMonitorTests {
    /// Reads the first value of a fresh stream, giving up after a bounded wait (NWPathMonitor reports its initial path on start).
    private func firstValue(of monitor: NetworkMonitor) async -> Bool? {
        await withTaskGroup(of: Bool?.self) { group in
            group.addTask {
                for await value in monitor.updates() { return value }
                return nil
            }
            group.addTask {
                try? await Task.sleep(for: .seconds(5))
                return nil
            }
            let result = await group.next() ?? nil
            group.cancelAll()
            return result
        }
    }

    @Test("A second subscription still yields after the first was cancelled, so the sync loop survives sign-out then sign-in")
    func restartable() async {
        let monitor = NetworkMonitor()
        #expect(await firstValue(of: monitor) != nil)
        #expect(await firstValue(of: monitor) != nil)
    }
}
