// Issue #1293: an Endpoint Security string token carries its length and need not be NUL-terminated, so it is read by its length.

@testable import EDRExtensionLogic
import XCTest

final class TokenStringTests: XCTestCase {
    private func read(_ bytes: [UInt8], length: Int) -> String {
        bytes.map { CChar(bitPattern: $0) }.withUnsafeBufferPointer { tokenString($0.baseAddress, length: length) }
    }

    // spec:endpoint-event-collection/exec-strings-read-by-token-length/a-token-is-read-to-its-length-and-no-further
    func testReadsExactlyTheTokensBytes() {
        // A token for "/bin/ls" whose buffer runs on into the next string without a NUL.
        let buffer = Array("/bin/lsGARBAGE".utf8)
        XCTAssertEqual(read(buffer, length: 7), "/bin/ls")
        XCTAssertEqual(read(Array("héllo".utf8), length: "héllo".utf8.count), "héllo")
        XCTAssertEqual(read(Array("a\u{0}b".utf8), length: 3), "a\u{0}b", "a NUL inside the token is part of it")
    }

    // spec:endpoint-event-collection/exec-strings-read-by-token-length/a-token-that-is-not-utf-8-reads-empty
    func testEmptyAndInvalidTokensReadEmpty() {
        XCTAssertEqual(read([], length: 0), "")
        XCTAssertEqual(tokenString(nil, length: 4), "")
        XCTAssertEqual(read([0x2F, 0xFF, 0xFE], length: 3), "", "invalid UTF-8 is not repaired with replacement characters")
    }
}
