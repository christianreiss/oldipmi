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
                    Kirigami.Heading { text: "OldIPMI " + AppSettings.version() }
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
            description: Theme.shownPath(Controller.profilesPath())
        }
        FormCard.FormDelegateSeparator {}
        FormCard.FormTextDelegate {
            text: "Preferences"
            description: Theme.shownPath(AppSettings.configPath())
        }
        FormCard.FormDelegateSeparator {}
        FormCard.FormTextDelegate {
            text: "Passwords"
            description: "KWallet, folder \"OldIPMI\"; one entry per connection"
        }
    }
    FormCard.FormHeader { title: "License" }
    FormCard.FormCard {
        FormCard.FormTextDelegate {
            text: "© 2026 Christian Reiß"
            description: "OldIPMI is free software: you can share and change it under the GNU General Public License, version 3 or later. It comes with no warranty."
        }
        FormCard.FormDelegateSeparator {}
        FormCard.FormTextDelegate {
            text: "Third-party components"
            description: "Provenance and license texts are listed in THIRD_PARTY.md and the licenses/ directory shipped with the application."
        }
        FormCard.FormDelegateSeparator {}
        FormCard.FormButtonDelegate {
            text: "Read the license"
            description: "GNU GPL 3.0 on gnu.org; also in the LICENSE file shipped with OldIPMI"
            icon.name: "license"
            onClicked: Qt.openUrlExternally("https://www.gnu.org/licenses/gpl-3.0.html")
        }
    }
}
