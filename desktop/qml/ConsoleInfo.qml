pragma ComponentBehavior: Bound
import QtQuick
import QtQuick.Controls as QQC2
import QtQuick.Layouts
import org.kde.kirigami as Kirigami

// Console information: protocol, resolution, features and endpoints. In the
// console pane's side panel, or floating over a narrow console with a close
// button (closable).
QQC2.ScrollView {
    id: infoScroll
    // A close button next to the title, when the panel floats over the console.
    property bool closable: false
    signal closeRequested()
    QQC2.ScrollBar.horizontal.policy: QQC2.ScrollBar.AlwaysOff
    contentWidth: availableWidth
    ColumnLayout {
        width: infoScroll.availableWidth
        spacing: Math.round(Theme.gu * 0.6)
        RowLayout {
            spacing: Kirigami.Units.smallSpacing
            Layout.fillWidth: true
            Layout.margins: Math.round(Theme.gu * 0.9)
            Layout.rightMargin: infoScroll.closable ? Math.round(Theme.gu * 0.4) : Math.round(Theme.gu * 0.9)
            Layout.bottomMargin: 0
            QQC2.Label {
                text: "Console information"
                font.pointSize: Theme.title
                font.weight: Font.Bold
                wrapMode: Text.WordWrap
                Layout.fillWidth: true
                Accessible.role: Accessible.Heading
            }
            IconButton {
                visible: infoScroll.closable
                icon.name: "window-close-symbolic"
                text: "Close console information"
                Layout.alignment: Qt.AlignTop
                onClicked: infoScroll.closeRequested()
            }
        }
        Repeater {
            model: [
                {label: "Protocol", value: controller.consoleInfo.protocol},
                {label: "Protocol version", value: controller.consoleInfo.protocolVersion},
                {label: "Resolution", value: controller.consoleInfo.resolution},
                {label: "Features", value: (controller.consoleInfo.capabilities || []).join(", ")}
            ].concat((controller.consoleInfo.endpoints || []).map(e => ({label: e.role, value: e.address + "\n" + e.transport, mono: true})))
             .filter(row => !!row.value)
            delegate: ColumnLayout {
                id: infoRow
                required property var modelData
                spacing: 2
                Layout.fillWidth: true
                Layout.leftMargin: Math.round(Theme.gu * 0.9)
                Layout.rightMargin: Math.round(Theme.gu * 0.9)
                Caption {
                    text: infoRow.modelData.label.toUpperCase()
                    wrapMode: Text.Wrap
                    Layout.fillWidth: true
                }
                QQC2.Label {
                    text: infoRow.modelData.value
                    font.family: infoRow.modelData.mono ? Theme.mono.family : Kirigami.Theme.defaultFont.family
                    font.pointSize: infoRow.modelData.mono ? Theme.small : Theme.fontBase
                    wrapMode: Text.WrapAtWordBoundaryOrAnywhere
                    Layout.fillWidth: true
                }
            }
        }
        QQC2.Label {
            visible: !controller.consoleInfo.protocol
            text: "The backend has not reported console details."
            color: Theme.muted
            wrapMode: Text.Wrap
            Layout.fillWidth: true
            Layout.leftMargin: Math.round(Theme.gu * 0.9)
            Layout.rightMargin: Math.round(Theme.gu * 0.9)
        }
        NoticeBox {
            visible: !!controller.consoleInfo.legacyTLS
            Layout.margins: Math.round(Theme.gu * 0.9)
            text: "Legacy TLS is enabled; the BMC certificate is not verified."
        }
        Item { Layout.preferredHeight: Math.round(Theme.gu * 0.4) }
    }
}
