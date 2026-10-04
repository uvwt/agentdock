import AppKit
import SwiftUI

/// AgentDock 品牌标志的原生矢量几何。
///
/// 坐标来自仓库内 1024×1024 品牌源图的轮廓，并归一化到 0...1。
/// 控制面板与菜单栏共用这一份几何，避免小尺寸 PNG 缩放产生锯齿或两套 Logo 漂移。
enum AgentDockLogoArtwork {
    private static let cyan = Color(red: 0.0, green: 226.0 / 255.0, blue: 252.0 / 255.0)
    private static let blue = Color(red: 0.0, green: 142.0 / 255.0, blue: 252.0 / 255.0)

    private static let artworkBounds = CGRect(x: 0.07324, y: 0.18555, width: 0.81934, height: 0.62695)

    private static let cyanPolygons: [[CGPoint]] = [
        // A 左侧笔画
        [
            .init(x: 0.27100, y: 0.68945), .init(x: 0.21240, y: 0.66992),
            .init(x: 0.17188, y: 0.63818), .init(x: 0.43750, y: 0.20264),
            .init(x: 0.46338, y: 0.18555), .init(x: 0.50146, y: 0.18555),
            .init(x: 0.52930, y: 0.20459), .init(x: 0.54785, y: 0.23486),
            .init(x: 0.27148, y: 0.68896),
        ],
    ]

    private static let bluePolygons: [[CGPoint]] = [
        // A 右侧笔画
        [
            .init(x: 0.70068, y: 0.68945), .init(x: 0.69189, y: 0.68848),
            .init(x: 0.68164, y: 0.67432), .init(x: 0.48242, y: 0.33936),
            .init(x: 0.54590, y: 0.23584), .init(x: 0.79297, y: 0.63721),
            .init(x: 0.77783, y: 0.65332), .init(x: 0.75244, y: 0.66992),
            .init(x: 0.70117, y: 0.68896),
        ],
        // 左侧环形轨道
        [
            .init(x: 0.41064, y: 0.81250), .init(x: 0.32471, y: 0.80273),
            .init(x: 0.24658, y: 0.78418), .init(x: 0.18115, y: 0.75977),
            .init(x: 0.12842, y: 0.72949), .init(x: 0.09277, y: 0.69580),
            .init(x: 0.07422, y: 0.65771), .init(x: 0.07520, y: 0.62256),
            .init(x: 0.09668, y: 0.58350), .init(x: 0.14795, y: 0.54102),
            .init(x: 0.22119, y: 0.50781), .init(x: 0.20605, y: 0.53467),
            .init(x: 0.16357, y: 0.56152), .init(x: 0.13867, y: 0.59033),
            .init(x: 0.13184, y: 0.62451), .init(x: 0.14062, y: 0.64795),
            .init(x: 0.16064, y: 0.67090), .init(x: 0.18213, y: 0.68652),
            .init(x: 0.24072, y: 0.71387), .init(x: 0.32178, y: 0.73535),
            .init(x: 0.41162, y: 0.74609), .init(x: 0.39551, y: 0.77490),
            .init(x: 0.39941, y: 0.79736), .init(x: 0.41113, y: 0.81201),
        ],
        // 右侧环形轨道
        [
            .init(x: 0.56885, y: 0.81250), .init(x: 0.55469, y: 0.81201),
            .init(x: 0.56836, y: 0.79053), .init(x: 0.56934, y: 0.77393),
            .init(x: 0.55322, y: 0.74609), .init(x: 0.65479, y: 0.73145),
            .init(x: 0.73975, y: 0.70703), .init(x: 0.79150, y: 0.68066),
            .init(x: 0.82520, y: 0.64697), .init(x: 0.83301, y: 0.62646),
            .init(x: 0.83301, y: 0.60791), .init(x: 0.82715, y: 0.59131),
            .init(x: 0.80029, y: 0.56055), .init(x: 0.75928, y: 0.53516),
            .init(x: 0.74365, y: 0.50781), .init(x: 0.78271, y: 0.52246),
            .init(x: 0.82178, y: 0.54395), .init(x: 0.84912, y: 0.56445),
            .init(x: 0.87695, y: 0.59521), .init(x: 0.89062, y: 0.62549),
            .init(x: 0.89258, y: 0.64697), .init(x: 0.88574, y: 0.67432),
            .init(x: 0.86719, y: 0.70264), .init(x: 0.82568, y: 0.73730),
            .init(x: 0.75146, y: 0.77344), .init(x: 0.65771, y: 0.79883),
            .init(x: 0.56934, y: 0.81201),
        ],
    ]

    static func coloredPaths(in bounds: CGRect) -> [(CGPath, Color)] {
        let drawingRect = fittedDrawingRect(in: bounds)
        var result = bluePolygons.map { (path(for: $0, in: drawingRect), blue) }
        result.append(contentsOf: cyanPolygons.map { (path(for: $0, in: drawingRect), cyan) })

        // 中心圆点与底部短横是规则几何，用原生曲线绘制比多边形拟合更平滑。
        result.append((ellipsePath(normalized: CGRect(x: 0.41602, y: 0.53125, width: 0.13281, height: 0.13184), in: drawingRect), cyan))
        result.append((roundedRectPath(normalized: CGRect(x: 0.41992, y: 0.76270, width: 0.12500, height: 0.04101), radius: 0.0205, in: drawingRect), cyan))
        return result
    }

    static func monochromePaths(in bounds: CGRect) -> [CGPath] {
        coloredPaths(in: bounds).map(\.0)
    }

    /// 菜单栏使用 Template Image；macOS 会根据菜单栏外观自动用黑/白着色。
    static func menuBarImage() -> NSImage {
        let size = NSSize(width: 18, height: 18)
        let image = NSImage(size: size, flipped: true) { rect in
            guard let context = NSGraphicsContext.current?.cgContext else { return false }
            context.setFillColor(NSColor.black.cgColor)
            for path in monochromePaths(in: rect.insetBy(dx: 0.75, dy: 0.75)) {
                context.addPath(path)
                context.fillPath()
            }
            return true
        }
        image.isTemplate = true
        image.accessibilityDescription = "AgentDock"
        return image
    }

    private static func fittedDrawingRect(in bounds: CGRect) -> CGRect {
        let available = bounds.insetBy(dx: bounds.width * 0.04, dy: bounds.height * 0.04)
        let scale = min(available.width / artworkBounds.width, available.height / artworkBounds.height)
        let width = artworkBounds.width * scale
        let height = artworkBounds.height * scale
        return CGRect(
            x: available.midX - width / 2,
            y: available.midY - height / 2,
            width: width,
            height: height
        )
    }

    private static func point(_ point: CGPoint, in rect: CGRect) -> CGPoint {
        CGPoint(
            x: rect.minX + ((point.x - artworkBounds.minX) / artworkBounds.width) * rect.width,
            y: rect.minY + ((point.y - artworkBounds.minY) / artworkBounds.height) * rect.height
        )
    }

    private static func path(for polygon: [CGPoint], in rect: CGRect) -> CGPath {
        let path = CGMutablePath()
        guard let first = polygon.first else { return path }
        path.move(to: point(first, in: rect))
        for value in polygon.dropFirst() {
            path.addLine(to: point(value, in: rect))
        }
        path.closeSubpath()
        return path
    }

    private static func ellipsePath(normalized: CGRect, in rect: CGRect) -> CGPath {
        CGPath(ellipseIn: mapped(normalized, to: rect), transform: nil)
    }

    private static func roundedRectPath(normalized: CGRect, radius: CGFloat, in rect: CGRect) -> CGPath {
        let mappedRect = mapped(normalized, to: rect)
        let mappedRadius = radius / artworkBounds.width * rect.width
        return CGPath(
            roundedRect: mappedRect,
            cornerWidth: mappedRadius,
            cornerHeight: mappedRadius,
            transform: nil
        )
    }

    private static func mapped(_ normalized: CGRect, to rect: CGRect) -> CGRect {
        let origin = point(normalized.origin, in: rect)
        return CGRect(
            x: origin.x,
            y: origin.y,
            width: normalized.width / artworkBounds.width * rect.width,
            height: normalized.height / artworkBounds.height * rect.height
        )
    }
}

struct AgentDockLogoView: View {
    let size: CGFloat

    var body: some View {
        Canvas { context, canvasSize in
            let bounds = CGRect(origin: .zero, size: canvasSize)
            for (path, color) in AgentDockLogoArtwork.coloredPaths(in: bounds) {
                context.fill(Path(path), with: .color(color))
            }
        }
        .frame(width: size, height: size)
        .accessibilityHidden(true)
    }
}
