pragma ComponentBehavior: Bound
import QtQuick
import QtQuick.Controls as QQC2
import QtQuick.Layouts
import org.kde.kirigami as Kirigami
import "families.js" as Families

// The selected connection, ready to open: what it is, how it logs in, and one
// primary action.
Item {
    id: page

    property var profile: ({})
    // App-level notice ("Profile saved."), shown under the card.
    property string notice: ""
    signal connectRequested()
    signal editRequested()
    signal deleteRequested()
    signal duplicateRequested()

    readonly property string exceptions: Families.exceptions(page.profile)
    readonly property bool wide: card.width >= Theme.gu * 30
    // A page too short for the whole card (large text on a small screen): no
    // picture, and Connect right below the name and address, before the details.
    // Measured without the picture's own visibility, so the choice is stable.
    readonly property bool compact: Theme.gu + cardGrid.anchors.margins * 2
        + (wide ? Math.max(disc.side, info.implicitHeight) : disc.side + cardGrid.rowSpacing + info.implicitHeight)
        > flick.height
    function capitalized(s) { return s ? s.charAt(0).toUpperCase() + s.slice(1) : "" }

    Flickable {
        id: flick
        anchors.fill: parent
        clip: true
        contentWidth: width
        contentHeight: Math.max(height, content.implicitHeight + Theme.gu * 2)
        boundsBehavior: Flickable.StopAtBounds
        QQC2.ScrollBar.vertical: QQC2.ScrollBar {}

        ColumnLayout {
            id: content
            width: Math.min(flick.width - Theme.gu * 2, Theme.gu * 43)
            x: Math.round((flick.width - width) / 2)
            y: Math.max(Theme.gu, Math.round((flick.contentHeight - implicitHeight) / 2 - Theme.gu))
            spacing: Math.round(Theme.gu * 1.2)

            Kirigami.ShadowedRectangle {
                id: card
                Layout.fillWidth: true
                implicitHeight: cardGrid.implicitHeight + cardGrid.anchors.margins * 2
                color: Theme.card
                radius: Theme.radiusHuge + 2
                border.width: 1
                border.color: Theme.dark ? Theme.line : Theme.lineSoft
                shadow.size: Math.round(Theme.gu * 1.6)
                shadow.yOffset: Math.round(Theme.gu * 0.4)
                shadow.color: Theme.alpha(Theme.shadow, Theme.dark ? 0.4 : 0.12)

                GridLayout {
                    id: cardGrid
                    anchors.fill: parent
                    anchors.margins: Math.round(Theme.gu * (page.wide ? 2 : 1.2))
                    columns: page.wide ? 2 : 1
                    columnSpacing: Math.round(Theme.gu * 2)
                    rowSpacing: Math.round(Theme.gu * 1)

                    // Family illustration on a disc.
                    Rectangle {
                        id: disc
                        visible: !page.compact
                        readonly property real side: Math.round(page.wide ? Theme.gu * 11 : Theme.gu * 6)
                        Layout.preferredWidth: side
                        Layout.preferredHeight: side
                        Layout.alignment: page.wide ? Qt.AlignVCenter : Qt.AlignLeft
                        radius: side / 2
                        color: Theme.raised
                        Image {
                            anchors.centerIn: parent
                            width: parent.width * 0.88
                            height: width
                            source: Families.image(page.profile.family)
                            sourceSize.width: width * 2
                            sourceSize.height: height * 2
                            fillMode: Image.PreserveAspectFit
                            smooth: true
                        }
                    }

                    // One column; a short page moves the buttons up (Layout.row).
                    GridLayout {
                        id: info
                        columns: 1
                        rowSpacing: Math.round(Theme.gu * 0.75)
                        Layout.fillWidth: true

                        Flow {
                            spacing: Kirigami.Units.smallSpacing
                            Layout.row: 0
                            Layout.fillWidth: true
                            Chip { text: Families.name(page.profile.family); tone: Theme.accent }
                            Chip { text: "Not connected"; neutral: true; dot: true; dotColor: Theme.muted; bold: false }
                            Chip {
                                visible: page.exceptions !== ""
                                text: page.exceptions
                                tone: Theme.warning
                                iconName: "security-medium"
                            }
                        }
                        QQC2.Label {
                            text: page.profile.name || page.profile.host || ""
                            font.pointSize: page.wide ? Theme.hero : Theme.display
                            font.weight: Font.Bold
                            elide: Text.ElideRight
                            Layout.row: 1
                            Layout.fillWidth: true
                            Accessible.role: Accessible.Heading
                        }
                        RowLayout {
                            spacing: Kirigami.Units.smallSpacing
                            Layout.row: 2
                            Layout.fillWidth: true
                            QQC2.Label {
                                text: page.profile.host || ""
                                font.family: Theme.mono.family
                                font.pointSize: Theme.fontBase * 1.05
                                color: Theme.cardText
                                elide: Text.ElideMiddle
                                Layout.fillWidth: true
                                Layout.maximumWidth: Math.ceil(implicitWidth)
                            }
                            IconButton {
                                icon.name: "edit-copy"
                                text: "Copy address"
                                size: Math.round(Theme.gu * 1.6)
                                onClicked: {
                                    Theme.copyText(page.profile.host || "");
                                    mainWindow.showPassiveNotification("Address copied");
                                }
                            }
                        }

                        Rectangle { Layout.row: 3; Layout.fillWidth: true; Layout.preferredHeight: 1; color: Theme.lineSoft }
                        GridLayout {
                            Layout.row: page.compact ? 6 : 4
                            columns: card.width >= Theme.gu * 22 ? 2 : 1
                            columnSpacing: Math.round(Theme.gu * 1.4)
                            rowSpacing: Math.round(Theme.gu * 0.7)
                            Layout.fillWidth: true
                            Repeater {
                                model: [
                                    {label: "Account", value: page.profile.username || (Families.needsAccount(page.profile.family) ? "Not set" : "Not needed"), mono: !!page.profile.username},
                                    {label: "Last connected", value: page.capitalized(Families.relativeTime(page.profile.lastConnected, Theme.minuteNow))},
                                    {label: "Password", value: !Families.needsAccount(page.profile.family) ? "Not needed"
                                                                : page.profile.remember ? "Kept in KWallet" : "Asked for on connect"},
                                    {label: "Security", value: page.exceptions !== "" ? "Exceptions enabled" : "No exceptions", warn: page.exceptions !== ""}
                                ]
                                delegate: ColumnLayout {
                                    id: fact
                                    required property var modelData
                                    spacing: 2
                                    Layout.fillWidth: true
                                    Layout.preferredWidth: 1
                                    QQC2.Label {
                                        text: fact.modelData.label.toUpperCase()
                                        font.pointSize: Theme.tiny
                                        font.weight: Font.DemiBold
                                        font.letterSpacing: 0.6
                                        color: Theme.muted
                                    }
                                    QQC2.Label {
                                        text: fact.modelData.value
                                        font.family: fact.modelData.mono ? Theme.mono.family : Kirigami.Theme.defaultFont.family
                                        color: fact.modelData.warn ? Theme.toneText(Theme.warning, Theme.card) : Theme.text
                                        elide: Text.ElideRight
                                        Layout.fillWidth: true
                                    }
                                }
                            }
                        }
                        Rectangle { Layout.row: 5; Layout.fillWidth: true; Layout.preferredHeight: 1; color: Theme.lineSoft }

                        Flow {
                            spacing: Kirigami.Units.largeSpacing
                            Layout.row: page.compact ? 4 : 6
                            Layout.fillWidth: true
                            Layout.topMargin: Kirigami.Units.smallSpacing
                            ActionButton {
                                id: connectButton
                                tone: "primary"
                                large: true
                                text: "Connect"
                                icon.name: "media-playback-start"
                                onClicked: page.connectRequested()
                            }
                            ActionButton {
                                tone: "secondary"
                                large: true
                                text: "Edit…"
                                icon.name: "document-edit"
                                onClicked: page.editRequested()
                            }
                            IconButton {
                                id: moreButton
                                icon.name: "overflow-menu"
                                text: "More actions"
                                size: Theme.controlHeightLarge
                                checked: moreMenu.visible
                                onClicked: moreMenu.popup(moreButton, 0, moreButton.height)
                                QQC2.Menu {
                                    id: moreMenu
                                    QQC2.MenuItem { text: "Duplicate"; icon.name: "edit-copy"; onTriggered: page.duplicateRequested() }
                                    QQC2.MenuItem { text: "Delete…"; icon.name: "edit-delete"; onTriggered: page.deleteRequested() }
                                }
                            }
                        }
                    }
                }
            }

            // What the keyboard can do from the connection list.
            Flow {
                spacing: Math.round(Theme.gu * 1)
                Layout.alignment: Qt.AlignHCenter
                Layout.maximumWidth: content.width
                KeyHint { keys: "Enter"; label: "Connect" }
                KeyHint { keys: "F2"; label: "Edit" }
                KeyHint { keys: "Del"; label: "Delete" }
                KeyHint { keys: "Ctrl+F"; label: "Find a server" }
            }

            QQC2.Label {
                visible: page.notice !== ""
                text: page.notice
                color: Theme.muted
                horizontalAlignment: Text.AlignHCenter
                wrapMode: Text.Wrap
                Layout.fillWidth: true
            }
        }
    }
}
