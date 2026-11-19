import QtQuick
import QtQuick.Controls as QQC2
import QtQuick.Layouts
import org.kde.kirigami as Kirigami
import "families.js" as Families

// Nothing selected: the first-run welcome, or a pointer to the list.
Item {
    id: page

    property bool hasHosts: false
    signal newRequested()
    signal demoRequested()

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
            width: Math.min(flick.width - Theme.gu * 2, Theme.gu * 34)
            x: Math.round((flick.width - width) / 2)
            y: Math.max(Theme.gu, Math.round((flick.contentHeight - implicitHeight) / 2 - Theme.gu * 1.5))
            spacing: Math.round(Theme.gu * 1)

            Image {
                readonly property real side: Math.round(Math.min(page.hasHosts ? Theme.gu * 6 : Theme.gu * 12, flick.height * 0.3))
                Layout.preferredWidth: side
                Layout.preferredHeight: side
                Layout.alignment: Qt.AlignHCenter
                // Bundled: themed icons are missing when OldIPMI is not installed.
                source: page.hasHosts ? "qrc:/oldipmi.svg" : Families.emptyImage
                sourceSize.width: side * 2
                sourceSize.height: side * 2
                fillMode: Image.PreserveAspectFit
                smooth: true
            }
            QQC2.Label {
                text: page.hasHosts ? "Select a connection" : "Connect your first BMC"
                font.pointSize: page.hasHosts ? Theme.display * 0.85 : Theme.hero
                font.weight: Font.Bold
                horizontalAlignment: Text.AlignHCenter
                wrapMode: Text.Wrap
                Layout.fillWidth: true
                Accessible.role: Accessible.Heading
            }
            QQC2.Label {
                text: page.hasHosts
                      ? "Choose a BMC in the list, or add a new one."
                      : "Add a server by its BMC address and account. OldIPMI logs in, finds the remote console and opens it right here. No Java, no browser plug-in."
                font.pointSize: Theme.fontBase * 1.08
                color: Theme.mix(Theme.text, Theme.window, 0.15)
                horizontalAlignment: Text.AlignHCenter
                wrapMode: Text.Wrap
                Layout.fillWidth: true
            }
            Flow {
                spacing: Kirigami.Units.largeSpacing
                Layout.alignment: Qt.AlignHCenter
                Layout.maximumWidth: content.width
                Layout.topMargin: Kirigami.Units.smallSpacing
                ActionButton {
                    tone: "primary"
                    large: true
                    text: "New connection…"
                    icon.name: "list-add"
                    onClicked: page.newRequested()
                }
                ActionButton {
                    visible: !page.hasHosts
                    tone: "secondary"
                    large: true
                    text: "Try the demo console"
                    icon.name: "media-playback-start"
                    onClicked: page.demoRequested()
                }
            }
            // The console families OldIPMI speaks.
            Flow {
                visible: !page.hasHosts
                spacing: Math.round(Theme.gu * 1.2)
                Layout.alignment: Qt.AlignHCenter
                Layout.maximumWidth: content.width
                Layout.topMargin: Math.round(Theme.gu * 1.2)
                Repeater {
                    model: Families.list.filter(f => f.id !== "demo")
                    delegate: RowLayout {
                        required property var modelData
                        spacing: Kirigami.Units.smallSpacing
                        Image {
                            Layout.preferredWidth: Math.round(Theme.gu * 1.7)
                            Layout.preferredHeight: Layout.preferredWidth
                            source: Families.image(modelData.id)
                            sourceSize.width: width * 2
                            sourceSize.height: height * 2
                            fillMode: Image.PreserveAspectFit
                            smooth: true
                        }
                        QQC2.Label {
                            text: modelData.name
                            font.pointSize: Theme.small
                            color: Theme.muted
                        }
                    }
                }
            }
        }
    }
}
