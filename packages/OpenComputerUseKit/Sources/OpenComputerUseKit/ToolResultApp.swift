import AppKit

public struct ToolResultApp: Sendable {
    public let name: String
    public let bundleIdentifier: String?
    public let iconPNGData: Data?

    init(app: RunningAppDescriptor) {
        name = app.name
        bundleIdentifier = app.bundleIdentifier
        iconPNGData = app.runningApplication.icon.flatMap(Self.iconPNG)
    }

    static func iconPNG(_ image: NSImage) -> Data? {
        guard let source = image.cgImage(forProposedRect: nil, context: nil, hints: nil),
              let context = CGContext(
                data: nil, width: 64, height: 64, bitsPerComponent: 8, bytesPerRow: 0,
                space: CGColorSpaceCreateDeviceRGB(), bitmapInfo: CGImageAlphaInfo.premultipliedLast.rawValue
              ) else { return nil }
        context.interpolationQuality = .high
        context.draw(source, in: CGRect(x: 0, y: 0, width: 64, height: 64))
        guard let scaled = context.makeImage() else { return nil }
        return NSBitmapImageRep(cgImage: scaled).representation(using: .png, properties: [:])
    }

    var dictionary: [String: Any] {
        var result: [String: Any] = ["name": name]
        if let bundleIdentifier { result["bundleIdentifier"] = bundleIdentifier }
        if let iconPNGData {
            result["icon"] = ["mimeType": "image/png", "data": iconPNGData.base64EncodedString()]
        }
        return result
    }
}
