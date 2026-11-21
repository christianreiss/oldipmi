import QtQuick
import QtQuick.Controls as QQC2
import QtQuick.Layouts

// A host or an account in small monospace, shortened in the middle.
QQC2.Label {
    font.family: Theme.mono.family
    font.pointSize: Theme.small
    color: Theme.muted
    elide: Text.ElideMiddle
    Layout.fillWidth: true
}
