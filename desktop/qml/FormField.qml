import QtQuick
import QtQuick.Controls as QQC2
import QtQuick.Layouts

// A text field of a form, on InputBackground; invalid draws its frame red.
QQC2.TextField {
    id: field
    property bool invalid: false

    placeholderTextColor: Theme.muted
    color: Theme.text
    implicitHeight: Math.max(Theme.controlHeight + 2, contentHeight + topPadding + bottomPadding)
    leftPadding: Math.round(Theme.gu * 0.65)
    rightPadding: Math.round(Theme.gu * 0.65)
    verticalAlignment: TextInput.AlignVCenter
    background: InputBackground { control: field; invalid: field.invalid }
    Layout.fillWidth: true
}
