import QtQuick
import QtQuick.Controls as QQC2
import QtQuick.Layouts

// The connection list without connections: a dashed box where they will appear.
Item {
    Layout.preferredHeight: emptyText.implicitHeight + Theme.gu * 2.2
    Canvas {
        id: dashed
        anchors { fill: parent; leftMargin: Math.round(Theme.gu * 0.65); rightMargin: Math.round(Theme.gu * 0.65) }
        readonly property color stroke: Theme.line
        onStrokeChanged: requestPaint()
        onWidthChanged: requestPaint()
        onHeightChanged: requestPaint()
        onPaint: {
            const ctx = getContext("2d");
            ctx.reset();
            ctx.strokeStyle = stroke;
            ctx.lineWidth = 1.5;
            ctx.setLineDash([4, 3]);
            const r = Theme.radiusLarge - 2, w = width - 1.5, h = height - 1.5;
            ctx.beginPath();
            ctx.roundedRect(0.75, 0.75, w, h, r, r);
            ctx.stroke();
        }
    }
    QQC2.Label {
        id: emptyText
        anchors.centerIn: parent
        width: dashed.width - Theme.gu * 2
        text: "Your BMCs appear here once you add them."
        color: Theme.muted
        wrapMode: Text.Wrap
        horizontalAlignment: Text.AlignHCenter
    }
}
