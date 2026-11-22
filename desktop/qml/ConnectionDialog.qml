pragma ComponentBehavior: Bound
import QtQuick
import QtQuick.Controls as QQC2
import QtQuick.Layouts
import org.kde.kirigami as Kirigami
import "families.js" as Families
import "stages.js" as Stages

// Editor for one connection profile ("New connection" / "Edit connection").
AppDialog {
    id: dialog
    objectName: "connectionDialog"

    property var original: ({})
    readonly property bool editing: !!original.id
    // Connecting an open profile only brings it to the front, so its edits would not be saved.
    readonly property bool openNow: editing && Stages.isOpen(controller.sessions, original.id)
    // The chosen family's tile; -1 keeps a stored family this version does not know.
    property int familyIndex: 0
    property bool showAdvanced: false
    property string error: ""
    signal saved(var profile)

    // Opens the advanced options and scrolls them into view once they are laid
    // out: after the toggle, and in the screenshot tour. A profile with
    // exceptions opens them without scrolling, so its name and address stay first.
    property bool revealPending: false
    function revealAdvanced() {
        showAdvanced = true;
        revealPending = true;
        tryReveal();
    }
    function tryReveal() {
        if (revealPending && advancedSection.height > 0 && dialog.scrollTo(advancedToggle, Math.round(Theme.gu * 0.5)))
            revealPending = false;
    }

    // A short window (large text on a small screen): the console type takes one
    // line and the explanations give way, so the address field is in view.
    readonly property bool compact: parent ? parent.height < Theme.gu * 24 : false

    title: editing ? "Edit connection" : "New connection"
    subheading: compact ? "" : "OldIPMI logs in to the BMC, finds its console and opens it here."
    preferredWidth: Math.round(Theme.gu * 37)
    footerHint: openNow ? "Disconnect this console to connect with the changes."
                        : valid ? "Ctrl+Enter saves and connects" : ""
    footerHintOptional: !openNow
    buttons: [
        ActionButton {
            tone: "ghost"
            text: "Cancel"
            onClicked: dialog.close()
        },
        ActionButton {
            tone: "secondary"
            text: "Save"
            icon.name: "document-save"
            enabled: dialog.valid
            onClicked: dialog.save(false)
        },
        ActionButton {
            tone: "primary"
            text: "Save and Connect"
            icon.name: "media-playback-start"
            enabled: dialog.valid && !dialog.openNow
            onClicked: dialog.save(true)
        }
    ]

    function openFor(profile) {
        original = profile || {};
        const p = original;
        nameField.text = p.name || "";
        hostField.text = p.host || "";
        familyIndex = Families.index(p.family || "aten");
        userField.text = p.username || "";
        passwordField.text = "";
        rememberSwitch.checked = p.remember === undefined ? settings.rememberPasswords : !!p.remember;
        advancedSection.modeIndex = Math.max(0, advancedSection.modes.findIndex(m => m.id === (p.consoleMode || "auto")));
        advancedSection.http = !!p.http;
        advancedSection.legacyTLS = !!p.legacyTLS;
        advancedSection.plainConsole = !!p.allowPlainConsole;
        advancedSection.jnlpPath = p.jnlpPath || "";
        advancedSection.certPath = p.clientCert || "";
        advancedSection.keyPath = p.clientKey || "";
        error = "";
        revealPending = false;
        showAdvanced = advancedSection.http || advancedSection.legacyTLS || advancedSection.plainConsole
                       || !!advancedSection.jnlpPath || !!advancedSection.certPath || advancedSection.modeIndex > 0;
        open();
        (editing || hostField.text !== "" ? nameField : hostField).forceActiveFocus();
    }

    // Accept a pasted URL ("https://bmc.example/#login") and keep only the host.
    readonly property string host: hostField.text.trim().replace(/^[a-z]+:\/\//i, "").replace(/[\/?#].*$/, "")
    readonly property string family: familyIndex >= 0 ? Families.list[familyIndex].id : (original.family || "")
    readonly property bool account: Families.needsAccount(family)
    readonly property string hostProblem: host === "" ? "" : /[\s@\\]/.test(host) ? "Enter a hostname or IP address only." : ""
    readonly property bool valid: host !== "" && hostProblem === "" && (userField.text !== "" || !account)

    function profile() {
        const p = Object.assign({}, original);
        p.name = nameField.text.trim() || host;
        p.host = host;
        p.family = family;
        p.username = account ? userField.text : "";
        p.remember = rememberSwitch.checked;
        p.http = advancedSection.http;
        p.legacyTLS = advancedSection.legacyTLS;
        p.allowPlainConsole = advancedSection.plainConsole;
        // A family this version knows says which console options apply; an
        // unknown one keeps its stored options.
        if (familyIndex >= 0) {
            p.consoleMode = Families.offers(family, "consoleMode") ? advancedSection.modes[advancedSection.modeIndex].id : "auto";
            p.jnlpPath = Families.offers(family, "jnlp") ? advancedSection.jnlpPath.trim() : "";
        }
        p.clientCert = advancedSection.certPath;
        p.clientKey = advancedSection.keyPath;
        return p;
    }
    function save(andConnect) {
        error = "";
        const p = profile();
        const password = account ? passwordField.text : "";
        if (andConnect) {
            // connectHost saves the profile first (and the password, when remembered).
            dialog.close();
            dialog.saved(p);
            controller.connectHost(p, password);
            passwordField.text = "";
            return;
        }
        const s = controller.saveHost(p, password, rememberSwitch.checked);
        if (!s.id) {
            error = controller.message;
            return;
        }
        passwordField.text = "";
        dialog.close();
        dialog.saved(s);
    }
    onClosed: passwordField.text = ""

    Shortcut {
        sequences: ["Ctrl+Return", "Ctrl+Enter"]
        enabled: dialog.opened && dialog.valid && !dialog.openNow
        onActivated: dialog.save(true)
    }

    Rectangle {
        visible: dialog.error !== ""
        Layout.fillWidth: true
        implicitHeight: errorText.implicitHeight + Math.round(Theme.gu * 1)
        radius: Theme.radius
        color: Theme.alpha(Theme.negative, Theme.dark ? 0.2 : 0.12)
        QQC2.Label {
            id: errorText
            anchors { fill: parent; margins: Math.round(Theme.gu * 0.5) }
            text: dialog.error
            color: Theme.toneText(Theme.negative, Theme.card)
            wrapMode: Text.Wrap
        }
    }

    // ── Console type: one tile per family ──
    FamilyTiles {
        familyIndex: dialog.familyIndex
        family: dialog.family
        compact: dialog.compact
        onChosen: index => dialog.familyIndex = index
    }

    // ── Server and account ──
    GridLayout {
        columns: width >= Theme.gu * 22 ? 2 : 1
        columnSpacing: Math.round(Theme.gu * 0.8)
        rowSpacing: Math.round(Theme.gu * 0.7)
        Layout.fillWidth: true

        LabeledField {
            label: "BMC address"
            Layout.preferredWidth: 1
            Layout.alignment: Qt.AlignTop
            FormField {
                id: hostField
                placeholderText: "bmc.example.net or 192.0.2.10"
                font.family: Theme.mono.family
                inputMethodHints: Qt.ImhUrlCharactersOnly | Qt.ImhNoAutoUppercase
                invalid: dialog.hostProblem !== ""
                Accessible.name: "BMC address"
            }
            QQC2.Label {
                visible: dialog.hostProblem !== ""
                text: dialog.hostProblem
                color: Theme.toneText(Theme.negative, Theme.card)
                font.pointSize: Theme.small
                wrapMode: Text.Wrap
                Layout.fillWidth: true
            }
        }
        LabeledField {
            label: "Name"
            Layout.preferredWidth: 1
            Layout.alignment: Qt.AlignTop
            FormField {
                id: nameField
                placeholderText: dialog.host || "Shown in the connection list"
                Accessible.name: "Name"
            }
        }
        LabeledField {
            visible: dialog.account
            label: "Username"
            Layout.preferredWidth: 1
            Layout.alignment: Qt.AlignTop
            FormField {
                id: userField
                font.family: Theme.mono.family
                inputMethodHints: Qt.ImhNoAutoUppercase | Qt.ImhNoPredictiveText
                Accessible.name: "Username"
            }
        }
        LabeledField {
            visible: dialog.account
            label: "Password"
            Layout.preferredWidth: 1
            Layout.alignment: Qt.AlignTop
            FormPasswordField {
                id: passwordField
                placeholderText: dialog.editing && dialog.original.remember && rememberSwitch.checked
                    ? "Saved in KWallet; leave empty to keep it"
                    : rememberSwitch.checked ? "Optional; asked for on first connect" : "Asked for on every connect"
                placeholderTextColor: Theme.muted
                Accessible.name: "Password"
                onAccepted: if (dialog.valid) dialog.save(!dialog.openNow)
            }
        }
    }

    Rectangle {
        visible: dialog.account
        Layout.fillWidth: true
        implicitHeight: rememberSwitch.implicitHeight + Math.round(Theme.gu * 1.3)
        radius: Theme.radiusPanel
        color: Theme.raised
        border.width: 1
        border.color: Theme.lineSoft
        SwitchRow {
            id: rememberSwitch
            anchors { fill: parent; leftMargin: Math.round(Theme.gu * 0.8); rightMargin: Math.round(Theme.gu * 0.8) }
            title: "Remember password in KWallet"
            description: "The password is never written to the profile file."
        }
    }
    // ── Advanced options ──
    QQC2.AbstractButton {
        id: advancedToggle
        Layout.fillWidth: true
        hoverEnabled: true
        padding: Math.round(Theme.gu * 0.7)
        Accessible.role: Accessible.Button
        Accessible.name: dialog.showAdvanced ? "Hide advanced options" : "Advanced options"
        onClicked: {
            if (dialog.showAdvanced)
                dialog.showAdvanced = false;
            else
                dialog.revealAdvanced();
        }
        background: Rectangle {
            radius: Theme.radiusPanel
            color: advancedToggle.down ? Theme.pressed : advancedToggle.hovered ? Theme.hover : "transparent"
            border.width: 1
            border.color: Theme.lineSoft
            FocusRing {
                visible: advancedToggle.visualFocus
            }
        }
        contentItem: RowLayout {
            spacing: Math.round(Theme.gu * 0.65)
            Kirigami.Icon {
                source: dialog.showAdvanced ? "arrow-down" : "arrow-right"
                implicitWidth: Kirigami.Units.iconSizes.small
                implicitHeight: implicitWidth
                color: Theme.text
            }
            ColumnLayout {
                spacing: 1
                Layout.fillWidth: true
                QQC2.Label {
                    text: dialog.showAdvanced ? "Hide advanced options" : "Advanced options"
                    font.weight: Font.DemiBold
                    Layout.fillWidth: true
                }
                QQC2.Label {
                    text: "Console mode, security exceptions, client certificates"
                    font.pointSize: Theme.small
                    color: Theme.muted
                    wrapMode: Text.Wrap
                    Layout.fillWidth: true
                }
            }
        }
    }

    AdvancedOptions {
        id: advancedSection
        visible: dialog.showAdvanced
        family: dialog.family
        onHeightChanged: dialog.tryReveal()
    }
}
