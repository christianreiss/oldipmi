import QtQuick
import QtQuick.Layouts
import "families.js" as Families

// The picture of a console family, side pixels square and drawn at twice that
// size. Drawn from side, not from width: in a layout, width follows the
// picture until the layout has run, so a sourceSize from width would double it
// on every load.
Image {
    // A family id, as Families.image() takes it: undefined while there is no
    // profile, which draws the default picture like any unknown family.
    property var family
    property real side

    width: side
    height: side
    Layout.preferredWidth: side
    Layout.preferredHeight: side
    source: Families.image(family)
    // Rounded explicitly: the engine rounds a fraction written to the int
    // source size, the code qmlcachegen compiles for it cuts it off.
    sourceSize.width: Math.round(side * 2)
    sourceSize.height: Math.round(side * 2)
    fillMode: Image.PreserveAspectFit
    smooth: true
}
