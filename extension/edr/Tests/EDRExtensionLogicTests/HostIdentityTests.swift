import Foundation
import os.log
@testable import EDRExtensionLogic
import XCTest

/// HostIdentity is the one host_id and platform both extensions stamp on their events.
final class HostIdentityTests: XCTestCase {
    func testTheHostIDIsThisMacsHardwareUUIDInCanonicalForm() throws {
        let uuid = try XCTUnwrap(HostIdentity.hardwareUUID())
        XCTAssertNotNil(UUID(uuidString: uuid))
        XCTAssertEqual(HostIdentity.hostID(logger: Logger(subsystem: "io.rawtrail.tests", category: "HostIdentity")), uuid)
    }

    func testThePlatformIsTheServersPlatformDarwinValue() {
        XCTAssertEqual(EventPlatform.macOS, "darwin")
    }
}
