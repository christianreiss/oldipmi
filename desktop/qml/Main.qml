import QtQuick
import QtQuick.Layouts
import org.kde.kirigami as Kirigami
import org.kde.kirigamiaddons.settings as KirigamiSettings
import "shortcuts.js" as Shortcuts

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

    // Used by --probe-ui and the screenshot tour: select (and show) a profile.
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
        // On the desktop, the item is the settings window.
        const shown = settingsView.configViewItem as Window;
        if (shown) shown.close();
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
    // application, as when the session ends: see app/application.h), so closing the window
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
    Shortcut { sequence: Shortcuts.settings; onActivated: window.openSettings("general") }
    Shortcut { sequences: [StandardKey.Find]; onActivated: { settings.showSidebar = true; sidebar.focusSearch() } }
    Shortcut { sequence: Shortcuts.connectionList; onActivated: settings.showSidebar = !settings.showSidebar }
    Shortcut { sequences: [StandardKey.FullScreen, Shortcuts.fullScreen]; onActivated: window.toggleFullscreen() }
    Shortcut { sequence: Shortcuts.disconnect; enabled: controller.state !== "disconnected"; onActivated: controller.disconnectHost() }
    Shortcut { sequence: Shortcuts.disconnectAll; enabled: controller.openCount > 0; onActivated: window.disconnectAll() }
    Shortcut {
        objectName: "copyScreenShortcut"
        sequence: Shortcuts.copyScreen
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
            objectName: "sidebar"
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
            objectName: "consolePane"
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
                powerDialog.action = action;
                powerDialog.profile = controller.activeProfile;
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

    // The console pane's power requests.
    PowerDialog {
        id: powerDialog
        objectName: "powerDialog"
    }

    ConfirmDialog {
        id: deleteDialog
        objectName: "deleteDialog"
        property var profile: ({})
        title: "Delete “" + (profile.name || profile.host || "") + "”?"
        iconName: "edit-delete"
        tone: Theme.negative
        consequence: "The connection is removed" + (deleteDialog.profile.remember ? " together with its password in KWallet." : ".") + " This cannot be undone."
        detail: deleteDialog.profile.host || ""
        verb: "Delete"
        verbTone: "danger"
        onConfirmed: {
            const id = deleteDialog.profile.id;
            if (controller.removeHost(id) && window.selectedId === id)
                window.selectedId = controller.hosts.length ? controller.hosts[0].id : "";
        }
    }

    // Disconnect all with several consoles open. Confirming closes exactly the
    // sessions it names, also when an agent opens another console meanwhile.
    DisconnectAllDialog {
        id: disconnectAllDialog
        objectName: "disconnectAllDialog"
        onConfirmed: {
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

    // Password prompt for a connection without a saved password.
    CredentialsDialog {
        id: credentialsDialog
        objectName: "credentialsDialog"
    }

    WalletDialog {
        id: walletDialog
        objectName: "walletDialog"
    }

    McpInstructionsDialog {
        id: mcpDialog
        objectName: "mcpDialog"
        preferredWidth: Math.min(window.width - Kirigami.Units.gridUnit * 4, Kirigami.Units.gridUnit * 40)
        preferredHeight: Math.min(window.height - Kirigami.Units.gridUnit * 4, Kirigami.Units.gridUnit * 34)
    }

    Connections {
        target: controller
        function onProfileActivated(profile) { window.selectedId = profile.id }
        function onPasswordRequired() {
            credentialsDialog.profile = controller.pendingProfile();
            credentialsDialog.remember = credentialsDialog.profile.remember === undefined
                ? settings.rememberPasswords : !!credentialsDialog.profile.remember;
            credentialsDialog.open();
        }
        function onWalletUnavailable() { walletDialog.open() }
    }
    // Requests of the pages and of the settings window (Ui.qml).
    Connections {
        target: Ui
        function onNotificationRequested(message, timeout) { window.showPassiveNotification(message, timeout) }
        function onSettingsRequested(page) { window.openSettings(page) }
        function onMcpInstructionsRequested() { window.showMcpInstructions() }
    }
}
