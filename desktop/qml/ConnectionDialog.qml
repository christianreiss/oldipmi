import QtQuick
import QtQuick.Controls as QQC2
import QtQuick.Layouts
import org.kde.kirigami as Kirigami
import "families.js" as Families

// Editor for one connection profile ("New connection" / "Edit connection").
AppDialog {
    id: dialog
    objectName: "connectionDialog"

    property var original: ({})
    readonly property bool editing: !!original.id
    // Connecting an open profile only brings it to the front, so its edits would not be saved.
    readonly property bool openNow: editing && (controller.sessions[original.id] || {state: "disconnected"}).state !== "disconnected"
    property int familyIndex: 0
    property string certPath: ""
    property string keyPath: ""
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
        const flick = dialog.contentItem ? dialog.contentItem.contentItem : null;
        if (!revealPending || !flick || advancedSection.height <= 0)
            return;
        revealPending = false;
        const top = advancedToggle.mapToItem(flick.contentItem, 0, 0).y - Math.round(Theme.gu * 0.5);
        flick.contentY = Math.max(0, Math.min(top, flick.contentHeight - flick.height));
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
        modeBox.currentIndex = Math.max(0, ["auto", "java", "html5"].indexOf(p.consoleMode || "auto"));
        httpSwitch.checked = !!p.http;
        tlsSwitch.checked = !!p.legacyTLS;
        plainSwitch.checked = !!p.allowPlainConsole;
        jnlpField.text = p.jnlpPath || "";
        certPath = p.clientCert || "";
        keyPath = p.clientKey || "";
        error = "";
        revealPending = false;
        showAdvanced = httpSwitch.checked || tlsSwitch.checked || plainSwitch.checked || !!jnlpField.text || !!certPath || modeBox.currentIndex > 0;
        open();
        (editing || hostField.text !== "" ? nameField : hostField).forceActiveFocus();
    }

    // Accept a pasted URL ("https://bmc.example/#login") and keep only the host.
    readonly property string host: hostField.text.trim().replace(/^[a-z]+:\/\//i, "").replace(/[\/?#].*$/, "")
    readonly property string family: Families.list[Math.max(0, familyIndex)].id
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
        p.http = httpSwitch.checked;
        p.legacyTLS = tlsSwitch.checked;
        p.allowPlainConsole = plainSwitch.checked;
        p.consoleMode = Families.offers(family, "consoleMode") ? ["auto", "java", "html5"][modeBox.currentIndex] : "auto";
        p.jnlpPath = Families.offers(family, "jnlp") ? jnlpField.text.trim() : "";
        p.clientCert = certPath;
        p.clientKey = keyPath;
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

    // A small upper-case label above a group or field.
    component Caption: QQC2.Label {
        font.pointSize: Theme.tiny
        font.weight: Font.DemiBold
        font.letterSpacing: 0.6
        color: Theme.muted
        Layout.fillWidth: true
    }
    component FieldLabel: QQC2.Label {
        font.pointSize: Theme.small
        font.weight: Font.DemiBold
        color: Theme.mix(Theme.text, Theme.card, 0.1)
        Layout.fillWidth: true
    }
    // A switch with a title and an explanation, as one row.
    component SwitchRow: RowLayout {
        id: switchRow
        property alias title: switchTitle.text
        property alias description: switchText.text
        property alias checked: toggle.checked
        spacing: Kirigami.Units.largeSpacing
        Layout.fillWidth: true
        ColumnLayout {
            spacing: 1
            Layout.fillWidth: true
            QQC2.Label { id: switchTitle; font.weight: Font.DemiBold; wrapMode: Text.Wrap; Layout.fillWidth: true }
            QQC2.Label { id: switchText; font.pointSize: Theme.small; color: Theme.muted; wrapMode: Text.Wrap; Layout.fillWidth: true }
        }
        QQC2.Switch {
            id: toggle
            Accessible.name: switchTitle.text
            // What switching it on means, also for a screen reader that Tabs straight here.
            Accessible.description: switchText.text
            Layout.alignment: Qt.AlignVCenter
        }
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
    ColumnLayout {
        spacing: Kirigami.Units.largeSpacing
        Layout.fillWidth: true
        Caption { text: "CONSOLE TYPE" }
        QQC2.ComboBox {
            id: familyBox
            visible: dialog.compact
            model: Families.names()
            currentIndex: dialog.familyIndex
            onActivated: index => dialog.familyIndex = index
            Accessible.name: "Console type"
            Layout.fillWidth: true
        }
        GridLayout {
            id: tiles
            visible: !dialog.compact
            readonly property real tileWidth: Theme.gu * 5.4
            columns: Math.max(2, Math.min(Families.list.length, Math.floor((width + columnSpacing) / (tileWidth + columnSpacing))))
            columnSpacing: Kirigami.Units.largeSpacing
            rowSpacing: Kirigami.Units.largeSpacing
            Layout.fillWidth: true
            Repeater {
                id: tileRepeater
                model: Families.list
                delegate: QQC2.AbstractButton {
                    id: tile
                    required property var modelData
                    required property int index
                    readonly property bool selected: dialog.familyIndex === index
                    function choose(i) {
                        dialog.familyIndex = (i + Families.list.length) % Families.list.length;
                        tileRepeater.itemAt(dialog.familyIndex).forceActiveFocus(Qt.TabFocusReason);
                    }
                    Layout.fillWidth: true
                    Layout.preferredWidth: 1
                    implicitHeight: Math.max(Math.round(Theme.gu * (tiles.columns < 5 ? 4.4 : 5.2)), tileContent.implicitHeight + Theme.gu)
                    hoverEnabled: true
                    focusPolicy: Qt.StrongFocus
                    padding: Kirigami.Units.smallSpacing
                    Accessible.role: Accessible.RadioButton
                    Accessible.name: modelData.name
                    Accessible.description: modelData.hint
                    Accessible.checkable: true
                    Accessible.checked: selected
                    onClicked: dialog.familyIndex = index
                    Keys.onLeftPressed: choose(index - 1)
                    Keys.onRightPressed: choose(index + 1)
                    Keys.onSpacePressed: dialog.familyIndex = index
                    background: Rectangle {
                        radius: Theme.radius + 3
                        color: tile.selected ? Theme.accentTint
                             : tile.hovered ? Theme.mix(Theme.raised, Theme.text, 0.05) : Theme.raised
                        border.width: tile.selected ? 2 : 1
                        border.color: tile.selected ? Theme.accent : Theme.lineSoft
                        Rectangle {
                            visible: tile.selected
                            anchors { top: parent.top; right: parent.right; margins: 6 }
                            width: Math.round(Theme.gu * 1.05)
                            height: width
                            radius: width / 2
                            color: Theme.accentStrong
                            Kirigami.Icon {
                                anchors.centerIn: parent
                                width: Math.round(parent.width * 0.7)
                                height: width
                                source: "checkmark"
                                color: Theme.white
                                isMask: true
                            }
                        }
                        Rectangle {
                            visible: tile.visualFocus
                            anchors.fill: parent
                            anchors.margins: -3
                            radius: parent.radius + 3
                            color: "transparent"
                            border.width: 2
                            border.color: Theme.accent
                        }
                    }
                    contentItem: ColumnLayout {
                        id: tileContent
                        spacing: 2
                        Image {
                            readonly property real side: Math.round(Theme.gu * (tiles.columns < 5 ? 2 : 2.55))
                            Layout.preferredWidth: side
                            Layout.preferredHeight: side
                            Layout.alignment: Qt.AlignHCenter
                            source: Families.image(tile.modelData.id)
                            sourceSize.width: side * 2
                            sourceSize.height: side * 2
                            fillMode: Image.PreserveAspectFit
                            smooth: true
                        }
                        QQC2.Label {
                            text: tile.modelData.short
                            font.pointSize: Theme.small
                            font.weight: tile.selected ? Font.DemiBold : Font.Normal
                            horizontalAlignment: Text.AlignHCenter
                            elide: Text.ElideRight
                            Layout.fillWidth: true
                        }
                    }
                }
            }
        }
        QQC2.Label {
            visible: !dialog.compact
            text: Families.list[Math.max(0, dialog.familyIndex)].hint
            font.pointSize: Theme.small
            color: Theme.muted
            wrapMode: Text.Wrap
            Layout.fillWidth: true
        }
    }

    // ── Server and account ──
    GridLayout {
        columns: width >= Theme.gu * 22 ? 2 : 1
        columnSpacing: Math.round(Theme.gu * 0.8)
        rowSpacing: Math.round(Theme.gu * 0.7)
        Layout.fillWidth: true

        ColumnLayout {
            spacing: Kirigami.Units.smallSpacing
            Layout.fillWidth: true
            Layout.preferredWidth: 1
            Layout.alignment: Qt.AlignTop
            FieldLabel { text: "BMC address" }
            QQC2.TextField {
                id: hostField
                placeholderText: "bmc.example.net or 192.0.2.10"
                placeholderTextColor: Theme.muted
                font.family: Theme.mono.family
                color: Theme.text
                inputMethodHints: Qt.ImhUrlCharactersOnly | Qt.ImhNoAutoUppercase
                implicitHeight: Math.max(Theme.controlHeight + 2, contentHeight + topPadding + bottomPadding)
                leftPadding: Math.round(Theme.gu * 0.65)
                rightPadding: Math.round(Theme.gu * 0.65)
                verticalAlignment: TextInput.AlignVCenter
                background: InputBackground { control: hostField; invalid: dialog.hostProblem !== "" }
                Accessible.name: "BMC address"
                Layout.fillWidth: true
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
        ColumnLayout {
            spacing: Kirigami.Units.smallSpacing
            Layout.fillWidth: true
            Layout.preferredWidth: 1
            Layout.alignment: Qt.AlignTop
            FieldLabel { text: "Name" }
            QQC2.TextField {
                id: nameField
                placeholderText: dialog.host || "Shown in the connection list"
                placeholderTextColor: Theme.muted
                color: Theme.text
                implicitHeight: Math.max(Theme.controlHeight + 2, contentHeight + topPadding + bottomPadding)
                leftPadding: Math.round(Theme.gu * 0.65)
                rightPadding: Math.round(Theme.gu * 0.65)
                verticalAlignment: TextInput.AlignVCenter
                background: InputBackground { control: nameField }
                Accessible.name: "Name"
                Layout.fillWidth: true
            }
        }
        ColumnLayout {
            visible: dialog.account
            spacing: Kirigami.Units.smallSpacing
            Layout.fillWidth: true
            Layout.preferredWidth: 1
            Layout.alignment: Qt.AlignTop
            FieldLabel { text: "Username" }
            QQC2.TextField {
                id: userField
                font.family: Theme.mono.family
                color: Theme.text
                inputMethodHints: Qt.ImhNoAutoUppercase | Qt.ImhNoPredictiveText
                implicitHeight: Math.max(Theme.controlHeight + 2, contentHeight + topPadding + bottomPadding)
                leftPadding: Math.round(Theme.gu * 0.65)
                rightPadding: Math.round(Theme.gu * 0.65)
                verticalAlignment: TextInput.AlignVCenter
                background: InputBackground { control: userField }
                Accessible.name: "Username"
                Layout.fillWidth: true
            }
        }
        ColumnLayout {
            visible: dialog.account
            spacing: Kirigami.Units.smallSpacing
            Layout.fillWidth: true
            Layout.preferredWidth: 1
            Layout.alignment: Qt.AlignTop
            FieldLabel { text: "Password" }
            Kirigami.PasswordField {
                id: passwordField
                placeholderText: dialog.editing && dialog.original.remember && rememberSwitch.checked
                    ? "Saved in KWallet; leave empty to keep it"
                    : rememberSwitch.checked ? "Optional; asked for on first connect" : "Asked for on every connect"
                placeholderTextColor: Theme.muted
                color: Theme.text
                implicitHeight: Math.max(Theme.controlHeight + 2, contentHeight + topPadding + bottomPadding)
                leftPadding: Math.round(Theme.gu * 0.65)
                verticalAlignment: TextInput.AlignVCenter
                background: InputBackground { control: passwordField }
                Accessible.name: "Password"
                Layout.fillWidth: true
                onAccepted: if (dialog.valid) dialog.save(!dialog.openNow)
            }
        }
    }

    Rectangle {
        visible: dialog.account
        Layout.fillWidth: true
        implicitHeight: rememberSwitch.implicitHeight + Math.round(Theme.gu * 1.3)
        radius: Theme.radius + 3
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
            radius: Theme.radius + 3
            color: advancedToggle.down ? Theme.pressed : advancedToggle.hovered ? Theme.hover : "transparent"
            border.width: 1
            border.color: Theme.lineSoft
            Rectangle {
                visible: advancedToggle.visualFocus
                anchors.fill: parent
                anchors.margins: -3
                radius: parent.radius + 3
                color: "transparent"
                border.width: 2
                border.color: Theme.accent
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

    ColumnLayout {
        id: advancedSection
        visible: dialog.showAdvanced
        spacing: Math.round(Theme.gu * 0.9)
        Layout.fillWidth: true
        onHeightChanged: dialog.tryReveal()

        ColumnLayout {
            visible: Families.offers(dialog.family, "consoleMode") || Families.offers(dialog.family, "jnlp")
            spacing: Kirigami.Units.largeSpacing
            Layout.fillWidth: true
            Caption { text: "CONSOLE" }
            RowLayout {
                visible: Families.offers(dialog.family, "consoleMode")
                spacing: Kirigami.Units.largeSpacing
                Layout.fillWidth: true
                ColumnLayout {
                    spacing: 1
                    Layout.fillWidth: true
                    QQC2.Label { text: "Console mode"; font.weight: Font.DemiBold }
                    QQC2.Label {
                        text: ["Use the Java iKVM protocol when the firmware offers it, otherwise the HTML5 console.",
                               "Always use the Java iKVM protocol (virtual media available).",
                               "Always use the HTML5 console (no virtual media)."][Math.max(0, modeBox.currentIndex)]
                        font.pointSize: Theme.small
                        color: Theme.muted
                        wrapMode: Text.Wrap
                        Layout.fillWidth: true
                    }
                }
                QQC2.ComboBox {
                    id: modeBox
                    model: ["Automatic", "Java iKVM", "HTML5"]
                    Accessible.name: "Console mode"
                    Layout.alignment: Qt.AlignVCenter
                }
            }
            ColumnLayout {
                visible: Families.offers(dialog.family, "jnlp")
                spacing: Kirigami.Units.smallSpacing
                Layout.fillWidth: true
                FieldLabel { text: "JNLP endpoint override" }
                QQC2.TextField {
                    id: jnlpField
                    placeholderText: "Automatic"
                    placeholderTextColor: Theme.muted
                    font.family: Theme.mono.family
                    color: Theme.text
                    implicitHeight: Math.max(Theme.controlHeight + 2, contentHeight + topPadding + bottomPadding)
                    leftPadding: Math.round(Theme.gu * 0.65)
                    rightPadding: Math.round(Theme.gu * 0.65)
                    verticalAlignment: TextInput.AlignVCenter
                    background: InputBackground { control: jnlpField }
                    Accessible.name: "JNLP endpoint override"
                    Layout.fillWidth: true
                }
            }
        }

        ColumnLayout {
            spacing: Kirigami.Units.largeSpacing
            Layout.fillWidth: true
            Caption { text: "SECURITY EXCEPTIONS FOR THIS HOST" }
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
            Rectangle {
                visible: httpSwitch.checked || tlsSwitch.checked || plainSwitch.checked
                Layout.fillWidth: true
                implicitHeight: warnRow.implicitHeight + Math.round(Theme.gu * 0.9)
                radius: Theme.radius
                color: Theme.alpha(Theme.warning, Theme.dark ? 0.18 : 0.12)
                RowLayout {
                    id: warnRow
                    anchors { fill: parent; margins: Math.round(Theme.gu * 0.45) }
                    spacing: Kirigami.Units.smallSpacing
                    Kirigami.Icon {
                        source: "security-medium"
                        implicitWidth: Kirigami.Units.iconSizes.small
                        implicitHeight: implicitWidth
                        color: Theme.toneText(Theme.warning, Theme.card)
                        isMask: true
                        Layout.alignment: Qt.AlignTop
                    }
                    QQC2.Label {
                        text: "Security exceptions are enabled for this host. Use them only on a trusted management network."
                        color: Theme.toneText(Theme.warning, Theme.card)
                        wrapMode: Text.Wrap
                        Layout.fillWidth: true
                    }
                }
            }
        }

        ColumnLayout {
            visible: Families.offers(dialog.family, "clientCertificate")
            spacing: Kirigami.Units.largeSpacing
            Layout.fillWidth: true
            Caption { text: "CLIENT CERTIFICATE (MUTUAL TLS)" }
            Repeater {
                model: [{label: "Certificate", key: false}, {label: "Private key", key: true}]
                delegate: RowLayout {
                    required property var modelData
                    readonly property string path: modelData.key ? dialog.keyPath : dialog.certPath
                    spacing: Kirigami.Units.largeSpacing
                    Layout.fillWidth: true
                    ColumnLayout {
                        spacing: 1
                        Layout.fillWidth: true
                        QQC2.Label { text: modelData.label; font.weight: Font.DemiBold }
                        QQC2.Label {
                            text: path || "None"
                            font.family: path ? Theme.mono.family : Kirigami.Theme.defaultFont.family
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
                            const f = controller.chooseCertificate(modelData.key);
                            if (!f) return;
                            if (modelData.key) dialog.keyPath = f; else dialog.certPath = f;
                        }
                    }
                    IconButton {
                        icon.name: "edit-clear"
                        text: "Clear " + modelData.label.toLowerCase()
                        visible: !!path
                        onClicked: if (modelData.key) dialog.keyPath = ""; else dialog.certPath = ""
                    }
                }
            }
        }
    }
}
