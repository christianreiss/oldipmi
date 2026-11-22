import QtQuick
import QtQuick.Controls as QQC2
import QtQuick.Layouts
import org.kde.kirigami as Kirigami

// A form field under its label. The field, and what goes below it (a
// problem with its value), are its children.
ColumnLayout {
    property alias label: fieldLabel.text
    spacing: Kirigami.Units.smallSpacing
    Layout.fillWidth: true
    QQC2.Label {
        id: fieldLabel
        font.pointSize: Theme.small
        font.weight: Font.DemiBold
        color: Theme.mix(Theme.text, Theme.card, 0.1)
        Layout.fillWidth: true
    }
}
