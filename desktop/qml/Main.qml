import QtQuick
import QtQuick.Controls as QQC2
import QtQuick.Layouts
import org.kde.kirigami as Kirigami
import org.kde.kirigamiaddons.formcard as FormCard
import org.kde.kirigamiaddons.settings as KirigamiSettings
import "families.js" as Families

Kirigami.ApplicationWindow {
    id: window

    title: (controller.activeProfile.name && controller.state !== "disconnected")
           ? controller.activeProfile.name + " — OldIPMI" : "OldIPMI"
    width: Kirigami.Units.gridUnit * 70
    height: Kirigami.Units.gridUnit * 44
    minimumWidth: Kirigami.Units.gridUnit * 36
    minimumHeight: Kirigami.Units.gridUnit * 24
    visible: true
    pageStack.globalToolBar.style: Kirigami.ApplicationHeaderStyle.None

    property string selectedId: ""
    readonly property var selectedProfile: controller.hosts.find(p => p.id === selectedId) || ({})
    readonly property bool fullscreen: visibility === Window.FullScreen
    property string pendingPower: ""
    // Both panes' toolbars share one height so their bottom edges line up; it grows
    // with the title and subtitle when the system font is larger.
    readonly property real headerHeight: Math.max(Kirigami.Units.gridUnit * 2 + Kirigami.Units.largeSpacing * 2,
                                                  consolePane.headerNeeds)
    property var powerProfile: ({})

    // Used by --probe-ui and the agent API: select (and show) a profile.
    function loadHost(p) { selectedId = (p && p.id) || "" }
    function connectProfile(p) {
        if (!p || !p.host || controller.state !== "disconnected") return;
        selectedId = p.id || selectedId;
        controller.connectHost(p, "");
    }
    function newConnection() { connectionDialog.openFor({}) }
    function editConnection(p) { if (p && p.id) connectionDialog.openFor(controller.host(p.id)) }
    function openSettings(module) {
        // ConfigurationView only raises an open window; reopen it on the requested page.
        if (settingsView.configViewItem) settingsView.configViewItem.close();
        settingsView.open(module || "general");
    }
    function showMcpInstructions() { mcpDialog.open() }
    function toggleFullscreen() { visibility = fullscreen ? Window.Windowed : Window.FullScreen }

    onSelectedIdChanged: if (selectedId) settings.lastProfile = selectedId
    Component.onCompleted: {
        const g = settings.windowGeometry;
        if (g.width > 0 && g.height > 0) { x = g.x; y = g.y; width = g.width; height = g.height }
        const last = controller.hosts.find(p => p.id === settings.lastProfile);
        selectedId = last ? last.id : (controller.hosts.length ? controller.hosts[0].id : "");
    }
    onClosing: if (!fullscreen) settings.windowGeometry = Qt.rect(x, y, width, height)

    Shortcut { sequences: [StandardKey.New]; onActivated: window.newConnection() }
    Shortcut { sequence: "Ctrl+,"; onActivated: window.openSettings("general") }
    Shortcut { sequences: [StandardKey.Find]; onActivated: { settings.showSidebar = true; sidebar.focusSearch() } }
    Shortcut { sequence: "F9"; onActivated: settings.showSidebar = !settings.showSidebar }
    Shortcut { sequences: [StandardKey.FullScreen, "F11"]; onActivated: window.toggleFullscreen() }
    Shortcut { sequence: "Ctrl+D"; enabled: controller.state !== "disconnected"; onActivated: controller.disconnectHost() }
    Shortcut { sequences: [StandardKey.Quit]; onActivated: Qt.quit() }
    Shortcut { sequence: "Escape"; enabled: window.fullscreen && !consolePane.consoleView.captured; onActivated: window.toggleFullscreen() }

    RowLayout {
        anchors.fill: parent
        spacing: 0

        ConnectionSidebar {
            id: sidebar
            visible: settings.showSidebar && !window.fullscreen
            Layout.fillWidth: false
            Layout.preferredWidth: Kirigami.Units.gridUnit * 16
            Layout.fillHeight: true
            headerHeight: window.headerHeight
            selectedId: window.selectedId
            onSelected: id => window.selectedId = id
            onConnectRequested: p => window.connectProfile(p)
            onEditRequested: p => window.editConnection(p)
            onDeleteRequested: p => { deleteDialog.profile = p; deleteDialog.open() }
            onJnlpRequested: p => { window.selectedId = p.id; controller.openJNLP(p) }
            onNewRequested: window.newConnection()
            onSettingsRequested: module => window.openSettings(module)
        }
        Kirigami.Separator { Layout.fillHeight: true; visible: sidebar.visible }

        ConsolePane {
            id: consolePane
            Layout.fillWidth: true
            Layout.fillHeight: true
            headerHeight: window.headerHeight
            selected: window.selectedProfile
            fullscreen: window.fullscreen
            onConnectRequested: p => window.connectProfile(p)
            onEditRequested: p => window.editConnection(p)
            onNewRequested: window.newConnection()
            onPowerRequested: action => {
                window.pendingPower = action;
                window.powerProfile = controller.activeProfile;
                powerDialog.open();
            }
            onToggleFullscreen: window.toggleFullscreen()
            onToggleSidebar: settings.showSidebar = !settings.showSidebar
        }
    }

    ConnectionDialog {
        id: connectionDialog
        onSaved: p => { if (p.id) window.selectedId = p.id }
    }

    KirigamiSettings.ConfigurationView {
        id: settingsView
        window: window
        modules: [
            KirigamiSettings.ConfigurationModule {
                moduleId: "general"
                text: "General"
                icon.name: "configure"
                page: () => Qt.createComponent("GeneralSettings.qml")
            },
            KirigamiSettings.ConfigurationModule {
                moduleId: "agents"
                text: "AI agents (MCP)"
                icon.name: "dialog-scripts"
                page: () => Qt.createComponent("AgentSettings.qml")
            },
            KirigamiSettings.ConfigurationModule {
                moduleId: "about"
                text: "About OldIPMI"
                icon.name: "help-about"
                page: () => Qt.createComponent("AboutSettings.qml")
            }
        ]
    }

    readonly property var powerText: ({
        on: { title: "Power on", verb: "Power On", text: "The server is switched on if it is off." },
        soft: { title: "Graceful shutdown", verb: "Shut Down", text: "The operating system is asked to shut down (ACPI power button)." },
        reset: { title: "Reset", verb: "Reset", text: "The server restarts immediately. Unsaved data in the operating system is lost." },
        off: { title: "Power off immediately", verb: "Power Off", text: "Power is cut immediately, like pulling the plug. Unsaved data is lost and file systems may need repair." }
    })
    Kirigami.PromptDialog {
        id: powerDialog
        objectName: "powerDialog"
        readonly property var info: window.powerText[window.pendingPower] || ({title: "", verb: "OK", text: ""})
        readonly property var target: window.powerProfile.host ? window.powerProfile : controller.activeProfile
        title: info.title + ": " + (target.name || target.host || "")
        subtitle: info.text + "\n\nBMC: " + (target.host || "")
        standardButtons: Kirigami.Dialog.NoButton
        customFooterActions: [
            Kirigami.Action { text: "Cancel"; icon.name: "dialog-cancel"; onTriggered: powerDialog.close() },
            Kirigami.Action {
                text: powerDialog.info.verb
                icon.name: window.pendingPower === "on" ? "system-run" : "system-shutdown"
                onTriggered: { powerDialog.close(); controller.power(window.pendingPower) }
            }
        ]
    }

    Kirigami.PromptDialog {
        id: deleteDialog
        property var profile: ({})
        title: "Delete “" + (profile.name || profile.host || "") + "”?"
        subtitle: "The connection is removed" + (profile.remember ? " together with its password in KWallet." : ".") + " This cannot be undone."
        standardButtons: Kirigami.Dialog.NoButton
        customFooterActions: [
            Kirigami.Action { text: "Cancel"; icon.name: "dialog-cancel"; onTriggered: deleteDialog.close() },
            Kirigami.Action {
                text: "Delete"
                icon.name: "edit-delete"
                onTriggered: {
                    const id = deleteDialog.profile.id;
                    deleteDialog.close();
                    if (controller.removeHost(id) && window.selectedId === id)
                        window.selectedId = controller.hosts.length ? controller.hosts[0].id : "";
                }
            }
        ]
    }

    // Password prompt for a connection without a saved password.
    Kirigami.Dialog {
        id: credentialsDialog
        property var profile: ({})
        title: "Password for " + (profile.name || profile.host || "")
        preferredWidth: Kirigami.Units.gridUnit * 22
        standardButtons: Kirigami.Dialog.NoButton
        customFooterActions: [
            Kirigami.Action { text: "Cancel"; icon.name: "dialog-cancel"; onTriggered: credentialsDialog.reject() },
            Kirigami.Action {
                text: "Connect"
                icon.name: "media-playback-start"
                enabled: credentialsPassword.text !== ""
                onTriggered: credentialsDialog.accept()
            }
        ]
        ColumnLayout {
            spacing: Kirigami.Units.largeSpacing
            QQC2.Label {
                text: (credentialsDialog.profile.username || "") + "@" + (credentialsDialog.profile.host || "")
                Layout.fillWidth: true
                wrapMode: Text.Wrap
            }
            Kirigami.PasswordField {
                id: credentialsPassword
                Layout.fillWidth: true
                onAccepted: if (text !== "") credentialsDialog.accept()
            }
            QQC2.CheckBox {
                id: credentialsRemember
                text: "Remember in KWallet"
            }
        }
        onOpened: credentialsPassword.forceActiveFocus()
        onAccepted: {
            const p = Object.assign({}, credentialsDialog.profile, {remember: credentialsRemember.checked});
            const pw = credentialsPassword.text;
            credentialsPassword.text = "";
            controller.connectHost(p, pw);
        }
        onRejected: { credentialsPassword.text = ""; controller.cancelCredentials() }
    }

    Kirigami.PromptDialog {
        id: walletDialog
        title: "Password not saved"
        subtitle: controller.message + "\n\nYou can still connect; the password is then used for this session only."
        standardButtons: Kirigami.Dialog.NoButton
        customFooterActions: [
            Kirigami.Action { text: "Cancel"; icon.name: "dialog-cancel"; onTriggered: { walletDialog.close(); controller.cancelCredentials() } },
            Kirigami.Action { text: "Connect without saving"; icon.name: "media-playback-start"; onTriggered: { walletDialog.close(); controller.continueWithoutWallet() } }
        ]
    }

    Kirigami.Dialog {
        id: mcpDialog
        objectName: "mcpDialog"
        title: "Instructions for AI agents"
        preferredWidth: Math.min(window.width - Kirigami.Units.gridUnit * 4, Kirigami.Units.gridUnit * 40)
        preferredHeight: Math.min(window.height - Kirigami.Units.gridUnit * 4, Kirigami.Units.gridUnit * 34)
        standardButtons: Kirigami.Dialog.NoButton
        customFooterActions: [
            Kirigami.Action {
                text: "Copy instructions"
                icon.name: "edit-copy"
                onTriggered: { mcpText.selectAll(); mcpText.copy(); mcpText.deselect() }
            },
            Kirigami.Action { text: "Close"; icon.name: "dialog-close"; onTriggered: mcpDialog.close() }
        ]
        QQC2.TextArea {
            id: mcpText
            objectName: "mcpText"
            readOnly: true
            wrapMode: TextEdit.Wrap
            selectByMouse: true
            textFormat: TextEdit.MarkdownText
            text: agent.mcpInstructions()
            background: null
        }
    }

    Connections {
        target: controller
        function onProfileActivated(profile) { window.selectedId = profile.id }
        function onPasswordRequired() {
            credentialsDialog.profile = controller.pendingProfile();
            credentialsRemember.checked = credentialsDialog.profile.remember === undefined
                ? settings.rememberPasswords : !!credentialsDialog.profile.remember;
            credentialsDialog.open();
        }
        function onWalletUnavailable() { walletDialog.open() }
    }
}
