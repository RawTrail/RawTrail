import Foundation
import IOKit

/// The platform value stamped on every event envelope the macOS extensions produce. Matches the server's PlatformDarwin constant and
/// Go's runtime.GOOS on macOS. Part of the platform-aware event contract (ADR-0018): a Windows or Linux agent stamps its own value.
enum EventPlatform {
    static let macOS = "darwin"
}

/// HostIdentity is the host_id both extensions stamp on their events: the Mac's hardware UUID, which the agent also enrolls with unless
/// EDR_HOST_ID overrides it. One definition, so the security extension's events and the network extension's cannot name the same Mac
/// differently. When it cannot be read, each extension keeps its own fallback ("unknown" and an empty string); the server refuses both,
/// with different reasons, and unifying them is a wire change of its own.
enum HostIdentity {

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
