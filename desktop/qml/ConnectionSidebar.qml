import QtQuick
import QtQuick.Controls as QQC2
import QtQuick.Layouts
import org.kde.kirigami as Kirigami
import OldIPMI 1.0
import "families.js" as Families

// Connection manager: open consoles first (with a live picture), then every
// saved BMC with its state and actions.
Rectangle {
    id: root
    color: Theme.sidebar

    required property string selectedId
    signal selected(string id)
    signal connectRequested(var profile)
    signal editRequested(var profile)
    signal deleteRequested(var profile)
    signal newRequested(string host)
    signal settingsRequested(string module)
    signal quitRequested()

    property alias searchText: search.text
    function focusSearch() { search.forceActiveFocus(); search.selectAll() }

    // Sessions are read once per change here; rows look themselves up in it.
    readonly property var sess: controller.sessions
    function isOpen(id) { const s = sess[id]; return !!s && s.state !== "disconnected" }
    // The list model changes only when the profiles, the filter or the set of
    // open consoles changes; controller.changed fires for every pointer ack.
    readonly property string openKey: Object.keys(sess).filter(id => isOpen(id)).sort().join("\n")
    property var rows: []
    function rebuild() {
        const q = search.text.trim().toLowerCase();
        const byName = (a, b) => (a.name || a.host).localeCompare(b.name || b.host, undefined, {sensitivity: "base"});
        const hits = controller.hosts
            .filter(p => !q || (p.name || "").toLowerCase().includes(q) || (p.host || "").toLowerCase().includes(q))
            .sort(byName);
        const open = hits.filter(p => isOpen(p.id)).map(p => ({profile: p, open: true}));
        const saved = hits.filter(p => !isOpen(p.id)).map(p => ({profile: p, open: false}));
        rows = open.concat(saved);
    }
    readonly property int openRows: rows.filter(r => r.open).length
    // Never synchronously from a row's own handler: rebuilding destroys the row.
    onOpenKeyChanged: Qt.callLater(rebuild)
    Connections {
        target: controller
        function onHostsChanged() { Qt.callLater(root.rebuild) }
    }
    Component.onCompleted: rebuild()

    ColumnLayout {
        anchors.fill: parent
        spacing: 0

        // ── Brand and global actions ──
        RowLayout {
            Layout.fillWidth: true
            Layout.preferredHeight: Math.max(Math.round(Theme.gu * 3.1), implicitHeight)
            Layout.leftMargin: Math.round(Theme.gu * 0.8)
            Layout.rightMargin: Math.round(Theme.gu * 0.55)
            spacing: Kirigami.Units.largeSpacing
            Image {
                Layout.preferredWidth: Math.round(Theme.gu * 1.45)
                Layout.preferredHeight: Layout.preferredWidth
                source: "qrc:/oldipmi.svg"
                sourceSize.width: width * 2
                sourceSize.height: height * 2
            }
            QQC2.Label {
                text: "OldIPMI"
                font.pointSize: Theme.title
                font.weight: Font.Bold
                elide: Text.ElideRight
                Layout.fillWidth: true
            }
            IconButton {
                icon.name: "list-add"
                text: "New connection…"
                shortcut: "Ctrl+N"
                onClicked: root.newRequested("")
            }
            IconButton {
                id: menuButton
                objectName: "mcpButton"
                icon.name: "application-menu"
                text: appMenu.visible ? "" : "Main menu"
                Accessible.name: "Main menu"
                checked: appMenu.visible
                onClicked: appMenu.visible ? appMenu.close() : appMenu.popup(menuButton, 0, menuButton.height)
                QQC2.Menu {
                    id: appMenu
                    QQC2.MenuItem { text: "Settings…"; icon.name: "configure"; onTriggered: root.settingsRequested("general") }
                    QQC2.MenuItem { text: "AI agents (MCP)…"; icon.name: "dialog-scripts"; onTriggered: root.settingsRequested("agents") }
                    QQC2.MenuSeparator {}
                    QQC2.MenuItem {
                        text: "Disconnect all"
                        icon.name: "network-disconnect"
                        enabled: controller.openCount > 0
                        onTriggered: controller.disconnectAll()
                    }
                    QQC2.MenuSeparator {}
                    QQC2.MenuItem { text: "About OldIPMI"; icon.name: "help-about"; onTriggered: root.settingsRequested("about") }
                    QQC2.MenuItem { text: "Quit"; icon.name: "application-exit"; onTriggered: root.quitRequested() }
                }
            }
        }

        // ── Filter, or the address of a new connection ──
        QQC2.TextField {
            id: search
            visible: controller.hosts.length > 0
            Layout.fillWidth: true
            Layout.leftMargin: Math.round(Theme.gu * 0.55)
            Layout.rightMargin: Math.round(Theme.gu * 0.55)
            Layout.bottomMargin: Math.round(Theme.gu * 0.55)
            implicitHeight: Math.max(Theme.controlHeight, contentHeight + topPadding + bottomPadding)
            placeholderText: "Filter or connect…"
            placeholderTextColor: Theme.muted
            color: Theme.text
            leftPadding: Math.round(Theme.gu * 1.85)
            rightPadding: (text !== "" ? clearButton.width : keyCap.width) + Math.round(Theme.gu * 0.6)
            verticalAlignment: TextInput.AlignVCenter
            Accessible.name: "Filter connections, or type the address of a new one"
            onTextChanged: root.rebuild()
            KeyNavigation.down: list
            Keys.onReturnPressed: root.acceptSearch()
            Keys.onEnterPressed: root.acceptSearch()
            Keys.onEscapePressed: event => { if (text !== "") { clear(); event.accepted = true } else event.accepted = false }
            background: Rectangle {
                radius: Theme.radius
                color: Theme.dark ? Theme.mix(Theme.window, Theme.black, 0.1) : Theme.view
                border.width: search.activeFocus ? 2 : 1
                border.color: search.activeFocus ? Theme.accent : Theme.line
            }
            Kirigami.Icon {
                anchors { left: parent.left; leftMargin: Math.round(Theme.gu * 0.55); verticalCenter: parent.verticalCenter }
                width: Kirigami.Units.iconSizes.small
                height: width
                source: "search"
                color: Theme.muted
                isMask: true
            }
            KeyHint {
                id: keyCap
                visible: search.text === ""
                anchors { right: parent.right; rightMargin: Math.round(Theme.gu * 0.4); verticalCenter: parent.verticalCenter }
                keys: "Ctrl+F"
            }
            IconButton {
                id: clearButton
                visible: search.text !== ""
                anchors { right: parent.right; rightMargin: 3; verticalCenter: parent.verticalCenter }
                size: search.height - 6
                icon.name: "edit-clear"
                text: "Clear filter"
                onClicked: search.clear()
            }
        }

        QQC2.ScrollView {
            Layout.fillWidth: true
            Layout.fillHeight: true
            QQC2.ScrollBar.horizontal.policy: QQC2.ScrollBar.AlwaysOff

            ListView {
                id: list
                model: root.rows
                clip: true
                keyNavigationEnabled: true
                activeFocusOnTab: true
                bottomMargin: Kirigami.Units.smallSpacing
                // Imperative: ListView resets currentIndex on model changes, which breaks a binding.
                function syncCurrent() { currentIndex = root.rows.findIndex(r => r.profile.id === root.selectedId) }
                onCountChanged: Qt.callLater(syncCurrent)
                Connections {
                    target: root
                    function onSelectedIdChanged() { list.syncCurrent() }
                    function onRowsChanged() { Qt.callLater(list.syncCurrent) }
                }
                Accessible.name: "Connections"

                Keys.onReturnPressed: if (currentItem) root.connectRequested(currentItem.profile)
                Keys.onEnterPressed: if (currentItem) root.connectRequested(currentItem.profile)
                Keys.onDeletePressed: if (currentItem && !currentItem.active) root.deleteRequested(currentItem.profile)
                Keys.onPressed: event => {
                    if (event.key === Qt.Key_F2 && currentItem) { root.editRequested(currentItem.profile); event.accepted = true }
                }
                // Only keyboard navigation selects; model resets must not change the selection.
                onCurrentIndexChanged: if (activeFocus && currentItem && currentItem.profile.id !== root.selectedId) root.selected(currentItem.profile.id)

                delegate: ColumnLayout {
                    id: entry
                    required property var modelData
                    required property int index
                    readonly property var profile: modelData.profile
                    readonly property bool open: modelData.open
                    readonly property var sess: root.sess[profile.id] || ({state: "disconnected", errorStage: ""})
                    readonly property bool active: sess.state !== "disconnected"
                    readonly property bool failed: !active && (sess.errorStage || "") !== ""
                    // A live miniature only where the list has room for it next to the name.
                    readonly property bool thumbnail: open && root.width >= Theme.gu * 14
                    readonly property bool current: ListView.isCurrentItem
                    readonly property bool firstOfGroup: index === 0 || root.rows[index - 1].open !== open
                    // The state in words: never colour alone.
                    readonly property string stateText: failed ? Families.failedTitle(sess.errorStage)
                        : sess.state === "connected" ? "Connected"
                        : active ? (Families.stages[Math.max(0, Families.stageIndex(sess.state))] || {name: "Connecting"}).name
                                   + (sess.startedAt ? " · " + Theme.elapsed(sess.startedAt) : "")
                        : ""
                    readonly property color stateColor: failed ? Theme.negative : sess.state === "connected" ? Theme.positive : Theme.accent
                    width: ListView.view.width
                    spacing: 0

                    // Group heading ("Open 2", "Connections 3").
                    RowLayout {
                        visible: entry.firstOfGroup
                        Layout.fillWidth: true
                        Layout.leftMargin: Math.round(Theme.gu * 1)
                        Layout.rightMargin: Math.round(Theme.gu * 1)
                        Layout.topMargin: entry.index === 0 ? Kirigami.Units.smallSpacing : Math.round(Theme.gu * 0.8)
                        Layout.bottomMargin: Kirigami.Units.smallSpacing
                        QQC2.Label {
                            text: entry.open ? "OPEN" : "CONNECTIONS"
                            font.pointSize: Theme.tiny
                            font.weight: Font.DemiBold
                            font.letterSpacing: 0.8
                            color: Theme.muted
                            Layout.fillWidth: true
                            Accessible.role: Accessible.Heading
                        }
                        QQC2.Label {
                            text: entry.open ? root.openRows : root.rows.length - root.openRows
                            font.pointSize: Theme.tiny
                            font.weight: Font.DemiBold
                            color: Theme.muted
                        }
                    }

                    QQC2.ItemDelegate {
                        id: row
                        readonly property bool showActions: hovered || entry.current || actionButton.hovered
                        Layout.fillWidth: true
                        Layout.leftMargin: Kirigami.Units.largeSpacing
                        Layout.rightMargin: Kirigami.Units.largeSpacing
                        Layout.bottomMargin: 2
                        implicitHeight: Math.max(Math.round(Theme.gu * (entry.open ? 3.35 : 2.9)), rowContent.implicitHeight + Kirigami.Units.largeSpacing * 2)
                        hoverEnabled: true
                        padding: Kirigami.Units.largeSpacing
                        focusPolicy: Qt.NoFocus // slop-ok: the ListView holds focus (Up/Down, Enter, F2, Del) and rings the current row
                        Accessible.name: (entry.profile.name || entry.profile.host) + (entry.stateText ? ", " + entry.stateText : "")
                        Accessible.description: entry.profile.host + ", " + Families.name(entry.profile.family)
                        onClicked: { list.currentIndex = entry.index; list.forceActiveFocus(); root.selected(entry.profile.id) }
                        onDoubleClicked: root.connectRequested(entry.profile)

                        background: Rectangle {
                            radius: Theme.radius + 1
                            color: entry.current ? Theme.accentTint : row.hovered ? Theme.hover : "transparent"
                            border.width: entry.current ? (list.activeFocus ? 2 : 1) : 0
                            border.color: list.activeFocus ? Theme.accent : Theme.accentEdge
                        }

                        contentItem: RowLayout {
                            id: rowContent
                            spacing: Math.round(Theme.gu * 0.55)

                            // Open console: a live miniature of its screen (the family until the first picture).
                            Rectangle {
                                visible: entry.thumbnail
                                Layout.preferredWidth: Math.round(Theme.gu * 3.55)
                                Layout.preferredHeight: Math.round(Theme.gu * 2.2)
                                Layout.alignment: Qt.AlignVCenter
                                radius: 5
                                // Until the first picture (and after a failure) the family stands in.
                                color: thumb.hasFrame ? Theme.stage : Theme.raised
                                border.width: 1
                                border.color: !thumb.hasFrame ? Theme.lineSoft
                                            : Theme.dark ? Theme.stageLine : Theme.alpha(Theme.black, 0.12)
                                FrameThumbnail {
                                    id: thumb
                                    anchors.fill: parent
                                    anchors.margins: 1
                                    radius: 4
                                    profileId: entry.thumbnail ? entry.profile.id : ""
                                }
                                Image {
                                    visible: !thumb.hasFrame
                                    anchors.centerIn: parent
                                    height: Math.round(parent.height * 0.78)
                                    width: height
                                    source: Families.image(entry.profile.family)
                                    sourceSize.width: width * 2
                                    sourceSize.height: height * 2
                                    fillMode: Image.PreserveAspectFit
                                    smooth: true
                                    opacity: entry.failed ? 0.6 : 1
                                }
                            }
                            Image {
                                visible: !entry.thumbnail
                                Layout.preferredWidth: Math.round(Theme.gu * 2)
                                Layout.preferredHeight: Layout.preferredWidth
                                Layout.alignment: Qt.AlignVCenter
                                source: Families.image(entry.profile.family)
                                sourceSize.width: width * 2
                                sourceSize.height: height * 2
                                fillMode: Image.PreserveAspectFit
                                smooth: true
                            }

                            ColumnLayout {
                                spacing: 1
                                Layout.fillWidth: true
                                Layout.alignment: Qt.AlignVCenter
                                QQC2.Label {
                                    text: entry.profile.name || entry.profile.host
                                    font.weight: entry.open || entry.current ? Font.DemiBold : Font.Medium
                                    color: Theme.text
                                    elide: Text.ElideRight
                                    Layout.fillWidth: true
                                }
                                // Address, or the state with its dot.
                                RowLayout {
                                    spacing: Math.round(Theme.gu * 0.35)
                                    Layout.fillWidth: true
                                    StatusDot {
                                        visible: entry.stateText !== ""
                                        color: entry.stateColor
                                        halo: entry.active
                                        Layout.alignment: Qt.AlignVCenter
                                    }
                                    QQC2.Label {
                                        text: entry.stateText !== "" ? entry.stateText : (entry.profile.host || "")
                                        font.family: entry.stateText !== "" ? Kirigami.Theme.defaultFont.family : Theme.mono.family
                                        font.pointSize: Theme.small
                                        color: entry.failed ? Theme.toneText(Theme.negative, Theme.sidebar) : Theme.muted
                                        elide: Text.ElideRight
                                        Layout.fillWidth: true
                                    }
                                }
                            }

                            // One fixed slot: the security shield at rest, the row's action
                            // on hover or when current. Only faded, never re-laid out, so the
                            // row does not jump under the pointer.
                            Item {
                                Layout.preferredWidth: actionButton.size
                                Layout.preferredHeight: actionButton.size
                                Layout.alignment: Qt.AlignVCenter
                                Kirigami.Icon {
                                    anchors.centerIn: parent
                                    visible: !entry.open && !row.showActions && Families.exceptions(entry.profile) !== ""
                                    width: Kirigami.Units.iconSizes.small
                                    height: width
                                    source: "security-medium"
                                    color: Theme.toneText(Theme.warning, Theme.sidebar)
                                    isMask: true
                                    QQC2.ToolTip.visible: exceptionsHover.hovered
                                    QQC2.ToolTip.text: "Security exceptions: " + Families.exceptions(entry.profile)
                                    HoverHandler { id: exceptionsHover }
                                }
                                IconButton {
                                    id: actionButton
                                    anchors.fill: parent
                                    opacity: row.showActions ? 1 : 0
                                    enabled: row.showActions
                                    Accessible.ignored: !row.showActions
                                    filled: !entry.active
                                    size: Math.round(Theme.gu * 1.55)
                                    icon.name: entry.active ? "network-disconnect" : "media-playback-start"
                                    text: (entry.active ? "Disconnect " : "Connect to ") + (entry.profile.name || entry.profile.host)
                                    onClicked: entry.active ? controller.disconnectProfile(entry.profile.id) : root.connectRequested(entry.profile)
                                }
                            }
                        }

                        TapHandler {
                            acceptedButtons: Qt.RightButton
                            onTapped: { list.currentIndex = entry.index; root.selected(entry.profile.id); contextMenu.popup() }
                        }
                        QQC2.Menu {
                            id: contextMenu
                            QQC2.MenuItem { text: entry.active ? "Show" : "Connect"; icon.name: "media-playback-start"; onTriggered: root.connectRequested(entry.profile) }
                            QQC2.MenuItem { text: "Disconnect"; icon.name: "network-disconnect"; enabled: entry.active; onTriggered: controller.disconnectProfile(entry.profile.id) }
                            QQC2.MenuSeparator {}
                            QQC2.MenuItem { text: "Edit…"; icon.name: "document-edit"; onTriggered: root.editRequested(entry.profile) }
                            QQC2.MenuItem {
                                text: "Duplicate"; icon.name: "edit-copy"
                                onTriggered: { const copy = controller.duplicateHost(entry.profile.id); if (copy.id) root.selected(copy.id) }
                            }
                            QQC2.MenuItem { text: "Delete…"; icon.name: "edit-delete"; enabled: !entry.active; onTriggered: root.deleteRequested(entry.profile) }
                        }
                    }
                }

                // Nothing matches the filter: offer it as the address of a new connection.
                ColumnLayout {
                    anchors.centerIn: parent
                    width: parent.width - Theme.gu * 2
                    visible: list.count === 0 && controller.hosts.length > 0
                    spacing: Kirigami.Units.largeSpacing
                    QQC2.Label {
                        text: "No matches"
                        font.weight: Font.DemiBold
                        horizontalAlignment: Text.AlignHCenter
                        Layout.fillWidth: true
                    }
                    QQC2.Label {
                        text: "No connection name or address contains “" + search.text + "”."
                        color: Theme.muted
                        wrapMode: Text.Wrap
                        horizontalAlignment: Text.AlignHCenter
                        Layout.fillWidth: true
                    }
                    ActionButton {
                        tone: "secondary"
                        text: "New connection to “" + search.text.trim() + "”…"
                        icon.name: "list-add"
                        Layout.alignment: Qt.AlignHCenter
                        Layout.maximumWidth: parent.width
                        onClicked: root.newRequested(search.text.trim())
                    }
                    ActionButton {
                        tone: "ghost"
                        text: "Clear filter"
                        icon.name: "edit-clear"
                        Layout.alignment: Qt.AlignHCenter
                        onClicked: search.clear()
                    }
                }
            }
        }

        // No connections yet: where they will appear.
        Item {
            visible: controller.hosts.length === 0
            Layout.fillWidth: true
            Layout.preferredHeight: emptyText.implicitHeight + Theme.gu * 2.2
            Canvas {
                id: dashed
                anchors { fill: parent; leftMargin: Math.round(Theme.gu * 0.65); rightMargin: Math.round(Theme.gu * 0.65) }
                readonly property color stroke: Theme.line
                onStrokeChanged: requestPaint()
                onWidthChanged: requestPaint()
                onHeightChanged: requestPaint()
                onPaint: {
                    const ctx = getContext("2d");
                    ctx.reset();
                    ctx.strokeStyle = stroke;
                    ctx.lineWidth = 1.5;
                    ctx.setLineDash([4, 3]);
                    const r = Theme.radiusLarge - 2, w = width - 1.5, h = height - 1.5;
                    ctx.beginPath();
                    ctx.roundedRect(0.75, 0.75, w, h, r, r);
                    ctx.stroke();
                }
            }
            QQC2.Label {
                id: emptyText
                anchors.centerIn: parent
                width: dashed.width - Theme.gu * 2
                text: "Your BMCs appear here once you add them."
                color: Theme.muted
                wrapMode: Text.Wrap
                horizontalAlignment: Text.AlignHCenter
            }
        }
        Item { Layout.fillHeight: true; visible: controller.hosts.length === 0 }

        // Footer: Settings and Disconnect all, always one click away (Quit sits at
        // the right end of the console header and in the main menu).
        Rectangle { Layout.fillWidth: true; Layout.preferredHeight: 1; color: Theme.lineSoft }
        RowLayout {
            Layout.fillWidth: true
            Layout.margins: Kirigami.Units.largeSpacing
            spacing: Kirigami.Units.smallSpacing
            ActionButton {
                tone: "ghost"
                icon.name: "configure"
                text: "Settings"
                tooltip: "Settings (Ctrl+,)"
                onClicked: root.settingsRequested("general")
            }
            Item { Layout.fillWidth: true }
            ActionButton {
                objectName: "disconnectAllButton"
                tone: "ghost"
                icon.name: "network-disconnect"
                text: "Disconnect all"
                enabled: controller.openCount > 0
                tooltip: (controller.openCount > 1 ? "Close all " + controller.openCount + " open consoles"
                          : controller.openCount === 1 ? "Close the open console" : "No console is open") + " (Ctrl+Shift+D)"
                // Only its icon in a narrow list, rather than a clipped label.
                display: root.width >= Theme.gu * 15 ? QQC2.AbstractButton.TextBesideIcon : QQC2.AbstractButton.IconOnly
                onClicked: controller.disconnectAll()
            }
        }
    }

    // Enter in the filter: open the list on the first match, or start a new
    // connection to what was typed.
    function acceptSearch() {
        if (rows.length) {
            list.currentIndex = 0;
            list.forceActiveFocus();
            root.selected(rows[0].profile.id);
        } else if (search.text.trim() !== "") {
            root.newRequested(search.text.trim());
        }
    }
}
