pragma ComponentBehavior: Bound
import QtQuick
import QtQuick.Controls as QQC2
import QtQuick.Layouts
import org.kde.kirigami as Kirigami
import "stages.js" as Stages
import "shortcuts.js" as Shortcuts

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
    signal duplicateRequested(var profile)
    signal disconnectRequested(var profile)
    signal newRequested(string host)
    signal settingsRequested(string module)
    signal disconnectAllRequested()
    signal quitRequested()

    property alias searchText: search.text
    function focusSearch() { search.forceActiveFocus(); search.selectAll() }

    // Sessions are read once per change here; rows look themselves up in it.
    readonly property var sess: Controller.sessions
    function isOpen(id) { return Stages.isOpen(sess, id) }
    // The list model changes only when the profiles, the filter or the set of
    // open consoles changes; Controller.changed fires for every pointer ack.
    readonly property string openKey: Object.keys(sess).filter(id => isOpen(id)).sort().join("\n")
    property var rows: []
    // True while the rows are replaced: ListView then jumps to the first row, which
    // must not select it.
    property bool replacingRows: false
    function rebuild() {
        const q = search.text.trim().toLowerCase();
        const byName = (a, b) => (a.name || a.host).localeCompare(b.name || b.host, undefined, {sensitivity: "base"});
        const hits = Controller.hosts
            .filter(p => !q || (p.name || "").toLowerCase().includes(q) || (p.host || "").toLowerCase().includes(q))
            .sort(byName);
        const open = hits.filter(p => isOpen(p.id)).map(p => ({profile: p, open: true}));
        const saved = hits.filter(p => !isOpen(p.id)).map(p => ({profile: p, open: false}));
        replacingRows = true;
        rows = open.concat(saved);
        replacingRows = false;
    }
    readonly property int openRows: rows.filter(r => r.open).length
    // Never synchronously from a row's own handler: rebuilding destroys the row.
    onOpenKeyChanged: Qt.callLater(rebuild)
    Connections {
        target: Controller
        function onHostsChanged() { Qt.callLater(root.rebuild) }
    }
    Component.onCompleted: rebuild()

    ColumnLayout {
        anchors.fill: parent
        spacing: 0

        // ── Brand and global actions ──
        SidebarHeader {
            onNewRequested: root.newRequested("")
            onSettingsRequested: module => root.settingsRequested(module)
            onDisconnectAllRequested: root.disconnectAllRequested()
            onQuitRequested: root.quitRequested()
        }

        // ── Filter, or the address of a new connection ──
        FilterField {
            id: search
            visible: Controller.hosts.length > 0
            Layout.fillWidth: true
            Layout.leftMargin: Math.round(Theme.gu * 0.55)
            Layout.rightMargin: Math.round(Theme.gu * 0.55)
            Layout.bottomMargin: Math.round(Theme.gu * 0.55)
            onTextChanged: root.rebuild()
            KeyNavigation.down: list
            Keys.onReturnPressed: root.acceptSearch()
            Keys.onEnterPressed: root.acceptSearch()
        }

        QQC2.ScrollView {
            Layout.fillWidth: true
            Layout.fillHeight: true
            QQC2.ScrollBar.horizontal.policy: QQC2.ScrollBar.AlwaysOff

            ListView {
                id: list
                objectName: "connectionList"
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
                    // The menu key or Shift+F10: the context menu of the current row.
                    else if ((event.key === Qt.Key_Menu || (event.key === Qt.Key_F10 && event.modifiers & Qt.ShiftModifier)) && currentItem) {
                        currentItem.openMenu();
                        event.accepted = true;
                    }
                }
                // Only keyboard navigation selects; model resets must not change the selection.
                onCurrentIndexChanged: if (!root.replacingRows && activeFocus && currentItem && currentItem.profile.id !== root.selectedId) root.selected(currentItem.profile.id)
                // Tab into a list without a current row lands on the first one, so the
                // focus ring shows where the keyboard is (clicks set the row first).
                onActiveFocusChanged: if (activeFocus && currentIndex < 0 && count > 0) currentIndex = 0

                delegate: ConnectionRow {
                    sessions: root.sess
                    // Also read once for a row the list is about to drop, past the end of new rows.
                    firstOfGroup: index === 0 || root.rows[index - 1]?.open !== open
                    groupSize: open ? root.openRows : root.rows.length - root.openRows
                    wide: root.width >= Theme.gu * 14
                    onSelected: id => root.selected(id)
                    onConnectRequested: p => root.connectRequested(p)
                    onDisconnectRequested: p => root.disconnectRequested(p)
                    onEditRequested: p => root.editRequested(p)
                    onDuplicateRequested: p => root.duplicateRequested(p)
                    onDeleteRequested: p => root.deleteRequested(p)
                }

                // Nothing matches the filter: offer it as the address of a new connection.
                ColumnLayout {
                    anchors.centerIn: parent
                    width: parent.width - Theme.gu * 2
                    visible: list.count === 0 && Controller.hosts.length > 0
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
        EmptyListHint {
            visible: Controller.hosts.length === 0
            Layout.fillWidth: true
        }
        Item { Layout.fillHeight: true; visible: Controller.hosts.length === 0 }

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
                tooltip: "Settings (" + Shortcuts.settings + ")"
                onClicked: root.settingsRequested("general")
            }
            Item { Layout.fillWidth: true }
            ActionButton {
                tone: "ghost"
                icon.name: "network-disconnect"
                text: "Disconnect all"
                enabled: Controller.openCount > 0
                tooltip: (Controller.openCount > 1 ? "Close all " + Controller.openCount + " open consoles"
                          : Controller.openCount === 1 ? "Close the open console" : "No console is open") + " (" + Shortcuts.disconnectAll + ")"
                // Only its icon in a narrow list, rather than a clipped label.
                display: root.width >= Theme.gu * 15 ? QQC2.AbstractButton.TextBesideIcon : QQC2.AbstractButton.IconOnly
                onClicked: root.disconnectAllRequested()
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
