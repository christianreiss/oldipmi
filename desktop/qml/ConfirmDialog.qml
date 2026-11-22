import QtQuick
import QtQuick.Controls as QQC2
import QtQuick.Layouts

// A confirmation: what happens (consequence), to what (detail, in mono), Cancel
// and the button that does it (verb). The verb closes the dialog, then emits
// confirmed().
AppDialog {
    id: dialog
    property string consequence
    property string detail
    property string verb
    // The verb's ActionButton tone ("primary" or "danger") and icon.
    property string verbTone: "primary"
    property string verbIcon: ""
    signal confirmed()

    closeButton: false
    QQC2.Label {
        text: dialog.consequence
        color: Theme.cardText
        wrapMode: Text.Wrap
        Layout.fillWidth: true
    }
    MonoCaption {
        text: dialog.detail
    }
    buttons: [
        ActionButton { tone: "ghost"; text: "Cancel"; onClicked: dialog.close() },
        ActionButton {
            tone: dialog.verbTone
            text: dialog.verb
            icon.name: dialog.verbIcon
            onClicked: { dialog.close(); dialog.confirmed() }
        }
    ]
}
