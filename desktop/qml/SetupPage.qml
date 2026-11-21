pragma ComponentBehavior: Bound
import QtQuick
import QtQuick.Controls as QQC2
import QtQuick.Layouts
import org.kde.kirigami as Kirigami
import "families.js" as Families

// A console being opened: the three setup stages, how long it has taken, Cancel.
Rectangle {
    id: page

    property var profile: ({})
    property string connState: ""
    property double startedAt: 0
    signal cancelRequested()

    readonly property int current: Families.stageIndex(connState)
    readonly property bool waiting: connState === "connected"
    // A short page (large text on a small screen): a small ring, a smaller title
    // and the current stage as one line instead of the stepper, so the stage and
    // Cancel are in view right below the server.
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
            width: Math.min(flick.width - Theme.gu * 2, Theme.gu * 30)
            x: Math.round((flick.width - width) / 2)
            y: Math.max(Theme.gu, Math.round((flick.contentHeight - implicitHeight) / 2))
            spacing: Math.round(Theme.gu * (page.compact ? 0.6 : 1.3))

            // The family illustration inside a turning ring.
            Item {
                readonly property real side: page.compact ? Math.round(Theme.gu * 3.4)
                                                          : Math.round(Math.min(Theme.gu * 10.5, flick.height * 0.34))
                Layout.preferredWidth: side
                Layout.preferredHeight: side
                Layout.alignment: Qt.AlignHCenter
                Rectangle {
                    anchors.fill: parent
                    radius: width / 2
                    color: "transparent"
                    border.width: 2
                    border.color: Theme.alpha(Theme.accent, 0.22)
                }
                Rectangle {
                    anchors.fill: parent
                    anchors.margins: parent.width * 0.095
                    radius: width / 2
                    color: Theme.accentTint
                }
                Canvas {
                    id: arc
                    anchors.fill: parent
                    readonly property color stroke: Theme.accent
                    onStrokeChanged: requestPaint()
                    onWidthChanged: requestPaint()
                    onPaint: {
                        const ctx = getContext("2d");
                        ctx.reset();
                        ctx.lineWidth = 3;
                        ctx.lineCap = "round";
                        ctx.strokeStyle = stroke;
                        ctx.beginPath();
                        ctx.arc(width / 2, height / 2, width / 2 - 2, -Math.PI / 2, Math.PI / 4);
                        ctx.stroke();
                    }
                    RotationAnimator on rotation {
                        from: 0
                        to: 360
                        duration: 1400
                        loops: Animation.Infinite
                        // Still when the system turns animations off (Kirigami's durations
                        // are then 1 ms, never 0), and in the self-test, whose clock stands
                        // still; the stages and the elapsed time show the progress.
                        running: page.visible && !page.waiting && Kirigami.Units.longDuration > 1 && Theme.clockRunning
                    }
                }
                Image {
                    anchors.centerIn: parent
                    width: parent.width * 0.68
                    height: width
                    source: Families.image(page.profile.family)
                    sourceSize.width: width * 2
                    sourceSize.height: height * 2
                    fillMode: Image.PreserveAspectFit
                    smooth: true
                }
            }

            ColumnLayout {
                spacing: Math.round(Theme.gu * 0.3)
                Layout.fillWidth: true
                QQC2.Label {
                    text: "Opening " + (page.profile.name || page.profile.host || "console") + "…"
                    font.pointSize: Theme.fontBase * (page.compact ? 1.35 : 1.75)
                    font.weight: Font.Bold
                    horizontalAlignment: Text.AlignHCenter
                    wrapMode: Text.Wrap
                    Layout.fillWidth: true
                    Accessible.role: Accessible.Heading
                }
                RowLayout {
                    spacing: Math.round(Theme.gu * 0.35)
                    // A nested layout fills by default; this line stays centred instead.
                    Layout.fillWidth: false
                    Layout.alignment: Qt.AlignHCenter
                    Layout.maximumWidth: content.width
                    MonoCaption {
                        text: (page.profile.username ? page.profile.username + "@" : "") + (page.profile.host || "")
                        Layout.maximumWidth: Math.ceil(implicitWidth)
                    }
                    QQC2.Label {
                        visible: page.startedAt > 0
                        text: "· " + Theme.elapsed(page.startedAt) + " elapsed"
                        color: Theme.muted
                    }
                }
            }

            // The current stage on a short page, instead of the stepper.
            Chip {
                visible: page.compact && page.current >= 0 && page.current < Families.stages.length
                dot: true
                text: visible ? "Step " + (page.current + 1) + " of " + Families.stages.length + " · " + Families.stages[page.current].name : ""
                Layout.alignment: Qt.AlignHCenter
                Layout.maximumWidth: content.width
            }

            // Stepper: done, current and pending stages.
            RowLayout {
                visible: !page.compact
                spacing: 0
                Layout.fillWidth: true
                Layout.maximumWidth: Theme.gu * 28
                Layout.alignment: Qt.AlignHCenter
                Repeater {
                    model: Families.stages
                    delegate: RowLayout {
                        id: step
                        required property var modelData
                        required property int index
                        readonly property bool done: index < page.current
                        readonly property bool active: index === page.current
                        spacing: 0
                        Layout.fillWidth: true
                        Layout.alignment: Qt.AlignTop
                        // Connector from the previous stage.
                        Rectangle {
                            visible: step.index > 0
                            Layout.fillWidth: true
                            Layout.preferredHeight: 3
                            Layout.alignment: Qt.AlignTop
                            Layout.topMargin: Math.round(dotBox.height / 2) - 1
                            radius: 2
                            color: step.done ? Theme.positive : step.active ? Theme.accent : Theme.lineSoft
                        }
                        ColumnLayout {
                            spacing: Math.round(Theme.gu * 0.45)
                            Layout.preferredWidth: Theme.gu * 6.2
                            Layout.alignment: Qt.AlignTop
                            Item {
                                id: dotBox
                                Layout.preferredWidth: Math.round(Theme.gu * 1.65)
                                Layout.preferredHeight: Layout.preferredWidth
                                Layout.alignment: Qt.AlignHCenter
                                Rectangle {
                                    anchors.centerIn: parent
                                    width: parent.width + 12
                                    height: width
                                    radius: width / 2
                                    color: Theme.alpha(Theme.accent, 0.2)
                                    visible: step.active
                                }
                                Rectangle {
                                    anchors.fill: parent
                                    radius: width / 2
                                    color: step.done ? Theme.positive : step.active ? Theme.accentStrong : Theme.card
                                    border.width: step.done || step.active ? 0 : 2
                                    border.color: Theme.line
                                    Kirigami.Icon {
                                        anchors.centerIn: parent
                                        visible: step.done
                                        width: Kirigami.Units.iconSizes.small
                                        height: width
                                        source: "checkmark"
                                        color: Theme.white
                                        isMask: true
                                    }
                                    Rectangle {
                                        anchors.centerIn: parent
                                        visible: step.active
                                        width: Math.round(parent.width * 0.34)
                                        height: width
                                        radius: width / 2
                                        color: Theme.white
                                    }
                                }
                            }
                            QQC2.Label {
                                text: step.modelData.name
                                font.weight: step.active ? Font.DemiBold : Font.Normal
                                color: step.done || step.active ? Theme.text : Theme.muted
                                horizontalAlignment: Text.AlignHCenter
                                wrapMode: Text.Wrap
                                Layout.fillWidth: true
                                Accessible.name: step.modelData.name + (step.done ? ", done" : step.active ? ", in progress" : ", pending")
                            }
                        }
                    }
                }
            }

            QQC2.Label {
                visible: page.waiting
                text: "Connected. Waiting for the first picture from the BMC…"
                color: Theme.muted
                horizontalAlignment: Text.AlignHCenter
                wrapMode: Text.Wrap
                Layout.fillWidth: true
            }

            ActionButton {
                text: page.waiting ? "Disconnect" : "Cancel"
                icon.name: page.waiting ? "network-disconnect" : "dialog-cancel"
                tone: "secondary"
                Layout.alignment: Qt.AlignHCenter
                onClicked: page.cancelRequested()
            }
        }
    }
}
