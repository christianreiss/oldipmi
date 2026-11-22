pragma ComponentBehavior: Bound
import QtQuick
import QtQuick.Controls as QQC2
import QtQuick.Layouts
import org.kde.kirigami as Kirigami
import "families.js" as Families

// A connection's advanced options: the console mode and the JNLP override,
// the security exceptions and the client certificate, each as far as the
// family offers it. The connection dialog loads and saves what they show.
ColumnLayout {
    id: advancedSection
    required property string family
    property alias modeIndex: modeBox.currentIndex
    property alias jnlpPath: jnlpField.text
    property alias http: httpSwitch.checked
    property alias legacyTLS: tlsSwitch.checked
    property alias plainConsole: plainSwitch.checked
    property string certPath: ""
    property string keyPath: ""
    // The console modes: as a profile stores them, as the list names them and
    // what each does.
    readonly property var modes: [
        {id: "auto", label: "Automatic", text: "Use the Java iKVM protocol when the firmware offers it, otherwise the HTML5 console."},
        {id: "java", label: "Java iKVM", text: "Always use the Java iKVM protocol (virtual media available)."},
        {id: "html5", label: "HTML5", text: "Always use the HTML5 console (no virtual media)."}
    ]
    spacing: Math.round(Theme.gu * 0.9)
    Layout.fillWidth: true

    ColumnLayout {
        visible: Families.offers(advancedSection.family, "consoleMode") || Families.offers(advancedSection.family, "jnlp")
        spacing: Kirigami.Units.largeSpacing
        Layout.fillWidth: true
        Caption { text: "CONSOLE"; Layout.fillWidth: true }
        RowLayout {
            visible: Families.offers(advancedSection.family, "consoleMode")
            spacing: Kirigami.Units.largeSpacing
            Layout.fillWidth: true
            ColumnLayout {
                spacing: 1
                Layout.fillWidth: true
                QQC2.Label { text: "Console mode"; font.weight: Font.DemiBold }
                QQC2.Label {
                    text: advancedSection.modes[Math.max(0, modeBox.currentIndex)].text
                    font.pointSize: Theme.small
                    color: Theme.muted
                    wrapMode: Text.Wrap
                    Layout.fillWidth: true
                }
            }
            QQC2.ComboBox {
                id: modeBox
                model: advancedSection.modes.map(m => m.label)
                Accessible.name: "Console mode"
                Layout.alignment: Qt.AlignVCenter
            }
        }
        LabeledField {
            visible: Families.offers(advancedSection.family, "jnlp")
            label: "JNLP endpoint override"
            FormField {
                id: jnlpField
                placeholderText: "Automatic"
                font.family: Theme.mono.family
                Accessible.name: "JNLP endpoint override"
            }
        }
    }

    ColumnLayout {
        spacing: Kirigami.Units.largeSpacing
        Layout.fillWidth: true
        Caption { text: "SECURITY EXCEPTIONS FOR THIS HOST"; Layout.fillWidth: true }
        SwitchRow {
            id: httpSwitch
            title: "Log in over plain HTTP"
            description: "Sends the password unencrypted. Only for BMCs without working HTTPS."
        }
        SwitchRow {
            id: tlsSwitch
            title: "Accept legacy or unverified TLS"
            description: "Allows TLS 1.0 and obsolete ciphers, and skips checking the BMC's certificate and name."
        }
        SwitchRow {
            id: plainSwitch
            title: "Allow unencrypted console and media"
            description: "Keystrokes and screen content travel in clear text."
        }
        NoticeBox {
            visible: httpSwitch.checked || tlsSwitch.checked || plainSwitch.checked
            text: "Security exceptions are enabled for this host. Use them only on a trusted management network."
        }
    }

    ColumnLayout {
        visible: Families.offers(advancedSection.family, "clientCertificate")
        spacing: Kirigami.Units.largeSpacing
        Layout.fillWidth: true
        Caption { text: "CLIENT CERTIFICATE (MUTUAL TLS)"; Layout.fillWidth: true }
        Repeater {
            model: [{label: "Certificate", key: false}, {label: "Private key", key: true}]
            delegate: RowLayout {
                id: certRow
                required property var modelData
                readonly property string path: modelData.key ? advancedSection.keyPath : advancedSection.certPath
                spacing: Kirigami.Units.largeSpacing
                Layout.fillWidth: true
                ColumnLayout {
                    spacing: 1
                    Layout.fillWidth: true
                    QQC2.Label { text: certRow.modelData.label; font.weight: Font.DemiBold }
                    QQC2.Label {
                        text: certRow.path || "None"
                        font.family: certRow.path ? Theme.mono.family : Kirigami.Theme.defaultFont.family
                        font.pointSize: Theme.small
                        color: Theme.muted
                        elide: Text.ElideMiddle
                        Layout.fillWidth: true
                    }
                }
                ActionButton {
                    tone: "secondary"
                    text: "Choose…"
                    onClicked: {
                        const f = Controller.chooseCertificate(certRow.modelData.key);
                        if (!f) return;
                        if (certRow.modelData.key) advancedSection.keyPath = f; else advancedSection.certPath = f;
                    }
                }
                IconButton {
                    icon.name: "edit-clear"
                    text: "Clear " + certRow.modelData.label.toLowerCase()
                    visible: !!certRow.path
                    onClicked: if (certRow.modelData.key) advancedSection.keyPath = ""; else advancedSection.certPath = ""
                }
            }
        }
    }
}
