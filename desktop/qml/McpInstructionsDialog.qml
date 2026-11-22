import QtQuick
import QtQuick.Controls as QQC2
import QtQuick.Layouts

// The instructions for AI agents, rendered from Markdown; Copy takes the text
// as shown. The main window sizes it.
AppDialog {
    id: mcpDialog
    title: "Instructions for AI agents"
    QQC2.TextArea {
        id: mcpText
        readOnly: true
        wrapMode: TextEdit.Wrap
        selectByMouse: true
        textFormat: TextEdit.MarkdownText
        text: agent.mcpInstructions()
        color: Theme.text
        background: null
        padding: 0
        Layout.fillWidth: true
    }
    buttons: [
        ActionButton {
            tone: "secondary"
            text: "Copy instructions"
            icon.name: "edit-copy"
            onClicked: { mcpText.selectAll(); mcpText.copy(); mcpText.deselect(); Ui.notify("Instructions copied") }
        },
        ActionButton { tone: "primary"; text: "Close"; onClicked: mcpDialog.close() }
    ]
}
