import QtQuick
import QtQuick.Controls as QQC2
import QtQuick.Layouts
import org.kde.kirigami as Kirigami
import "families.js" as Families

// Connection manager: every saved BMC, its live state and its actions.
ColumnLayout {
    id: root
    spacing: 0
    property real headerHeight: Kirigami.Units.gridUnit * 3

    required property string selectedId
    signal selected(string id)
    signal connectRequested(var profile)
    signal editRequested(var profile)
    signal deleteRequested(var profile)
    signal jnlpRequested(var profile)
    signal newRequested()
    signal settingsRequested(string module)
    signal quitRequested()

    property alias searchText: search.text
    function focusSearch() { search.forceActiveFocus(); search.selectAll() }

    readonly property var profiles: {
        const q = search.text.trim().toLowerCase();
        return controller.hosts
            .filter(p => !q || (p.name || "").toLowerCase().includes(q) || (p.host || "").toLowerCase().includes(q))
            .sort((a, b) => (a.name || a.host).localeCompare(b.name || b.host, undefined, {sensitivity: "base"}));
    }
    readonly property bool busy: controller.state !== "disconnected"

    QQC2.ToolBar {
        Layout.fillWidth: true
        Layout.preferredHeight: root.headerHeight
        contentItem: RowLayout {
            id: pageHeader
            spacing: Kirigami.Units.smallSpacing
            Kirigami.Heading {
                text: "Connections"
                level: 2
                Layout.fillWidth: true
                Layout.leftMargin: Kirigami.Units.smallSpacing
                elide: Text.ElideRight
            }
            QQC2.ToolButton {
                icon.name: "list-add"
                text: "New connection…"
                display: QQC2.AbstractButton.IconOnly
                QQC2.ToolTip.visible: hovered
                QQC2.ToolTip.text: text + " (Ctrl+N)"
                onClicked: root.newRequested()
            }
            QQC2.ToolButton {
                id: menuButton
                objectName: "mcpButton"
                icon.name: "application-menu"
                text: "Main menu"
                display: QQC2.AbstractButton.IconOnly
                QQC2.ToolTip.visible: hovered && !appMenu.visible
                QQC2.ToolTip.text: text
                down: appMenu.visible
                onClicked: appMenu.visible ? appMenu.close() : appMenu.popup(menuButton, 0, menuButton.height)
                QQC2.Menu {
                    id: appMenu
                    QQC2.MenuItem { text: "Settings…"; icon.name: "configure"; onTriggered: root.settingsRequested("general") }
                    QQC2.MenuItem { text: "AI agents (MCP)…"; icon.name: "dialog-scripts"; onTriggered: root.settingsRequested("agents") }
                    QQC2.MenuSeparator {}
                    QQC2.MenuItem { text: "About OldIPMI"; icon.name: "help-about"; onTriggered: root.settingsRequested("about") }
                    QQC2.MenuItem { text: "Quit"; icon.name: "application-exit"; onTriggered: root.quitRequested() }
                }
            }
        }
    }

    Kirigami.SearchField {
        id: search
        Layout.fillWidth: true
        Layout.margins: Kirigami.Units.smallSpacing
        visible: controller.hosts.length > 0
        placeholderText: "Filter connections…"
        KeyNavigation.down: list
        Keys.onReturnPressed: if (root.profiles.length) { list.currentIndex = 0; list.forceActiveFocus() }
    }

    QQC2.ScrollView {
        Layout.fillWidth: true
        Layout.fillHeight: true
        QQC2.ScrollBar.horizontal.policy: QQC2.ScrollBar.AlwaysOff

        ListView {
            id: list
            model: root.profiles
            clip: true
            keyNavigationEnabled: true
            activeFocusOnTab: true
            // Imperative: ListView resets currentIndex on model changes, which breaks a binding.
            function syncCurrent() { currentIndex = root.profiles.findIndex(p => p.id === root.selectedId) }
            Component.onCompleted: syncCurrent()
            onCountChanged: Qt.callLater(syncCurrent)
            Connections {
                target: root
                function onSelectedIdChanged() { list.syncCurrent() }
                function onProfilesChanged() { Qt.callLater(list.syncCurrent) }
            }
            Accessible.name: "Connections"

            Keys.onReturnPressed: if (currentItem) root.connectRequested(currentItem.profile)
            Keys.onDeletePressed: if (currentItem && !currentItem.active) root.deleteRequested(currentItem.profile)
            Keys.onPressed: event => {
                if (event.key === Qt.Key_F2 && currentItem) { root.editRequested(currentItem.profile); event.accepted = true }
            }
            // Only keyboard navigation selects; model resets must not change the selection.
            onCurrentIndexChanged: if (activeFocus && currentItem && currentItem.profile.id !== root.selectedId) root.selected(currentItem.profile.id)

            delegate: QQC2.ItemDelegate {
                id: row
                required property var modelData
                required property int index
                readonly property var profile: modelData
                readonly property bool active: controller.activeProfile.id === profile.id && controller.state !== "disconnected"
                readonly property bool failed: controller.activeProfile.id === profile.id && controller.state === "disconnected" && controller.errorStage !== ""
                readonly property string stateText: !active ? (failed ? Families.failedTitle(controller.errorStage) : "")
                    : controller.state === "connected" ? "Connected" : "Connecting…"

                width: ListView.view.width
                highlighted: ListView.isCurrentItem
                onClicked: { list.currentIndex = index; root.selected(profile.id) }
                onDoubleClicked: root.connectRequested(profile)
                Accessible.name: (profile.name || profile.host) + (stateText ? ", " + stateText : "")
                Accessible.description: profile.host + ", " + Families.name(profile.family)

                contentItem: RowLayout {
                    spacing: Kirigami.Units.largeSpacing
                    Kirigami.Icon {
                        source: "computer"
                        implicitWidth: Kirigami.Units.iconSizes.medium
                        implicitHeight: implicitWidth
                        Layout.alignment: Qt.AlignVCenter
                        // Live state is shown as an emblem plus text, never colour alone.
                        Kirigami.Icon {
                            visible: row.active || row.failed
                            anchors { right: parent.right; bottom: parent.bottom }
                            width: Kirigami.Units.iconSizes.small; height: width
                            source: row.failed ? "data-error" : controller.state === "connected" ? "emblem-success" : "emblem-synchronizing"
                        }
                    }
                    ColumnLayout {
                        spacing: 0
                        Layout.fillWidth: true
                        QQC2.Label {
                            text: row.profile.name || row.profile.host
                            font.weight: row.active ? Font.DemiBold : Font.Normal
                            elide: Text.ElideRight
                            Layout.fillWidth: true
                            color: row.highlighted ? Kirigami.Theme.highlightedTextColor : Kirigami.Theme.textColor
                        }
                        QQC2.Label {
                            text: row.stateText || (row.profile.host + " · " + Families.name(row.profile.family))
                            font: Kirigami.Theme.smallFont
                            elide: Text.ElideRight
                            Layout.fillWidth: true
                            color: row.highlighted ? Kirigami.Theme.highlightedTextColor
                                 : row.failed ? Kirigami.Theme.negativeTextColor
                                 : row.active ? Kirigami.Theme.positiveTextColor : Kirigami.ColorUtils.linearInterpolation(Kirigami.Theme.textColor, Kirigami.Theme.backgroundColor, 0.15)
                        }
                    }
                    QQC2.ToolButton {
                        icon.name: "media-playback-start"
                        text: "Connect to " + (row.profile.name || row.profile.host)
                        display: QQC2.AbstractButton.IconOnly
                        // Always laid out, only faded in: toggling visible resized the
                        // row on hover and made the whole list jump.
                        readonly property bool shown: !row.active && (row.hovered || row.highlighted)
                        opacity: shown ? 1 : 0
                        enabled: shown
                        Accessible.ignored: !shown
                        QQC2.ToolTip.visible: hovered
                        QQC2.ToolTip.text: root.busy ? "Disconnect the open console and connect" : "Connect"
                        onClicked: root.connectRequested(row.profile)
                    }
                }

                TapHandler {
                    acceptedButtons: Qt.RightButton
                    onTapped: { list.currentIndex = row.index; root.selected(row.profile.id); contextMenu.popup() }
                }
                QQC2.Menu {
                    id: contextMenu
                    QQC2.MenuItem { text: "Connect"; icon.name: "media-playback-start"; enabled: !row.active; onTriggered: root.connectRequested(row.profile) }
                    QQC2.MenuItem {
                        text: "Connect with JNLP file…"; icon.name: "document-open"
                        enabled: !root.busy && Families.hasMedia(row.profile.family)
                        onTriggered: root.jnlpRequested(row.profile)
                    }
                    QQC2.MenuSeparator {}
                    QQC2.MenuItem { text: "Edit…"; icon.name: "document-edit"; onTriggered: root.editRequested(row.profile) }
                    QQC2.MenuItem {
                        text: "Duplicate"; icon.name: "edit-copy"
                        onTriggered: { const copy = controller.duplicateHost(row.profile.id); if (copy.id) root.selected(copy.id) }
                    }
                    QQC2.MenuItem { text: "Delete…"; icon.name: "edit-delete"; enabled: !row.active; onTriggered: root.deleteRequested(row.profile) }
                }
            }

            Kirigami.PlaceholderMessage {
                anchors.centerIn: parent
                width: parent.width - Kirigami.Units.gridUnit * 2
                visible: list.count === 0 && controller.hosts.length > 0
                icon.name: "edit-none"
                text: "No matches"
                explanation: "No connection name or address contains “" + search.text + "”."
                helpfulAction: Kirigami.Action { text: "Clear filter"; icon.name: "edit-clear"; onTriggered: search.clear() }
            }
        }
    }

    // Footer: Settings and Quit, always one click away.
    Kirigami.Separator { Layout.fillWidth: true }
    RowLayout {
        Layout.fillWidth: true
        Layout.margins: Kirigami.Units.smallSpacing
        spacing: Kirigami.Units.smallSpacing
        QQC2.ToolButton {
            icon.name: "configure"
            text: "Settings"
            QQC2.ToolTip.visible: hovered
            QQC2.ToolTip.text: "Settings (Ctrl+,)"
            onClicked: root.settingsRequested("general")
        }
        Item { Layout.fillWidth: true }
        QQC2.ToolButton {
            objectName: "quitButton"
            icon.name: "application-exit"
            text: "Quit"
            QQC2.ToolTip.visible: hovered
            QQC2.ToolTip.text: "Quit OldIPMI (Ctrl+Q)"
            onClicked: root.quitRequested()
        }
    }
}
