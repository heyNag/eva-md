// Draws eva-md's plugin icon, an original brain illustration, on macOS.
// Regenerate internal/install/templates/icon.png with:
//
//	swift scripts/icon/brain.swift /tmp/brain.png 1024
//	sips -Z 256 /tmp/brain.png --out internal/install/templates/icon.png
//
import AppKit

// An original, softly shaded brain in side view, drawn with plain shapes:
// a pink cerebrum with raised folds, a darker cerebellum below the back.
let out = CommandLine.arguments[1], px = Int(CommandLine.arguments[2])!
let s = CGFloat(px) / 256
guard let rep = NSBitmapImageRep(bitmapDataPlanes: nil, pixelsWide: px, pixelsHigh: px, bitsPerSample: 8, samplesPerPixel: 4,
                                 hasAlpha: true, isPlanar: false, colorSpaceName: .deviceRGB, bytesPerRow: 0, bitsPerPixel: 0) else { exit(1) }
NSGraphicsContext.saveGraphicsState()
let ctx = NSGraphicsContext(bitmapImageRep: rep)!
NSGraphicsContext.current = ctx
let cg = ctx.cgContext
cg.translateBy(x: 0, y: CGFloat(px)); cg.scaleBy(x: s, y: -s) // top-left origin, 256 units

func rgb(_ hex: Int, _ a: CGFloat = 1) -> CGColor {
    CGColor(srgbRed: CGFloat((hex >> 16) & 255) / 255, green: CGFloat((hex >> 8) & 255) / 255, blue: CGFloat(hex & 255) / 255, alpha: a)
}
// The union of the circles, as one outline.
func circles(_ cs: [(CGFloat, CGFloat, CGFloat)]) -> CGPath {
    var u: CGPath = CGPath(ellipseIn: CGRect(x: cs[0].0 - cs[0].2, y: cs[0].1 - cs[0].2, width: 2 * cs[0].2, height: 2 * cs[0].2), transform: nil)
    for (x, y, r) in cs.dropFirst() {
        u = u.union(CGPath(ellipseIn: CGRect(x: x - r, y: y - r, width: 2 * r, height: 2 * r), transform: nil))
    }
    return u
}
func radial(_ path: CGPath, center: CGPoint, radius: CGFloat, _ stops: [(CGFloat, Int)]) {
    cg.saveGState(); cg.addPath(path); cg.clip()
    let g = CGGradient(colorsSpace: CGColorSpace(name: CGColorSpace.sRGB), colors: stops.map { rgb($0.1) } as CFArray, locations: stops.map { $0.0 })!
    cg.drawRadialGradient(g, startCenter: center, startRadius: 0, endCenter: center, endRadius: radius, options: [.drawsAfterEndLocation])
    cg.restoreGState()
}
// Darken the inside edge of a shape softly, for volume without a hard outline.
func innerShade(_ path: CGPath, color: CGColor, blur: CGFloat) {
    cg.saveGState(); cg.addPath(path); cg.clip()
    let ring = CGMutablePath(); ring.addRect(CGRect(x: -100, y: -100, width: 456, height: 456)); ring.addPath(path)
    cg.setShadow(offset: CGSize(width: 0, height: 0), blur: blur * s, color: color)
    cg.addPath(ring); cg.setFillColor(rgb(0x000000)); cg.fillPath(using: .evenOdd)
    cg.restoreGState()
}
func wiggle(_ pts: [(CGFloat, CGFloat)]) -> CGPath {
    // A smooth curve through the points (Catmull-Rom to Bezier).
    let p = CGMutablePath(); let q = pts.map { CGPoint(x: $0.0, y: $0.1) }
    p.move(to: q[0])
    for i in 0..<(q.count - 1) {
        let p0 = q[max(i - 1, 0)], p1 = q[i], p2 = q[i + 1], p3 = q[min(i + 2, q.count - 1)]
        p.addCurve(to: p2, control1: CGPoint(x: p1.x + (p2.x - p0.x) / 6, y: p1.y + (p2.y - p0.y) / 6),
                   control2: CGPoint(x: p2.x - (p3.x - p1.x) / 6, y: p2.y - (p3.y - p1.y) / 6))
    }
    return p
}
// A groove between folds: a soft dark channel with a light rim on its upper side.
func groove(_ pts: [(CGFloat, CGFloat)], width: CGFloat, clip: CGPath, dark: Int, rim: Int) {
    let path = wiggle(pts)
    cg.saveGState(); cg.addPath(clip); cg.clip()
    cg.setLineCap(.round); cg.setLineJoin(.round)
    cg.saveGState()
    cg.setShadow(offset: CGSize(width: 0.6 * s, height: -1.2 * s), blur: 3.2 * s, color: rgb(0x7E2246, 0.95))
    cg.addPath(path); cg.setStrokeColor(rgb(dark, 0.95)); cg.setLineWidth(width + 0.8); cg.strokePath()
    cg.restoreGState()
    cg.translateBy(x: -1.3, y: -1.6)
    cg.setShadow(offset: .zero, blur: 2 * s, color: rgb(0xFFFFFF, 0.6))
    cg.addPath(path); cg.setStrokeColor(rgb(rim, 0.75)); cg.setLineWidth(width * 0.42); cg.strokePath()
    cg.restoreGState()
}

// Brain stem and cerebellum sit behind the cerebrum.
let stem = CGMutablePath()
stem.move(to: CGPoint(x: 150, y: 170)); stem.addCurve(to: CGPoint(x: 154, y: 222), control1: CGPoint(x: 156, y: 188), control2: CGPoint(x: 158, y: 206))
stem.addQuadCurve(to: CGPoint(x: 170, y: 222), control: CGPoint(x: 162, y: 228))
stem.addCurve(to: CGPoint(x: 174, y: 170), control1: CGPoint(x: 168, y: 206), control2: CGPoint(x: 170, y: 188)); stem.closeSubpath()
radial(stem, center: CGPoint(x: 156, y: 180), radius: 60, [(0, 0xE58AA6), (1, 0xB8456C)])
innerShade(stem, color: rgb(0x8F2C52, 0.8), blur: 6)

let cerebellum = circles([(184, 172, 30), (208, 162, 21)])
radial(cerebellum, center: CGPoint(x: 182, y: 158), radius: 50, [(0, 0xEE9BB3), (0.6, 0xD8698D), (1, 0xB24A70)])
for dy in stride(from: CGFloat(-18), through: 26, by: 8) {
    groove([(156, 168 + dy), (176, 160 + dy), (198, 158 + dy), (222, 163 + dy), (238, 170 + dy)], width: 3.2, clip: cerebellum, dark: 0x9C3560, rim: 0xF6B8C9)
}
innerShade(cerebellum, color: rgb(0x8A2A50, 0.85), blur: 9)

// Cerebrum.
let brain = circles([
    (64, 118, 40), (90, 84, 38), (128, 70, 40), (168, 74, 38), (200, 100, 34), (212, 128, 24),
    (58, 148, 30), (90, 154, 38), (130, 156, 40), (166, 144, 34), (118, 112, 46), (160, 110, 40), (196, 130, 22),
])
radial(brain, center: CGPoint(x: 100, y: 80), radius: 175, [(0, 0xFFD8E2), (0.3, 0xFAB0C4), (0.7, 0xEE88A5), (1, 0xCF5880)])
let dark = 0xC24A74, rim = 0xFFE0E8
func fold(_ pts: [(CGFloat, CGFloat)], _ w: CGFloat = 5.5) { groove(pts, width: w, clip: brain, dark: dark, rim: rim) }
// Lateral fissure, separating the temporal lobe.
groove([(64, 152), (84, 140), (110, 134), (140, 134), (164, 138), (188, 146)], width: 7.5, clip: brain, dark: 0xB03C66, rim: rim)
// Central sulcus.
groove([(136, 34), (132, 48), (140, 60), (134, 76), (142, 92), (138, 108), (142, 124)], width: 6.5, clip: brain, dark: 0xB8426C, rim: rim)
// Frontal lobe: meandering folds with short branches.
fold([(44, 106), (56, 98), (64, 104), (70, 94), (82, 92), (90, 100), (104, 96), (112, 86)])
fold([(82, 92), (84, 80), (94, 74)], 4.5)
fold([(42, 132), (54, 124), (68, 128), (78, 118), (92, 120), (100, 112), (116, 114)])
fold([(78, 118), (76, 106)], 4.5)
fold([(72, 62), (84, 68), (92, 60), (106, 64), (114, 56)], 5)
fold([(52, 84), (62, 78), (70, 84)], 4.5)
fold([(104, 128), (114, 124), (122, 130)], 4.5)
// Parietal and occipital lobes.
fold([(154, 42), (160, 54), (154, 66), (162, 78), (156, 92)])
fold([(162, 78), (174, 82), (180, 74)], 4.5)
fold([(182, 60), (190, 72), (184, 84), (194, 96), (188, 110), (196, 120)])
fold([(184, 84), (172, 92)], 4.5)
fold([(204, 96), (214, 108), (208, 118), (216, 128)], 5)
fold([(150, 104), (162, 110), (170, 104), (176, 116)], 4.5)
// Temporal lobe.
fold([(60, 170), (74, 164), (86, 170), (98, 162), (112, 166), (124, 160), (138, 166), (152, 160)])
fold([(98, 162), (102, 152)], 4.5)
fold([(88, 186), (102, 180), (114, 186), (128, 178), (144, 184), (158, 178)], 5)
// Soft gloss on the upper lobes, lit from the top left.
func gloss(_ x: CGFloat, _ y: CGFloat, _ r: CGFloat, _ a: CGFloat) {
    cg.saveGState(); cg.addPath(brain); cg.clip()
    let g = CGGradient(colorsSpace: CGColorSpace(name: CGColorSpace.sRGB), colors: [rgb(0xFFFFFF, a), rgb(0xFFFFFF, 0)] as CFArray, locations: [0, 1])!
    cg.drawRadialGradient(g, startCenter: CGPoint(x: x, y: y), startRadius: 0, endCenter: CGPoint(x: x, y: y), endRadius: r, options: [])
    cg.restoreGState()
}
gloss(96, 76, 30, 0.45); gloss(150, 58, 20, 0.32); gloss(60, 100, 14, 0.28); gloss(122, 98, 15, 0.24); gloss(176, 72, 13, 0.22)
gloss(200, 112, 11, 0.2); gloss(88, 140, 13, 0.2); gloss(112, 170, 13, 0.18); gloss(150, 122, 11, 0.18)
innerShade(brain, color: rgb(0xA8325E, 0.9), blur: 10)
// A thin, soft edge so the shape reads at small sizes.
cg.saveGState(); cg.addPath(brain); cg.setStrokeColor(rgb(0xB03C66, 0.55)); cg.setLineWidth(2); cg.strokePath(); cg.restoreGState()

NSGraphicsContext.restoreGraphicsState()
try! rep.representation(using: .png, properties: [:])!.write(to: URL(fileURLWithPath: out))
