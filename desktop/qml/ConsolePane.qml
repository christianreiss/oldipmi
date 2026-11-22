pragma ComponentBehavior: Bound
import QtQuick
import QtQuick.Controls as QQC2
import QtQuick.Layouts
import org.kde.kirigami as Kirigami
import OldIPMI 1.0
import "families.js" as Families
import "shortcuts.js" as Shortcuts

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

    readonly property string connState: Controller.state
    readonly property bool connected: connState === "connected"
    readonly property bool settingUp: connState !== "disconnected" && !connected
    // One page per state, in this order of precedence.
    readonly property string mode: Controller.hasFrame ? "console"
        : settingUp || connected ? "setup"
        : Controller.errorStage !== "" ? "error"
        : selected.id ? "idle" : "welcome"
    readonly property var shown: mode === "idle" || mode === "welcome" ? selected : Controller.activeProfile
    readonly property var session: Controller.sessions[Controller.shownId] || ({})
    readonly property string displayName: shown.name || shown.host || ""
    // Full screen leaves only the remote screen while a console is shown. Without
    // one (selected, connecting, failed, or after losing the connection) the header
    // and the connection list stay, with Quit and the way back.
    readonly property bool immersive: fullscreen && mode === "console"
    // Said where the keyboard takes control, when only the mouse can release.
    readonly property string mouseOnlyRelease: "Releasing needs the mouse: tapping Right Ctrl is off in Settings."
    // What the console supports, by the core's feature names (testdata/ipc/names.json, features).
    function has(feature) { return (Controller.consoleInfo.features || []).indexOf(feature) >= 0 }
    // The remote screen on the clipboard, at its own resolution and without the stage.
    function copyScreen() {
        const size = Controller.copyFrame();
        if (size.width > 0)
            Ui.notify("Screen image copied (" + size.width + " × " + size.height + ")", "short");
    }

    // ── Header: what is shown, and what can be done with it ────────────────
    ConsoleHeader {
        id: header
        objectName: "consoleHeader"
        mode: root.mode
        shown: root.shown
        displayName: root.displayName
        connected: root.connected
        fullscreen: root.fullscreen
        immersive: root.immersive
        sidebarShown: root.sidebarShown
        connectedActions: root.connectedActions
        disconnectAction: disconnectAction
        fullscreenAction: fullscreenAction
        quitAction: quitAction
        onToggleSidebar: root.toggleSidebar()
    }

    // Actions of a live console, for the header and the full-screen bar.
    Kirigami.Action {
        id: sendKeyAction
        text: "Send key"
        icon.name: "input-keyboard"
        tooltip: "Send a key combination the local desktop would intercept"
        Kirigami.Action { text: "Ctrl+Alt+Del"; onTriggered: Controller.sendKeys([224, 226, 76]) }
        Kirigami.Action { text: "Ctrl+Alt+Backspace"; onTriggered: Controller.sendKeys([224, 226, 42]) }
        Kirigami.Action { text: "Alt+Tab"; onTriggered: Controller.sendKeys([226, 43]) }
        Kirigami.Action { text: "Alt+F4"; onTriggered: Controller.sendKeys([226, 61]) }
        Kirigami.Action { text: "Print Screen"; onTriggered: Controller.sendKeys([70]) }
        Kirigami.Action { separator: true }
        Kirigami.Action { text: "Ctrl+Alt+F1 (console 1)"; onTriggered: Controller.sendKeys([224, 226, 58]) }
        Kirigami.Action { text: "Ctrl+Alt+F2 (console 2)"; onTriggered: Controller.sendKeys([224, 226, 59]) }
        Kirigami.Action { text: "Ctrl+Alt+F7 (graphical)"; onTriggered: Controller.sendKeys([224, 226, 64]) }
        Kirigami.Action { separator: true }
        Kirigami.Action { text: "Super (Windows key)"; onTriggered: Controller.sendKeys([227]) }
        Kirigami.Action { text: "Right Ctrl"; onTriggered: Controller.sendKeys([228]) }
    }
    Kirigami.Action {
        id: mediaAction
        text: Controller.mounted ? "Eject ISO" : "Mount ISO…"
        icon.name: Controller.mounted ? "media-eject" : "media-optical"
        visible: root.has("media")
        // Only its icon while the header is tight, so Power keeps its label.
        displayHint: header.width >= Theme.gu * 56 ? Kirigami.DisplayHint.NoPreference : Kirigami.DisplayHint.IconOnly
        tooltip: Controller.mounted ? "Eject ISO: disconnect the virtual CD-ROM" : "Mount ISO: attach a local ISO image read-only as a virtual CD-ROM"
        onTriggered: Controller.mounted ? Controller.ejectISO() : Controller.mountISO()
    }
    Kirigami.Action {
        id: powerAction
        text: "Power"
        icon.name: "system-shutdown"
        tooltip: Families.powerHint(root.shown.family)
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
        tooltip: "Copy the remote screen as an image (" + Shortcuts.copyScreen + ")"
        onTriggered: root.copyScreen()
    }
    Kirigami.Action {
        id: fitAction
        objectName: "fitAction"
        text: view.fit ? "Actual size" : "Fit to window"
        icon.name: view.fit ? "zoom-original" : "zoom-fit-best"
        displayHint: Kirigami.DisplayHint.IconOnly
        tooltip: text
        // The setting, which the view follows; assigning the view's fit would
        // break that binding.
        onTriggered: AppSettings.fitToWindow = !AppSettings.fitToWindow
    }
    Kirigami.Action {
        id: fullscreenAction
        text: root.fullscreen ? "Leave full screen" : "Full screen"
        icon.name: root.fullscreen ? "view-restore" : "view-fullscreen"
        displayHint: Kirigami.DisplayHint.IconOnly
        tooltip: text + " (" + Shortcuts.fullScreen + ")"
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
    // Disconnect and Quit have buttons of their own, in the header and in full
    // screen. Their keys are Main.qml's shortcuts.
    Kirigami.Action {
        id: disconnectAction
        text: "Disconnect"
        icon.name: "network-disconnect"
        tooltip: "Close this console session (" + Shortcuts.disconnect + ")"
        onTriggered: Controller.disconnectHost()
    }
    Kirigami.Action {
        id: quitAction
        text: "Quit OldIPMI"
        icon.name: "application-exit"
        onTriggered: root.quitRequested()
    }
    // In order of importance: what does not fit moves to the overflow menu from the end.
    readonly property list<Kirigami.Action> connectedActions: [sendKeyAction, powerAction, mediaAction, copyAction, fitAction, fullscreenAction, infoAction]

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

                    // The console item always exists: app/application.cpp connects frames and input to it.
                    ConsoleView {
                        id: view
                        objectName: "console"
                        anchors.fill: parent
                        anchors.margins: root.fullscreen ? 0 : Math.round(Theme.gu * 0.8)
                        visible: Controller.hasFrame
                        fit: AppSettings.fitToWindow
                        rightCtrlReleases: AppSettings.rightCtrlReleases
                        Accessible.role: Accessible.Graphic
                        Accessible.name: "Remote console of " + root.displayName
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
                    FullscreenBar {
                        id: fsBar
                        objectName: "fullscreenBar"
                        z: 5
                        fullscreen: root.fullscreen
                        captured: view.captured
                        displayName: root.displayName
                        family: root.shown.family
                        mouseOnlyRelease: root.mouseOnlyRelease
                        pointerAtTop: screenHover.hovered && screenHover.point.position.y < Theme.gu * 0.8
                        peeking: peek.running
                        quitItem: fsQuit
                        keysMenu: keysMenu
                        powerMenu: powerMenu
                        sendKeyAction: sendKeyAction
                        mediaAction: mediaAction
                        powerAction: powerAction
                        copyAction: copyAction
                        fitAction: fitAction
                        fullscreenAction: fullscreenAction
                        disconnectAction: disconnectAction
                        onCaptureRequested: view.capture()
                        onReleaseRequested: view.release()
                    }
                    // Quit stays in the corner for the whole time in full screen, also
                    // while the bar is hidden.
                    QuitCorner {
                        id: fsQuit
                        visible: root.fullscreen
                        anchors { top: parent.top; right: parent.right; margins: Math.round(Theme.gu * 0.8) }
                        z: 5
                        action: quitAction
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
                    CaptureNote {
                        id: toast
                        objectName: "fullscreenNote"
                        captured: view.captured
                        displayName: root.displayName
                        barFocused: fsBar.focused
                        shown: root.fullscreen && toastTimer.running
                        room: parent.width - 2 * fsQuit.clearance
                        anchors.horizontalCenter: parent.horizontalCenter
                        y: fsBar.visible ? fsBar.y + fsBar.height + Kirigami.Units.largeSpacing : Math.round(Theme.gu * 0.8)
                        z: 4
                    }
                }

                // Status strip: capture state, release, media and resolution.
                StatusStrip {
                    id: strip
                    visible: !root.fullscreen
                    anchors { left: parent.left; right: parent.right; bottom: parent.bottom }
                    radius: stage.radius
                    captured: view.captured
                    displayName: root.displayName
                    mouseOnlyRelease: root.mouseOnlyRelease
                    onCaptureRequested: view.capture()
                    onReleaseRequested: view.release()
                }
            }

            SetupPage {
                anchors { fill: parent; leftMargin: surface.margin; rightMargin: surface.margin; bottomMargin: surface.margin }
                visible: root.mode === "setup"
                profile: root.shown
                connState: root.connState
                startedAt: root.session.startedAt || 0
                onCancelRequested: Controller.disconnectHost()
            }
            ErrorPage {
                anchors { fill: parent; leftMargin: surface.margin; rightMargin: surface.margin; bottomMargin: surface.margin }
                visible: root.mode === "error"
                profile: root.shown
                errorStage: root.session.errorStage || ""
                message: visible ? Controller.message : ""
                errorCode: visible ? Controller.errorCode : ""
                onRetryRequested: root.connectRequested(Controller.activeProfile)
                onEditRequested: root.editRequested(Controller.host(Controller.activeProfile.id) || Controller.activeProfile)
            }
            IdlePage {
                anchors.fill: parent
                visible: root.mode === "idle"
                profile: root.selected
                notice: visible ? Controller.message : ""
                onConnectRequested: root.connectRequested(root.selected)
                onEditRequested: root.editRequested(root.selected)
                onDeleteRequested: root.deleteRequested(root.selected)
                onDuplicateRequested: root.duplicateRequested(root.selected)
            }
            WelcomePage {
                anchors.fill: parent
                visible: root.mode === "welcome"
                hasHosts: Controller.hosts.length > 0
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
                ConsoleInfo {
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
            ConsoleInfo {
                anchors.fill: parent
                anchors.margins: 1
            }
        }
    }

    Component { id: menuSeparator; QQC2.MenuSeparator {} }
    // The full-screen bar opens its own menus (so it can stay up while they are
    // open) built from the same actions as the header. A separator has no
    // parent: a Menu is no item, and the one addItem() puts it in keeps it
    // alive (checked with gc()).
    function fillMenu(menu, actions) {
        for (const a of actions) {
            if (a.separator)
                menu.addItem(menuSeparator.createObject(null));
            else
                menu.addAction(a);
        }
    }
}
