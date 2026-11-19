import QtQuick
import QtQuick.Controls as QQC2
import org.kde.kirigami as Kirigami

// A square icon-only button. `text` is its accessible name and tooltip.
//   filled   accent-filled (Connect in a list row)
//   onStage  light-on-dark, for the console stage and the full-screen bar
QQC2.Button {
    id: control

    property bool filled: false
    property bool onStage: false
    property string shortcut: ""
    property real size: Math.round(Theme.gu * 1.9)
    property color iconColor: filled ? Theme.white : onStage ? Theme.stageText : Theme.text

    display: QQC2.AbstractButton.IconOnly
    hoverEnabled: true
    padding: 0
    implicitWidth: size
    implicitHeight: size
    opacity: enabled ? 1 : 0.45

    QQC2.ToolTip.visible: hovered && text !== ""
    QQC2.ToolTip.text: text + (shortcut !== "" ? " (" + shortcut + ")" : "")
    QQC2.ToolTip.delay: Kirigami.Units.toolTipDelay

    contentItem: Item {
        Kirigami.Icon {
            anchors.centerIn: parent
            width: Kirigami.Units.iconSizes.small
            height: width
            source: control.icon.name !== "" ? control.icon.name : control.icon.source
            color: control.iconColor
            isMask: control.filled || control.onStage
        }
    }

    background: Rectangle {
        radius: control.filled ? Theme.radiusSmall + 1 : Theme.radius - 1
        color: {
            if (control.filled)
                return control.down ? Theme.mix(Theme.accentStrong, Theme.black, 0.14)
                     : control.hovered ? Theme.mix(Theme.accentStrong, Theme.white, 0.1) : Theme.accentStrong;
            if (control.onStage)
                return control.down || control.checked ? Theme.stageHover : control.hovered ? Theme.stageRaised : "transparent";
            return control.down || control.checked ? Theme.pressed : control.hovered ? Theme.hover : "transparent";
        }
        Rectangle {
            anchors.fill: parent
            anchors.margins: -2
            radius: parent.radius + 2
            color: "transparent"
            border.width: 2
            border.color: Theme.accent
            visible: control.visualFocus
        }
    }
}
