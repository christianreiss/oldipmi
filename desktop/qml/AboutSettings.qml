import QtQuick
import QtQuick.Controls as QQC2
import QtQuick.Layouts
import org.kde.kirigami as Kirigami
import org.kde.kirigamiaddons.formcard as FormCard

FormCard.FormCardPage {
    title: "About OldIPMI"

    FormCard.FormCard {
        Layout.topMargin: Kirigami.Units.gridUnit
        FormCard.AbstractFormDelegate {
            background: null
            contentItem: Row {
                spacing: Kirigami.Units.largeSpacing * 2
                Kirigami.Icon {
                    // Bundled, as in the sidebar: an installed theme icon may be missing or older.
                    source: "qrc:/oldipmi.svg"
                    width: Kirigami.Units.iconSizes.huge
                    height: width
                }
                Column {
                    anchors.verticalCenter: parent.verticalCenter
                    Kirigami.Heading { text: "OldIPMI " + settings.version() }
                    QQC2.Label {
                        text: "Native remote consoles for older BMCs: AMI MegaRAC, Supermicro (ATEN), Dell iDRAC (Avocent) and Cisco IMC. No Java, no browser."
                        wrapMode: Text.Wrap
                        width: Kirigami.Units.gridUnit * 22
                    }
                }
            }
        }
    }
    FormCard.FormHeader { title: "Files" }
    FormCard.FormCard {
        FormCard.FormTextDelegate {
            text: "Connection profiles"
            description: controller.profilesPath()
        }
        FormCard.FormDelegateSeparator {}
        FormCard.FormTextDelegate {
            text: "Preferences"
            description: settings.configPath()
        }
        FormCard.FormDelegateSeparator {}
        FormCard.FormTextDelegate {
            text: "Passwords"
            description: "KWallet, folder \"OldIPMI\"; one entry per connection"
        }
    }
    FormCard.FormHeader { title: "Licenses" }
    FormCard.FormCard {
        FormCard.FormTextDelegate {
            text: "OldIPMI and third-party components"
            description: "Provenance and license texts are listed in THIRD_PARTY.md and the licenses/ directory shipped with the application."
        }
    }
}
