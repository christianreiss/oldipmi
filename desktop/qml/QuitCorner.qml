import QtQuick
import org.kde.kirigami as Kirigami
import "shortcuts.js" as Shortcuts

// Quit, in a corner of the screen for the whole time in full screen. The
// console pane places it.
Rectangle {
    id: corner
    required property Kirigami.Action action
    // The room a centred bar or note leaves it on each side.
    readonly property real clearance: width + Math.round(Theme.gu * 1.2)

    width: quitButton.implicitWidth + 8
    height: quitButton.implicitHeight + 8
    radius: Theme.radiusPanel
    color: Theme.alpha(Theme.stagePopup, 0.9)
    border.width: 1
    border.color: Theme.stageLine
    IconButton {
        id: quitButton
        objectName: "quitCorner"
        onStage: true
        anchors.centerIn: parent
        icon.name: corner.action.icon.name
        text: corner.action.text
        shortcut: Shortcuts.quit
        onClicked: corner.action.trigger()
    }
}
