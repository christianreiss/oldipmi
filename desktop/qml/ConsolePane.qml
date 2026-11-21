import QtQuick
import QtQuick.Controls as QQC2
import QtQuick.Layouts
import org.kde.kirigami as Kirigami
import OldIPMI 1.0
import "families.js" as Families

// Right-hand side: the selected connection, its setup, a failure, or the live
// console on its dark stage.
ColumnLayout {
    id: root
    spacing: 0

    required property var selected          // profile chosen in the sidebar ({} if none)
    property bool fullscreen: false
    property bool sidebarShown: true
    property alias consoleView: view
    signal connectRequested(var profile)
    signal editRequested(var profile)
    signal deleteRequested(var profile)
    signal duplicateRequested(var profile)
    signal newRequested()
    signal demoRequested()
    signal powerRequested(string action)
    signal toggleFullscreen()
    signal toggleSidebar()
    signal quitRequested()

    readonly property string connState: controller.state
    readonly property bool connected: connState === "connected"
    readonly property bool settingUp: connState !== "disconnected" && !connected
    // One page per state, in this order of precedence.
    readonly property string mode: controller.hasFrame ? "console"
        : settingUp || connected ? "setup"
        : controller.errorStage !== "" ? "error"
        : selected.id ? "idle" : "welcome"
    readonly property var shown: mode === "idle" || mode === "welcome" ? selected : controller.activeProfile
    readonly property var session: controller.sessions[controller.shownId] || ({})
    readonly property string displayName: shown.name || shown.host || ""
    // Full screen leaves only the remote screen while a console is shown. Without
    // one (selected, connecting, failed, or after losing the connection) the header
    // and the connection list stay, with Quit and the way back.
    readonly property bool immersive: fullscreen && mode === "console"
    // Said where the keyboard takes control, when only the mouse can release.
    readonly property string mouseOnlyRelease: "Releasing needs the mouse: tapping Right Ctrl is off in Settings."
    // What the console supports, by the core's feature names (testdata/ipc/names.json, features).
    function has(feature) { return (controller.consoleInfo.features || []).indexOf(feature) >= 0 }
    // The remote screen on the clipboard, at its own resolution and without the stage.
    function copyScreen() {
        const size = controller.copyFrame();
        if (size.width > 0)
            mainWindow.showPassiveNotification("Screen image copied (" + size.width + " × " + size.height + ")", "short");
    }

    // ── Header: what is shown, and what can be done with it ────────────────
    Item {
        id: header
        visible: !root.immersive
        Layout.fillWidth: true
        Layout.preferredHeight: visible ? Math.max(Theme.headerHeight, headerRow.implicitHeight + Kirigami.Units.largeSpacing) : 0
        // Labels give way to icons, then the title gives way, before anything overflows.
        readonly property bool roomy: width > Theme.gu * 40

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
                text: root.sidebarShown ? "Hide connection list" : "Show connection list"
                shortcut: "F9"
                Layout.minimumWidth: implicitWidth
                onClicked: root.toggleSidebar()
            }

            // Identity of the console being opened, failed or shown.
            Image {
                visible: (root.mode === "console" || root.mode === "setup" || root.mode === "error") && header.width >= Theme.gu * 20
                Layout.preferredWidth: Math.round(Theme.gu * 1.9)
                Layout.preferredHeight: Layout.preferredWidth
                source: Families.image(root.shown.family)
                sourceSize.width: width * 2
                sourceSize.height: height * 2
                fillMode: Image.PreserveAspectFit
                smooth: true
            }
            ColumnLayout {
                visible: root.mode === "console" || root.mode === "setup" || root.mode === "error"
                spacing: 0
                Layout.fillWidth: !actionBar.visible
                Layout.minimumWidth: Theme.gu * 2
                Layout.maximumWidth: actionBar.visible ? Math.max(Theme.gu * 6, header.width * 0.3) : -1
                QQC2.Label {
                    text: root.displayName || "OldIPMI"
                    font.pointSize: Theme.title
                    font.weight: Font.Bold
                    elide: Text.ElideRight
                    Layout.fillWidth: true
                    Accessible.role: Accessible.Heading
                }
                QQC2.Label {
                    // Address and family; the protocol is in the information panel.
                    text: [root.shown.host, root.shown.family ? Families.shortName(root.shown.family) : ""].filter(x => !!x).join(" · ")
                    visible: text !== ""
                    font.pointSize: Theme.small
                    color: Theme.muted
                    elide: Text.ElideRight
                    Layout.fillWidth: true
                }
            }
            // Breadcrumb for a selected, closed connection.
            QQC2.Label {
                visible: root.mode === "idle"
                textFormat: Text.StyledText
                text: "Connections <font color='" + Theme.muted + "'>›</font> <b>" + root.displayName.replace(/&/g, "&amp;").replace(/</g, "&lt;") + "</b>"
                color: Theme.muted
                elide: Text.ElideRight
                Layout.fillWidth: true
            }
            Item { Layout.fillWidth: true; visible: root.mode === "welcome" }

            IconButton {
                // Shown whenever an AI agent is attached to the control socket.
                visible: agent.clientCount > 0
                Layout.minimumWidth: implicitWidth
                icon.name: "dialog-scripts"
                text: "AI agent connected: " + agent.clientName + (agent.activity.length ? "\nLast action: " + agent.activity[0].text : "")
                onClicked: mainWindow.openSettings("agents")
            }

            Kirigami.ActionToolBar {
                id: actionBar
                visible: root.mode === "console" && root.connected
                // Takes only the space left, so Disconnect and Quit always fit;
                // what does not fit moves to the overflow menu.
                Layout.fillWidth: true
                Layout.preferredWidth: 0
                Layout.minimumWidth: Theme.gu * 2
                alignment: Qt.AlignRight
                display: header.roomy ? QQC2.Button.TextBesideIcon : QQC2.Button.IconOnly
                actions: root.connectedActions
            }
            Item { Layout.fillWidth: true; visible: root.mode !== "console" && root.mode !== "idle" && root.mode !== "welcome" }
            ActionButton {
                visible: root.mode === "console" && root.connected
                tone: "secondary"
                text: "Disconnect"
                icon.name: "network-disconnect"
                display: header.roomy ? QQC2.AbstractButton.TextBesideIcon : QQC2.AbstractButton.IconOnly
                tooltip: "Close this console session (Ctrl+D)"
                Layout.minimumWidth: implicitWidth
                onClicked: controller.disconnectHost()
            }
            IconButton {
                visible: root.fullscreen && !root.immersive
                icon.name: "view-restore"
                text: "Leave full screen"
                shortcut: "F11"
                Layout.minimumWidth: implicitWidth
                onClicked: root.toggleFullscreen()
            }
            IconButton {
                objectName: "headerQuitButton"
                icon.name: "application-exit"
                text: "Quit OldIPMI"
                shortcut: "Ctrl+Q"
                Layout.minimumWidth: implicitWidth
                onClicked: root.quitRequested()
            }
        }
    }

    // Actions of a live console (header, and full-screen bar for keys and power).
    Kirigami.Action {
        id: sendKeyAction
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
    }
    Kirigami.Action {
        id: mediaAction
        text: controller.mounted ? "Eject ISO" : "Mount ISO…"
        icon.name: controller.mounted ? "media-eject" : "media-optical"
        visible: root.has("media")
        // Only its icon while the header is tight, so Power keeps its label.
        displayHint: header.width >= Theme.gu * 56 ? Kirigami.DisplayHint.NoPreference : Kirigami.DisplayHint.IconOnly
        tooltip: controller.mounted ? "Eject ISO: disconnect the virtual CD-ROM" : "Mount ISO: attach a local ISO image read-only as a virtual CD-ROM"
        onTriggered: controller.mounted ? controller.ejectISO() : controller.mountISO()
    }
    Kirigami.Action {
        id: powerAction
        text: "Power"
        icon.name: "system-shutdown"
        tooltip: "Chassis power over IPMI (UDP 623) with this connection's account"
        Kirigami.Action { text: "Power on"; icon.name: "system-run"; onTriggered: root.powerRequested("on") }
        Kirigami.Action { text: "Graceful shutdown"; icon.name: "system-shutdown"; onTriggered: root.powerRequested("soft") }
        Kirigami.Action { text: "Reset"; icon.name: "system-reboot"; onTriggered: root.powerRequested("reset") }
        Kirigami.Action { separator: true }
        Kirigami.Action { text: "Power off immediately"; icon.name: "process-stop"; onTriggered: root.powerRequested("off") }
    }
    Kirigami.Action {
        id: copyAction
        text: "Copy screen image"
        icon.name: "edit-copy"
        displayHint: Kirigami.DisplayHint.IconOnly
        tooltip: "Copy the remote screen as an image (Ctrl+Shift+C)"
        onTriggered: root.copyScreen()
    }
    Kirigami.Action {
        id: fitAction
        text: view.fit ? "Actual size" : "Fit to window"
        icon.name: view.fit ? "zoom-original" : "zoom-fit-best"
        displayHint: Kirigami.DisplayHint.IconOnly
        tooltip: text
        onTriggered: view.fit = !view.fit
    }
    Kirigami.Action {
        id: fullscreenAction
        text: root.fullscreen ? "Leave full screen" : "Full screen"
        icon.name: root.fullscreen ? "view-restore" : "view-fullscreen"
        displayHint: Kirigami.DisplayHint.IconOnly
        tooltip: text + " (F11)"
        onTriggered: root.toggleFullscreen()
    }
    Kirigami.Action {
        id: infoAction
        objectName: "consoleDetails" // --probe-ui and the screenshot tour check it
        text: "Console information"
        icon.name: "documentinfo"
        checkable: true
        displayHint: Kirigami.DisplayHint.IconOnly
        tooltip: text
    }
    // In order of importance: what does not fit moves to the overflow menu from the end.
    readonly property list<Kirigami.Action> connectedActions: [sendKeyAction, powerAction, mediaAction, copyAction, fitAction, fullscreenAction, infoAction]

    // An icon button of the full-screen bar; it keeps its size when the bar is short of room.
    component BarButton: IconButton {
        onStage: true
        Layout.minimumWidth: implicitWidth
    }

    // Console information: protocol, resolution, features and endpoints.
    component InfoContent: QQC2.ScrollView {
        id: infoScroll
        // A close button next to the title, when the panel floats over the console.
        property bool closable: false
        signal closeRequested()
        QQC2.ScrollBar.horizontal.policy: QQC2.ScrollBar.AlwaysOff
        contentWidth: availableWidth
        ColumnLayout {
            width: infoScroll.availableWidth
            spacing: Math.round(Theme.gu * 0.6)
            RowLayout {
                spacing: Kirigami.Units.smallSpacing
                Layout.fillWidth: true
                Layout.margins: Math.round(Theme.gu * 0.9)
                Layout.rightMargin: infoScroll.closable ? Math.round(Theme.gu * 0.4) : Math.round(Theme.gu * 0.9)
                Layout.bottomMargin: 0
                QQC2.Label {
                    text: "Console information"
                    font.pointSize: Theme.title
                    font.weight: Font.Bold
                    wrapMode: Text.WordWrap
                    Layout.fillWidth: true
                    Accessible.role: Accessible.Heading
                }
                IconButton {
                    visible: infoScroll.closable
                    icon.name: "window-close-symbolic"
                    text: "Close console information"
                    Layout.alignment: Qt.AlignTop
                    onClicked: infoScroll.closeRequested()
                }
            }
            Repeater {
                model: [
                    {label: "Protocol", value: controller.consoleInfo.protocol},
                    {label: "Protocol version", value: controller.consoleInfo.protocolVersion},
                    {label: "Resolution", value: controller.consoleInfo.resolution},
                    {label: "Features", value: (controller.consoleInfo.capabilities || []).join(", ")}
                ].concat((controller.consoleInfo.endpoints || []).map(e => ({label: e.role, value: e.address + "\n" + e.transport, mono: true})))
                 .filter(row => !!row.value)
                delegate: ColumnLayout {
                    required property var modelData
                    spacing: 2
                    Layout.fillWidth: true
                    Layout.leftMargin: Math.round(Theme.gu * 0.9)
                    Layout.rightMargin: Math.round(Theme.gu * 0.9)
                    QQC2.Label {
                        text: modelData.label.toUpperCase()
                        font.pointSize: Theme.tiny
                        font.weight: Font.DemiBold
                        font.letterSpacing: 0.6
                        color: Theme.muted
                        wrapMode: Text.Wrap
                        Layout.fillWidth: true
                    }
                    QQC2.Label {
                        text: modelData.value
                        font.family: modelData.mono ? Theme.mono.family : Kirigami.Theme.defaultFont.family
                        font.pointSize: modelData.mono ? Theme.small : Theme.fontBase
                        wrapMode: Text.WrapAtWordBoundaryOrAnywhere
                        Layout.fillWidth: true
                    }
                }
            }
            QQC2.Label {
                visible: !controller.consoleInfo.protocol
                text: "The backend has not reported console details."
                color: Theme.muted
                wrapMode: Text.Wrap
                Layout.fillWidth: true
                Layout.leftMargin: Math.round(Theme.gu * 0.9)
                Layout.rightMargin: Math.round(Theme.gu * 0.9)
            }
            Rectangle {
                visible: !!controller.consoleInfo.legacyTLS
                Layout.fillWidth: true
                Layout.margins: Math.round(Theme.gu * 0.9)
                implicitHeight: tlsRow.implicitHeight + Math.round(Theme.gu * 0.9)
                radius: Theme.radius
                color: Theme.alpha(Theme.warning, Theme.dark ? 0.18 : 0.12)
                RowLayout {
                    id: tlsRow
                    anchors { fill: parent; margins: Math.round(Theme.gu * 0.45) }
                    spacing: Kirigami.Units.smallSpacing
                    Kirigami.Icon {
                        source: "security-medium"
                        implicitWidth: Kirigami.Units.iconSizes.small
                        implicitHeight: implicitWidth
                        color: Theme.toneText(Theme.warning, Theme.card)
                        isMask: true
                        Layout.alignment: Qt.AlignTop
                    }
                    QQC2.Label {
                        text: "Legacy TLS is enabled; the BMC certificate is not verified."
                        color: Theme.toneText(Theme.warning, Theme.card)
                        wrapMode: Text.Wrap
                        Layout.fillWidth: true
                    }
                }
            }
            Item { Layout.preferredHeight: Math.round(Theme.gu * 0.4) }
        }
    }

    // In a narrow pane the information floats over the console instead of
    // squeezing it.
    readonly property bool infoDocked: width >= Theme.gu * 40

    // ── Content ────────────────────────────────────────────────────────────
    RowLayout {
        Layout.fillWidth: true
        Layout.fillHeight: true
        spacing: 0

        Item {
            id: surface
            Layout.fillWidth: true
            Layout.fillHeight: true
            readonly property int margin: root.fullscreen ? 0 : Theme.stageMargin

            // The stage: the remote screen on a constant dark backdrop, with a
            // status strip of fixed height below it.
            Rectangle {
                id: stage
                anchors {
                    fill: parent
                    leftMargin: surface.margin
                    rightMargin: surface.margin
                    bottomMargin: surface.margin
                }
                visible: root.mode === "console"
                radius: root.fullscreen ? 0 : Theme.radiusLarge
                color: Theme.stage

                Item {
                    id: screen
                    anchors { left: parent.left; right: parent.right; top: parent.top; bottom: strip.visible ? strip.top : parent.bottom }

                    HoverHandler { id: screenHover }

                    // The console item always exists: main.cpp connects frames and input to it.
                    ConsoleView {
                        id: view
                        objectName: "console"
                        anchors.fill: parent
                        anchors.margins: root.fullscreen ? 0 : Math.round(Theme.gu * 0.8)
                        visible: controller.hasFrame
                        fit: settings.fitToWindow
                        rightCtrlReleases: settings.rightCtrlReleases
                        Accessible.role: Accessible.Graphic
                        Accessible.name: "Remote console of " + root.displayName
                        onFitChanged: if (settings.fitToWindow !== fit) settings.fitToWindow = fit
                    }

                    // Frame around the picture, outside it: a hairline when free, the
                    // accent ring while keyboard and mouse go to the server. It never
                    // covers a remote pixel and never moves the picture.
                    Item {
                        id: ring
                        readonly property rect r: view.imageRect
                        // The picture's part inside the view (at actual size it can be larger).
                        readonly property real visibleX: Math.max(0, r.x)
                        readonly property real visibleY: Math.max(0, r.y)
                        x: view.x + visibleX
                        y: view.y + visibleY
                        width: Math.max(0, Math.min(view.width, r.x + r.width) - visibleX)
                        height: Math.max(0, Math.min(view.height, r.y + r.height) - visibleY)
                        visible: view.visible && width > 0 && height > 0
                        Rectangle {
                            anchors.fill: parent
                            anchors.margins: view.captured ? -4 : -1
                            radius: view.captured ? 5 : 2
                            color: "transparent"
                            border.width: view.captured ? 2 : 1
                            border.color: view.captured ? Theme.accent : Theme.stageLine
                        }
                        Rectangle {
                            visible: view.captured
                            anchors.fill: parent
                            anchors.margins: -9
                            radius: 10
                            color: "transparent"
                            border.width: 4
                            border.color: Theme.alpha(Theme.accent, 0.24)
                        }
                    }

                    // Full screen: a floating bar, shown from the top edge, for a few
                    // seconds after entering, while one of its menus is open, and while
                    // the keyboard focus is in it or on Quit. Released, Tab reaches Quit,
                    // which is always shown, and the bar comes with it.
                    Timer { id: peek; objectName: "fullscreenPeek"; interval: 2500 }
                    Connections {
                        target: root
                        function onFullscreenChanged() { if (root.fullscreen) peek.restart() }
                    }
                    Rectangle {
                        id: fsBar
                        objectName: "fullscreenBar"
                        // A small screen with large text: the family picture goes,
                        // Release and Take control lose their icon, Disconnect shows only
                        // its icon, and the bar starts at the left edge instead of the
                        // centre, so the name keeps more room.
                        readonly property bool narrow: parent.width < Theme.gu * 34
                        readonly property int edge: Math.round(Theme.gu * 0.8)
                        // Never wider than the screen, leaving room for Quit in the corner
                        // (and, while centred, its mirror image): the name gives way first.
                        readonly property real room: narrow ? parent.width - fsQuit.width - 3 * edge - 12
                                                            : parent.width - 2 * (fsQuit.width + Math.round(Theme.gu * 1.2))
                        // A focused button never disappears under the keyboard.
                        readonly property bool focused: {
                            for (var item = Window.activeFocusItem; item; item = item.parent)
                                if (item === fsBar || item === fsQuit)
                                    return true
                            return false
                        }
                        readonly property bool revealed: (screenHover.hovered && screenHover.point.position.y < Theme.gu * 0.8)
                                                         || barHover.hovered || keysMenu.visible || powerMenu.visible || peek.running || focused
                        visible: root.fullscreen && opacity > 0
                        opacity: root.fullscreen && revealed ? 1 : 0
                        Behavior on opacity { NumberAnimation { duration: Kirigami.Units.shortDuration } }
                        x: narrow ? edge : Math.round((parent.width - width) / 2)
                        y: edge
                        z: 5
                        width: Math.min(barRow.implicitWidth, room) + 12
                        height: barRow.implicitHeight + 12
                        radius: Theme.radiusLarge
                        color: Theme.alpha(Theme.stagePopup, 0.95)
                        border.width: 1
                        border.color: Theme.stageLine
                        HoverHandler { id: barHover }
                        RowLayout {
                            id: barRow
                            anchors { fill: parent; margins: 6 }
                            spacing: 4
                            Image {
                                visible: !fsBar.narrow
                                Layout.preferredWidth: Math.round(Theme.gu * 1.35)
                                Layout.preferredHeight: Layout.preferredWidth
                                Layout.leftMargin: 4
                                source: Families.image(root.shown.family)
                                sourceSize.width: width * 2
                                sourceSize.height: height * 2
                                fillMode: Image.PreserveAspectFit
                            }
                            QQC2.Label {
                                text: root.displayName
                                color: Theme.stageText
                                font.weight: Font.DemiBold
                                elide: Text.ElideRight
                                Layout.fillWidth: true
                                Layout.maximumWidth: Math.min(Math.ceil(implicitWidth), Theme.gu * 12)
                                Layout.leftMargin: fsBar.narrow ? 6 : 0
                                Layout.rightMargin: 4
                            }
                            Rectangle { Layout.preferredWidth: 1; Layout.preferredHeight: Theme.gu * 1.1; color: Theme.stageEdge }
                            // While captured, the way back for anyone who turned off the
                            // Right Ctrl release. It does not take the focus itself, so the
                            // press does not release before the click.
                            ActionButton {
                                visible: view.captured
                                onStage: true
                                tone: "secondary"
                                text: "Release"
                                icon.name: fsBar.narrow ? "" : "input-mouse"
                                focusPolicy: Qt.NoFocus // slop-ok: shown only while captured, when every key goes to the server; the keyboard releases with Right Ctrl
                                Layout.minimumWidth: implicitWidth
                                onClicked: view.release()
                            }
                            // Released, the keyboard way back to the server once the focus
                            // is in the bar (a Right Ctrl tap takes control only while
                            // nothing has the keyboard focus).
                            ActionButton {
                                objectName: "fullscreenTakeControl"
                                visible: !view.captured
                                onStage: true
                                tone: "secondary"
                                text: "Take control"
                                icon.name: fsBar.narrow ? "" : "input-mouse"
                                Layout.minimumWidth: implicitWidth
                                Accessible.description: settings.rightCtrlReleases ? "" : root.mouseOnlyRelease
                                QQC2.ToolTip.visible: visualFocus && !settings.rightCtrlReleases
                                QQC2.ToolTip.text: root.mouseOnlyRelease
                                Keys.onReturnPressed: clicked()
                                Keys.onEnterPressed: clicked()
                                onClicked: view.capture()
                            }
                            BarButton {
                                id: keysButton
                                objectName: "fullscreenKeys"
                                icon.name: "input-keyboard"
                                text: "Send key"
                                checked: keysMenu.visible
                                onClicked: keysMenu.popup(keysButton, 0, keysButton.height + 4)
                            }
                            BarButton {
                                visible: mediaAction.visible
                                icon.name: mediaAction.icon.name
                                text: mediaAction.text
                                onClicked: mediaAction.trigger()
                            }
                            BarButton {
                                id: powerButton
                                icon.name: "system-shutdown"
                                text: "Power"
                                checked: powerMenu.visible
                                onClicked: powerMenu.popup(powerButton, 0, powerButton.height + 4)
                            }
                            Rectangle { Layout.preferredWidth: 1; Layout.preferredHeight: Theme.gu * 1.1; color: Theme.stageEdge }
                            // Gives way in a narrow bar, so the name stays; Ctrl+Shift+C still copies.
                            BarButton {
                                visible: !fsBar.narrow
                                icon.name: copyAction.icon.name
                                text: copyAction.text
                                shortcut: "Ctrl+Shift+C"
                                onClicked: copyAction.trigger()
                            }
                            BarButton {
                                icon.name: fitAction.icon.name
                                text: fitAction.text
                                onClicked: fitAction.trigger()
                            }
                            BarButton {
                                icon.name: "view-restore"
                                text: "Leave full screen"
                                shortcut: "F11"
                                onClicked: root.toggleFullscreen()
                            }
                            ActionButton {
                                visible: !fsBar.narrow
                                onStage: true
                                tone: "secondary"
                                text: "Disconnect"
                                icon.name: "network-disconnect"
                                Layout.leftMargin: 4
                                Layout.minimumWidth: implicitWidth
                                onClicked: controller.disconnectHost()
                            }
                            BarButton {
                                visible: fsBar.narrow
                                icon.name: "network-disconnect"
                                text: "Disconnect"
                                onClicked: controller.disconnectHost()
                            }
                        }
                    }
                    // Quit stays in the corner for the whole time in full screen, also
                    // while the bar is hidden.
                    Rectangle {
                        id: fsQuit
                        visible: root.fullscreen
                        anchors { top: parent.top; right: parent.right; margins: Math.round(Theme.gu * 0.8) }
                        z: 5
                        width: quitCorner.implicitWidth + 8
                        height: quitCorner.implicitHeight + 8
                        radius: Theme.radius + 3
                        color: Theme.alpha(Theme.stagePopup, 0.9)
                        border.width: 1
                        border.color: Theme.stageLine
                        BarButton {
                            id: quitCorner
                            objectName: "quitCorner"
                            anchors.centerIn: parent
                            icon.name: "application-exit"
                            text: "Quit OldIPMI"
                            shortcut: "Ctrl+Q"
                            onClicked: root.quitRequested()
                        }
                    }
                    QQC2.Menu {
                        id: keysMenu
                        objectName: "fullscreenKeysMenu"
                        Component.onCompleted: root.fillMenu(keysMenu, sendKeyAction.children)
                    }
                    QQC2.Menu {
                        id: powerMenu
                        Component.onCompleted: root.fillMenu(powerMenu, powerAction.children)
                    }

                    // Full screen has no status strip: taking or giving back the keyboard
                    // shows a short note instead, how to release or how to take control
                    // again. It covers nothing for long and moves nothing.
                    Timer { id: toastTimer; objectName: "fullscreenNoteTimer"; interval: 2600 }
                    Connections {
                        target: view
                        function onCapturedChanged() { if (root.fullscreen) toastTimer.restart() }
                    }
                    Rectangle {
                        id: toast
                        objectName: "fullscreenNote"
                        // One line when it fits; otherwise the release instruction takes its
                        // own line and the server name elides first. It keeps clear of the
                        // Quit corner, also while the bar is hidden and the note moves up.
                        readonly property int pad: Math.round(Theme.gu * 0.7)
                        readonly property real room: parent.width - 2 * (fsQuit.width + Math.round(Theme.gu * 1.2))
                        // Released with the keyboard focus in the bar or on Quit, a Right Ctrl
                        // tap does not take control; the bar's Take control does.
                        readonly property string how: view.captured
                            ? (settings.rightCtrlReleases ? "Tap Right Ctrl to release" : "Release from the bar at the top edge")
                            : fsBar.focused ? "Take control from the bar, or click the screen"
                            : (settings.rightCtrlReleases ? "Tap Right Ctrl or click the screen to take control" : "Click the screen to take control")
                        readonly property bool stacked: whoRow.implicitWidth + toastGrid.columnSpacing + oneLine.advanceWidth + 2 * pad > room
                        TextMetrics { id: oneLine; font: howLabel.font; text: "· " + toast.how }
                        visible: opacity > 0
                        opacity: root.fullscreen && toastTimer.running ? 1 : 0
                        Behavior on opacity { NumberAnimation { duration: Kirigami.Units.shortDuration } }
                        anchors.horizontalCenter: parent.horizontalCenter
                        y: fsBar.visible ? fsBar.y + fsBar.height + Kirigami.Units.largeSpacing : Math.round(Theme.gu * 0.8)
                        z: 4
                        width: Math.min(toastGrid.implicitWidth + 2 * pad, room)
                        height: toastGrid.implicitHeight + Math.round(Theme.gu * 0.8)
                        radius: Theme.radius + 3
                        color: Theme.alpha(Theme.stagePopup, 0.95)
                        border.width: 1
                        border.color: Theme.stageLine
                        GridLayout {
                            id: toastGrid
                            anchors { left: parent.left; right: parent.right; verticalCenter: parent.verticalCenter; leftMargin: toast.pad; rightMargin: toast.pad }
                            columns: toast.stacked ? 1 : 2
                            columnSpacing: Kirigami.Units.largeSpacing
                            rowSpacing: Math.round(Theme.gu * 0.15)
                            RowLayout {
                                id: whoRow
                                spacing: Kirigami.Units.largeSpacing
                                Layout.fillWidth: true
                                Layout.maximumWidth: implicitWidth
                                Kirigami.Icon {
                                    source: view.captured ? "input-keyboard" : "input-mouse"
                                    implicitWidth: Kirigami.Units.iconSizes.small
                                    implicitHeight: implicitWidth
                                    color: Theme.stageText
                                    isMask: true
                                }
                                QQC2.Label {
                                    text: view.captured ? "Keyboard and mouse go to " + root.displayName : "Keyboard and mouse released"
                                    color: Theme.stageText
                                    elide: Text.ElideRight
                                    Layout.fillWidth: true
                                    Layout.maximumWidth: Math.ceil(implicitWidth)
                                }
                            }
                            QQC2.Label {
                                id: howLabel
                                text: (toast.stacked ? "" : "· ") + toast.how
                                color: Theme.stageTone(Theme.accent)
                                wrapMode: Text.Wrap
                                Layout.fillWidth: toast.stacked
                                Layout.maximumWidth: toast.room - 2 * toast.pad
                            }
                        }
                    }
                }

                // Status strip: capture state, release, media and resolution. One
                // fixed height whatever it shows, so the picture above never moves.
                Rectangle {
                    id: strip
                    // Shorter words when the stage is narrow. Then, in order of
                    // importance, the capture state, Release, how to release, and the
                    // resolution: what does not fit whole is left out, not clipped.
                    readonly property bool compact: width < Theme.gu * 24
                    readonly property real room: width - 2 * Math.round(Theme.gu * 0.75)
                    readonly property real gap: Math.round(Theme.gu * 0.6)
                    // Chip, Release and the spacer after it, with the gaps between them.
                    readonly property real essentials: Math.ceil(captureChip.implicitWidth) + gap + releaseButton.implicitWidth + gap
                        + (isoChip.visible ? gap + Math.ceil(isoChip.implicitWidth) : 0)
                    readonly property bool showHint: view.captured && settings.rightCtrlReleases && !compact
                        && room >= essentials + gap + Math.ceil(hintLabel.implicitWidth)
                    readonly property bool showResolution: !compact && resolutionLabel.text !== ""
                        && room >= essentials + (showHint ? gap + Math.ceil(hintLabel.implicitWidth) : 0) + gap + Math.ceil(resolutionLabel.implicitWidth)
                    visible: !root.fullscreen
                    anchors { left: parent.left; right: parent.right; bottom: parent.bottom }
                    height: Theme.stripHeight
                    radius: stage.radius
                    color: Theme.stageBar
                    Rectangle {
                        anchors { left: parent.left; right: parent.right; top: parent.top }
                        height: parent.radius
                        color: parent.color
                    }
                    Rectangle {
                        anchors { left: parent.left; right: parent.right; top: parent.top }
                        height: 1
                        color: Theme.stageLine
                    }
                    RowLayout {
                        anchors {
                            fill: parent
                            leftMargin: Math.round(Theme.gu * 0.75)
                            rightMargin: Math.round(Theme.gu * 0.75)
                        }
                        spacing: strip.gap
                        // The capture state. While free it is also the keyboard way in:
                        // Tab to it, then Enter or Space sends keyboard and mouse to the server.
                        QQC2.AbstractButton {
                            id: captureButton
                            objectName: "captureButton"
                            enabled: !view.captured
                            focusPolicy: Qt.StrongFocus
                            hoverEnabled: true
                            padding: 0
                            implicitWidth: captureChip.implicitWidth
                            implicitHeight: captureChip.implicitHeight
                            Layout.fillWidth: true
                            Layout.maximumWidth: Math.ceil(implicitWidth)
                            Accessible.role: view.captured ? Accessible.StaticText : Accessible.Button
                            Accessible.name: view.captured ? captureChip.text : "Control " + root.displayName + " with keyboard and mouse"
                            // Without the Right Ctrl release only the mouse releases: say so
                            // before the keyboard goes to the server.
                            Accessible.description: settings.rightCtrlReleases ? "" : root.mouseOnlyRelease
                            QQC2.ToolTip.visible: visualFocus && !view.captured && !settings.rightCtrlReleases
                            QQC2.ToolTip.text: root.mouseOnlyRelease
                            Keys.onReturnPressed: captureButton.clicked()
                            Keys.onEnterPressed: captureButton.clicked()
                            onClicked: view.capture()
                            HoverHandler { enabled: captureButton.enabled; cursorShape: Qt.PointingHandCursor }
                            contentItem: Chip {
                                id: captureChip
                                onStage: true
                                neutral: !view.captured
                                tone: Theme.accent
                                iconName: view.captured ? "input-keyboard" : "input-mouse"
                                bold: view.captured
                                text: view.captured ? (strip.compact ? "Captured" : "Keyboard and mouse captured")
                                                    : (strip.compact ? "Click to control" : "Click the screen to control the server")
                            }
                            background: Rectangle {
                                visible: captureButton.visualFocus
                                anchors.fill: parent
                                anchors.margins: -3
                                radius: height / 2
                                color: "transparent"
                                border.width: 2
                                border.color: Theme.stageTone(Theme.accent)
                            }
                        }
                        QQC2.Label {
                            id: hintLabel
                            visible: strip.showHint
                            text: "Tap Right Ctrl to release"
                            color: Theme.stageMuted
                            font.pointSize: Theme.small
                            elide: Text.ElideRight
                            Layout.fillWidth: true
                            Layout.maximumWidth: Math.ceil(implicitWidth)
                        }
                        ActionButton {
                            id: releaseButton
                            // Always laid out, only faded in: toggling visible would
                            // resize the strip's contents on every capture change.
                            onStage: true
                            tone: "secondary"
                            text: "Release"
                            implicitHeight: Math.round(Theme.gu * 1.45)
                            opacity: view.captured ? 1 : 0
                            enabled: view.captured
                            Accessible.ignored: !view.captured
                            onClicked: view.release()
                        }
                        Item { Layout.fillWidth: true }
                        QQC2.Label {
                            text: controller.message
                            visible: text !== "" && !/^Connected/.test(text) && !strip.compact
                            color: Theme.stageMuted
                            font.pointSize: Theme.small
                            elide: Text.ElideRight
                            // Gives way first; hovering shows what is cut off, such
                            // as why an ISO went away.
                            Layout.fillWidth: true
                            Layout.maximumWidth: Math.min(Math.ceil(implicitWidth), strip.width / 3)
                            QQC2.ToolTip.visible: messageHover.hovered && truncated
                            QQC2.ToolTip.text: text
                            QQC2.ToolTip.delay: Kirigami.Units.toolTipDelay
                            HoverHandler { id: messageHover }
                        }
                        Chip {
                            id: isoChip
                            visible: controller.mounted
                            onStage: true
                            tone: Theme.positive
                            iconName: "media-optical"
                            text: "ISO mounted"
                        }
                        QQC2.Label {
                            id: resolutionLabel
                            visible: strip.showResolution
                            text: (controller.consoleInfo.resolution || "").replace("x", " × ")
                            font.family: Theme.mono.family
                            font.pointSize: Theme.small
                            color: Theme.stageMuted
                        }
                    }
                }
            }

            SetupPage {
                anchors { fill: parent; leftMargin: surface.margin; rightMargin: surface.margin; bottomMargin: surface.margin }
                visible: root.mode === "setup"
                profile: root.shown
                connState: root.connState
                startedAt: root.session.startedAt || 0
                onCancelRequested: controller.disconnectHost()
            }
            ErrorPage {
                anchors { fill: parent; leftMargin: surface.margin; rightMargin: surface.margin; bottomMargin: surface.margin }
                visible: root.mode === "error"
                profile: root.shown
                errorStage: controller.errorStage
                message: visible ? controller.message : ""
                onRetryRequested: root.connectRequested(controller.activeProfile)
                onEditRequested: root.editRequested(controller.host(controller.activeProfile.id) || controller.activeProfile)
            }
            IdlePage {
                anchors.fill: parent
                visible: root.mode === "idle"
                profile: root.selected
                notice: visible ? controller.message : ""
                onConnectRequested: root.connectRequested(root.selected)
                onEditRequested: root.editRequested(root.selected)
                onDeleteRequested: root.deleteRequested(root.selected)
                onDuplicateRequested: root.duplicateRequested(root.selected)
            }
            WelcomePage {
                anchors.fill: parent
                visible: root.mode === "welcome"
                hasHosts: controller.hosts.length > 0
                onNewRequested: root.newRequested()
                onDemoRequested: root.demoRequested()
            }
            Kirigami.ShadowedRectangle {
                visible: infoAction.checked && root.mode === "console" && !root.fullscreen && !root.infoDocked
                anchors {
                    top: parent.top
                    right: parent.right
                    bottom: parent.bottom
                    topMargin: Math.round(Theme.gu * 0.5)
                    rightMargin: surface.margin + Math.round(Theme.gu * 0.5)
                    bottomMargin: surface.margin + Theme.stripHeight + Math.round(Theme.gu * 0.5)
                }
                width: Math.round(Math.min(Theme.gu * 14, parent.width - Theme.gu * 3))
                radius: Theme.radiusLarge
                color: Theme.card
                border.width: 1
                border.color: Theme.lineSoft
                shadow.size: Math.round(Theme.gu * 1.2)
                shadow.yOffset: Math.round(Theme.gu * 0.3)
                shadow.color: Theme.shadow
                InfoContent {
                    anchors.fill: parent
                    anchors.margins: 1
                    closable: true
                    onCloseRequested: infoAction.checked = false
                }
            }
        }

        // Console information next to the stage, while that leaves the console
        // most of the pane.
        Rectangle {
            id: infoPanel
            visible: infoAction.checked && root.mode === "console" && !root.fullscreen && root.infoDocked
            Layout.preferredWidth: Math.round(Math.max(Theme.gu * 9, Math.min(Theme.gu * 15, root.width * 0.32)))
            Layout.fillHeight: true
            Layout.rightMargin: Theme.stageMargin
            Layout.bottomMargin: Theme.stageMargin
            radius: Theme.radiusLarge
            color: Theme.card
            border.width: 1
            border.color: Theme.lineSoft
            InfoContent {
                anchors.fill: parent
                anchors.margins: 1
            }
        }
    }

    Component { id: menuSeparator; QQC2.MenuSeparator {} }
    // The full-screen bar opens its own menus (so it can stay up while they are
    // open) built from the same actions as the header.
    function fillMenu(menu, actions) {
        for (const a of actions) {
            if (a.separator)
                menu.addItem(menuSeparator.createObject(null));
            else
                menu.addAction(a);
        }
    }
}
