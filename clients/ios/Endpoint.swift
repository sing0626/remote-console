import Foundation

enum Endpoint {
    static func isLocal(_ host: String) -> Bool {
        let host = host.lowercased()
        if host == "localhost" || host == "::1" || host.hasSuffix(".local") { return true }
        let labels = host.split(separator: ".", omittingEmptySubsequences: false)
        guard labels.count == 4, labels.allSatisfy({ !$0.isEmpty && $0.count <= 3 && $0.allSatisfy({ $0.isASCII && $0.isNumber }) && ($0.count == 1 || $0.first != "0") }) else { return false }
        let parts = labels.compactMap { Int($0) }
        guard parts.count == 4, parts.allSatisfy({ (0...255).contains($0) }) else { return false }
        return parts[0] == 10 || parts[0] == 127 || (parts[0] == 192 && parts[1] == 168) || (parts[0] == 172 && (16...31).contains(parts[1]))
    }
    static func parse(_ input: String) -> URL? {
        var value = input.trimmingCharacters(in: .whitespacesAndNewlines)
        if !value.contains("://") {
            let local = isLocal(URLComponents(string: "http://" + value)?.host ?? "")
            value = (local ? "http://" : "https://") + value
        }
        guard var parts = URLComponents(string: value), let host = parts.host,
              ["http", "https"].contains(parts.scheme ?? ""), parts.user == nil, parts.password == nil,
              parts.query == nil, parts.fragment == nil, ["", "/", "/control"].contains(parts.path),
              parts.scheme == "https" || isLocal(host) else { return nil }
        parts.path = "/control"
        return parts.url
    }
}

