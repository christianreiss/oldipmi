import QtQuick

// The keyboard focus ring, just outside its parent, the background of a
// button or a tile, so that it never covers a label. The user binds visible
// to the focus it shows.
Rectangle {
    readonly property Rectangle frame: parent as Rectangle
    anchors.fill: parent
    anchors.margins: -3
    radius: frame ? frame.radius + 3 : 0
    color: "transparent"
    border.width: 2
    border.color: Theme.accent
}
