import ApplicationServices
import Foundation

/// Scrolls without input events by asking accessibility to reveal the element just past the
/// viewport edge. Chromium windows drop background wheel events but honor `AXScrollToVisible`.
/// Target selection is adapted from Cua Driver's `reveal_scroll.rs` (MIT, trycua/cua@50d1e84).
enum RevealScroll {
    enum Direction: String {
        case up, down, left, right

        var isVertical: Bool { self == .up || self == .down }
    }

    struct Node: Equatable {
        let frame: CGRect
        let isLeaf: Bool
    }

    struct ClampedPick: Equatable {
        let index: Int
        let rows: Int
        let pitch: CGFloat
    }

    static let containerRoles: Set<String> = ["AXScrollArea", "AXWebArea"]
    static let maxNodes = 4_000
    static let maxDepth = 80
    static let maxAscent = 40
    private static let sliverThickness: CGFloat = 1.5

    /// The element whose far edge lies closest to `distance` past the viewport edge.
    /// Elements longer than the viewport are containers and are skipped.
    static func pickRevealTarget(viewport: CGRect, direction: Direction, distance: CGFloat, candidates: [CGRect]) -> Int? {
        var best: (index: Int, gap: CGFloat)?
        for (index, rect) in candidates.enumerated() where !isSliver(rect) {
            let edge: CGFloat
            let goal: CGFloat
            let beyond: Bool
            let fits: Bool
            switch direction {
            case .down:
                (edge, goal, beyond, fits) = (rect.maxY, viewport.maxY + distance, rect.maxY > viewport.maxY + 1, rect.height <= viewport.height)
            case .up:
                (edge, goal, beyond, fits) = (rect.minY, viewport.minY - distance, rect.minY < viewport.minY - 1, rect.height <= viewport.height)
            case .right:
                (edge, goal, beyond, fits) = (rect.maxX, viewport.maxX + distance, rect.maxX > viewport.maxX + 1, rect.width <= viewport.width)
            case .left:
                (edge, goal, beyond, fits) = (rect.minX, viewport.minX - distance, rect.minX < viewport.minX - 1, rect.width <= viewport.width)
            }
            guard beyond, fits, overlapsCrossAxis(rect, viewport, direction) else { continue }
            let gap = abs(edge - goal)
            if gap < best?.gap ?? .infinity { best = (index, gap) }
        }
        return best?.index
    }

    /// Chromium reports off-screen elements as slivers clamped to the viewport edge and centers
    /// the element it reveals, so revealing the k-th clamped leaf moves the content about half a
    /// viewport plus (k - 0.5) rows. Row pitch is the median gap between visible leaves.
    static func pickClamped(viewport: CGRect, direction: Direction, distance: CGFloat, nodes: [Node]) -> ClampedPick? {
        let vertical = direction.isVertical
        func sliver(_ rect: CGRect) -> Bool { (vertical ? rect.height : rect.width) <= sliverThickness }
        func atEdge(_ rect: CGRect) -> Bool {
            switch direction {
            case .down: return abs(rect.maxY - viewport.maxY) <= 1.5
            case .up: return abs(rect.minY - viewport.minY) <= 1.5
            case .right: return abs(rect.maxX - viewport.maxX) <= 1.5
            case .left: return abs(rect.minX - viewport.minX) <= 1.5
            }
        }
        var clamped = nodes.indices.filter {
            let node = nodes[$0]
            return node.isLeaf && sliver(node.frame) && atEdge(node.frame)
                && overlapsCrossAxis(node.frame, viewport, direction)
        }
        guard !clamped.isEmpty else { return nil }
        if direction == .up || direction == .left { clamped.reverse() }

        var starts = nodes
            .filter { $0.isLeaf && !sliver($0.frame) && viewport.contains($0.frame) }
            .map { vertical ? $0.frame.minY : $0.frame.minX }
            .sorted()
        starts = starts.reduce(into: []) { kept, start in
            if let last = kept.last, abs(start - last) < 2 { return }
            kept.append(start)
        }
        let gaps = zip(starts.dropFirst(), starts).map { $0 - $1 }.sorted()
        let pitch = max(gaps.isEmpty ? 40 : gaps[gaps.count / 2], 8)
        let half = (vertical ? viewport.height : viewport.width) / 2
        let requested = ((distance - half) / pitch + 0.5).rounded()
        let rows = Int(min(max(requested, 1), CGFloat(clamped.count)))
        return ClampedPick(index: clamped[rows - 1], rows: rows, pitch: pitch)
    }

    static func moved(before: CGRect, after: CGRect, viewport: CGRect, direction: Direction) -> Bool {
        guard !isSliver(after), after.intersects(viewport) else { return false }
        switch direction {
        case .down: return before.minY - after.minY > 0.5
        case .up: return after.minY - before.minY > 0.5
        case .right: return before.minX - after.minX > 0.5
        case .left: return after.minX - before.minX > 0.5
        }
    }

    private static func overlapsCrossAxis(_ rect: CGRect, _ viewport: CGRect, _ direction: Direction) -> Bool {
        direction.isVertical
            ? rect.maxX > viewport.minX && rect.minX < viewport.maxX
            : rect.maxY > viewport.minY && rect.minY < viewport.maxY
    }

    /// Captures the same container for selection and verification across all delivery paths.
    struct Context {
        let container: AXUIElement
        let viewport: CGRect
        let direction: Direction
        private let elements: [AXUIElement]
        private let nodes: [Node]
        private let anchors: [Int]
        private let scrollbar: AXUIElement?
        private let scrollValue: Double?
        private let windowElement: AXUIElement?
        private let windowFrame: CGRect?

        init?(target: AXUIElement, window: CGRect?, direction: Direction) {
            guard let container = RevealScroll.container(for: target), let bounds = frame(of: container) else { return nil }
            let viewport = window.map { bounds.intersection($0) } ?? bounds
            guard !viewport.isNull, !isSliver(viewport) else { return nil }
            self.container = container
            self.viewport = viewport
            self.direction = direction
            windowElement = element(container, kAXWindowAttribute)
            windowFrame = windowElement.flatMap { frame(of: $0) }
            scrollbar = element(container, direction.isVertical ? kAXVerticalScrollBarAttribute : kAXHorizontalScrollBarAttribute)
            scrollValue = scrollbar.flatMap { attribute($0, kAXValueAttribute) as? Double }

            var elements: [AXUIElement] = []
            var nodes: [Node] = []
            var stack = children(of: container).reversed().map { ($0, 1) }
            var visited = 0
            while visited < maxNodes, let (child, depth) = stack.popLast() {
                visited += 1
                // A nested scroll area belongs to a different scroll operation.
                guard !containerRoles.contains(string(child, kAXRoleAttribute) ?? "") else { continue }
                let kids = children(of: child)
                if depth < maxDepth { stack.append(contentsOf: kids.reversed().map { ($0, depth + 1) }) }
                if let rect = frame(of: child) {
                    elements.append(child)
                    nodes.append(Node(frame: rect, isLeaf: kids.isEmpty))
                }
            }
            self.elements = elements
            self.nodes = nodes
            anchors = Array(nodes.indices.filter { nodes[$0].isLeaf && !isSliver(nodes[$0].frame) && nodes[$0].frame.intersects(viewport) }.prefix(64))
        }

        func hasMoved(revealed index: Int? = nil) -> Bool {
            if let windowElement, let windowFrame, frame(of: windowElement) != windowFrame { return false }
            if let scrollbar, let scrollValue, let value = attribute(scrollbar, kAXValueAttribute) as? Double {
                let delta = value - scrollValue
                if direction == .down || direction == .right ? delta > 0.000001 : delta < -0.000001 { return true }
            }
            let indices = index.map { anchors + [$0] } ?? anchors
            return indices.contains { index in
                guard let after = frame(of: elements[index]) else { return false }
                return moved(before: nodes[index].frame, after: after, viewport: viewport, direction: direction)
            }
        }

        func reveal(pages: Double) -> Bool {
            let distance = (direction.isVertical ? viewport.height : viewport.width) * CGFloat(pages)
            let index = pickRevealTarget(viewport: viewport, direction: direction, distance: distance, candidates: nodes.map(\.frame))
                ?? pickClamped(viewport: viewport, direction: direction, distance: distance, nodes: nodes)?.index
            guard let index else { return false }
            _ = AXUIElementPerformAction(elements[index], "AXScrollToVisible" as CFString)
            Thread.sleep(forTimeInterval: 0.15)
            return hasMoved(revealed: index)
        }
    }

    private static func container(for target: AXUIElement) -> AXUIElement? {
        if let bounds = frame(of: target), !isSliver(bounds), !children(of: target).isEmpty { return target }
        var current = target
        for _ in 0..<maxAscent {
            guard let parent = element(current, kAXParentAttribute) else { break }
            let role = string(parent, kAXRoleAttribute)
            if role == kAXWindowRole as String || role == kAXApplicationRole as String { break }
            if let role, containerRoles.contains(role) { return parent }
            current = parent
        }
        return nil
    }

    private static func isSliver(_ rect: CGRect) -> Bool {
        rect.width <= sliverThickness || rect.height <= sliverThickness
    }

    private static func attribute(_ element: AXUIElement, _ name: String) -> CFTypeRef? {
        var value: CFTypeRef?
        return AXUIElementCopyAttributeValue(element, name as CFString, &value) == .success ? value : nil
    }

    private static func element(_ element: AXUIElement, _ name: String) -> AXUIElement? {
        guard let value = attribute(element, name), CFGetTypeID(value) == AXUIElementGetTypeID() else { return nil }
        return (value as! AXUIElement)
    }

    private static func string(_ element: AXUIElement, _ name: String) -> String? {
        attribute(element, name) as? String
    }

    private static func children(of element: AXUIElement) -> [AXUIElement] {
        attribute(element, kAXChildrenAttribute) as? [AXUIElement] ?? []
    }

    private static func frame(of element: AXUIElement) -> CGRect? {
        guard let position = attribute(element, kAXPositionAttribute), let size = attribute(element, kAXSizeAttribute),
              CFGetTypeID(position) == AXValueGetTypeID(), CFGetTypeID(size) == AXValueGetTypeID() else { return nil }
        var origin = CGPoint.zero
        var dimensions = CGSize.zero
        guard AXValueGetValue(position as! AXValue, .cgPoint, &origin),
              AXValueGetValue(size as! AXValue, .cgSize, &dimensions) else { return nil }
        return CGRect(origin: origin, size: dimensions)
    }
}
