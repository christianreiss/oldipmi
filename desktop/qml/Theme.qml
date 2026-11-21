pragma Singleton
import QtQuick
import org.kde.kirigami as Kirigami

// Design tokens of the OldIPMI interface (DESIGN.md). Every chrome colour is
// derived from the system colour scheme, so light, dark and custom schemes and
// the user's accent colour carry through. Only the console stage is always dark:
// it is the backdrop of the remote screen, not chrome.
Item {
    id: theme

    // Base colours, bound by Main.qml to its Kirigami.Theme: a singleton is in no
    // window, and Kirigami.Theme reads the colour scheme only inside one. The
    // defaults are Breeze Light.
    property color window: "#eff0f1"
    property color text: "#232629"
    property color view: "#ffffff"
    property color accent: "#3daee9"
    property color positive: "#27ae60"
    property color warning: "#f67400"
    property color negative: "#da4453"
    property font mono: Qt.font({family: "monospace"})

    readonly property bool dark: luminance(window) < 0.35

    // Chrome surfaces.
    readonly property color sidebar: dark ? mix(window, black, 0.22) : mix(window, text, 0.04)
    // Raised surfaces: cards, dialogs, the page of the selected connection.
    readonly property color card: dark ? mix(window, white, 0.05) : mix(view, white, 0.6)
    // Tinted band at the bottom of a card (dialog footers).
    readonly property color cardFooter: dark ? mix(card, black, 0.18) : mix(card, text, 0.025)
    // Secondary buttons, tiles, inset boxes.
    readonly property color raised: dark ? mix(window, white, 0.09) : mix(view, text, 0.035)
    readonly property color hover: alpha(text, dark ? 0.08 : 0.06)
    // Dimmed window behind a modal dialog, and the drop shadow of raised cards.
    readonly property color scrim: Qt.rgba(0.07, 0.08, 0.09, dark ? 0.6 : 0.46)
    readonly property color shadow: Qt.rgba(0, 0, 0, dark ? 0.5 : 0.24)
    readonly property color pressed: alpha(text, dark ? 0.14 : 0.11)
    readonly property color line: mix(window, text, dark ? 0.2 : 0.16)
    readonly property color lineSoft: mix(window, text, dark ? 0.13 : 0.1)
    // Secondary text: addresses, captions, group headers (≥ 4.5:1 on window and card).
    readonly property color muted: mix(text, window, dark ? 0.3 : 0.28)

    // Accent. Filled surfaces with white text use a darker step until white reaches 4.6:1.
    readonly property color accentStrong: forWhiteText(accent)
    readonly property color accentTint: alpha(accent, dark ? 0.22 : 0.14)
    readonly property color accentEdge: alpha(accent, dark ? 0.6 : 0.55)
    readonly property color accentText: toneText(accent)
    readonly property color dangerStrong: forWhiteText(negative)

    // The console stage, its status strip and everything drawn on them. These are
    // constant: they frame the remote picture whatever the scheme.
    readonly property color stage: "#0c0d10"
    readonly property color stageBar: "#141619"
    readonly property color stageText: "#e6e8eb"
    readonly property color stageMuted: "#a0a7b0"
    readonly property color stageLine: Qt.rgba(1, 1, 1, 0.09)
    readonly property color stageRaised: Qt.rgba(1, 1, 1, 0.08)
    readonly property color stageHover: Qt.rgba(1, 1, 1, 0.12)
    readonly property color stageSubtle: Qt.rgba(1, 1, 1, 0.04)
    readonly property color stageEdge: Qt.rgba(1, 1, 1, 0.16)
    readonly property color stagePopup: "#1c1e22"

    // Sizes. Everything that holds text follows the grid unit, the height of a line
    // of text (as Kirigami defines it), so a larger system font grows the chrome
    // with it even where Kirigami's own unit stays fixed. Corner radii stay fixed.
    property real fontHeight: 0 // bound by Main.qml, like the colours
    readonly property real gu: Math.max(Kirigami.Units.gridUnit, Math.round(fontHeight))
    readonly property int radiusSmall: 6
    readonly property int radius: 9
    readonly property int radiusLarge: 14
    readonly property int radiusHuge: 16
    readonly property int controlHeight: Math.round(gu * 2)
    readonly property int controlHeightLarge: Math.round(gu * 2.35)
    readonly property int headerHeight: Math.round(gu * 3.35)
    // The stage's status strip: one fixed height whatever it shows, so the remote
    // screen above it never moves.
    readonly property int stripHeight: Math.round(gu * 2.35)
    readonly property int gap: Kirigami.Units.largeSpacing
    readonly property int stageMargin: Math.round(gu * 0.67)

    // Font sizes as factors of the system font.
    property real fontBase: 10 // bound by Main.qml, like the colours
    readonly property real small: fontBase * 0.88
    readonly property real tiny: fontBase * 0.8
    readonly property real title: fontBase * 1.12
    readonly property real heading: fontBase * 1.4
    readonly property real display: fontBase * 2.05
    readonly property real hero: fontBase * 2.3

    // One clock for every elapsed-time label ("Logging in · 0:04"). The self-test
    // stops it at a fixed time (testClock), so its screenshots do not change.
    function clock() { return testClock > 0 ? testClock : Date.now() }
    property double now: clock()
    // The same clock, read once a minute, for labels such as "5 min ago": what
    // binds to it re-evaluates once a minute instead of every second.
    property double minuteNow: clock()
    // False in the self-test: motion that shows time passing (the connecting
    // spinner) stands still with its clock.
    readonly property bool clockRunning: testClock === 0
    Timer {
        interval: 1000
        repeat: true
        running: theme.clockRunning
        onTriggered: theme.now = theme.clock()
    }
    Timer {
        interval: 60000
        repeat: true
        running: theme.clockRunning
        onTriggered: theme.minuteNow = theme.clock()
    }
    // A file path as pages show it. The self-test shows its configuration folder
    // (testConfigDir) as ~/.config, where an installed OldIPMI has it.
    function shownPath(path) {
        return testConfigDir !== "" && path.startsWith(testConfigDir + "/")
            ? "~/.config" + path.slice(testConfigDir.length) : path
    }

    // Clipboard without another import: a hidden text editor copies its selection.
    TextEdit { id: clipboard; visible: false }
    function copyText(value) {
        clipboard.text = value;
        clipboard.selectAll();
        clipboard.copy();
        clipboard.text = "";
    }

    readonly property color black: Qt.rgba(0, 0, 0, 1)
    readonly property color white: Qt.rgba(1, 1, 1, 1)
    // Channel by channel in sRGB: unlike ColorUtils.linearInterpolation (HSL), a
    // colour mixed towards black or white keeps its hue and saturation.
    function mix(a, b, t) {
        return Qt.rgba(a.r + (b.r - a.r) * t, a.g + (b.g - a.g) * t, a.b + (b.b - a.b) * t, a.a + (b.a - a.a) * t);
    }
    function alpha(c, a) { return Qt.rgba(c.r, c.g, c.b, a) }
    function luminance(c) {
        const f = v => v <= 0.03928 ? v / 12.92 : Math.pow((v + 0.055) / 1.055, 2.4);
        return 0.2126 * f(c.r) + 0.7152 * f(c.g) + 0.0722 * f(c.b);
    }
    function contrast(a, b) {
        const la = luminance(a), lb = luminance(b);
        return (Math.max(la, lb) + 0.05) / (Math.min(la, lb) + 0.05);
    }
    // The colour, darkened until white text on it reaches 4.6:1.
    function forWhiteText(c) {
        let out = c;
        for (let t = 0.05; t <= 0.9 && contrast(out, white) < 4.6; t += 0.05)
            out = mix(c, black, t);
        return out;
    }
    // A state colour as text on a tint of itself (chips): moved towards the text
    // colour until it reaches 4.8:1 on the surface.
    function toneText(c, surface) {
        const bg = surface === undefined ? (dark ? card : view) : surface;
        let out = c;
        for (let t = 0.1; t <= 0.9 && contrast(out, bg) < 4.8; t += 0.1)
            out = mix(c, text, t);
        return out;
    }
    // The same on the dark stage.
    function stageTone(c) { return mix(c, white, 0.62) }
    // Elapsed time as m:ss (or h:mm:ss).
    function elapsed(since) {
        if (!since) return "";
        const s = Math.max(0, Math.floor((now - since) / 1000));
        const pad = n => (n < 10 ? "0" : "") + n;
        return s >= 3600 ? Math.floor(s / 3600) + ":" + pad(Math.floor(s / 60) % 60) + ":" + pad(s % 60)
                         : Math.floor(s / 60) + ":" + pad(s % 60);
    }
}
