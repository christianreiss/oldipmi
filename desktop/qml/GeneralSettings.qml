import QtQuick
import QtQuick.Controls as QQC2
import org.kde.kirigami as Kirigami
import org.kde.kirigamiaddons.formcard as FormCard

FormCard.FormCardPage {
    title: "General"

    FormCard.FormHeader { title: "Connections" }
    FormCard.FormCard {
        FormCard.FormSwitchDelegate {
            text: "Remember passwords in KWallet by default"
            description: "Preselects the option for new connections. Each connection can override it."
            checked: settings.rememberPasswords
            onToggled: settings.rememberPasswords = checked
        }
    }

    FormCard.FormHeader { title: "Console" }
    FormCard.FormCard {
        FormCard.FormSwitchDelegate {
            text: "Scale the remote screen to the window"
            description: "Off shows the remote screen pixel for pixel."
            checked: settings.fitToWindow
            onToggled: settings.fitToWindow = checked
        }
        FormCard.FormDelegateSeparator {}
        FormCard.FormSwitchDelegate {
            text: "Tap Right Ctrl to release keyboard and mouse"
            description: "The tap is still sent to the server. Released, the next tap gives keyboard and mouse back to it. When off, use the Release button in the status bar, or in the bar at the top edge in full screen."
            checked: settings.rightCtrlReleases
            onToggled: settings.rightCtrlReleases = checked
        }
    }

    FormCard.FormHeader { title: "Keyboard shortcuts" }
    FormCard.FormCard {
        Repeater {
            model: [
                ["New connection", "Ctrl+N"], ["Filter connections", "Ctrl+F"],
                ["Show or hide the connection list", "F9"], ["Full screen", "F11"],
                ["Disconnect", "Ctrl+D"], ["Settings", "Ctrl+,"], ["Quit", "Ctrl+Q"]
            ]
            delegate: FormCard.FormTextDelegate {
                required property var modelData
                text: modelData[0]
                trailing: QQC2.Label { text: modelData[1]; color: Kirigami.ColorUtils.linearInterpolation(Kirigami.Theme.textColor, Kirigami.Theme.backgroundColor, 0.15) }
            }
        }
        FormCard.FormSectionText {
            text: "While the console has the keyboard, every key, including these shortcuts, goes to the server."
        }
    }
}
