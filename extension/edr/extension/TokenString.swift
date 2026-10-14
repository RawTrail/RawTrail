import Foundation

/// tokenString reads exactly `length` bytes at `data` as UTF-8: the bytes of an Endpoint Security string token, which carries its
/// length and is not guaranteed to be NUL-terminated (issue #1293). It never reads past `length`, so a NUL inside the bytes is kept
/// and a missing one is not looked for. No bytes, or bytes that are not valid UTF-8, read as the empty string rather than as text with
/// replacement characters, which a rule comparing paths could miscompare. esTokenString applies it to a token; it is separate so it can
/// be tested without EndpointSecurity.
func tokenString(_ data: UnsafePointer<CChar>?, length: Int) -> String {
    guard let data, length > 0 else {
        return ""
    }
    return String(bytes: UnsafeRawBufferPointer(start: data, count: length), encoding: .utf8) ?? ""
}
