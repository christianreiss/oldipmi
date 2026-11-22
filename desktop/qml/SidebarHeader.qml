import QtQuick
import QtQuick.Controls as QQC2
import QtQuick.Layouts
import org.kde.kirigami as Kirigami
import "shortcuts.js" as Shortcuts

// The top of the connection list: the brand, New connection and the main menu.
RowLayout {
    id: header
    signal newRequested()
    signal settingsRequested(string module)
    signal disconnectAllRequested()
    signal quitRequested()
    Layout.fillWidth: true
    Layout.preferredHeight: Math.max(Math.round(Theme.gu * 3.1), implicitHeight)
    Layout.leftMargin: Math.round(Theme.gu * 0.8)
    Layout.rightMargin: Math.round(Theme.gu * 0.55)
    spacing: Kirigami.Units.largeSpacing
    Image {
        Layout.preferredWidth: Math.round(Theme.gu * 1.45)
        Layout.preferredHeight: Layout.preferredWidth
        source: "qrc:/oldipmi.svg"
        // From the layout's size: width follows the picture until the layout
        // has run, so a sourceSize from width would double it on every load.
        sourceSize.width: Layout.preferredWidth * 2
        sourceSize.height: Layout.preferredHeight * 2
    }
    QQC2.Label {
        text: "OldIPMI"
        font.pointSize: Theme.title
        font.weight: Font.Bold
        elide: Text.ElideRight
        Layout.fillWidth: true
    }
    IconButton {
        icon.name: "list-add"
        text: "New connection…"
        shortcut: Shortcuts.newConnection
        onClicked: header.newRequested()
    }
    IconButton {
        id: menuButton
        objectName: "mainMenuButton"
        icon.name: "application-menu"
        text: appMenu.visible ? "" : "Main menu"
        Accessible.name: "Main menu"
        checked: appMenu.visible
        onClicked: appMenu.visible ? appMenu.close() : appMenu.popup(menuButton, 0, menuButton.height)
        QQC2.Menu {
            id: appMenu
            objectName: "mainMenu"
            QQC2.MenuItem { text: "Settings…"; icon.name: "configure"; onTriggered: header.settingsRequested("general") }
            QQC2.MenuItem { text: "AI agents (MCP)…"; icon.name: "dialog-scripts"; onTriggered: header.settingsRequested("agents") }
            QQC2.MenuSeparator {}
            QQC2.MenuItem {
                text: "Disconnect all"
                icon.name: "network-disconnect"
                enabled: Controller.openCount > 0
                onTriggered: header.disconnectAllRequested()
            }
            QQC2.MenuSeparator {}
            QQC2.MenuItem { text: "About OldIPMI"; icon.name: "help-about"; onTriggered: header.settingsRequested("about") }
            QQC2.MenuItem { text: "Quit"; icon.name: "application-exit"; onTriggered: header.quitRequested() }
        }
    }
}
