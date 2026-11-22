import QtQuick
import QtQuick.Controls as QQC2
import QtQuick.Layouts
import org.kde.kirigami as Kirigami

// Disconnect all, or quit, with several consoles open: it names the consoles
// (id, name, host and the start of each session). What confirming does is the
// main window's: it closes exactly these sessions, and asks again when another
// console opened meanwhile.
AppDialog {
    id: disconnectAllDialog
    property var consoles: []
    // Also asks before quitting, which closes them all the same way.
    property bool quitting: false
    signal confirmed()
    title: (quitting ? "Quit and disconnect " : "Disconnect all ") + consoles.length
           + (consoles.length === 1 ? " console?" : " consoles?")
    iconName: quitting ? "application-exit" : "network-disconnect"
    tone: Theme.warning
    closeButton: false
    QQC2.Label {
        text: "Their remote consoles close. The servers keep running, and the connections stay in the list."
        color: Theme.cardText
        wrapMode: Text.Wrap
        Layout.fillWidth: true
    }
    Repeater {
        model: disconnectAllDialog.consoles
        delegate: RowLayout {
            id: openRow
            required property var modelData
            spacing: Kirigami.Units.largeSpacing
            Layout.fillWidth: true
            QQC2.Label {
                text: openRow.modelData.name || openRow.modelData.host || ""
                font.weight: Font.DemiBold
                elide: Text.ElideRight
                Layout.fillWidth: true
                Layout.maximumWidth: Math.ceil(implicitWidth)
            }
            MonoCaption {
                text: openRow.modelData.host || ""
            }
        }
    }
    buttons: [
        ActionButton { tone: "ghost"; text: "Cancel"; onClicked: disconnectAllDialog.close() },
        ActionButton {
            objectName: "disconnectAllConfirm"
            tone: "primary"
            text: disconnectAllDialog.quitting ? "Quit" : "Disconnect all"
            onClicked: disconnectAllDialog.confirmed()
        }
    ]
}
