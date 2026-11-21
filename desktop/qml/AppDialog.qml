import QtQuick
import QtQuick.Controls as QQC2
import QtQuick.Layouts
import org.kde.kirigami as Kirigami

// Frame of every OldIPMI dialog: a raised card with a heading (and an optional
// icon tile), the content, and a footer band. The rightmost footer button is
// the dialog's verb; Escape and Cancel close it.
Kirigami.Dialog {
    id: dialog

    property string heading: title
    property string subheading: ""
    // Icon tile next to the heading (confirmations), tinted in `tone`.
    property string iconName: ""
    property color tone: Theme.accent
    property string footerHint: ""
    // An optional hint (a shortcut) shows only while it fits whole; otherwise
    // the hint is a notice and wraps.
    property bool footerHintOptional: false
    property bool closeButton: true
    // The footer buttons, left to right.
    property alias buttons: buttonRow.data
    // The content; it gets the dialog's width.
    default property alias body: bodyColumn.data

    padding: 0
    showCloseButton: false
    standardButtons: Kirigami.Dialog.NoButton
    preferredWidth: Math.round(Theme.gu * 25)
    maximumWidth: absoluteMaximumWidth - Theme.gu
    maximumHeight: absoluteMaximumHeight - Theme.gu * 2
    // Centred, sliding up while it fades in, but never past the window edge: Qt
    // shortens a popup that sticks out, and a y bound to that height loops. The
    // implicit height is what Qt measures, and it never changes it.
    readonly property real edge: Math.max(0, margins)
    y: parent ? Math.round(Math.max(edge, Math.min(parent.height - implicitHeight - edge,
                                                   (parent.height - implicitHeight) / 2 + Theme.gu * 2 * (1 - opacity)))) : 0

    // Scrolls the body until the top of item is margin below its top edge.
    // Kirigami's Dialog puts the body into a ScrollView around a Flickable;
    // false while that is not there yet.
    function scrollTo(item, margin) {
        const view = dialog.contentItem as QQC2.ScrollView;
        const flick = view ? view.contentItem as Flickable : null;
        if (!flick)
            return false;
        const top = item.mapToItem(flick.contentItem, 0, 0).y - margin;
        flick.contentY = Math.max(0, Math.min(top, flick.contentHeight - flick.height));
        return true;
    }

    QQC2.Overlay.modal: Rectangle {
        color: Theme.scrim
        Behavior on opacity { OpacityAnimator { duration: Kirigami.Units.longDuration } }
    }

    background: Kirigami.ShadowedRectangle {
        color: Theme.card
        radius: Theme.radiusHuge
        border.width: 1
        border.color: Theme.dark ? Theme.line : Theme.alpha(Theme.text, 0.1)
        shadow.size: Math.round(Theme.gu * 2.4)
        shadow.yOffset: Math.round(Theme.gu * 0.6)
        shadow.color: Theme.shadow
    }

    header: RowLayout {
        spacing: Math.round(Theme.gu * 0.8)
        Rectangle {
            visible: dialog.iconName !== ""
            Layout.preferredWidth: Math.round(Theme.gu * 2.25)
            Layout.preferredHeight: Layout.preferredWidth
            Layout.alignment: Qt.AlignTop
            Layout.leftMargin: Math.round(Theme.gu * 1.25)
            Layout.topMargin: Math.round(Theme.gu * 1.2)
            radius: Theme.radiusPanel
            color: Theme.tint(dialog.tone)
            Kirigami.Icon {
                anchors.centerIn: parent
                width: Kirigami.Units.iconSizes.smallMedium
                height: width
                source: dialog.iconName
                color: Theme.toneText(dialog.tone, Theme.card)
                isMask: true
            }
        }
        ColumnLayout {
            spacing: Math.round(Theme.gu * 0.2)
            Layout.fillWidth: true
            Layout.leftMargin: dialog.iconName !== "" ? 0 : Math.round(Theme.gu * 1.4)
            Layout.topMargin: Math.round(Theme.gu * 1.2)
            Layout.bottomMargin: Math.round(Theme.gu * 0.6)
            QQC2.Label {
                text: dialog.heading
                font.pointSize: Theme.heading
                font.weight: Font.Bold
                wrapMode: Text.Wrap
                Layout.fillWidth: true
                Accessible.role: Accessible.Heading
            }
            QQC2.Label {
                visible: text !== ""
                text: dialog.subheading
                color: Theme.muted
                wrapMode: Text.Wrap
                Layout.fillWidth: true
            }
        }
        IconButton {
            visible: dialog.closeButton
            icon.name: "window-close-symbolic"
            text: "Close"
            Layout.alignment: Qt.AlignTop
            Layout.topMargin: Math.round(Theme.gu * 0.9)
            Layout.rightMargin: Math.round(Theme.gu * 0.9)
            onClicked: dialog.reject()
        }
    }

    // Horizontal margins of the content; with an icon tile, text lines up with the heading.
    readonly property int sideMargin: Math.round(Theme.gu * 1.4)
    readonly property int textInset: iconName !== "" ? Math.round(Theme.gu * 1.25) + Math.round(Theme.gu * 2.25) + Math.round(Theme.gu * 0.8)
                                                     : sideMargin

    // The first content item gets the dialog's width; the body sits inside its margins.
    ColumnLayout {
        spacing: 0
        ColumnLayout {
            id: bodyColumn
            spacing: Math.round(Theme.gu * 0.9)
            Layout.fillWidth: true
            Layout.leftMargin: dialog.textInset
            Layout.rightMargin: dialog.sideMargin
            Layout.topMargin: Math.round(Theme.gu * 0.2)
            Layout.bottomMargin: Math.round(Theme.gu * 1.2)
        }
    }

    footer: Item {
        implicitHeight: footerRow.implicitHeight + Math.round(Theme.gu * 1.5)
        implicitWidth: footerRow.implicitWidth + Math.round(Theme.gu * 2.4)
        // A tinted band with square top and rounded bottom corners, inset by the
        // card's border so that stays visible.
        Rectangle {
            id: band
            anchors { fill: parent; leftMargin: 1; rightMargin: 1; bottomMargin: 1 }
            radius: Theme.radiusHuge - 1
            color: Theme.cardFooter
            Rectangle {
                anchors { left: parent.left; right: parent.right; top: parent.top }
                height: parent.radius
                color: parent.color
            }
            Rectangle {
                anchors { left: parent.left; right: parent.right; top: parent.top }
                height: 1
                color: Theme.lineSoft
            }
        }
        RowLayout {
            id: footerRow
            anchors { fill: parent; leftMargin: Math.round(Theme.gu * 1.4); rightMargin: Math.round(Theme.gu * 1.1) }
            spacing: Kirigami.Units.largeSpacing
            QQC2.Label {
                text: dialog.footerHint
                visible: text !== ""
                color: Theme.muted
                font.pointSize: Theme.small
                wrapMode: dialog.footerHintOptional ? Text.NoWrap : Text.Wrap
                maximumLineCount: 3
                elide: Text.ElideRight
                // Cut short, an optional hint only hides; it keeps its place, so the
                // footer's width does not depend on it.
                opacity: dialog.footerHintOptional && truncated ? 0 : 1
                Accessible.ignored: opacity === 0
                Layout.fillWidth: true
            }
            Item { Layout.fillWidth: true; visible: dialog.footerHint === "" }
            RowLayout {
                id: buttonRow
                spacing: Kirigami.Units.smallSpacing
            }
        }
    }
}
