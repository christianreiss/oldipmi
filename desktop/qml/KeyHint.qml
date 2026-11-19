import QtQuick
import QtQuick.Controls as QQC2
import QtQuick.Layouts

// A key cap ("Ctrl+F") with an optional label of what it does.
RowLayout {
    id: hint
    property string keys: ""
    property string label: ""
    spacing: Math.round(Theme.gu * 0.35)

    Rectangle {
        implicitWidth: cap.implicitWidth + Math.round(Theme.gu * 0.6)
        implicitHeight: cap.implicitHeight + 4
        radius: Theme.radiusSmall - 1
        color: "transparent"
        border.width: 1
        border.color: Theme.line
        Layout.alignment: Qt.AlignVCenter
        QQC2.Label {
            id: cap
            anchors.centerIn: parent
            text: hint.keys
            font.family: Theme.mono.family
            font.pointSize: Theme.tiny
            color: Theme.text
        }
    }
    QQC2.Label {
        visible: hint.label !== ""
        text: hint.label
        font.pointSize: Theme.small
        color: Theme.muted
        Layout.alignment: Qt.AlignVCenter
    }
}
