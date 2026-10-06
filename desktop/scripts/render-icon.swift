// Renders an icon master (SVG or PNG) to a 1024px PNG, optionally with the "DEV" badge.
// Usage: swift scripts/render-icon.swift <source> <output-1024.png> [--dev]
import AppKit

let args = CommandLine.arguments
guard args.count >= 3, let source = NSImage(contentsOfFile: args[1]) else {
    FileHandle.standardError.write("usage: render-icon.swift <source> <output.png> [--dev]\n".data(using: .utf8)!)
    exit(2)
}
let size = 1024.0
let rep = NSBitmapImageRep(bitmapDataPlanes: nil, pixelsWide: Int(size), pixelsHigh: Int(size), bitsPerSample: 8, samplesPerPixel: 4, hasAlpha: true, isPlanar: false, colorSpaceName: .deviceRGB, bytesPerRow: 0, bitsPerPixel: 0)!
NSGraphicsContext.saveGraphicsState()
NSGraphicsContext.current = NSGraphicsContext(bitmapImageRep: rep)
NSGraphicsContext.current?.imageInterpolation = .high
source.draw(in: NSRect(x: 0, y: 0, width: size, height: size))

if args.dropFirst(3).contains("--dev") {
    let badge = NSRect(x: size - 470, y: size - 230, width: 440, height: 200)
    let path = NSBezierPath(roundedRect: badge, xRadius: 56, yRadius: 56)
    NSColor(red: 0.85, green: 0.42, blue: 0.16, alpha: 1).setFill()
    path.fill()
    NSColor.white.setStroke()
    path.lineWidth = 14
    path.stroke()
    let style = NSMutableParagraphStyle()
    style.alignment = .center
    let text = NSAttributedString(string: "DEV", attributes: [
        .font: NSFont.systemFont(ofSize: 150, weight: .heavy),
        .foregroundColor: NSColor.white,
        .paragraphStyle: style,
    ])
    let height = text.size().height
    text.draw(in: NSRect(x: badge.minX, y: badge.midY - height / 2, width: badge.width, height: height))
}
NSGraphicsContext.restoreGraphicsState()
try! rep.representation(using: .png, properties: [:])!.write(to: URL(fileURLWithPath: args[2]))
