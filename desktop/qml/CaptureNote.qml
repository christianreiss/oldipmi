import QtQuick
import QtQuick.Controls as QQC2
import QtQuick.Layouts
import org.kde.kirigami as Kirigami

// Full screen has no status strip: taking or giving back the keyboard shows
// this short note instead, how to release or how to take control again. It
// covers nothing for long and moves nothing. The console pane places it below
// the full-screen bar and shows it for a moment after each change.
Rectangle {
    id: toast
    required property bool captured
    required property string displayName
    // The keyboard focus is in the full-screen bar or on Quit.
    required property bool barFocused
    required property bool shown
    // The width it may take: clear of the Quit corner and its mirror image.
    required property real room
    // One line when it fits; otherwise the release instruction takes its
    // own line and the server name elides first. It keeps clear of the
    // Quit corner, also while the bar is hidden and the note moves up.
    readonly property int pad: Math.round(Theme.gu * 0.7)
    // Released with the keyboard focus in the bar or on Quit, a Right Ctrl
    // tap does not take control; the bar's Take control does.
    readonly property string how: toast.captured
        ? (settings.rightCtrlReleases ? "Tap Right Ctrl to release" : "Release from the bar at the top edge")
        : toast.barFocused ? "Take control from the bar, or click the screen"
        : (settings.rightCtrlReleases ? "Tap Right Ctrl or click the screen to take control" : "Click the screen to take control")
    readonly property bool stacked: whoRow.implicitWidth + toastGrid.columnSpacing + oneLine.advanceWidth + 2 * pad > room
    TextMetrics { id: oneLine; font: howLabel.font; text: "· " + toast.how }
    visible: opacity > 0
    opacity: toast.shown ? 1 : 0
    Behavior on opacity { NumberAnimation { duration: Kirigami.Units.shortDuration } }
    width: Math.min(toastGrid.implicitWidth + 2 * pad, room)
    height: toastGrid.implicitHeight + Math.round(Theme.gu * 0.8)
    radius: Theme.radiusPanel
    color: Theme.alpha(Theme.stagePopup, 0.95)
    border.width: 1
    border.color: Theme.stageLine
    GridLayout {
        id: toastGrid
        anchors { left: parent.left; right: parent.right; verticalCenter: parent.verticalCenter; leftMargin: toast.pad; rightMargin: toast.pad }
        columns: toast.stacked ? 1 : 2
        columnSpacing: Kirigami.Units.largeSpacing
        rowSpacing: Math.round(Theme.gu * 0.15)
        RowLayout {
            id: whoRow
            spacing: Kirigami.Units.largeSpacing
            Layout.fillWidth: true
            Layout.maximumWidth: implicitWidth
            Kirigami.Icon {
                source: toast.captured ? "input-keyboard" : "input-mouse"
                implicitWidth: Kirigami.Units.iconSizes.small
                implicitHeight: implicitWidth
                color: Theme.stageText
                isMask: true
            }
            QQC2.Label {
                text: toast.captured ? "Keyboard and mouse go to " + toast.displayName : "Keyboard and mouse released"
                color: Theme.stageText
                elide: Text.ElideRight
                Layout.fillWidth: true
                Layout.maximumWidth: Math.ceil(implicitWidth)
            }
        }
        QQC2.Label {
            id: howLabel
            text: (toast.stacked ? "" : "· ") + toast.how
            color: Theme.stageTone(Theme.accent)
            wrapMode: Text.Wrap
            Layout.fillWidth: toast.stacked
            Layout.maximumWidth: toast.room - 2 * toast.pad
        }
    }
}
