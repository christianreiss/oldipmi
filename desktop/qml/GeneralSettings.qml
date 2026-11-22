pragma ComponentBehavior: Bound
import QtQuick
import QtQuick.Controls as QQC2
import org.kde.kirigami as Kirigami
import org.kde.kirigamiaddons.formcard as FormCard
import "shortcuts.js" as Shortcuts

FormCard.FormCardPage {
    objectName: "generalSettings"
    title: "General"

    FormCard.FormHeader { title: "Connections" }
    FormCard.FormCard {
        FormCard.FormSwitchDelegate {
            text: "Remember passwords in KWallet by default"
            description: "Preselects the option for new connections. Each connection can override it."
            checked: AppSettings.rememberPasswords
            onToggled: AppSettings.rememberPasswords = checked
        }
    }

    FormCard.FormHeader { title: "Console" }
    FormCard.FormCard {
        FormCard.FormSwitchDelegate {
            text: "Scale the remote screen to the window"
            description: "Off shows the remote screen pixel for pixel."
            checked: AppSettings.fitToWindow
            onToggled: AppSettings.fitToWindow = checked
        }
        FormCard.FormDelegateSeparator {}
        FormCard.FormSwitchDelegate {
            text: "Tap Right Ctrl to release keyboard and mouse"
            description: "The tap is still sent to the server. Released, the next tap gives keyboard and mouse back to it. When off, releasing needs the mouse: click Release in the status bar, or in the bar at the top edge in full screen."
            checked: AppSettings.rightCtrlReleases
            onToggled: AppSettings.rightCtrlReleases = checked
        }
    }

    FormCard.FormHeader { title: "Keyboard shortcuts" }
    FormCard.FormCard {
        Repeater {
            model: [
                ["New connection", Shortcuts.newConnection], ["Filter connections", Shortcuts.find],
                ["Show or hide the connection list", Shortcuts.connectionList], ["Full screen", Shortcuts.fullScreen],
                ["Copy screen image", Shortcuts.copyScreen],
                ["Disconnect", Shortcuts.disconnect], ["Settings", Shortcuts.settings], ["Quit", Shortcuts.quit]
            ]
            delegate: FormCard.FormTextDelegate {
                id: shortcutRow
                required property var modelData
                text: modelData[0]
                trailing: QQC2.Label { text: shortcutRow.modelData[1]; color: Kirigami.ColorUtils.linearInterpolation(Kirigami.Theme.textColor, Kirigami.Theme.backgroundColor, 0.15) }
            }
        }
        FormCard.FormSectionText {
            text: "While the console has the keyboard, every key, including these shortcuts, goes to the server."
        }
    }
}
