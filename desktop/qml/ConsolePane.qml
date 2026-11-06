import QtQuick
import QtQuick.Controls as QQC2
import QtQuick.Layouts
import org.kde.kirigami as Kirigami
import OldIPMI 1.0
import "families.js" as Families

// Right-hand side: the selected connection, its setup progress, errors and the live console.
ColumnLayout {
    id: root
    spacing: 0
    property real headerHeight: Kirigami.Units.gridUnit * 3

    required property var selected          // profile chosen in the sidebar ({} if none)
    property bool fullscreen: false
    property alias consoleView: view
    signal connectRequested(var profile)
    signal editRequested(var profile)
    signal newRequested()
    signal powerRequested(string action)
    signal toggleFullscreen()
    signal toggleSidebar()

    readonly property string connState: controller.state
    readonly property bool live: controller.hasFrame || connState === "connected"
    readonly property bool settingUp: connState !== "disconnected" && connState !== "connected"
    readonly property var shown: live || settingUp || controller.errorStage !== "" ? controller.activeProfile : selected
    readonly property string family: shown.family || ""
    function has(capability) { return (controller.consoleInfo.capabilities || []).indexOf(capability) >= 0 }

    // ── Toolbar ────────────────────────────────────────────────────────────
    QQC2.ToolBar {
        Layout.fillWidth: true
        Layout.preferredHeight: root.headerHeight
        visible: !root.fullscreen || hoverArea.containsMouse
        contentItem: RowLayout {
            spacing: Kirigami.Units.smallSpacing
            QQC2.ToolButton {
                icon.name: "sidebar-show-symbolic"
                text: "Toggle connection list"
                display: QQC2.AbstractButton.IconOnly
                visible: !root.fullscreen
                QQC2.ToolTip.visible: hovered
                QQC2.ToolTip.text: text + " (F9)"
                onClicked: root.toggleSidebar()
            }
            ColumnLayout {
                spacing: 0
                Layout.fillWidth: true
                Layout.leftMargin: Kirigami.Units.smallSpacing
                Kirigami.Heading {
                    level: 2
                    text: root.shown.name || root.shown.host || "OldIPMI"
                    elide: Text.ElideRight
                    Layout.fillWidth: true
                }
                QQC2.Label {
                    visible: text !== ""
                    font: Kirigami.Theme.smallFont
                    color: Kirigami.Theme.disabledTextColor
                    elide: Text.ElideRight
                    Layout.fillWidth: true
                    text: root.live
                        ? [controller.consoleInfo.protocol, controller.consoleInfo.resolution].filter(x => !!x).join(" · ")
                        : root.shown.host ? root.shown.host + " · " + Families.name(root.family) : ""
                }
            }
            Kirigami.ActionToolBar {
                Layout.fillWidth: true
                alignment: Qt.AlignRight
                actions: root.live && root.connState === "connected" ? connectedActions
                       : root.settingUp ? setupActions
                       : root.shown.id ? idleActions : []
            }
        }
    }

    // Actions while a console is live.
    property list<Kirigami.Action> connectedActions: [
        Kirigami.Action {
            text: "Send key"
            icon.name: "input-keyboard"
            tooltip: "Send a key combination the local desktop would intercept"
            Kirigami.Action { text: "Ctrl+Alt+Del"; onTriggered: controller.sendKeys([224, 226, 76]) }
            Kirigami.Action { text: "Ctrl+Alt+Backspace"; onTriggered: controller.sendKeys([224, 226, 42]) }
            Kirigami.Action { text: "Alt+Tab"; onTriggered: controller.sendKeys([226, 43]) }
            Kirigami.Action { text: "Alt+F4"; onTriggered: controller.sendKeys([226, 61]) }
            Kirigami.Action { text: "Print Screen"; onTriggered: controller.sendKeys([70]) }
            Kirigami.Action { separator: true }
            Kirigami.Action { text: "Ctrl+Alt+F1 (console 1)"; onTriggered: controller.sendKeys([224, 226, 58]) }
            Kirigami.Action { text: "Ctrl+Alt+F2 (console 2)"; onTriggered: controller.sendKeys([224, 226, 59]) }
            Kirigami.Action { text: "Ctrl+Alt+F7 (graphical)"; onTriggered: controller.sendKeys([224, 226, 64]) }
            Kirigami.Action { separator: true }
            Kirigami.Action { text: "Super (Windows key)"; onTriggered: controller.sendKeys([227]) }
            Kirigami.Action { text: "Right Ctrl"; onTriggered: controller.sendKeys([228]) }
        },
        Kirigami.Action {
            text: controller.mounted ? "Eject ISO" : "Mount ISO…"
            icon.name: controller.mounted ? "media-eject" : "media-optical"
            visible: root.has("Read-only ISO")
            tooltip: controller.mounted ? "Disconnect the virtual CD-ROM" : "Attach a local ISO image read-only as a virtual CD-ROM"
            onTriggered: controller.mounted ? controller.ejectISO() : controller.mountISO()
        },
        Kirigami.Action {
            text: "Power"
            icon.name: "system-shutdown"
            visible: root.has("Power")
            Kirigami.Action { text: "Power on"; icon.name: "system-run"; onTriggered: root.powerRequested("on") }
            Kirigami.Action { text: "Graceful shutdown"; icon.name: "system-shutdown"; onTriggered: root.powerRequested("soft") }
            Kirigami.Action { text: "Reset"; icon.name: "system-reboot"; onTriggered: root.powerRequested("reset") }
            Kirigami.Action { text: "Power off immediately"; icon.name: "process-stop"; onTriggered: root.powerRequested("off") }
        },
        Kirigami.Action {
            text: view.fit ? "Actual size" : "Fit to window"
            icon.name: view.fit ? "zoom-original" : "zoom-fit-best"
            displayHint: Kirigami.DisplayHint.IconOnly
            tooltip: text
            onTriggered: view.fit = !view.fit
        },
        Kirigami.Action {
            text: root.fullscreen ? "Leave full screen" : "Full screen"
            icon.name: root.fullscreen ? "view-restore" : "view-fullscreen"
            displayHint: Kirigami.DisplayHint.IconOnly
            tooltip: text + " (F11)"
            onTriggered: root.toggleFullscreen()
        },
        Kirigami.Action {
            id: infoAction
            text: "Console information"
            icon.name: "documentinfo"
            checkable: true
            displayHint: Kirigami.DisplayHint.IconOnly
            tooltip: text
        },
        Kirigami.Action {
            text: "Disconnect"
            icon.name: "network-disconnect"
            tooltip: "Close this console session (Ctrl+D)"
            onTriggered: controller.disconnectHost()
        }
    ]
    property list<Kirigami.Action> setupActions: [
        Kirigami.Action { text: "Cancel"; icon.name: "dialog-cancel"; onTriggered: controller.disconnectHost() }
    ]
    property list<Kirigami.Action> idleActions: [
        Kirigami.Action {
            text: "Edit…"
            icon.name: "document-edit"
            displayHint: Kirigami.DisplayHint.KeepVisible
            onTriggered: root.editRequested(root.shown)
        }
    ]

    // Kept for --probe-ui and tests: toggles the console information panel.
    QQC2.ToolButton {
        id: details
        objectName: "consoleDetails"
        visible: false
        checkable: true
        checked: infoAction.checked
        onCheckedChanged: infoAction.checked = checked
    }

    Kirigami.Separator { Layout.fillWidth: true; visible: !root.fullscreen }

    // ── Content ────────────────────────────────────────────────────────────
    RowLayout {
        Layout.fillWidth: true
        Layout.fillHeight: true
        spacing: 0

        Item {
            Layout.fillWidth: true
            Layout.fillHeight: true

            MouseArea {
                id: hoverArea
                anchors { left: parent.left; right: parent.right; top: parent.top }
                height: Kirigami.Units.gridUnit
                hoverEnabled: root.fullscreen
                enabled: root.fullscreen
                acceptedButtons: Qt.NoButton
                z: 2
            }

            // The console item always exists: main.cpp connects frames and input to it.
            ConsoleView {
                id: view
                objectName: "console"
                anchors.fill: parent
                visible: controller.hasFrame
                fit: settings.fitToWindow
                rightCtrlReleases: settings.rightCtrlReleases
                accent: Kirigami.Theme.highlightColor
                Accessible.role: Accessible.Graphic
                Accessible.name: "Remote console of " + (root.shown.name || "")
                onFitChanged: if (settings.fitToWindow !== fit) settings.fitToWindow = fit
            }

            // Setup progress: one row per stage.
            ColumnLayout {
                anchors.centerIn: parent
                width: Math.min(parent.width - Kirigami.Units.gridUnit * 4, Kirigami.Units.gridUnit * 22)
                visible: root.settingUp && !controller.hasFrame
                spacing: Kirigami.Units.largeSpacing
                Kirigami.Heading {
                    level: 2
                    text: "Opening " + (root.shown.name || root.shown.host || "console") + "…"
                    wrapMode: Text.Wrap
                    horizontalAlignment: Text.AlignHCenter
                    Layout.fillWidth: true
                }
                Repeater {
                    model: Families.stages
                    delegate: RowLayout {
                        required property var modelData
                        required property int index
                        readonly property int current: Families.stageIndex(controller.state)
                        spacing: Kirigami.Units.largeSpacing
                        Layout.alignment: Qt.AlignHCenter
                        Layout.preferredWidth: Kirigami.Units.gridUnit * 14
                        Item {
                            implicitWidth: Kirigami.Units.iconSizes.smallMedium
                            implicitHeight: implicitWidth
                            Kirigami.Icon {
                                anchors.fill: parent
                                visible: index !== current
                                source: index < current ? "checkmark" : "go-next-symbolic"
                                opacity: index < current ? 1 : 0.4
                            }
                            QQC2.BusyIndicator {
                                anchors.fill: parent
                                padding: 0
                                visible: index === current
                                running: visible
                            }
                        }
                        QQC2.Label {
                            text: modelData.name
                            Layout.fillWidth: true
                            opacity: index <= current ? 1 : 0.5
                            font.weight: index === current ? Font.DemiBold : Font.Normal
                        }
                    }
                }
                QQC2.Button {
                    Layout.alignment: Qt.AlignHCenter
                    text: "Cancel"
                    icon.name: "dialog-cancel"
                    onClicked: controller.disconnectHost()
                }
            }

            // Failure of a setup stage, with what to do next.
            Kirigami.PlaceholderMessage {
                anchors.centerIn: parent
                width: Math.min(parent.width - Kirigami.Units.gridUnit * 4, Kirigami.Units.gridUnit * 30)
                visible: root.connState === "disconnected" && !controller.hasFrame && controller.errorStage !== ""
                icon.name: "dialog-error"
                text: Families.failedTitle(controller.errorStage)
                explanation: Families.explainError(controller.message)
                helpfulAction: Kirigami.Action {
                    text: "Try again"
                    icon.name: "view-refresh"
                    onTriggered: root.connectRequested(controller.activeProfile)
                }
                QQC2.Button {
                    Layout.alignment: Qt.AlignHCenter
                    text: "Edit connection…"
                    icon.name: "document-edit"
                    flat: true
                    onClicked: root.editRequested(controller.host(controller.activeProfile.id) || controller.activeProfile)
                }
            }

            // Idle: the selected connection, ready to open.
            Kirigami.PlaceholderMessage {
                anchors.centerIn: parent
                width: Math.min(parent.width - Kirigami.Units.gridUnit * 4, Kirigami.Units.gridUnit * 26)
                visible: root.connState === "disconnected" && !controller.hasFrame && controller.errorStage === "" && !!root.selected.id
                icon.name: "computer"
                text: root.selected.name || root.selected.host || ""
                explanation: (root.selected.username ? root.selected.username + "@" : "") + (root.selected.host || "")
                             + "\n" + Families.name(root.selected.family)
                             + "\nLast connected: " + Families.relativeTime(root.selected.lastConnected)
                             + (controller.message ? "\n\n" + controller.message : "")
                helpfulAction: Kirigami.Action {
                    text: "Connect"
                    icon.name: "media-playback-start"
                    onTriggered: root.connectRequested(root.selected)
                }
            }

            // First run.
            Kirigami.PlaceholderMessage {
                anchors.centerIn: parent
                width: Math.min(parent.width - Kirigami.Units.gridUnit * 4, Kirigami.Units.gridUnit * 26)
                visible: root.connState === "disconnected" && !controller.hasFrame && controller.errorStage === "" && !root.selected.id
                icon.name: "oldipmi"
                text: controller.hosts.length ? "Select a connection" : "No connections yet"
                explanation: controller.hosts.length
                    ? "Choose a BMC in the list, or add a new one."
                    : "Add a BMC by its address and account. OldIPMI logs in, finds the console and opens it here; no Java or browser needed."
                helpfulAction: Kirigami.Action {
                    text: "New connection…"
                    icon.name: "list-add"
                    onTriggered: root.newRequested()
                }
            }
        }

        Kirigami.Separator { Layout.fillHeight: true; visible: infoPanel.visible }

        // Console information side panel.
        QQC2.ScrollView {
            id: infoPanel
            visible: infoAction.checked && root.live
            Layout.preferredWidth: Kirigami.Units.gridUnit * 16
            Layout.fillHeight: true
            QQC2.ScrollBar.horizontal.policy: QQC2.ScrollBar.AlwaysOff
            ColumnLayout {
                width: infoPanel.availableWidth
                spacing: Kirigami.Units.smallSpacing
                Kirigami.Heading { level: 3; text: "Console information"; Layout.margins: Kirigami.Units.largeSpacing; Layout.bottomMargin: 0 }
                Repeater {
                    model: [
                        ["Protocol", controller.consoleInfo.protocol],
                        ["Protocol version", controller.consoleInfo.protocolVersion],
                        ["Resolution", controller.consoleInfo.resolution],
                        ["Features", (controller.consoleInfo.capabilities || []).join(", ")]
                    ].concat((controller.consoleInfo.endpoints || []).map(e => [e.role, e.address + "\n" + e.transport]))
                     .filter(row => !!row[1])
                    delegate: ColumnLayout {
                        required property var modelData
                        spacing: 0
                        Layout.fillWidth: true
                        Layout.leftMargin: Kirigami.Units.largeSpacing
                        Layout.rightMargin: Kirigami.Units.largeSpacing
                        QQC2.Label { text: modelData[0]; font: Kirigami.Theme.smallFont; color: Kirigami.Theme.disabledTextColor }
                        QQC2.Label { text: modelData[1]; wrapMode: Text.WrapAnywhere; Layout.fillWidth: true }
                    }
                }
                QQC2.Label {
                    visible: !controller.consoleInfo.protocol
                    text: "The backend has not reported console details."
                    wrapMode: Text.Wrap
                    Layout.fillWidth: true
                    Layout.margins: Kirigami.Units.largeSpacing
                    color: Kirigami.Theme.disabledTextColor
                }
                Kirigami.InlineMessage {
                    Layout.fillWidth: true
                    Layout.margins: Kirigami.Units.largeSpacing
                    visible: !!controller.consoleInfo.legacyTLS
                    type: Kirigami.MessageType.Warning
                    text: "Legacy TLS is enabled; the BMC certificate is not verified."
                }
            }
        }
    }

    // ── Status bar ─────────────────────────────────────────────────────────
    Kirigami.Separator { Layout.fillWidth: true; visible: statusBar.visible }
    RowLayout {
        id: statusBar
        visible: root.live && !root.fullscreen
        Layout.fillWidth: true
        Layout.margins: Kirigami.Units.smallSpacing
        Layout.leftMargin: Kirigami.Units.largeSpacing
        Layout.rightMargin: Kirigami.Units.largeSpacing
        spacing: Kirigami.Units.largeSpacing
        Kirigami.Icon {
            source: view.captured ? "input-keyboard" : "input-mouse"
            implicitWidth: Kirigami.Units.iconSizes.small
            implicitHeight: implicitWidth
        }
        QQC2.Label {
            Layout.fillWidth: true
            elide: Text.ElideRight
            text: view.captured
                ? (settings.rightCtrlReleases ? "Keyboard and mouse go to the server. Tap Right Ctrl to release them." : "Keyboard and mouse go to the server.")
                : "Click the screen to control the server."
        }
        QQC2.Button {
            visible: view.captured
            text: "Release"
            icon.name: "input-keyboard"
            flat: true
            onClicked: view.release()
        }
        QQC2.Label {
            visible: text !== ""
            text: controller.mounted ? "ISO mounted" : ""
            color: Kirigami.Theme.positiveTextColor
        }
        QQC2.Label {
            text: controller.message
            visible: text !== "" && !/^Connected/.test(text)
            elide: Text.ElideRight
            Layout.maximumWidth: root.width / 3
            color: Kirigami.Theme.disabledTextColor
        }
    }
}
