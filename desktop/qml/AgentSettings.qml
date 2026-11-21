import QtQuick
import QtQuick.Controls as QQC2
import QtQuick.Layouts
import org.kde.kirigami as Kirigami
import org.kde.kirigamiaddons.formcard as FormCard

// AI agents (MCP): the local control socket used by oldipmi-mcp.
FormCard.FormCardPage {
    id: page
    title: "AI agents (MCP)"

    readonly property string registerCommand: "claude mcp add oldipmi -- " + agent.mcpPath

    Kirigami.InlineMessage {
        Layout.fillWidth: true
        Layout.margins: Kirigami.Units.gridUnit
        Layout.bottomMargin: 0
        visible: true
        type: agent.listening ? Kirigami.MessageType.Information : Kirigami.MessageType.Warning
        text: !agentAllowed
            ? "The agent socket is off for this session (started with --no-agent-api or --self-test)."
            : agent.listening
              ? "Agents can control the open console: screen, keyboard, mouse, power and ISO images. There is no authentication; any program running as your user can use the socket."
              : settings.agentApiEnabled
                ? (agent.errorString || "The agent socket is not open.")
                : "Agent control is off."
    }

    Kirigami.InlineMessage {
        Layout.fillWidth: true
        Layout.margins: Kirigami.Units.gridUnit
        Layout.bottomMargin: 0
        visible: text !== ""
        type: Kirigami.MessageType.Warning
        text: agent.versionNote
    }

    FormCard.FormHeader { title: "Control socket" }
    FormCard.FormCard {
        FormCard.FormSwitchDelegate {
            text: "Allow AI agents to control OldIPMI"
            description: agentAllowed ? "Takes effect immediately." : "Applies from the next start; this session runs without the agent socket."
            checked: settings.agentApiEnabled
            onToggled: settings.agentApiEnabled = checked
        }
        FormCard.FormDelegateSeparator {}
        FormCard.FormTextDelegate {
            text: "Status"
            description: agent.listening
                ? (agent.clientCount > 0 ? agent.clientCount + " agent" + (agent.clientCount === 1 ? "" : "s") + " connected"
                                           + (agent.clientName ? " (" + agent.clientName + ")" : "")
                                         : "Listening, no agent connected")
                : "Not listening"
        }
        FormCard.FormDelegateSeparator {}
        FormCard.FormTextDelegate {
            text: "Socket"
            description: agent.socketPath
        }
    }

    FormCard.FormHeader { title: "Set up an agent" }
    FormCard.FormCard {
        FormCard.FormTextDelegate {
            text: "Register with Claude Code"
            description: page.registerCommand
            trailing: QQC2.Button {
                text: "Copy"
                icon.name: "edit-copy"
                onClicked: { clip.text = page.registerCommand; clip.selectAll(); clip.copy() }
            }
        }
        FormCard.FormDelegateSeparator {}
        FormCard.FormButtonDelegate {
            text: "Show instructions for the agent…"
            description: "The tool list and workflow, also sent to agents as MCP instructions"
            icon.name: "help-contents"
            onClicked: mainWindow.showMcpInstructions()
        }
    }

    FormCard.FormHeader { title: "Recent agent activity"; visible: agent.activity.length > 0 }
    FormCard.FormCard {
        visible: agent.activity.length > 0
        Repeater {
            model: agent.activity
            delegate: FormCard.FormTextDelegate {
                required property var modelData
                text: modelData.text
                description: Qt.formatTime(modelData.time, "hh:mm:ss") + (modelData.client ? " · " + modelData.client : "")
            }
        }
        FormCard.FormDelegateSeparator {}
        FormCard.FormButtonDelegate {
            text: "Clear activity"
            icon.name: "edit-clear-history"
            onClicked: agent.clearActivity()
        }
    }

    TextEdit { id: clip; visible: false }
}
