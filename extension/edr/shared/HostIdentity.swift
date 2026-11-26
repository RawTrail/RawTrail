import Foundation
import IOKit
import os.log

/// The platform value stamped on every event envelope the macOS extensions produce. Matches the server's PlatformDarwin constant and
/// Go's runtime.GOOS on macOS. Part of the platform-aware event contract (ADR-0018): a Windows or Linux agent stamps its own value.
enum EventPlatform {
    static let macOS = "darwin"
}

/// HostIdentity is the host_id both extensions stamp on their events: the Mac's hardware UUID, which the agent also enrolls with unless
/// EDR_HOST_ID overrides it. One definition, so the security extension's events and the network extension's cannot name the same Mac
/// differently.
enum HostIdentity {
    /// unavailable is stamped when the hardware UUID cannot be read. The server refuses the events, since they match no enrolled
    /// host, and the warning logged at startup says why.
    static let unavailable = "unknown"

    /// hostID returns the hardware UUID, or `unavailable` after logging a warning to logger.
    static func hostID(logger: Logger) -> String {
        guard let uuid = hardwareUUID() else {
            logger.warning("Hardware UUID not available; events will carry host_id \(unavailable, privacy: .public)")
            return unavailable
        }
        return uuid
    }

    /// hardwareUUID reads IOPlatformUUID from the platform expert, the identifier System Information shows as Hardware UUID.
    static func hardwareUUID() -> String? {
        let platformExpert = IOServiceGetMatchingService(
            kIOMainPortDefault,
            IOServiceMatching("IOPlatformExpertDevice")
        )
        guard platformExpert != 0 else { return nil }
        defer { IOObjectRelease(platformExpert) }

        let uuidCF = IORegistryEntryCreateCFProperty(
            platformExpert,
            kIOPlatformUUIDKey as CFString,
            kCFAllocatorDefault,
            0
        )
        return uuidCF?.takeRetainedValue() as? String
    }
}
