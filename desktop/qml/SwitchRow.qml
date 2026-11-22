import QtQuick
import QtQuick.Controls as QQC2
import QtQuick.Layouts
import org.kde.kirigami as Kirigami

// A switch with a title and an explanation, as one row.
RowLayout {
    id: switchRow
    property alias title: switchTitle.text
    property alias description: switchText.text
    property alias checked: toggle.checked
    spacing: Kirigami.Units.largeSpacing
    Layout.fillWidth: true
    ColumnLayout {
        spacing: 1
        Layout.fillWidth: true
        QQC2.Label { id: switchTitle; font.weight: Font.DemiBold; wrapMode: Text.Wrap; Layout.fillWidth: true }
        QQC2.Label { id: switchText; font.pointSize: Theme.small; color: Theme.muted; wrapMode: Text.Wrap; Layout.fillWidth: true }
    }
    QQC2.Switch {
        id: toggle
        Accessible.name: switchTitle.text
        // What switching it on means, also for a screen reader that Tabs straight here.
        Accessible.description: switchText.text
        Layout.alignment: Qt.AlignVCenter
    }
}
