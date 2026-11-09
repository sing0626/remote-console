import XCTest
import Foundation

final class MotionUITests: XCTestCase {
    private var app: XCUIApplication!
    override func setUpWithError() throws {
        continueAfterFailure = false
        XCUIDevice.shared.orientation = .portrait
        app = XCUIApplication()
        app.launchArguments = ["--preview-session"]
    }
    private func tap(_ id: String, file: StaticString = #filePath, line: UInt = #line) {
        let button = app.buttons[id]
        XCTAssertTrue(button.waitForExistence(timeout: 6), "Missing control: \(id)", file: file, line: line)
        // tap() scrolls an offscreen control into view before interacting.
        button.tap()
    }
    private func title(_ value: String, file: StaticString = #filePath, line: UInt = #line) {
        XCTAssertTrue(app.staticTexts[value].waitForExistence(timeout: 6), "Missing panel: \(value)", file: file, line: line)
    }
    private func shot(_ name: String) {
        let attachment = XCTAttachment(screenshot: app.screenshot())
        attachment.name = name; attachment.lifetime = .keepAlways; add(attachment)
    }
    private func connectFixture(endpoint: String = "http://127.0.0.1:18741/control", code: String = "") {
        let address = app.textFields["home.address"]
        XCTAssertTrue(address.waitForExistence(timeout: 6)); address.tap()
        address.press(forDuration: 1.1)
        if app.menuItems["Select All"].waitForExistence(timeout: 2) { app.menuItems["Select All"].tap(); address.typeText(endpoint) }
        else {
            let value = address.value as? String ?? ""
            address.typeText(String(repeating: XCUIKeyboardKey.delete.rawValue, count: value.count) + endpoint)
        }
        // Move focus away from the URL field. Trusted authorization has no PIN.
        if app.keyboards.firstMatch.exists { tap("home.dismissKeyboard") }
        tap("home.connect")
    }
    private func microphone(_ allow: Bool) {
        let alert = XCUIApplication(bundleIdentifier: "com.apple.springboard").alerts.firstMatch
        XCTAssertTrue(alert.waitForExistence(timeout: 15), "Microphone authorization must be requested before the native audio engine starts")
        let button = alert.buttons.element(boundBy: allow ? 1 : 0)
        XCTAssertTrue(button.exists); button.tap()
    }
    func test01HomeSettingsAndAdvanced() {
        app.launchArguments = []; app.launch()
        tap("home.settings"); title("畫質與連線"); shot("home-settings")
        tap("panel.close"); tap("home.advanced"); shot("advanced-expanded")
        tap("home.advanced"); tap("home.settings"); title("畫質與連線")
        tap("panel.close"); XCTAssertTrue(app.buttons["home.settings"].isHittable)
    }
    func test02ToolsToQualityRepeated() {
        app.launch()
        for round in 0..<2 {
            tap("session.tools"); title("遙控工具")
            tap("tools.settings"); title("畫質與連線"); shot("tools-to-quality-\(round)")
            XCTAssertFalse(app.staticTexts["遙控工具"].isHittable)
            tap("panel.close"); XCTAssertTrue(app.buttons["session.tools"].isHittable)
        }
    }
    func test03ComposerKeyboardAndDismiss() {
        app.launch(); tap("session.keyboard"); title("傳送文字")
        let editor = app.textViews["composer.input"]
        XCTAssertTrue(editor.waitForExistence(timeout: 6)); editor.tap(); editor.typeText("motion check")
        XCTAssertTrue(app.keyboards.firstMatch.waitForExistence(timeout: 6)); shot("composer-with-keyboard")
        tap("panel.close")
        XCTAssertTrue(app.buttons["session.tools"].waitForExistence(timeout: 6))
        XCTAssertFalse(app.keyboards.firstMatch.exists)
        tap("session.tools"); title("遙控工具"); tap("panel.close")
    }
    func test04FullscreenAndInteractiveDismiss() {
        app.launch(); tap("session.fullscreen")
        XCTAssertFalse(app.buttons["session.fullscreen"].exists)
        tap("session.tools"); tap("tools.fullscreen")
        XCTAssertTrue(app.buttons["session.fullscreen"].waitForExistence(timeout: 6)); shot("fullscreen-restored")
        tap("session.tools"); title("遙控工具")
        let close = app.buttons["panel.close"].frame
        let start = app.coordinate(withNormalizedOffset: .zero).withOffset(CGVector(dx: close.midX - 90, dy: close.midY))
        let end = app.coordinate(withNormalizedOffset: CGVector(dx: 0.5, dy: 0.96))
        start.press(forDuration: 0.05, thenDragTo: end)
        XCTAssertTrue(app.buttons["session.tools"].waitForExistence(timeout: 6))
        XCTAssertTrue(app.buttons["session.tools"].isHittable)
        tap("session.tools"); title("遙控工具"); tap("panel.close")
    }
    func test05DeniedMicrophoneDoesNotCrash() {
        app.terminate()
        app.resetAuthorizationStatus(for: .microphone)
        app.launchArguments = []; app.launch(); connectFixture(); microphone(false)
        let status = app.staticTexts["session.status"]
        let denied = NSPredicate(format: "label CONTAINS %@", "未允許麥克風權限")
        expectation(for: denied, evaluatedWith: status)
        waitForExpectations(timeout: 10)
        XCTAssertEqual(app.state, .runningForeground); shot("microphone-denied-no-crash")
        app.buttons["電話清單"].tap()
        XCTAssertTrue(app.buttons["home.connect"].waitForExistence(timeout: 6))
    }
    func test06AuthorizedNativeMediaAndReconnect() {
        app.terminate()
        app.resetAuthorizationStatus(for: .microphone)
        app.launchArguments = []; app.launch()
        for round in 0..<2 {
            connectFixture()
            if round == 0 { microphone(true) }
            let status = app.staticTexts["session.status"]
            expectation(for: NSPredicate(format: "label == %@", "已連接 · 可操作"), evaluatedWith: status)
            waitForExpectations(timeout: 45)
            let detail = app.staticTexts["session.detail"]
            let decoding = NSPredicate { _, _ in detail.label.range(of: "[1-9][0-9]* / [0-9]+ FPS", options: .regularExpression) != nil }
            expectation(for: decoding, evaluatedWith: detail); waitForExpectations(timeout: 20)
            tap("session.home")
            app.buttons["開聲"].tap()
            XCTAssertTrue(app.buttons["靜音"].waitForExistence(timeout: 6)); shot("native-media-connected-\(round)")
            app.buttons["靜音"].tap()
            tap("session.tools"); tap("tools.disconnect")
            XCTAssertTrue(app.buttons["home.connect"].waitForExistence(timeout: 8))
            XCTAssertEqual(app.state, .runningForeground)
        }
    }
    func test07OneEndpointSelectsDifferentPhones() {
        app.launchArguments = []; app.launch()
        connectFixture(endpoint: "http://127.0.0.1:18742/control")
        title("揀受控電話"); tap("picker.host.fixture-b")
        let status = app.staticTexts["session.status"]
        expectation(for: NSPredicate(format: "label == %@", "已連接 · 可操作"), evaluatedWith: status)
        waitForExpectations(timeout: 45)
        XCTAssertTrue(app.staticTexts["測試電話 B"].exists); shot("directory-selected-phone-b")
        tap("session.home"); tap("session.tools"); tap("tools.disconnect")
        XCTAssertTrue(app.buttons["home.hosts"].waitForExistence(timeout: 8)); tap("home.hosts")
        title("揀受控電話"); tap("picker.host.fixture-a")
        connectFixture(endpoint: "http://127.0.0.1:18742/control")
        expectation(for: NSPredicate(format: "label == %@", "已連接 · 可操作"), evaluatedWith: app.staticTexts["session.status"])
        waitForExpectations(timeout: 45)
        XCTAssertTrue(app.staticTexts["測試電話 A"].exists); shot("directory-selected-phone-a")
        tap("session.home"); tap("session.tools"); tap("tools.disconnect")
        XCTAssertTrue(app.buttons["home.connect"].waitForExistence(timeout: 8))
    }
    func test08RevokedControllerRequiresNewApproval() {
        app.launchArguments = []; app.launch()
        connectFixture(endpoint: "http://127.0.0.1:18742/control")
        title("揀受控電話"); tap("picker.host.fixture-b")
        expectation(for: NSPredicate(format: "label == %@", "已連接 · 可操作"), evaluatedWith: app.staticTexts["session.status"])
        waitForExpectations(timeout: 50)
        var request = URLRequest(url: URL(string: "http://127.0.0.1:18742/fixture/revoke")!)
        request.httpMethod = "POST"; request.httpBody = Data("{}".utf8)
        let revoked = expectation(description: "fixture revokes the current credential")
        var responseCode = 0
        URLSession.shared.dataTask(with: request) { _, response, _ in responseCode = (response as? HTTPURLResponse)?.statusCode ?? 0; revoked.fulfill() }.resume()
        waitForExpectations(timeout: 12); XCTAssertEqual(responseCode, 200)
        expectation(for: NSPredicate(format: "label != %@", "已連接 · 可操作"), evaluatedWith: app.staticTexts["session.status"])
        waitForExpectations(timeout: 15)
        app.buttons["電話清單"].tap()
        connectFixture(endpoint: "http://127.0.0.1:18742/control")
        expectation(for: NSPredicate(format: "label == %@", "已連接 · 可操作"), evaluatedWith: app.staticTexts["session.status"])
        waitForExpectations(timeout: 50)
        shot("revoked-controller-approved-again")
        tap("session.home"); tap("session.tools"); tap("tools.disconnect")
        XCTAssertTrue(app.buttons["home.connect"].waitForExistence(timeout: 8))
    }

}
