import CoreGraphics

enum WindowCaptureGeometry {
    static func isValid(_ bounds: CGRect) -> Bool {
        !bounds.isInfinite && !bounds.isNull
            && [bounds.origin.x, bounds.origin.y, bounds.width, bounds.height, bounds.maxX, bounds.maxY].allSatisfy(\.isFinite)
            && bounds.size.width > 0 && bounds.size.height > 0
    }

    static func matchesFullWindow(pixelSize: CGSize, bounds: CGRect) -> Bool {
        guard isValid(bounds), pixelSize.width.isFinite, pixelSize.height.isFinite,
              pixelSize.width > 0, pixelSize.height > 0 else { return false }
        // Allow pixel rounding, but reject captures cropped to a different aspect ratio.
        let scale = min(pixelSize.width / bounds.width, pixelSize.height / bounds.height)
        return abs(pixelSize.width - bounds.width * scale) <= 2
            && abs(pixelSize.height - bounds.height * scale) <= 2
    }
}
