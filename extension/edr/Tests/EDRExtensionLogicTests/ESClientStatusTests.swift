import XCTest
@testable import EDRExtensionLogic

/// The refusal record the agent reads to say why the security extension is not running (issue #1436). Its shape is the contract with
/// the agent, which parses it in agent/fda; these tests pin the bytes.
final class ESClientStatusTests: XCTestCase {
    // XCTest makes a new instance for each test, so each test has its own directory.
    private let dir = FileManager.default.temporaryDirectory.appendingPathComponent("esclient-\(UUID().uuidString)")

    override func tearDownWithError() throws {
        try? FileManager.default.removeItem(at: dir)
    }

    // spec:agent-status-reporting/a-missing-full-disk-access-grant-is-reported/the-extension-records-why-its-client-was-refused
    func testRecordWritesTheReasonAndTimeTheAgentReads() throws {
        let path = dir.appendingPathComponent("nested/es-client-status.json").path
        try ESClientStatus.record(ESClientStatus.notPermitted, at: Date(timeIntervalSince1970: 1_793_500_000), path: path)

        let object = try XCTUnwrap(JSONSerialization.jsonObject(with: Data(contentsOf: URL(fileURLWithPath: path))) as? [String: String])
        XCTAssertEqual(object, ["refused": "not_permitted", "at": "2026-11-01T02:26:40Z"])
    }

    func testClearRemovesARecordAndToleratesNone() throws {
        let path = dir.appendingPathComponent("es-client-status.json").path
        try ESClientStatus.record(ESClientStatus.notEntitled, path: path)
        XCTAssertTrue(FileManager.default.fileExists(atPath: path))

        ESClientStatus.clear(path: path)
        XCTAssertFalse(FileManager.default.fileExists(atPath: path))
        ESClientStatus.clear(path: path)
    }
}
