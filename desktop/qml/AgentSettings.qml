import QtQuick
import QtQuick.Controls as QQC2
import QtQuick.Layouts
import org.kde.kirigami as Kirigami
import org.kde.kirigamiaddons.formcard as FormCard

// AI agents (MCP): the local control socket used by oldipmi-mcp.
FormCard.FormCardPage {
    id: page
    title: "AI agents (MCP)"

    readonly property string registerCommand: "claude mcp add oldipmi -- " + AgentApi.mcpPath

    Kirigami.InlineMessage {
        Layout.fillWidth: true
        Layout.margins: Kirigami.Units.gridUnit
        Layout.bottomMargin: 0
        visible: true
        type: AgentApi.listening ? Kirigami.MessageType.Information : Kirigami.MessageType.Warning
        text: !Launch.agentAllowed
            ? "The agent socket is off for this session (started with --no-agent-api or --self-test)."
            : AgentApi.listening
              ? "Agents can control the open console: screen, keyboard, mouse, power and ISO images. There is no authentication; any program running as your user can use the socket."
              : AppSettings.agentApiEnabled
                ? (AgentApi.errorString || "The agent socket is not open.")
                : "Agent control is off."
    }

    Kirigami.InlineMessage {
        Layout.fillWidth: true
        Layout.margins: Kirigami.Units.gridUnit
        Layout.bottomMargin: 0
        visible: text !== ""
        type: Kirigami.MessageType.Warning
        text: AgentApi.versionNote
    }

    FormCard.FormHeader { title: "Control socket" }
    FormCard.FormCard {
        FormCard.FormSwitchDelegate {
            text: "Allow AI agents to control OldIPMI"
            description: Launch.agentAllowed ? "Takes effect immediately." : "Applies from the next start; this session runs without the agent socket."
            checked: AppSettings.agentApiEnabled
            onToggled: AppSettings.agentApiEnabled = checked
        }
        FormCard.FormDelegateSeparator {}
        FormCard.FormSwitchDelegate {
            text: "Allow AI agents to manage connection profiles"
            description: "Allows programs using the agent socket to add, change and delete profiles and their saved passwords."
            enabled: AppSettings.agentApiEnabled
            checked: AppSettings.agentProfileWrites
            onToggled: AppSettings.agentProfileWrites = checked
        }
        FormCard.FormDelegateSeparator {}
        FormCard.FormTextDelegate {
            text: "Status"
            description: AgentApi.listening
                ? (AgentApi.clientCount > 0 ? AgentApi.clientCount + " agent" + (AgentApi.clientCount === 1 ? "" : "s") + " connected"
                                           + (AgentApi.clientName ? " (" + AgentApi.clientName + ")" : "")
                                         : "Listening, no agent connected")
                : "Not listening"
        }
        FormCard.FormDelegateSeparator {}
        FormCard.FormTextDelegate {
            text: "Socket"
            description: AgentApi.socketPath
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
                onClicked: Ui.copyText(page.registerCommand)
            }
        }
        FormCard.FormDelegateSeparator {}
        FormCard.FormButtonDelegate {
            text: "Show instructions for the agent…"
            description: "The tool list and workflow, also sent to agents as MCP instructions"
            icon.name: "help-contents"
            onClicked: Ui.mcpInstructionsRequested()
        }
    }

    FormCard.FormHeader { title: "Recent agent activity"; visible: AgentApi.activity.length > 0 }
    FormCard.FormCard {
        visible: AgentApi.activity.length > 0
        Repeater {
            model: AgentApi.activity
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
            onClicked: AgentApi.clearActivity()
        }
    }
}
