import QtQuick
import QtQuick.Layouts
import org.kde.kirigami as Kirigami

// A password field of a form, on InputBackground. The right padding stays
// Kirigami's: it makes room for the button that shows the password.
Kirigami.PasswordField {
    id: field
    color: Theme.text
    implicitHeight: Math.max(Theme.controlHeight + 2, contentHeight + topPadding + bottomPadding)
    leftPadding: Math.round(Theme.gu * 0.65)
    verticalAlignment: TextInput.AlignVCenter
    background: InputBackground { control: field }
    Layout.fillWidth: true
}
