import QtQuick

// The coloured dot of a connection state. It always sits next to a text that
// names the state: colour is never the only signal.
Item {
    id: dot
    property color color: Theme.muted
    // A soft ring for states that are alive (connected, setting up).
    property bool halo: false
    property real size: Math.max(7, Math.round(Theme.gu * 0.45))

    implicitWidth: size
    implicitHeight: size

    Rectangle {
        anchors.centerIn: parent
        width: dot.size + 6
        height: width
        radius: width / 2
        color: Theme.alpha(dot.color, 0.24)
        visible: dot.halo
    }
    Rectangle {
        anchors.fill: parent
        radius: width / 2
        color: dot.color
    }
}
