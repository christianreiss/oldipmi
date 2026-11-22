import QtQuick
import QtQuick.Controls as QQC2
import org.kde.kirigami as Kirigami
import "shortcuts.js" as Shortcuts

// The connection list's filter, which also takes the address of a new
// connection: a search icon, the key cap of Find while the whole placeholder
// fits beside it, and a clear button once there is text. Escape clears it.
QQC2.TextField {
    id: search
    implicitHeight: Math.max(Theme.controlHeight, contentHeight + topPadding + bottomPadding)
    placeholderText: "Filter or connect…"
    placeholderTextColor: Theme.muted
    color: Theme.text
    leftPadding: Math.round(Theme.gu * 1.85)
    rightPadding: (text !== "" ? clearButton.width : keyCap.visible ? keyCap.width : 0) + Math.round(Theme.gu * 0.6)
    verticalAlignment: TextInput.AlignVCenter
    Accessible.name: "Filter connections, or type the address of a new one"
    Keys.onEscapePressed: event => { if (text !== "") { clear(); event.accepted = true } else event.accepted = false }
    background: Rectangle {
        radius: Theme.radius
        color: Theme.dark ? Theme.mix(Theme.window, Theme.black, 0.1) : Theme.view
        border.width: search.activeFocus ? 2 : 1
        border.color: search.activeFocus ? Theme.accent : Theme.line
    }
    Kirigami.Icon {
        anchors { left: parent.left; leftMargin: Math.round(Theme.gu * 0.55); verticalCenter: parent.verticalCenter }
        width: Kirigami.Units.iconSizes.small
        height: width
        source: "search"
        color: Theme.muted
        isMask: true
    }
    // The key cap only while the whole placeholder still fits beside it: what
    // the field does matters more than its shortcut.
    TextMetrics { id: placeholderMetrics; font: search.font; text: search.placeholderText }
    KeyHint {
        id: keyCap
        visible: search.text === "" && search.width - search.leftPadding - implicitWidth
                 - Math.round(Theme.gu * 0.6) >= Math.ceil(placeholderMetrics.advanceWidth)
        anchors { right: parent.right; rightMargin: Math.round(Theme.gu * 0.4); verticalCenter: parent.verticalCenter }
        keys: Shortcuts.find
    }
    IconButton {
        id: clearButton
        visible: search.text !== ""
        anchors { right: parent.right; rightMargin: 3; verticalCenter: parent.verticalCenter }
        size: search.height - 6
        icon.name: "edit-clear"
        text: "Clear filter"
        onClicked: search.clear()
    }
}
