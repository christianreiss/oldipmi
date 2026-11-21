import QtQuick
import QtQuick.Controls as QQC2
import QtQuick.Layouts
import org.kde.kirigami as Kirigami

// A security warning: a shield and a sentence in a box tinted with the warning
// colour.
Rectangle {
    property alias text: label.text

    Layout.fillWidth: true
    implicitHeight: row.implicitHeight + Math.round(Theme.gu * 0.9)
    radius: Theme.radius
    color: Theme.alpha(Theme.warning, Theme.dark ? 0.18 : 0.12)
    RowLayout {
        id: row
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
            id: label
            color: Theme.toneText(Theme.warning, Theme.card)
            wrapMode: Text.Wrap
            Layout.fillWidth: true
        }
    }
}
