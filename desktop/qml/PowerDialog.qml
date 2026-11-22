import QtQuick

// Every power action is confirmed; the dialog names the server, the BMC and
// the consequence, and its button carries the verb.
ConfirmDialog {
    id: powerDialog
    // The action (on, soft, reset or off) and the profile it is for; one
    // without a host stands for the console in front.
    property string action: ""
    property var profile: ({})
    readonly property var powerText: ({
        on: { title: "Power on %1?", verb: "Power On", icon: "system-run",
              text: "The server is switched on if it is off." },
        soft: { title: "Shut down %1?", verb: "Shut Down", icon: "system-shutdown",
                text: "The operating system is asked to shut down (ACPI power button)." },
        reset: { title: "Reset %1?", verb: "Reset", icon: "system-reboot",
                 text: "The server restarts immediately. Unsaved data in the operating system is lost." },
        off: { title: "Power off %1 immediately?", verb: "Power Off", icon: "process-stop",
               text: "Power is cut immediately, like pulling the plug. Unsaved data is lost and file systems may need repair." }
    })
    readonly property var info: powerText[action] || ({title: "%1", verb: "OK", icon: "system-shutdown", text: ""})
    readonly property var target: profile.host ? profile : Controller.activeProfile
    readonly property bool destructive: action === "reset" || action === "off"
    title: info.title.arg(target.name || target.host || "the server")
    iconName: info.icon
    tone: destructive ? Theme.negative : action === "soft" ? Theme.warning : Theme.accent
    consequence: info.text
    detail: "BMC " + (target.host || "")
    verb: info.verb
    verbTone: destructive ? "danger" : "primary"
    // Breeze draws some of these icons red, which vanishes on the red button.
    verbIcon: destructive ? "" : info.icon
    onConfirmed: Controller.powerFor(profile.id || "", action)
}
