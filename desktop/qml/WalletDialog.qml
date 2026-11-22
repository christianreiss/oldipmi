import QtQuick
import QtQuick.Controls as QQC2
import QtQuick.Layouts

// The password was to be saved, but KWallet is not available: the reason, and
// the offer to connect for this session only.
AppDialog {
    id: walletDialog
    title: "Password not saved"
    iconName: "dialog-warning"
    tone: Theme.warning
    closeButton: false
    QQC2.Label {
        text: controller.message
        visible: text !== ""
        color: Theme.cardText
        wrapMode: Text.Wrap
        Layout.fillWidth: true
    }
    QQC2.Label {
        text: "You can still connect; the password is then used for this session only."
        color: Theme.cardText
        wrapMode: Text.Wrap
        Layout.fillWidth: true
    }
    buttons: [
        ActionButton { tone: "ghost"; text: "Cancel"; onClicked: { walletDialog.close(); controller.cancelCredentials() } },
        ActionButton {
            tone: "primary"
            text: "Connect without saving"
            icon.name: "media-playback-start"
            onClicked: { walletDialog.close(); controller.continueWithoutWallet() }
        }
    ]
}
