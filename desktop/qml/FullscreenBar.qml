import QtQuick
import QtQuick.Controls as QQC2
import QtQuick.Layouts
import org.kde.kirigami as Kirigami
import "shortcuts.js" as Shortcuts

// The floating bar of full screen, at the top edge of the screen: the console's
// name, release or take control, its actions, and Disconnect. The console pane
// shows it for a few seconds after entering full screen (peeking), while the
// pointer is at the top edge, while one of its menus is open and while the
// keyboard focus is in it or on Quit.
Rectangle {
    id: fsBar
    // What it shows and controls.
    required property bool fullscreen
    required property bool captured
    required property string displayName
    required property var family
    // What to say where only the mouse releases (Right Ctrl is off).
    required property string mouseOnlyRelease
    // What keeps it shown, besides the pointer on it and its own focus.
    required property bool pointerAtTop
    required property bool peeking
    required property QuitCorner quitItem
    // The menus its Send key and Power buttons open.
    required property QQC2.Menu keysMenu
    required property QQC2.Menu powerMenu
    // The console pane's actions it offers.
    required property Kirigami.Action sendKeyAction
    required property Kirigami.Action mediaAction
    required property Kirigami.Action powerAction
    required property Kirigami.Action copyAction
    required property Kirigami.Action fitAction
    required property Kirigami.Action fullscreenAction
    required property Kirigami.Action disconnectAction
    signal captureRequested()
    signal releaseRequested()

    // An icon button of the bar; it keeps its size when the bar is short of room.
    component BarButton: IconButton {
        onStage: true
        Layout.minimumWidth: implicitWidth
    }

    // A small screen with large text: the family picture goes,
    // Release and Take control lose their icon, Disconnect shows only
    // its icon, and the bar starts at the left edge instead of the
    // centre, so the name keeps more room.
    readonly property bool narrow: parent.width < Theme.gu * 34
    readonly property int edge: Math.round(Theme.gu * 0.8)
    // Never wider than the screen, leaving room for Quit in the corner
    // (and, while centred, its mirror image): the name gives way first.
    readonly property real room: narrow ? parent.width - fsBar.quitItem.width - 3 * edge - 12
                                        : parent.width - 2 * fsBar.quitItem.clearance
    // A focused button never disappears under the keyboard.
    readonly property bool focused: {
        for (var item = Window.activeFocusItem; item; item = item.parent)
            if (item === fsBar || item === fsBar.quitItem)
                return true
        return false
    }
    readonly property bool revealed: fsBar.pointerAtTop || barHover.hovered || fsBar.keysMenu.visible
                                     || fsBar.powerMenu.visible || fsBar.peeking || focused
    visible: fsBar.fullscreen && opacity > 0
    opacity: fsBar.fullscreen && revealed ? 1 : 0
    Behavior on opacity { NumberAnimation { duration: Kirigami.Units.shortDuration } }
    x: narrow ? edge : Math.round((parent.width - width) / 2)
    y: edge
    width: Math.min(barRow.implicitWidth, room) + 12
    height: barRow.implicitHeight + 12
    radius: Theme.radiusLarge
    color: Theme.alpha(Theme.stagePopup, 0.95)
    border.width: 1
    border.color: Theme.stageLine
    HoverHandler { id: barHover }
    RowLayout {
        id: barRow
        anchors { fill: parent; margins: 6 }
        spacing: 4
        FamilyImage {
            visible: !fsBar.narrow
            Layout.leftMargin: 4
            family: fsBar.family
            side: Math.round(Theme.gu * 1.35)
        }
        QQC2.Label {
            text: fsBar.displayName
            color: Theme.stageText
            font.weight: Font.DemiBold
            elide: Text.ElideRight
            Layout.fillWidth: true
            Layout.maximumWidth: Math.min(Math.ceil(implicitWidth), Theme.gu * 12)
            Layout.leftMargin: fsBar.narrow ? 6 : 0
            Layout.rightMargin: 4
        }
        Rectangle { Layout.preferredWidth: 1; Layout.preferredHeight: Theme.gu * 1.1; color: Theme.stageEdge }
        // While captured, the way back for anyone who turned off the
        // Right Ctrl release. It does not take the focus itself, so the
        // press does not release before the click.
        ActionButton {
            visible: fsBar.captured
            onStage: true
            tone: "secondary"
            text: "Release"
            icon.name: fsBar.narrow ? "" : "input-mouse"
            focusPolicy: Qt.NoFocus // slop-ok: shown only while captured, when every key goes to the server; the keyboard releases with Right Ctrl
            Layout.minimumWidth: implicitWidth
            onClicked: fsBar.releaseRequested()
        }
        // Released, the keyboard way back to the server once the focus
        // is in the bar (a Right Ctrl tap takes control only while
        // nothing has the keyboard focus).
        ActionButton {
            objectName: "fullscreenTakeControl"
            visible: !fsBar.captured
            onStage: true
            tone: "secondary"
            text: "Take control"
            icon.name: fsBar.narrow ? "" : "input-mouse"
            Layout.minimumWidth: implicitWidth
            Accessible.description: AppSettings.rightCtrlReleases ? "" : fsBar.mouseOnlyRelease
            QQC2.ToolTip.visible: visualFocus && !AppSettings.rightCtrlReleases
            QQC2.ToolTip.text: fsBar.mouseOnlyRelease
            Keys.onReturnPressed: clicked()
            Keys.onEnterPressed: clicked()
            onClicked: fsBar.captureRequested()
        }
        BarButton {
            id: keysButton
            objectName: "fullscreenKeys"
            icon.name: fsBar.sendKeyAction.icon.name
            text: fsBar.sendKeyAction.text
            checked: fsBar.keysMenu.visible
            onClicked: fsBar.keysMenu.popup(keysButton, 0, keysButton.height + 4)
        }
        BarButton {
            visible: fsBar.mediaAction.visible
            icon.name: fsBar.mediaAction.icon.name
            text: fsBar.mediaAction.text
            onClicked: fsBar.mediaAction.trigger()
        }
        BarButton {
            id: powerButton
            icon.name: fsBar.powerAction.icon.name
            text: fsBar.powerAction.text
            checked: fsBar.powerMenu.visible
            onClicked: fsBar.powerMenu.popup(powerButton, 0, powerButton.height + 4)
        }
        Rectangle { Layout.preferredWidth: 1; Layout.preferredHeight: Theme.gu * 1.1; color: Theme.stageEdge }
        // Gives way in a narrow bar, so the name stays; Ctrl+Shift+C still copies.
        BarButton {
            visible: !fsBar.narrow
            icon.name: fsBar.copyAction.icon.name
            text: fsBar.copyAction.text
            shortcut: Shortcuts.copyScreen
            onClicked: fsBar.copyAction.trigger()
        }
        BarButton {
            icon.name: fsBar.fitAction.icon.name
            text: fsBar.fitAction.text
            onClicked: fsBar.fitAction.trigger()
        }
        BarButton {
            icon.name: fsBar.fullscreenAction.icon.name
            text: fsBar.fullscreenAction.text
            shortcut: Shortcuts.fullScreen
            onClicked: fsBar.fullscreenAction.trigger()
        }
        ActionButton {
            visible: !fsBar.narrow
            onStage: true
            tone: "secondary"
            text: fsBar.disconnectAction.text
            icon.name: fsBar.disconnectAction.icon.name
            Layout.leftMargin: 4
            Layout.minimumWidth: implicitWidth
            onClicked: fsBar.disconnectAction.trigger()
        }
        BarButton {
            visible: fsBar.narrow
            icon.name: fsBar.disconnectAction.icon.name
            text: fsBar.disconnectAction.text
            onClicked: fsBar.disconnectAction.trigger()
        }
    }
}
