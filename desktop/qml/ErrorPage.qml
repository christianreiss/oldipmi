import QtQuick
import QtQuick.Controls as QQC2
import QtQuick.Layouts
import org.kde.kirigami as Kirigami
import "families.js" as Families

// A setup stage failed: which one, the likely fix, the technical detail, and
// what to do next. Transport errors can be long, so the page scrolls.
Rectangle {
    id: page

    property var profile: ({})
    property string errorStage: ""
    property string message: ""
    // The class of the error in message (testdata/ipc/names.json, codes), "" if none is known.
    property string errorCode: ""
    signal retryRequested()
    signal editRequested()

    readonly property int stage: Families.stageIndex(errorStage)
    readonly property string hint: Families.errorHint(message, errorCode)
    readonly property string detail: Families.errorDetail(message)
    // A short page (large text on a small screen): no picture, a smaller title and
    // tighter spacing, so both buttons are in view.
    readonly property bool compact: flick.height < Theme.gu * 20

    color: Theme.card
    radius: Theme.radiusLarge
    border.width: 1
    border.color: Theme.lineSoft

    Flickable {
        id: flick
        anchors.fill: parent
        anchors.margins: 1
        clip: true
        contentWidth: width
        contentHeight: Math.max(height, content.implicitHeight + Theme.gu * 2)
        boundsBehavior: Flickable.StopAtBounds
        QQC2.ScrollBar.vertical: QQC2.ScrollBar {}

        ColumnLayout {
            id: content
            width: Math.min(flick.width - Theme.gu * 2, Theme.gu * 34)
            x: Math.round((flick.width - width) / 2)
            y: Math.max(Theme.gu, Math.round((flick.contentHeight - implicitHeight) / 2))
            spacing: Math.round(Theme.gu * (page.compact ? 0.6 : 0.9))

            Image {
                visible: !page.compact
                readonly property real side: Math.round(Math.min(Theme.gu * 8.3, flick.height * 0.26))
                Layout.preferredWidth: side
                Layout.preferredHeight: side
                Layout.alignment: Qt.AlignHCenter
                source: Families.errorImage
                sourceSize.width: side * 2
                sourceSize.height: side * 2
                fillMode: Image.PreserveAspectFit
                smooth: true
            }
            Chip {
                visible: page.stage >= 0 && page.stage < Families.stages.length
                tone: Theme.negative
                iconName: "dialog-error-symbolic"
                text: visible ? "Step " + (page.stage + 1) + " of " + Families.stages.length + " · " + Families.stages[page.stage].name : ""
                Layout.alignment: Qt.AlignHCenter
            }
            QQC2.Label {
                text: Families.failedTitle(page.errorStage)
                font.pointSize: page.compact ? Theme.display * 0.85 : Theme.display
                font.weight: Font.Bold
                horizontalAlignment: Text.AlignHCenter
                wrapMode: Text.Wrap
                Layout.fillWidth: true
                Accessible.role: Accessible.Heading
            }
            QQC2.Label {
                visible: page.hint !== ""
                text: page.hint
                font.pointSize: Theme.title
                horizontalAlignment: Text.AlignHCenter
                wrapMode: Text.Wrap
                Layout.fillWidth: true
            }

            // Right after the likely fix, so they are in view on a short page; they
            // wrap instead of widening the page when they do not fit side by side.
            Flow {
                spacing: Kirigami.Units.largeSpacing
                Layout.alignment: Qt.AlignHCenter
                Layout.maximumWidth: content.width
                Layout.topMargin: Math.round(Theme.gu * 0.3)
                ActionButton {
                    tone: "primary"
                    large: true
                    text: "Try again"
                    icon.name: "view-refresh"
                    onClicked: page.retryRequested()
                }
                ActionButton {
                    tone: "secondary"
                    large: true
                    text: "Edit connection…"
                    icon.name: "document-edit"
                    onClicked: page.editRequested()
                }
            }

            // Technical details, selectable and copyable.
            Rectangle {
                visible: page.detail !== ""
                Layout.fillWidth: true
                Layout.topMargin: Math.round(Theme.gu * 0.3)
                implicitHeight: detailColumn.implicitHeight
                radius: Theme.radiusPanel
                color: Theme.dark ? Theme.mix(Theme.card, Theme.black, 0.3) : Theme.mix(Theme.card, Theme.text, 0.03)
                border.width: 1
                border.color: Theme.lineSoft
                ColumnLayout {
                    id: detailColumn
                    anchors { left: parent.left; right: parent.right }
                    spacing: 0
                    RowLayout {
                        Layout.fillWidth: true
                        Layout.leftMargin: Math.round(Theme.gu * 0.8)
                        Layout.rightMargin: Kirigami.Units.smallSpacing
                        Layout.topMargin: Kirigami.Units.smallSpacing
                        Layout.bottomMargin: Kirigami.Units.smallSpacing
                        QQC2.Label {
                            text: "TECHNICAL DETAILS"
                            font.pointSize: Theme.tiny
                            font.weight: Font.DemiBold
                            font.letterSpacing: 0.6
                            color: Theme.muted
                            Layout.fillWidth: true
                        }
                        ActionButton {
                            tone: "ghost"
                            text: "Copy"
                            icon.name: "edit-copy"
                            tooltip: "Copy the technical details"
                            implicitHeight: Math.round(Theme.gu * 1.6)
                            onClicked: {
                                Theme.copyText(page.detail);
                                mainWindow.showPassiveNotification("Technical details copied");
                            }
                        }
                    }
                    Rectangle { Layout.fillWidth: true; Layout.preferredHeight: 1; color: Theme.lineSoft }
                    TextEdit {
                        text: page.detail
                        readOnly: true
                        selectByMouse: true
                        wrapMode: TextEdit.Wrap
                        font.family: Theme.mono.family
                        font.pointSize: Theme.small
                        color: Theme.text
                        selectionColor: Theme.accent
                        selectedTextColor: Theme.white
                        Layout.fillWidth: true
                        Layout.margins: Math.round(Theme.gu * 0.8)
                        Accessible.name: "Technical details"
                    }
                }
            }
        }
    }
}
