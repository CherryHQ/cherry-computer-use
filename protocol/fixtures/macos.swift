import AppKit

final class Counter: NSObject {
    var count = 0
    @objc func increment(_ sender: NSButton) {
        count += 1
        sender.title = "Count: \(count)"
        NSAccessibility.post(element: sender, notification: .titleChanged)
    }
}
let app = NSApplication.shared
app.setActivationPolicy(.regular)
let window = NSWindow(contentRect: NSRect(x: 100, y: 100, width: 320, height: 160), styleMask: [.titled, .closable], backing: .buffered, defer: false)
window.title = "Cherry SDK Fixture"
let counter = Counter()
let button = NSButton(title: "Count: 0", target: counter, action: #selector(Counter.increment(_:)))
button.frame = NSRect(x: 60, y: 55, width: 200, height: 50)
window.contentView?.addSubview(button)
window.orderFrontRegardless()
app.run()
