import QtQuick
import QtQuick.Controls as QQC2
import QtQuick.Layouts
import org.kde.kirigami as Kirigami
import "families.js" as Families
import "shortcuts.js" as Shortcuts

// The console pane's header: what is shown, and what can be done with it. It
// hides in immersive full screen, where only the remote screen is left.
Item {
    id: header
    // The pane's page (console, setup, error, idle or welcome) and its profile.
    required property string mode
    required property var shown
    required property string displayName
    required property bool connected
    required property bool fullscreen
    required property bool immersive
    required property bool sidebarShown
    // The live console's actions, in order of importance, and the three that
    // have buttons of their own.
    required property list<Kirigami.Action> connectedActions
    required property Kirigami.Action disconnectAction
    required property Kirigami.Action fullscreenAction
    required property Kirigami.Action quitAction
    signal toggleSidebar()

    visible: !header.immersive
    Layout.fillWidth: true
    Layout.preferredHeight: visible ? Math.max(Theme.headerHeight, headerRow.implicitHeight + Kirigami.Units.largeSpacing) : 0
    // Labels give way to icons, then the title gives way, before anything
    // overflows. Icons only below 40 gu, where the information panel floats
    // (ConsolePane's infoDocked); the header is as wide as the pane.
    readonly property bool roomy: width >= Theme.gu * 40

    RowLayout {
        id: headerRow
        anchors {
            fill: parent
            leftMargin: Kirigami.Units.largeSpacing
            rightMargin: Math.round(Theme.gu * 0.75)
        }
        spacing: Kirigami.Units.largeSpacing

        // Fixed buttons keep their size (a layout would squeeze them to
        // nothing); the title gives way instead.
        IconButton {
            icon.name: "sidebar-show-symbolic"
            text: header.sidebarShown ? "Hide connection list" : "Show connection list"
            shortcut: Shortcuts.connectionList
            Layout.minimumWidth: implicitWidth
            onClicked: header.toggleSidebar()
        }

        // Identity of the console being opened, failed or shown.
        FamilyImage {
            visible: (header.mode === "console" || header.mode === "setup" || header.mode === "error") && header.width >= Theme.gu * 20
            family: header.shown.family
            side: Math.round(Theme.gu * 1.9)
        }
        ColumnLayout {
            visible: header.mode === "console" || header.mode === "setup" || header.mode === "error"
            spacing: 0
            Layout.fillWidth: !actionBar.visible
            Layout.minimumWidth: Theme.gu * 2
            Layout.maximumWidth: actionBar.visible ? Math.max(Theme.gu * 6, header.width * 0.3) : -1
            QQC2.Label {
                text: header.displayName || "OldIPMI"
                font.pointSize: Theme.title
                font.weight: Font.Bold
                elide: Text.ElideRight
                Layout.fillWidth: true
                Accessible.role: Accessible.Heading
            }
            QQC2.Label {
                // Address and family; the protocol is in the information panel.
                text: [header.shown.host, header.shown.family ? Families.shortName(header.shown.family) : ""].filter(x => !!x).join(" · ")
                visible: text !== ""
                font.pointSize: Theme.small
                color: Theme.muted
                elide: Text.ElideRight
                Layout.fillWidth: true
            }
        }
        // Breadcrumb for a selected, closed connection.
        QQC2.Label {
            visible: header.mode === "idle"
            textFormat: Text.StyledText
            text: "Connections <font color='" + Theme.muted + "'>›</font> <b>" + header.displayName.replace(/&/g, "&amp;").replace(/</g, "&lt;") + "</b>"
            color: Theme.muted
            elide: Text.ElideRight
            Layout.fillWidth: true
        }
        Item { Layout.fillWidth: true; visible: header.mode === "welcome" }

        IconButton {
            // Shown whenever an AI agent is attached to the control socket.
            visible: AgentApi.clientCount > 0
            Layout.minimumWidth: implicitWidth
            icon.name: "dialog-scripts"
            text: "AI agent connected: " + AgentApi.clientName + (AgentApi.activity.length ? "\nLast action: " + AgentApi.activity[0].text : "")
            onClicked: Ui.settingsRequested("agents")
        }

        Kirigami.ActionToolBar {
            id: actionBar
            visible: header.mode === "console" && header.connected
            // Takes only the space left, so Disconnect and Quit always fit;
            // what does not fit moves to the overflow menu.
            Layout.fillWidth: true
            Layout.preferredWidth: 0
            Layout.minimumWidth: Theme.gu * 2
            alignment: Qt.AlignRight
            display: header.roomy ? QQC2.Button.TextBesideIcon : QQC2.Button.IconOnly
            actions: header.connectedActions
        }
        Item { Layout.fillWidth: true; visible: header.mode !== "console" && header.mode !== "idle" && header.mode !== "welcome" }
        ActionButton {
            visible: header.mode === "console" && header.connected
            tone: "secondary"
            text: header.disconnectAction.text
            icon.name: header.disconnectAction.icon.name
            display: header.roomy ? QQC2.AbstractButton.TextBesideIcon : QQC2.AbstractButton.IconOnly
            tooltip: header.disconnectAction.tooltip
            Layout.minimumWidth: implicitWidth
            onClicked: header.disconnectAction.trigger()
        }
        IconButton {
            visible: header.fullscreen && !header.immersive
            icon.name: header.fullscreenAction.icon.name
            text: header.fullscreenAction.text
            shortcut: Shortcuts.fullScreen
            Layout.minimumWidth: implicitWidth
            onClicked: header.fullscreenAction.trigger()
        }
        IconButton {
            objectName: "headerQuitButton"
            icon.name: header.quitAction.icon.name
            text: header.quitAction.text
            shortcut: Shortcuts.quit
            Layout.minimumWidth: implicitWidth
            onClicked: header.quitAction.trigger()
        }
    }
}
