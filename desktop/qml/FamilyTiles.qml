pragma ComponentBehavior: Bound
import QtQuick
import QtQuick.Controls as QQC2
import QtQuick.Layouts
import org.kde.kirigami as Kirigami
import "families.js" as Families

// A connection's console type: one tile per family, a list instead where the
// window is short (compact), and the chosen family's hint. A familyIndex of -1
// is a stored family this version does not know; family is its id.
ColumnLayout {
    id: familyTiles
    required property int familyIndex
    required property string family
    required property bool compact
    signal chosen(int index)
    spacing: Kirigami.Units.largeSpacing
    Layout.fillWidth: true
    Caption { text: "CONSOLE TYPE"; Layout.fillWidth: true }
    QQC2.ComboBox {
        id: familyBox
        visible: familyTiles.compact
        model: Families.names()
        currentIndex: familyTiles.familyIndex
        onActivated: index => familyTiles.chosen(index)
        Accessible.name: "Console type"
        Layout.fillWidth: true
    }
    GridLayout {
        id: tiles
        visible: !familyTiles.compact
        readonly property real tileWidth: Theme.gu * 5.4
        columns: Math.max(2, Math.min(Families.list.length, Math.floor((width + columnSpacing) / (tileWidth + columnSpacing))))
        columnSpacing: Kirigami.Units.largeSpacing
        rowSpacing: Kirigami.Units.largeSpacing
        Layout.fillWidth: true
        Repeater {
            id: tileRepeater
            model: Families.list
            delegate: QQC2.AbstractButton {
                id: tile
                required property var modelData
                required property int index
                readonly property bool selected: familyTiles.familyIndex === index
                function choose(i) {
                    const k = (i + Families.list.length) % Families.list.length;
                    familyTiles.chosen(k);
                    tileRepeater.itemAt(k).forceActiveFocus(Qt.TabFocusReason);
                }
                Layout.fillWidth: true
                Layout.preferredWidth: 1
                implicitHeight: Math.max(Math.round(Theme.gu * (tiles.columns < 5 ? 4.4 : 5.2)), tileContent.implicitHeight + Theme.gu)
                hoverEnabled: true
                focusPolicy: Qt.StrongFocus
                padding: Kirigami.Units.smallSpacing
                Accessible.role: Accessible.RadioButton
                Accessible.name: modelData.name
                Accessible.description: modelData.hint
                Accessible.checkable: true
                Accessible.checked: selected
                onClicked: familyTiles.chosen(index)
                Keys.onLeftPressed: choose(index - 1)
                Keys.onRightPressed: choose(index + 1)
                Keys.onSpacePressed: familyTiles.chosen(index)
                background: Rectangle {
                    radius: Theme.radiusPanel
                    color: tile.selected ? Theme.accentTint
                         : tile.hovered ? Theme.raisedHover : Theme.raised
                    border.width: tile.selected ? 2 : 1
                    border.color: tile.selected ? Theme.accent : Theme.lineSoft
                    Rectangle {
                        visible: tile.selected
                        anchors { top: parent.top; right: parent.right; margins: 6 }
                        width: Math.round(Theme.gu * 1.05)
                        height: width
                        radius: width / 2
                        color: Theme.accentStrong
                        Kirigami.Icon {
                            anchors.centerIn: parent
                            width: Math.round(parent.width * 0.7)
                            height: width
                            source: "checkmark"
                            color: Theme.white
                            isMask: true
                        }
                    }
                    FocusRing {
                        visible: tile.visualFocus
                    }
                }
                contentItem: ColumnLayout {
                    id: tileContent
                    spacing: 2
                    FamilyImage {
                        Layout.alignment: Qt.AlignHCenter
                        family: tile.modelData.id
                        side: Math.round(Theme.gu * (tiles.columns < 5 ? 2 : 2.55))
                    }
                    QQC2.Label {
                        text: tile.modelData.short
                        font.pointSize: Theme.small
                        font.weight: tile.selected ? Font.DemiBold : Font.Normal
                        horizontalAlignment: Text.AlignHCenter
                        elide: Text.ElideRight
                        Layout.fillWidth: true
                    }
                }
            }
        }
    }
    QQC2.Label {
        visible: !familyTiles.compact
        text: familyTiles.familyIndex >= 0 ? Families.list[familyTiles.familyIndex].hint
            : "This connection's console type, \"" + familyTiles.family + "\", is not one this version of OldIPMI knows. Choose the BMC's type above; saving without one keeps it."
        font.pointSize: Theme.small
        color: Theme.muted
        wrapMode: Text.Wrap
        Layout.fillWidth: true
    }
}
