import QtQuick
import QtQuick.Controls as QQC2
import QtQuick.Layouts
import org.kde.kirigami as Kirigami

// A text button in the OldIPMI style.
//   tone "primary"    filled accent: the one main action of a state
//   tone "danger"     filled red: the verb of a destructive confirmation
//   tone "secondary"  raised and outlined: the alternatives
//   tone "ghost"      text only: Cancel and minor actions
// onStage switches to the light-on-dark variants of the console stage.
QQC2.Button {
    id: control

    property string tone: "secondary"
    property bool large: false
    property bool onStage: false
    property string tooltip: ""

    readonly property bool filled: tone === "primary" || tone === "danger"
    readonly property color fill: tone === "danger" ? Theme.dangerStrong : Theme.accentStrong
    readonly property color ink: filled ? Theme.white : onStage ? Theme.stageText : Theme.text
    readonly property bool iconOnly: display === QQC2.AbstractButton.IconOnly || text === ""
    readonly property bool hasIcon: icon.name !== "" || icon.source.toString() !== ""

    hoverEnabled: true
    opacity: enabled ? 1 : 0.45
    font.pointSize: large ? Theme.fontBase * 1.02 : Theme.fontBase
    font.weight: filled ? Font.DemiBold : Font.Medium
    padding: 0
    topPadding: Kirigami.Units.smallSpacing
    bottomPadding: Kirigami.Units.smallSpacing
    leftPadding: iconOnly ? Kirigami.Units.smallSpacing : Math.round(Theme.gu * (large ? 1.05 : 0.8))
    rightPadding: leftPadding
    implicitHeight: Math.max(large ? Theme.controlHeightLarge : Theme.controlHeight,
                             implicitContentHeight + topPadding + bottomPadding)
    implicitWidth: Math.max(implicitHeight, implicitContentWidth + leftPadding + rightPadding)

    QQC2.ToolTip.visible: hovered && (tooltip !== "" || (iconOnly && text !== ""))
    QQC2.ToolTip.text: tooltip !== "" ? tooltip : text
    QQC2.ToolTip.delay: Kirigami.Units.toolTipDelay

    contentItem: RowLayout {
        spacing: Math.round(Theme.gu * 0.45)
        Kirigami.Icon {
            visible: control.hasIcon
            source: control.icon.name !== "" ? control.icon.name : control.icon.source
            implicitWidth: Kirigami.Units.iconSizes.small
            implicitHeight: implicitWidth
            color: control.ink
            isMask: control.filled || control.onStage
            Layout.alignment: Qt.AlignCenter
        }
        QQC2.Label {
            visible: !control.iconOnly
            text: control.text
            font: control.font
            color: control.ink
            elide: Text.ElideRight
            horizontalAlignment: Text.AlignHCenter
            verticalAlignment: Text.AlignVCenter
            Layout.fillWidth: true
            Layout.alignment: Qt.AlignVCenter
        }
    }

    background: Rectangle {
        radius: Theme.radius
        color: {
            if (control.filled)
                return control.down ? Theme.mix(control.fill, Theme.black, 0.14)
                     : control.hovered ? Theme.mix(control.fill, Theme.white, 0.1) : control.fill;
            if (control.onStage)
                return control.down ? Theme.stageHover : control.hovered ? Theme.stageRaised
                     : control.tone === "secondary" ? Theme.stageSubtle : "transparent";
            if (control.tone === "secondary")
                return control.down ? Theme.mix(Theme.raised, Theme.text, 0.1)
                     : control.hovered ? Theme.raisedHover : Theme.raised;
            return control.down ? Theme.pressed : control.hovered ? Theme.hover : "transparent";
        }
        border.width: control.tone === "secondary" ? 1 : 0
        border.color: control.onStage ? Theme.stageEdge : Theme.line

        // Keyboard focus ring, outside the button so it never covers the label.
        Rectangle {
            anchors.fill: parent
            anchors.margins: -3
            radius: parent.radius + 3
            color: "transparent"
            border.width: 2
            border.color: Theme.accent
            visible: control.visualFocus
        }
    }
}
