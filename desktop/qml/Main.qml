import QtQuick
import QtQuick.Controls as QQC2
import QtQuick.Layouts
import org.kde.kirigami as Kirigami
import org.kde.kirigamiaddons.settings as KirigamiSettings

Kirigami.ApplicationWindow {
    id: window

    title: (controller.activeProfile.name && controller.state !== "disconnected")
           ? controller.activeProfile.name + (controller.openCount > 1 ? " (+" + (controller.openCount - 1) + ")" : "") + " — OldIPMI"
           : "OldIPMI"
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
    property var powerProfile: ({})

    // The design tokens follow the system colour scheme. Kirigami.Theme works only
    // inside a window, so the window hands its colours to the Theme singleton.
    Item {
        id: viewColors
        Kirigami.Theme.colorSet: Kirigami.Theme.View
        Kirigami.Theme.inherit: false
    }
    Binding { target: Theme; property: "window"; value: Kirigami.Theme.backgroundColor }
    Binding { target: Theme; property: "text"; value: Kirigami.Theme.textColor }
    Binding { target: Theme; property: "view"; value: viewColors.Kirigami.Theme.backgroundColor }
    Binding { target: Theme; property: "accent"; value: Kirigami.Theme.highlightColor }
    Binding { target: Theme; property: "positive"; value: Kirigami.Theme.positiveTextColor }
    Binding { target: Theme; property: "warning"; value: Kirigami.Theme.neutralTextColor }
    Binding { target: Theme; property: "negative"; value: Kirigami.Theme.negativeTextColor }
    Binding { target: Theme; property: "mono"; value: Kirigami.Theme.fixedWidthFont }
    Binding { target: Theme; property: "fontBase"; value: Kirigami.Theme.defaultFont.pointSize > 0 ? Kirigami.Theme.defaultFont.pointSize : 10 }
    FontMetrics { id: fontMetrics; font: Kirigami.Theme.defaultFont }
    Binding { target: Theme; property: "fontHeight"; value: fontMetrics.height }

    // Used by --probe-ui and the agent API: select (and show) a profile.
    function loadHost(p) { selectedId = (p && p.id) || "" }
    // Connect (double-click, Enter, row button, context menu). Other consoles stay
    // open; a profile that is already open or opening is just brought to the front.
    function connectProfile(p) {
        if (!p || !p.host) return;
        if (p.id && controller.isOpen(p.id)) { selectedId = p.id; controller.show(p.id); return }
        selectedId = p.id || selectedId;
        controller.connectHost(p, "");
    }
    function newConnection() { connectionDialog.openFor({}) }
    // New connection with the address typed into the filter.
    function newConnectionTo(host) { connectionDialog.openFor(host ? {host: host} : {}) }
    function editConnection(p) { if (p && p.id) connectionDialog.openFor(controller.host(p.id)) }
    function deleteConnection(p) { if (p && p.id) { deleteDialog.profile = p; deleteDialog.open() } }
    function duplicateConnection(p) {
        const copy = p && p.id ? controller.duplicateHost(p.id) : ({});
        if (copy.id) selectedId = copy.id;
    }
    function openSettings(module) {
        // ConfigurationView only raises an open window; reopen it on the requested page.
        if (settingsView.configViewItem) settingsView.configViewItem.close();
        settingsView.open(module || "general");
    }
    function showMcpInstructions() { mcpDialog.open() }
    // Each open console with the start of its session, so confirming acts on these
    // sessions and not on one opened again later under the same profile.
    function openConsoles() {
        return controller.hosts.filter(p => controller.isOpen(p.id)).map(p => ({
            id: p.id, name: p.name, host: p.host, startedAt: controller.sessions[p.id].startedAt
        }));
    }
    // Closing several consoles at once asks first and names them; a single one
    // closes right away, like Disconnect.
    function disconnectAll() {
        const open = openConsoles();
        if (open.length > 1) {
            disconnectAllDialog.quitting = false;
            disconnectAllDialog.consoles = open;
            disconnectAllDialog.open();
        } else {
            controller.disconnectAll();
        }
    }
    // Quitting closes every console, so with more than one open it asks the same
    // way (Quit, Ctrl+Q and closing the window); otherwise it quits right away.
    function requestQuit() {
        const open = openConsoles();
        if (open.length > 1) {
            disconnectAllDialog.quitting = true;
            disconnectAllDialog.consoles = open;
            disconnectAllDialog.open();
        } else {
            quitApp();
        }
    }
    // Set once quitting is decided (confirmed here, or any quit request of the
    // application, as when the session ends: see main.cpp), so closing the window
    // does not ask again.
    property bool quitConfirmed: false
    // Close first so onClosing stores the window geometry, then end the app
    // (the settings window would otherwise keep it running).
    function quitApp() { quitConfirmed = true; close(); Qt.quit() }
    function toggleFullscreen() { visibility = fullscreen ? Window.Windowed : Window.FullScreen }

    // The console view follows the sidebar selection: an open session is shown,
    // any other profile shows its page while the open consoles keep running.
    onSelectedIdChanged: {
        if (selectedId) settings.lastProfile = selectedId;
        controller.show(selectedId);
    }
    // A console that comes to the front by itself (an agent opened it) takes the selection along.
    readonly property string shownId: controller.shownId
    onShownIdChanged: if (shownId) selectedId = shownId
    Component.onCompleted: {
        const g = settings.windowGeometry;
        if (g.width > 0 && g.height > 0) { x = g.x; y = g.y; width = g.width; height = g.height }
        const last = controller.hosts.find(p => p.id === settings.lastProfile);
        selectedId = last ? last.id : (controller.hosts.length ? controller.hosts[0].id : "");
    }
    onClosing: event => {
        // The window's close button and Alt+F4 quit too: with several consoles open
        // they ask first, like Quit.
        if (!quitConfirmed && openConsoles().length > 1) {
            event.accepted = false;
            requestQuit();
            return;
        }
        if (!fullscreen) settings.windowGeometry = Qt.rect(x, y, width, height);
    }

    Shortcut { sequences: [StandardKey.New]; onActivated: window.newConnection() }
    Shortcut { sequence: "Ctrl+,"; onActivated: window.openSettings("general") }
    Shortcut { sequences: [StandardKey.Find]; onActivated: { settings.showSidebar = true; sidebar.focusSearch() } }
    Shortcut { sequence: "F9"; onActivated: settings.showSidebar = !settings.showSidebar }
    Shortcut { sequences: [StandardKey.FullScreen, "F11"]; onActivated: window.toggleFullscreen() }
    Shortcut { sequence: "Ctrl+D"; enabled: controller.state !== "disconnected"; onActivated: controller.disconnectHost() }
    Shortcut { sequence: "Ctrl+Shift+D"; enabled: controller.openCount > 0; onActivated: window.disconnectAll() }
    Shortcut {
        objectName: "copyScreenShortcut"
        sequence: "Ctrl+Shift+C"
        enabled: controller.hasFrame && !consolePane.consoleView.captured
        onActivated: consolePane.copyScreen()
    }
    Shortcut { sequences: [StandardKey.Quit]; onActivated: window.requestQuit() }
    Shortcut { sequence: "Escape"; enabled: window.fullscreen && !consolePane.consoleView.captured; onActivated: window.toggleFullscreen() }

    RowLayout {
        anchors.fill: parent
        spacing: 0

        ConnectionSidebar {
            id: sidebar
            visible: settings.showSidebar && !consolePane.immersive
            // A third of the window, within limits, so the console keeps most of it.
            Layout.preferredWidth: Math.round(Math.min(Theme.gu * 16, window.width * 0.42, Math.max(Theme.gu * 11, window.width * 0.3)))
            Layout.fillHeight: true
            selectedId: window.selectedId
            onSelected: id => window.selectedId = id
            onConnectRequested: p => window.connectProfile(p)
            onEditRequested: p => window.editConnection(p)
            onDeleteRequested: p => window.deleteConnection(p)
            onNewRequested: host => window.newConnectionTo(host)
            onSettingsRequested: module => window.openSettings(module)
            onDisconnectAllRequested: window.disconnectAll()
            onQuitRequested: window.requestQuit()
        }
        Rectangle {
            visible: sidebar.visible
            Layout.preferredWidth: 1
            Layout.fillHeight: true
            color: Theme.lineSoft
        }

        ConsolePane {
            id: consolePane
            Layout.fillWidth: true
            Layout.fillHeight: true
            selected: window.selectedProfile
            fullscreen: window.fullscreen
            sidebarShown: sidebar.visible
            onConnectRequested: p => window.connectProfile(p)
            onEditRequested: p => window.editConnection(p)
            onDeleteRequested: p => window.deleteConnection(p)
            onDuplicateRequested: p => window.duplicateConnection(p)
            onNewRequested: window.newConnection()
            onDemoRequested: window.connectProfile({name: "Demo console", host: "demo", family: "demo"})
            onPowerRequested: action => {
                window.pendingPower = action;
                window.powerProfile = controller.activeProfile;
                powerDialog.open();
            }
            onToggleFullscreen: window.toggleFullscreen()
            onToggleSidebar: settings.showSidebar = !settings.showSidebar
            onQuitRequested: window.requestQuit()
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

    // Every power action is confirmed; the dialog names the server, the BMC and
    // the consequence, and its button carries the verb.
    readonly property var powerText: ({
        on: { title: "Power on %1?", verb: "Power On", icon: "system-run",
              text: "The server is switched on if it is off." },
        soft: { title: "Shut down %1?", verb: "Shut Down", icon: "system-shutdown",
                text: "The operating system is asked to shut down (ACPI power button)." },
        reset: { title: "Reset %1?", verb: "Reset", icon: "system-reboot",
                 text: "The server restarts immediately. Unsaved data in the operating system is lost." },
        off: { title: "Power off %1 immediately?", verb: "Power Off", icon: "process-stop",
               text: "Power is cut immediately, like pulling the plug. Unsaved data is lost and file systems may need repair." }
    })
    AppDialog {
        id: powerDialog
        objectName: "powerDialog"
        readonly property var info: window.powerText[window.pendingPower] || ({title: "%1", verb: "OK", icon: "system-shutdown", text: ""})
        readonly property var target: window.powerProfile.host ? window.powerProfile : controller.activeProfile
        readonly property bool destructive: window.pendingPower === "reset" || window.pendingPower === "off"
        title: info.title.arg(target.name || target.host || "the server")
        iconName: info.icon
        tone: destructive ? Theme.negative : window.pendingPower === "soft" ? Theme.warning : Theme.accent
        closeButton: false
        preferredWidth: Math.round(Theme.gu * 25)
        QQC2.Label {
            text: powerDialog.info.text
            color: Theme.mix(Theme.text, Theme.card, 0.12)
            wrapMode: Text.Wrap
            Layout.fillWidth: true
        }
        QQC2.Label {
            text: "BMC " + (powerDialog.target.host || "")
            font.family: Theme.mono.family
            font.pointSize: Theme.small
            color: Theme.muted
            elide: Text.ElideMiddle
            Layout.fillWidth: true
        }
        buttons: [
            ActionButton { tone: "ghost"; text: "Cancel"; onClicked: powerDialog.close() },
            ActionButton {
                tone: powerDialog.destructive ? "danger" : "primary"
                text: powerDialog.info.verb
                // Breeze draws some of these icons red, which vanishes on the red button.
                icon.name: powerDialog.destructive ? "" : powerDialog.info.icon
                onClicked: { powerDialog.close(); controller.powerFor(window.powerProfile.id || "", window.pendingPower) }
            }
        ]
    }

    AppDialog {
        id: deleteDialog
        objectName: "deleteDialog"
        property var profile: ({})
        title: "Delete “" + (profile.name || profile.host || "") + "”?"
        iconName: "edit-delete"
        tone: Theme.negative
        closeButton: false
        preferredWidth: Math.round(Theme.gu * 25)
        QQC2.Label {
            text: "The connection is removed" + (deleteDialog.profile.remember ? " together with its password in KWallet." : ".") + " This cannot be undone."
            color: Theme.mix(Theme.text, Theme.card, 0.12)
            wrapMode: Text.Wrap
            Layout.fillWidth: true
        }
        QQC2.Label {
            text: deleteDialog.profile.host || ""
            font.family: Theme.mono.family
            font.pointSize: Theme.small
            color: Theme.muted
            elide: Text.ElideMiddle
            Layout.fillWidth: true
        }
        buttons: [
            ActionButton { tone: "ghost"; text: "Cancel"; onClicked: deleteDialog.close() },
            ActionButton {
                tone: "danger"
                text: "Delete"
                onClicked: {
                    const id = deleteDialog.profile.id;
                    deleteDialog.close();
                    if (controller.removeHost(id) && window.selectedId === id)
                        window.selectedId = controller.hosts.length ? controller.hosts[0].id : "";
                }
            }
        ]
    }

    // Disconnect all with several consoles open. Confirming closes exactly the
    // sessions it names, also when an agent opens another console meanwhile.
    AppDialog {
        id: disconnectAllDialog
        objectName: "disconnectAllDialog"
        property var consoles: []
        // Also asks before quitting, which closes them all the same way.
        property bool quitting: false
        title: (quitting ? "Quit and disconnect " : "Disconnect all ") + consoles.length
               + (consoles.length === 1 ? " console?" : " consoles?")
        iconName: quitting ? "application-exit" : "network-disconnect"
        tone: Theme.warning
        closeButton: false
        preferredWidth: Math.round(Theme.gu * 25)
        QQC2.Label {
            text: "Their remote consoles close. The servers keep running, and the connections stay in the list."
            color: Theme.mix(Theme.text, Theme.card, 0.12)
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
                QQC2.Label {
                    text: openRow.modelData.host || ""
                    font.family: Theme.mono.family
                    font.pointSize: Theme.small
                    color: Theme.muted
                    elide: Text.ElideMiddle
                    Layout.fillWidth: true
                }
            }
        }
        buttons: [
            ActionButton { tone: "ghost"; text: "Cancel"; onClicked: disconnectAllDialog.close() },
            ActionButton {
                objectName: "disconnectAllConfirm"
                tone: "primary"
                text: disconnectAllDialog.quitting ? "Quit" : "Disconnect all"
                onClicked: {
                    if (disconnectAllDialog.quitting) {
                        // Quits only with the consoles it named: one opened meanwhile is named first.
                        const open = window.openConsoles();
                        const named = c => disconnectAllDialog.consoles.some(o => o.id === c.id && o.startedAt === c.startedAt);
                        if (!open.every(named)) {
                            disconnectAllDialog.consoles = open;
                            return;
                        }
                        disconnectAllDialog.close();
                        window.quitApp();
                        return;
                    }
                    disconnectAllDialog.close();
                    for (const c of disconnectAllDialog.consoles) {
                        const s = controller.sessions[c.id];
                        if (s && s.startedAt === c.startedAt && controller.isOpen(c.id))
                            controller.disconnectProfile(c.id);
                    }
                }
            }
        ]
    }

    // Password prompt for a connection without a saved password.
    AppDialog {
        id: credentialsDialog
        objectName: "credentialsDialog"
        property var profile: ({})
        title: "Password for " + (profile.name || profile.host || "")
        iconName: "dialog-password"
        preferredWidth: Math.round(Theme.gu * 23)
        QQC2.Label {
            text: (credentialsDialog.profile.username || "") + "@" + (credentialsDialog.profile.host || "")
            font.family: Theme.mono.family
            font.pointSize: Theme.small
            color: Theme.muted
            elide: Text.ElideMiddle
            Layout.fillWidth: true
        }
        Kirigami.PasswordField {
            id: credentialsPassword
            color: Theme.text
            implicitHeight: Math.max(Theme.controlHeight + 2, contentHeight + topPadding + bottomPadding)
            leftPadding: Math.round(Theme.gu * 0.65)
            verticalAlignment: TextInput.AlignVCenter
            background: InputBackground { control: credentialsPassword }
            Accessible.name: "Password"
            Layout.fillWidth: true
            onAccepted: if (text !== "") credentialsDialog.accept()
        }
        QQC2.CheckBox {
            id: credentialsRemember
            text: "Remember in KWallet"
        }
        buttons: [
            ActionButton { tone: "ghost"; text: "Cancel"; onClicked: credentialsDialog.reject() },
            ActionButton {
                tone: "primary"
                text: "Connect"
                icon.name: "media-playback-start"
                enabled: credentialsPassword.text !== ""
                onClicked: credentialsDialog.accept()
            }
        ]
        onOpened: credentialsPassword.forceActiveFocus()
        onAccepted: {
            const p = Object.assign({}, credentialsDialog.profile, {remember: credentialsRemember.checked});
            const pw = credentialsPassword.text;
            credentialsPassword.text = "";
            controller.connectHost(p, pw);
        }
        onRejected: { credentialsPassword.text = ""; controller.cancelCredentials() }
    }

    AppDialog {
        id: walletDialog
        objectName: "walletDialog"
        title: "Password not saved"
        iconName: "dialog-warning"
        tone: Theme.warning
        closeButton: false
        preferredWidth: Math.round(Theme.gu * 25)
        QQC2.Label {
            text: controller.message
            visible: text !== ""
            color: Theme.mix(Theme.text, Theme.card, 0.12)
            wrapMode: Text.Wrap
            Layout.fillWidth: true
        }
        QQC2.Label {
            text: "You can still connect; the password is then used for this session only."
            color: Theme.mix(Theme.text, Theme.card, 0.12)
            wrapMode: Text.Wrap
            Layout.fillWidth: true
        }
        buttons: [
            ActionButton { tone: "ghost"; text: "Cancel"; onClicked: { walletDialog.close(); controller.cancelCredentials() } },
            ActionButton {
                tone: "primary"
                text: "Connect without saving"
                icon.name: "media-playback-start"
                onClicked: { walletDialog.close(); controller.continueWithoutWallet() }
            }
        ]
    }

    AppDialog {
        id: mcpDialog
        objectName: "mcpDialog"
        title: "Instructions for AI agents"
        preferredWidth: Math.min(window.width - Kirigami.Units.gridUnit * 4, Kirigami.Units.gridUnit * 40)
        preferredHeight: Math.min(window.height - Kirigami.Units.gridUnit * 4, Kirigami.Units.gridUnit * 34)
        QQC2.TextArea {
            id: mcpText
            objectName: "mcpText"
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
                onClicked: { mcpText.selectAll(); mcpText.copy(); mcpText.deselect(); window.showPassiveNotification("Instructions copied") }
            },
            ActionButton { tone: "primary"; text: "Close"; onClicked: mcpDialog.close() }
        ]
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
