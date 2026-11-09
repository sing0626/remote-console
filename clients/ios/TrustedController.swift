import Foundation
import Security

enum ControllerCredentialVault {
    private static let service = (Bundle.main.bundleIdentifier ?? "com.sing.halfcloud.controller") + ".trusted-controller.v1"
    private enum StorageFailure: LocalizedError {
        case unavailable
        var errorDescription: String? { "未能讀寫此 iPhone 嘅授權憑證，請解鎖手機後再連接。" }
    }
    private static func query(_ origin: URL) -> [String: Any] {
        [kSecClass as String: kSecClassGenericPassword,
         kSecAttrService as String: service,
         kSecAttrAccount as String: origin.absoluteString,
         kSecAttrSynchronizable as String: false]
    }
    static func load(_ origin: URL) throws -> String? {
        var q = query(origin); q[kSecReturnData as String] = true; q[kSecMatchLimit as String] = kSecMatchLimitOne
        var result: CFTypeRef?
        let status = SecItemCopyMatching(q as CFDictionary, &result)
        if status == errSecItemNotFound { return nil }
        guard status == errSecSuccess, let data = result as? Data, data.count <= 2048,
              let credential = String(data: data, encoding: .utf8) else { throw StorageFailure.unavailable }
        return credential
    }
    static func save(_ credential: String, origin: URL) throws {
        let values = [kSecValueData as String: Data(credential.utf8)]
        var status = SecItemUpdate(query(origin) as CFDictionary, values as CFDictionary)
        if status == errSecItemNotFound {
            var q = query(origin); q[kSecValueData as String] = Data(credential.utf8)
            q[kSecAttrAccessible as String] = kSecAttrAccessibleWhenUnlockedThisDeviceOnly
            status = SecItemAdd(q as CFDictionary, nil)
        }
        guard status == errSecSuccess else { throw StorageFailure.unavailable }
    }
    static func remove(_ origin: URL) throws {
        let status = SecItemDelete(query(origin) as CFDictionary)
        guard status == errSecSuccess || status == errSecItemNotFound else { throw StorageFailure.unavailable }
    }
}
