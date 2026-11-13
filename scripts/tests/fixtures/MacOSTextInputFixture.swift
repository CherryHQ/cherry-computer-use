import AppKit

final class TextInputApplication: NSApplication {
    var hideFocus = false
    override func accessibilityAttributeValue(_ attribute: NSAccessibility.Attribute) -> Any? {
        if attribute == .focusedUIElement && hideFocus { return nil }
        return super.accessibilityAttributeValue(attribute)
    }
}
// The legacy attribute bridge lets a real NSTextView expose deliberately
// restricted capabilities and reject writes without replacing the AX client.
final class ProbeTextView: NSTextView {
    var valueWritable = true
    var selectedWritable = true
    var rejectSelectedWrite = false
    var rejectValueWrite = false
    var rejectCaretWrite = false
    var valueMode = "normal"
    var valueWrites = 0
    var selectedWrites = 0
    var moveFocusOnProbe = false
    var moveFocus: (() -> Void)?
    override func accessibilityAttributeValue(_ attribute: NSAccessibility.Attribute) -> Any? {
        if attribute == .value { return string }
        if attribute == .selectedTextRange { return NSValue(range: selectedRange()) }
        return super.accessibilityAttributeValue(attribute)
    }
    override func accessibilityIsAttributeSettable(_ attribute: NSAccessibility.Attribute) -> Bool {
        if attribute == .value { return valueWritable }
        if attribute == .selectedTextRange { return true }
        if attribute == .selectedText {
            if moveFocusOnProbe { moveFocusOnProbe = false; moveFocus?() }
            return selectedWritable
        }
        return super.accessibilityIsAttributeSettable(attribute)
    }
    override func accessibilitySetValue(_ value: Any?, forAttribute attribute: NSAccessibility.Attribute) {
        if attribute == .value, let value = value as? String {
            valueWrites += 1
            if rejectValueWrite {
                NSException(name: .invalidArgumentException, reason: "Fixture rejects value writes", userInfo: nil).raise()
                return
            }
            switch valueMode {
            case "ignore": return
            case "trim": string = value.trimmingCharacters(in: .whitespacesAndNewlines)
            case "numeric": string = String(Int(value) ?? 0)
            case "delay": DispatchQueue.main.asyncAfter(deadline: .now() + 0.2) { self.string = value }
            default: string = value
            }
        } else if attribute == .selectedTextRange, let value = value as? NSValue {
            if rejectCaretWrite {
                NSException(name: .invalidArgumentException, reason: "Fixture rejects caret writes", userInfo: nil).raise()
                return
            }
            setSelectedRange(value.rangeValue)
        } else if attribute == .selectedText, let value = value as? String {
            selectedWrites += 1
            if rejectSelectedWrite {
                NSException(name: .invalidArgumentException, reason: "Fixture rejects selected text writes", userInfo: nil).raise()
                return
            }
            insertText(value, replacementRange: selectedRange())
        } else {
            super.accessibilitySetValue(value, forAttribute: attribute)
        }
    }
}
final class Controller {
    let app: TextInputApplication
    let window = NSWindow(contentRect: NSRect(x: 180, y: 180, width: 500, height: 330),
        styleMask: [.titled], backing: .buffered, defer: false)
    let view = ProbeTextView(frame: NSRect(x: 20, y: 100, width: 460, height: 190))
    let field = NSTextField(frame: NSRect(x: 20, y: 40, width: 460, height: 30))
    let sheetField = NSTextField(frame: NSRect(x: 20, y: 45, width: 320, height: 30))
    var sheet: NSPanel?
    var otherWindow: NSWindow?
    let otherField = NSTextField(frame: NSRect(x: 20, y: 40, width: 300, height: 30))
    let stateURL = URL(fileURLWithPath: CommandLine.arguments[1])
    let commandURL = URL(fileURLWithPath: CommandLine.arguments[2])
    var serial = 0
    var target = "view"
    init(app: TextInputApplication) {
        self.app = app
        window.title = "OCU Text Input Fixture"
        window.isReleasedWhenClosed = false
        view.isRichText = false
        view.setAccessibilityLabel("Document input")
        field.setAccessibilityLabel("Secondary input")
        sheetField.setAccessibilityLabel("Sheet input")
        window.contentView!.addSubview(view)
        window.contentView!.addSubview(field)
        view.moveFocus = { [weak self] in
            guard let self else { return }
            self.window.makeFirstResponder(self.field)
        }
        window.makeKeyAndOrderFront(nil)
        Timer.scheduledTimer(withTimeInterval: 0.03, repeats: true) { [self] _ in tick() }
    }
    func tick() {
        if let data = try? Data(contentsOf: commandURL),
           let cmd = (try? JSONSerialization.jsonObject(with: data)) as? [String: Any],
           let next = cmd["serial"] as? Int, next != serial {
            if cmd["reset"] as? Bool == true {
                otherWindow?.close(); otherWindow = nil
                if let sheet { window.endSheet(sheet); sheet.orderOut(nil); self.sheet = nil }
                view.valueWritable = true; view.selectedWritable = true; view.rejectSelectedWrite = false; view.rejectValueWrite = false; view.rejectCaretWrite = false; view.moveFocusOnProbe = false
                view.string = "hello world"; field.stringValue = "untouched"; sheetField.stringValue = "save.txt"
                view.valueMode = "normal"
                view.valueWrites = 0; view.selectedWrites = 0
            }
            if let mode = cmd["valueMode"] as? String { view.valueMode = mode }
            if let flag = cmd["valueWritable"] as? Bool { view.valueWritable = flag }
            if let flag = cmd["selectedWritable"] as? Bool { view.selectedWritable = flag }
            if let flag = cmd["rejectSelectedWrite"] as? Bool { view.rejectSelectedWrite = flag }
            if let flag = cmd["rejectValueWrite"] as? Bool { view.rejectValueWrite = flag }
            if let flag = cmd["rejectCaretWrite"] as? Bool { view.rejectCaretWrite = flag }
            if let flag = cmd["moveFocusOnProbe"] as? Bool { view.moveFocusOnProbe = flag }
            app.hideFocus = cmd["target"] as? String == "none"
            target = cmd["target"] as? String ?? "view"
            if target == "savePanel" && sheet == nil {
                let panel = NSSavePanel()
                panel.directoryURL = stateURL.deletingLastPathComponent()
                panel.nameFieldStringValue = "recovery.txt"
                panel.beginSheetModal(for: window) { _ in }
                sheet = panel
            }
            if target == "sheet" && sheet == nil {
                let panel = NSPanel(contentRect: NSRect(x: 0, y: 0, width: 360, height: 130),
                    styleMask: [.titled], backing: .buffered, defer: false)
                panel.isReleasedWhenClosed = false
                panel.contentView!.addSubview(sheetField)
                window.beginSheet(panel)
                sheet = panel
            }
            if target == "other" && otherWindow == nil {
                let other = NSWindow(contentRect: NSRect(x: 220, y: 220, width: 380, height: 180),
                    styleMask: [.titled], backing: .buffered, defer: false)
                other.isReleasedWhenClosed = false
                other.title = "Other document"
                otherField.setAccessibilityLabel("Other document input")
                other.contentView!.addSubview(otherField)
                otherWindow = other
            }
            let owner = target == "other" ? otherWindow! : target == "sheet" ? sheet! : window
            let input: NSView = target == "other" ? otherField : target == "field" ? field : target == "sheet" ? sheetField : view
            if let text = cmd["text"] as? String {
                if input === view { view.string = text } else { (input as! NSTextField).stringValue = text }
            }
            if target != "savePanel" { owner.makeFirstResponder(input) }
            if target == "other" { owner.makeKeyAndOrderFront(nil) }
            if let range = cmd["selection"] as? [Int], range.count == 2 {
                let selection = NSRange(location: range[0], length: range[1])
                if input === view { view.setSelectedRange(selection) }
                else { (input as! NSTextField).currentEditor()?.selectedRange = selection }
            }
            serial = next
        }
        let range = view.selectedRange()
        let data = try! JSONSerialization.data(withJSONObject: [
            "serial": serial, "view": view.string, "field": field.stringValue, "sheet": sheetField.stringValue,
            "windowID": window.windowNumber, "sheetWindowID": sheet?.windowNumber ?? 0,
            "otherWindowID": otherWindow?.windowNumber ?? 0,
            "selection": [range.location, range.length], "target": target,
            "valueWrites": view.valueWrites, "selectedWrites": view.selectedWrites])
        try? data.write(to: stateURL, options: .atomic)
    }
}
let app = TextInputApplication.shared as! TextInputApplication
app.setActivationPolicy(.accessory)
let controller = Controller(app: app)
Timer.scheduledTimer(withTimeInterval: 240, repeats: false) { _ in app.terminate(nil) }
app.run()
