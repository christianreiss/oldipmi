import QtQuick

// Background of a text field in the OldIPMI style: a framed well with an accent
// border and a soft accent halo while focused, red when the input is invalid.
Rectangle {
    id: well
    required property Item control
    property bool invalid: false

    radius: Theme.radius
    color: Theme.dark ? Theme.mix(Theme.card, Theme.black, 0.22) : Theme.view
    border.width: control.activeFocus || invalid ? 2 : 1
    border.color: invalid ? Theme.negative : control.activeFocus ? Theme.accent : Theme.line

    Rectangle {
        visible: well.control.activeFocus
        anchors.fill: parent
        anchors.margins: -3
        radius: parent.radius + 3
        color: "transparent"
        border.width: 3
        border.color: Theme.alpha(well.invalid ? Theme.negative : Theme.accent, 0.22)
    }
}
