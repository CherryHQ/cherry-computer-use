import CoreGraphics
import Foundation

struct SDKObservationOptions {
    let observation: SnapshotObservationOptions
    let image: ImageCaptureConfig
    let windowID: CGWindowID?
    let placement: String

    init(_ params: [String: Any]) throws {
        let invalid = SDKDomainError("INVALID_ARGUMENT", "Invalid observation parameters")
        guard Set(params.keys).isSubset(of: ["appSessionId", "activation", "maxTreeNodes", "maxTreeDepth", "textLimit", "includeScreenshot", "includeAX", "screenshot", "windowId", "windowPlacement"]),
              let session = params["appSessionId"] as? String, !session.isEmpty else { throw invalid }
        if let activation = params["activation"], !["never", "allow"].contains(activation as? String ?? "") { throw invalid }
        observation = SnapshotObservationOptions(includeAX: try sdkBoolean(params["includeAX"], fallback: true),
                                                 includeScreenshot: try sdkBoolean(params["includeScreenshot"], fallback: true))
        guard observation.includeAX || observation.includeScreenshot else { throw invalid }
        for key in ["maxTreeNodes", "maxTreeDepth", "textLimit"] where params[key] != nil {
            if key == "textLimit", params[key] as? String == "max" { continue }
            _ = try sdkPositiveInteger(params[key], fallback: 1)
        }
        windowID = try sdkWindowID(params["windowId"])
        placement = params["windowPlacement"] as? String ?? "keep"
        guard ["keep", "agentDisplay", "restore"].contains(placement), params["windowPlacement"] == nil || params["windowPlacement"] is String else { throw invalid }
        var image = ImageCaptureConfig.defaults
        if let value = params["screenshot"] {
            guard observation.includeScreenshot, let settings = value as? [String: Any],
                  Set(settings.keys).isSubset(of: ["format", "maxDimension", "jpegQuality"]) else { throw invalid }
            if let format = settings["format"] {
                guard let format = format as? String, ["png", "jpeg", "webp"].contains(format) else { throw invalid }
                image.format = format == "jpeg" ? "jpg" : format
            }
            if let maximum = settings["maxDimension"] {
                let maximum = try sdkPositiveInteger(maximum, fallback: 1280)
                guard maximum <= 16384 else { throw invalid }
                image.maxDimension = CGFloat(maximum)
            }
            if let quality = settings["jpegQuality"] {
                guard let value = quality as? NSNumber, CFGetTypeID(value) != CFBooleanGetTypeID(),
                      value.doubleValue.isFinite, (0...1).contains(value.doubleValue) else { throw invalid }
                image.jpegQuality = value.doubleValue
            }
        }
        self.image = image
    }
}

func sdkBoolean(_ value: Any?, fallback: Bool) throws -> Bool {
    guard let value else { return fallback }
    guard let number = value as? NSNumber, CFGetTypeID(number) == CFBooleanGetTypeID() else {
        throw SDKDomainError("INVALID_ARGUMENT", "Expected a boolean")
    }
    return number.boolValue
}

func sdkPositiveInteger(_ value: Any?, fallback: Int) throws -> Int {
    guard let value else { return fallback }
    guard let number = value as? NSNumber, CFGetTypeID(number) != CFBooleanGetTypeID(),
          number.doubleValue.isFinite, number.doubleValue >= 1, number.doubleValue <= 1e9,
          number.doubleValue == Double(number.intValue) else {
        throw SDKDomainError("INVALID_ARGUMENT", "Expected a positive integer")
    }
    return number.intValue
}

func sdkWindowID(_ value: Any?) throws -> CGWindowID? {
    guard let value else { return nil }
    guard let string = value as? String, let id = UInt32(string), id > 0, String(id) == string else {
        throw SDKDomainError("INVALID_ARGUMENT", "Expected a native window ID from this application")
    }
    return id
}

struct SDKQueryOptions {
    let criteria: TargetedAX.Criteria
    let windowID: CGWindowID?
    init(_ params: [String: Any]) throws {
        let invalid = SDKDomainError("INVALID_ARGUMENT", "Query requires text or role and bounded search options")
        guard Set(params.keys).isSubset(of: ["appSessionId", "text", "role", "exact", "limit", "maxNodes", "windowId"]),
              let session = params["appSessionId"] as? String, !session.isEmpty else { throw invalid }
        for key in ["text", "role"] where params[key] != nil {
            guard let value = params[key] as? String, !value.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty,
                  value.utf16.count <= 1000 else { throw invalid }
        }
        guard params["text"] != nil || params["role"] != nil else { throw invalid }
        let limit = try sdkPositiveInteger(params["limit"], fallback: 20)
        let maxNodes = try sdkPositiveInteger(params["maxNodes"], fallback: 500)
        guard limit <= 100, maxNodes <= 5000 else { throw invalid }
        criteria = TargetedAX.Criteria(text: params["text"] as? String, exact: try sdkBoolean(params["exact"], fallback: false),
                                       role: params["role"] as? String, limit: limit, maxNodes: maxNodes)
        windowID = try sdkWindowID(params["windowId"])
    }
}
