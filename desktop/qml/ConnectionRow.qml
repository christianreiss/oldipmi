import QtQuick
import QtQuick.Controls as QQC2
import QtQuick.Layouts
import org.kde.kirigami as Kirigami
import OldIPMI 1.0
import "families.js" as Families
import "format.js" as Format
import "stages.js" as Stages

// A row of the connection list: the group's heading where a group starts, then
// the connection with its picture, name, state and action, and its context
// menu. The list reads profile, active and openMenu() through currentItem.
ColumnLayout {
    id: entry
    required property var modelData
    required property int index
    // From the list: the sessions by profile id; whether this row starts its
    // group (the open consoles, then the other connections), and the group's
    // size; whether the list is wide enough for a live miniature.
    required property var sessions
    required property bool firstOfGroup
    required property int groupSize
    required property bool wide
    signal selected(string id)
    signal connectRequested(var profile)
    signal disconnectRequested(var profile)
    signal editRequested(var profile)
    signal duplicateRequested(var profile)
    signal deleteRequested(var profile)
    readonly property var profile: modelData.profile
    readonly property bool open: modelData.open
    readonly property var sess: sessions[profile.id] || ({state: "disconnected", errorStage: ""})
    readonly property bool active: Stages.isOpen(sessions, profile.id)
    readonly property bool failed: !active && (sess.errorStage || "") !== ""
    // The context menu from the keyboard, below the row instead of at the pointer.
    function openMenu() { contextMenu.popup(row, Kirigami.Units.largeSpacing * 2, row.height) }
    // A live miniature only where the list has room for it next to the name.
    readonly property bool thumbnail: open && wide
    readonly property bool current: ListView.isCurrentItem
    // The state in words: never colour alone.
    readonly property string stateText: failed ? Stages.failedTitle(sess.errorStage)
        : sess.state === "connected" ? "Connected"
        : active ? (Stages.stages[Math.max(0, Stages.stageIndex(sess.state))] || {name: "Connecting"}).name
                   + (sess.startedAt ? " · " + Format.elapsed(sess.startedAt, Theme.now) : "")
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
            text: entry.groupSize
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
        onClicked: { entry.ListView.view.currentIndex = entry.index; entry.ListView.view.forceActiveFocus(); entry.selected(entry.profile.id) }
        onDoubleClicked: entry.connectRequested(entry.profile)

        background: Rectangle {
            radius: Theme.radius + 1
            color: entry.current ? Theme.accentTint : row.hovered ? Theme.hover : "transparent"
            border.width: entry.current ? (entry.ListView.view.activeFocus ? 2 : 1) : 0
            border.color: entry.ListView.view.activeFocus ? Theme.accent : Theme.accentEdge
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
                FamilyImage {
                    visible: !thumb.hasFrame
                    anchors.centerIn: parent
                    opacity: entry.failed ? 0.6 : 1
                    family: entry.profile.family
                    side: Math.round(parent.height * 0.78)
                }
            }
            FamilyImage {
                visible: !entry.thumbnail
                Layout.alignment: Qt.AlignVCenter
                family: entry.profile.family
                side: Math.round(Theme.gu * 2)
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
                    visible: !entry.open && !row.showActions && Format.exceptions(entry.profile) !== ""
                    width: Kirigami.Units.iconSizes.small
                    height: width
                    source: "security-medium"
                    color: Theme.toneText(Theme.warning, Theme.sidebar)
                    isMask: true
                    QQC2.ToolTip.visible: exceptionsHover.hovered
                    QQC2.ToolTip.text: "Security exceptions: " + Format.exceptions(entry.profile)
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
                    onClicked: entry.active ? entry.disconnectRequested(entry.profile) : entry.connectRequested(entry.profile)
                }
            }
        }

        TapHandler {
            acceptedButtons: Qt.RightButton
            onTapped: { entry.ListView.view.currentIndex = entry.index; entry.selected(entry.profile.id); contextMenu.popup() }
        }
        QQC2.Menu {
            id: contextMenu
            objectName: "rowMenu"
            QQC2.MenuItem { text: entry.active ? "Show" : "Connect"; icon.name: "media-playback-start"; onTriggered: entry.connectRequested(entry.profile) }
            QQC2.MenuItem { text: "Disconnect"; icon.name: "network-disconnect"; enabled: entry.active; onTriggered: entry.disconnectRequested(entry.profile) }
            QQC2.MenuSeparator {}
            QQC2.MenuItem { text: "Edit…"; icon.name: "document-edit"; onTriggered: entry.editRequested(entry.profile) }
            QQC2.MenuItem {
                text: "Duplicate"; icon.name: "edit-copy"
                onTriggered: entry.duplicateRequested(entry.profile)
            }
            QQC2.MenuItem { text: "Delete…"; icon.name: "edit-delete"; enabled: !entry.active; onTriggered: entry.deleteRequested(entry.profile) }
        }
    }
}
