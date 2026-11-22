import QtQuick
import QtQuick.Controls as QQC2

// The password for a connection without a saved one, for the pending profile.
// Connecting hands it to the controller once and clears the field; Cancel
// tells the controller too.
AppDialog {
    id: credentialsDialog
    property var profile: ({})
    // Remember in KWallet; the main window sets it from the profile or the settings.
    property alias remember: credentialsRemember.checked
    title: "Password for " + (profile.name || profile.host || "")
    iconName: "dialog-password"
    preferredWidth: Math.round(Theme.gu * 23)
    MonoCaption {
        text: (credentialsDialog.profile.username || "") + "@" + (credentialsDialog.profile.host || "")
    }
    FormPasswordField {
        id: credentialsPassword
        Accessible.name: "Password"
        onAccepted: if (text !== "") credentialsDialog.accept()
    }
    QQC2.CheckBox {
        id: credentialsRemember
        text: "Remember in KWallet"
    }
    buttons: [
        ActionButton { tone: "ghost"; text: "Cancel"; onClicked: credentialsDialog.reject() },
        ActionButton {
            tone: "primary"
            text: "Connect"
            icon.name: "media-playback-start"
            enabled: credentialsPassword.text !== ""
            onClicked: credentialsDialog.accept()
        }
    ]
    onOpened: credentialsPassword.forceActiveFocus()
    onAccepted: {
        const p = Object.assign({}, credentialsDialog.profile, {remember: credentialsRemember.checked});
        const pw = credentialsPassword.text;
        credentialsPassword.text = "";
        controller.connectHost(p, pw);
    }
    onRejected: { credentialsPassword.text = ""; controller.cancelCredentials() }
}
