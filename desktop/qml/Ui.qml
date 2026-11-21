pragma Singleton
import QtQuick

// What the pages and the settings window ask of the main window, which answers
// these requests (Main.qml); and the clipboard, for text.
Item {
    id: ui

    signal notificationRequested(string message, string timeout)
    signal settingsRequested(string page)
    signal mcpInstructionsRequested()

    // A passive notification in the main window. timeout as Kirigami takes it:
    // "short", "long", or empty for its default.
    function notify(message, timeout) { ui.notificationRequested(message, timeout || "") }

    // Text on the clipboard without another import: a hidden text editor copies
    // its selection. With a notice, the main window says so.
    TextEdit { id: clipboard; visible: false }
    function copyText(value, notice) {
        clipboard.text = value;
        clipboard.selectAll();
        clipboard.copy();
        clipboard.text = "";
        if (notice)
            ui.notify(notice);
    }
}
