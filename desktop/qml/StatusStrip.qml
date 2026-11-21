import QtQuick
import QtQuick.Controls as QQC2
import QtQuick.Layouts
import org.kde.kirigami as Kirigami

// The status strip under the remote screen: capture state, release, media and
// resolution. One fixed height whatever it shows, so the picture above never
// moves. The console pane places it on the stage.
Rectangle {
    id: strip
    // Whether keyboard and mouse go to the server; the strip asks to change it.
    required property bool captured
    required property string displayName
    // What to say where only the mouse releases (Right Ctrl is off).
    required property string mouseOnlyRelease
    signal captureRequested()
    signal releaseRequested()

    // Shorter words when the stage is narrow. Then, in order of
    // importance, the capture state, Release, how to release, and the
    // resolution: what does not fit whole is left out, not clipped.
    readonly property bool compact: width < Theme.gu * 24
    readonly property real room: width - 2 * Math.round(Theme.gu * 0.75)
    readonly property real gap: Math.round(Theme.gu * 0.6)
    // Chip, Release and the spacer after it, with the gaps between them.
    readonly property real essentials: Math.ceil(captureChip.implicitWidth) + gap + releaseButton.implicitWidth + gap
        + (isoChip.visible ? gap + Math.ceil(isoChip.implicitWidth) : 0)
    readonly property bool showHint: strip.captured && settings.rightCtrlReleases && !compact
        && room >= essentials + gap + Math.ceil(hintLabel.implicitWidth)
    readonly property bool showResolution: !compact && resolutionLabel.text !== ""
        && room >= essentials + (showHint ? gap + Math.ceil(hintLabel.implicitWidth) : 0) + gap + Math.ceil(resolutionLabel.implicitWidth)
    height: Theme.stripHeight
    color: Theme.stageBar
    Rectangle {
        anchors { left: parent.left; right: parent.right; top: parent.top }
        height: parent.radius
        color: parent.color
    }
    Rectangle {
        anchors { left: parent.left; right: parent.right; top: parent.top }
        height: 1
        color: Theme.stageLine
    }
    RowLayout {
        anchors {
            fill: parent
            leftMargin: Math.round(Theme.gu * 0.75)
            rightMargin: Math.round(Theme.gu * 0.75)
        }
        spacing: strip.gap
        // The capture state. While free it is also the keyboard way in:
        // Tab to it, then Enter or Space sends keyboard and mouse to the server.
        QQC2.AbstractButton {
            id: captureButton
            objectName: "captureButton"
            enabled: !strip.captured
            focusPolicy: Qt.StrongFocus
            hoverEnabled: true
            padding: 0
            implicitWidth: captureChip.implicitWidth
            implicitHeight: captureChip.implicitHeight
            Layout.fillWidth: true
            Layout.maximumWidth: Math.ceil(implicitWidth)
            Accessible.role: strip.captured ? Accessible.StaticText : Accessible.Button
            Accessible.name: strip.captured ? captureChip.text : "Control " + strip.displayName + " with keyboard and mouse"
            // Without the Right Ctrl release only the mouse releases: say so
            // before the keyboard goes to the server.
            Accessible.description: settings.rightCtrlReleases ? "" : strip.mouseOnlyRelease
            QQC2.ToolTip.visible: visualFocus && !strip.captured && !settings.rightCtrlReleases
            QQC2.ToolTip.text: strip.mouseOnlyRelease
            Keys.onReturnPressed: captureButton.clicked()
            Keys.onEnterPressed: captureButton.clicked()
            onClicked: strip.captureRequested()
            HoverHandler { enabled: captureButton.enabled; cursorShape: Qt.PointingHandCursor }
            contentItem: Chip {
                id: captureChip
                onStage: true
                neutral: !strip.captured
                tone: Theme.accent
                iconName: strip.captured ? "input-keyboard" : "input-mouse"
                bold: strip.captured
                text: strip.captured ? (strip.compact ? "Captured" : "Keyboard and mouse captured")
                                    : (strip.compact ? "Click to control" : "Click the screen to control the server")
            }
            background: Rectangle {
                visible: captureButton.visualFocus
                anchors.fill: parent
                anchors.margins: -3
                radius: height / 2
                color: "transparent"
                border.width: 2
                border.color: Theme.stageTone(Theme.accent)
            }
        }
        QQC2.Label {
            id: hintLabel
            visible: strip.showHint
            text: "Tap Right Ctrl to release"
            color: Theme.stageMuted
            font.pointSize: Theme.small
            elide: Text.ElideRight
            Layout.fillWidth: true
            Layout.maximumWidth: Math.ceil(implicitWidth)
        }
        ActionButton {
            id: releaseButton
            // Always laid out, only faded in: toggling visible would
            // resize the strip's contents on every capture change.
            onStage: true
            tone: "secondary"
            text: "Release"
            implicitHeight: Math.round(Theme.gu * 1.45)
            opacity: strip.captured ? 1 : 0
            enabled: strip.captured
            Accessible.ignored: !strip.captured
            onClicked: strip.releaseRequested()
        }
        Item { Layout.fillWidth: true }
        QQC2.Label {
            text: controller.message
            visible: text !== "" && !/^Connected/.test(text) && !strip.compact
            color: Theme.stageMuted
            font.pointSize: Theme.small
            elide: Text.ElideRight
            // Gives way first; hovering shows what is cut off, such
            // as why an ISO went away.
            Layout.fillWidth: true
            Layout.maximumWidth: Math.min(Math.ceil(implicitWidth), strip.width / 3)
            QQC2.ToolTip.visible: messageHover.hovered && truncated
            QQC2.ToolTip.text: text
            QQC2.ToolTip.delay: Kirigami.Units.toolTipDelay
            HoverHandler { id: messageHover }
        }
        Chip {
            id: isoChip
            visible: controller.mounted
            onStage: true
            tone: Theme.positive
            iconName: "media-optical"
            text: "ISO mounted"
        }
        QQC2.Label {
            id: resolutionLabel
            visible: strip.showResolution
            text: (controller.consoleInfo.resolution || "").replace("x", " × ")
            font.family: Theme.mono.family
            font.pointSize: Theme.small
            color: Theme.stageMuted
        }
    }
}
