import QtQuick
import QtQuick.Controls as QQC2
import QtQuick.Layouts
import org.kde.kirigami as Kirigami

// A small pill: a console family, a state, a flag such as "ISO mounted".
Rectangle {
    id: chip

    property string text: ""
    property color tone: Theme.accent
    // Grey pill with muted text instead of a tinted one.
    property bool neutral: false
    property bool onStage: false
    property string iconName: ""
    // Leading state dot in `dotColor`.
    property bool dot: false
    property color dotColor: tone
    property bool bold: true

    readonly property color ink: onStage ? (neutral ? Theme.stageText : Theme.stageTone(tone))
                                         : (neutral ? Theme.muted : Theme.toneText(tone))

    readonly property int padding: Math.round(Theme.gu * 0.55)
    implicitHeight: Math.max(Math.round(Theme.gu * 1.35), label.implicitHeight + 4)
    implicitWidth: row.implicitWidth + padding * 2
    radius: height / 2
    color: neutral ? (onStage ? Theme.stageRaised : Theme.raised)
                   : onStage ? Theme.alpha(tone, 0.22) : Theme.tint(tone)

    Accessible.role: Accessible.StaticText
    Accessible.name: text

    RowLayout {
        id: row
        // Fills the chip, so a chip given less than its width elides its text.
        anchors {
            left: parent.left
            right: parent.right
            verticalCenter: parent.verticalCenter
            leftMargin: chip.padding
            rightMargin: chip.padding
        }
        spacing: Math.round(Theme.gu * 0.35)
        StatusDot {
            visible: chip.dot
            color: chip.dotColor
            size: Math.max(6, Math.round(Theme.gu * 0.38))
            Layout.alignment: Qt.AlignVCenter
        }
        Kirigami.Icon {
            visible: chip.iconName !== ""
            source: chip.iconName
            implicitWidth: Math.round(Kirigami.Units.iconSizes.small * 0.85)
            implicitHeight: implicitWidth
            color: chip.ink
            isMask: true
            Layout.alignment: Qt.AlignVCenter
        }
        QQC2.Label {
            id: label
            text: chip.text
            color: chip.ink
            font.pointSize: Theme.small
            font.weight: chip.bold ? Font.DemiBold : Font.Normal
            elide: Text.ElideRight
            Layout.fillWidth: true
            Layout.alignment: Qt.AlignVCenter
        }
    }
}
