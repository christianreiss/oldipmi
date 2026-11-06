import QtQuick
import QtQuick.Controls as QQC2
import QtQuick.Layouts
import org.kde.kirigami as Kirigami
import org.kde.kirigamiaddons.formcard as FormCard
import "families.js" as Families

// Modal editor for one connection profile ("New connection" / "Edit connection").
Kirigami.Dialog {
    id: dialog
    objectName: "connectionDialog"

    property var original: ({})
    readonly property bool editing: !!original.id
    property string certPath: ""
    property string keyPath: ""
    property bool showAdvanced: false
    property string error: ""
    signal saved(var profile)

    title: editing ? "Edit connection" : "New connection"
    preferredWidth: Kirigami.Units.gridUnit * 30
    // Leave room for the open animation: a dialog as tall as the window loops on y.
    maximumHeight: absoluteMaximumHeight - Kirigami.Units.gridUnit * 4
    padding: 0
    standardButtons: Kirigami.Dialog.NoButton
    customFooterActions: [
        Kirigami.Action {
            text: "Cancel"
            icon.name: "dialog-cancel"
            onTriggered: dialog.close()
        },
        Kirigami.Action {
            text: "Save"
            icon.name: "document-save"
            enabled: dialog.valid
            onTriggered: dialog.save(false)
        },
        Kirigami.Action {
            text: "Save and Connect"
            icon.name: "media-playback-start"
            enabled: dialog.valid && controller.state === "disconnected"
            onTriggered: dialog.save(true)
        }
    ]

    function openFor(profile) {
        original = profile || {};
        const p = original;
        nameField.text = p.name || "";
        hostField.text = p.host || "";
        familyBox.currentIndex = Families.index(p.family || "aten");
        userField.text = p.username || "";
        passwordField.text = "";
        rememberSwitch.checked = p.remember === undefined ? settings.rememberPasswords : !!p.remember;
        modeBox.currentIndex = Math.max(0, ["auto", "java", "html5"].indexOf(p.consoleMode || "auto"));
        httpSwitch.checked = !!p.http;
        tlsSwitch.checked = !!p.legacyTLS;
        plainSwitch.checked = !!p.allowPlainConsole;
        jnlpField.text = p.jnlpPath || "";
        certPath = p.clientCert || "";
        keyPath = p.clientKey || "";
        error = "";
        showAdvanced = httpSwitch.checked || tlsSwitch.checked || plainSwitch.checked || !!jnlpField.text || !!certPath || modeBox.currentIndex > 0;
        open();
        (editing ? nameField : hostField).forceActiveFocus();
    }

    // Accept a pasted URL ("https://bmc.example/#login") and keep only the host.
    readonly property string host: hostField.text.trim().replace(/^[a-z]+:\/\//i, "").replace(/[\/?#].*$/, "")
    readonly property string family: Families.list[Math.max(0, familyBox.currentIndex)].id
    readonly property string hostProblem: host === "" ? "" : /[\s@\\]/.test(host) ? "Enter a hostname or IP address only." : ""
    readonly property bool valid: host !== "" && hostProblem === "" && (userField.text !== "" || !Families.needsAccount(family))

    function profile() {
        const p = Object.assign({}, original);
        p.name = nameField.text.trim() || host;
        p.host = host;
        p.family = family;
        p.username = userField.text;
        p.remember = rememberSwitch.checked;
        p.http = httpSwitch.checked;
        p.legacyTLS = tlsSwitch.checked;
        p.allowPlainConsole = plainSwitch.checked;
        p.consoleMode = family === "aten" ? ["auto", "java", "html5"][modeBox.currentIndex] : "auto";
        p.jnlpPath = Families.hasMedia(family) ? jnlpField.text.trim() : "";
        p.clientCert = certPath;
        p.clientKey = keyPath;
        return p;
    }
    function save(andConnect) {
        error = "";
        const p = profile();
        if (andConnect) {
            // connectHost saves the profile first (and the password, when remembered).
            dialog.close();
            dialog.saved(p);
            controller.connectHost(p, passwordField.text);
            passwordField.text = "";
            return;
        }
        const s = controller.saveHost(p, passwordField.text, rememberSwitch.checked);
        if (!s.id) {
            error = controller.message;
            return;
        }
        passwordField.text = "";
        dialog.close();
        dialog.saved(s);
    }
    onClosed: passwordField.text = ""

    ColumnLayout {
        spacing: 0

        Kirigami.InlineMessage {
            Layout.fillWidth: true
            Layout.margins: Kirigami.Units.largeSpacing
            visible: dialog.error !== ""
            type: Kirigami.MessageType.Error
            text: dialog.error
        }

        FormCard.FormHeader { title: "Server" }
        FormCard.FormCard {
            FormCard.FormTextFieldDelegate {
                id: hostField
                label: "BMC address"
                placeholderText: "bmc.example.net or 192.0.2.10"
                status: Kirigami.MessageType.Error
                statusMessage: dialog.hostProblem
                inputMethodHints: Qt.ImhUrlCharactersOnly | Qt.ImhNoAutoUppercase
            }
            FormCard.FormDelegateSeparator {}
            FormCard.FormTextFieldDelegate {
                id: nameField
                label: "Name"
                placeholderText: dialog.host || "Shown in the connection list"
            }
            FormCard.FormDelegateSeparator {}
            FormCard.FormComboBoxDelegate {
                id: familyBox
                text: "Console type"
                description: Families.list[Math.max(0, currentIndex)].hint
                model: Families.names()
                displayMode: FormCard.FormComboBoxDelegate.ComboBox
            }
        }

        FormCard.FormHeader { title: "Account" }
        FormCard.FormCard {
            FormCard.FormTextFieldDelegate {
                id: userField
                label: "Username"
                inputMethodHints: Qt.ImhNoAutoUppercase | Qt.ImhNoPredictiveText
            }
            FormCard.FormDelegateSeparator {}
            FormCard.FormPasswordFieldDelegate {
                id: passwordField
                label: "Password"
                placeholderText: dialog.editing && dialog.original.remember && rememberSwitch.checked
                    ? "Saved in KWallet; leave empty to keep it"
                    : rememberSwitch.checked ? "Optional; asked for on first connect" : "Asked for on every connect"
                onAccepted: if (dialog.valid) dialog.save(controller.state === "disconnected")
            }
            FormCard.FormDelegateSeparator {}
            FormCard.FormSwitchDelegate {
                id: rememberSwitch
                text: "Remember password in KWallet"
                description: "The password is never written to the profile file."
            }
        }

        FormCard.FormCard {
            Layout.topMargin: Kirigami.Units.largeSpacing
            FormCard.FormButtonDelegate {
                text: dialog.showAdvanced ? "Hide advanced options" : "Advanced options"
                description: "Console mode, security exceptions, client certificates"
                icon.name: dialog.showAdvanced ? "arrow-up" : "arrow-down"
                trailingLogo.visible: false
                onClicked: dialog.showAdvanced = !dialog.showAdvanced
            }
        }

        FormCard.FormHeader { title: "Console"; visible: dialog.showAdvanced && (dialog.family === "aten" || Families.hasMedia(dialog.family)) }
        FormCard.FormCard {
            visible: dialog.showAdvanced && (dialog.family === "aten" || Families.hasMedia(dialog.family))
            FormCard.FormComboBoxDelegate {
                id: modeBox
                visible: dialog.family === "aten"
                text: "Console mode"
                description: ["Use the Java iKVM protocol when the firmware offers it, otherwise the HTML5 console.",
                              "Always use the Java iKVM protocol (virtual media available).",
                              "Always use the HTML5 console (no virtual media)."][Math.max(0, currentIndex)]
                model: ["Automatic", "Java iKVM", "HTML5"]
                displayMode: FormCard.FormComboBoxDelegate.ComboBox
            }
            FormCard.FormDelegateSeparator { visible: modeBox.visible }
            FormCard.FormTextFieldDelegate {
                id: jnlpField
                label: "JNLP endpoint override"
                placeholderText: "Automatic"
            }
        }

        FormCard.FormHeader { title: "Security exceptions for this host"; visible: dialog.showAdvanced }
        FormCard.FormCard {
            visible: dialog.showAdvanced
            FormCard.FormSwitchDelegate {
                id: httpSwitch
                text: "Log in over plain HTTP"
                description: "Sends the password unencrypted. Only for BMCs without working HTTPS."
            }
            FormCard.FormDelegateSeparator {}
            FormCard.FormSwitchDelegate {
                id: tlsSwitch
                text: "Accept legacy or unverified TLS"
                description: "Allows TLS 1.0, obsolete ciphers and self-signed certificates."
            }
            FormCard.FormDelegateSeparator {}
            FormCard.FormSwitchDelegate {
                id: plainSwitch
                text: "Allow unencrypted console and media"
                description: "Keystrokes and screen content travel in clear text."
            }
        }
        Kirigami.InlineMessage {
            Layout.fillWidth: true
            Layout.leftMargin: Kirigami.Units.gridUnit
            Layout.rightMargin: Kirigami.Units.gridUnit
            Layout.topMargin: Kirigami.Units.smallSpacing
            visible: dialog.showAdvanced && (httpSwitch.checked || tlsSwitch.checked || plainSwitch.checked)
            type: Kirigami.MessageType.Warning
            text: "Security exceptions are enabled for this host. Use them only on a trusted management network."
        }

        FormCard.FormHeader { title: "Client certificate (mutual TLS)"; visible: dialog.showAdvanced && dialog.family === "aten" }
        FormCard.FormCard {
            visible: dialog.showAdvanced && dialog.family === "aten"
            Repeater {
                model: [{label: "Certificate", key: false}, {label: "Private key", key: true}]
                delegate: FormCard.FormTextDelegate {
                    required property var modelData
                    readonly property string path: modelData.key ? dialog.keyPath : dialog.certPath
                    text: modelData.label
                    description: path || "None"
                    trailing: RowLayout {
                        QQC2.Button {
                            text: "Choose…"
                            onClicked: {
                                const f = controller.chooseCertificate(modelData.key);
                                if (!f) return;
                                if (modelData.key) dialog.keyPath = f; else dialog.certPath = f;
                            }
                        }
                        QQC2.ToolButton {
                            icon.name: "edit-clear"
                            text: "Clear " + modelData.label.toLowerCase()
                            display: QQC2.AbstractButton.IconOnly
                            visible: !!path
                            onClicked: if (modelData.key) dialog.keyPath = ""; else dialog.certPath = ""
                        }
                    }
                }
            }
        }
        Item { implicitHeight: Kirigami.Units.largeSpacing }
    }
}
