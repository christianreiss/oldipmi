.pragma library
// Console families understood by oldipmi-core. `id` is stored in the profile.
var list = [
    { id: "aten", name: "Supermicro (ATEN)",
      hint: "Supermicro X9 and newer, and most X8 boards. Uses the Java iKVM protocol or the HTML5 console, whichever the firmware offers." },
    { id: "ami", name: "AMI MegaRAC",
      hint: "AMI MegaRAC firmware, including Supermicro X8 boards that shipped with it (e.g. X8DTU), and ASUS, Tyan or Gigabyte BMCs." },
    { id: "avocent", name: "Dell iDRAC (Avocent)",
      hint: "Dell PowerEdge 12th generation and older with an Avocent-based iDRAC 6/7 virtual console." },
    { id: "cisco", name: "Cisco IMC",
      hint: "Cisco UCS C-Series Integrated Management Controller (Avocent-based KVM)." },
    { id: "demo", name: "Demo console (no hardware)",
      hint: "A synthetic console that echoes keys, pointer and clicks. For trying OldIPMI and testing AI agents without a BMC. Address \"demo\", or e.g. \"demo-1920x1080\"." }
];
function index(id) {
    for (var i = 0; i < list.length; ++i)
        if (list[i].id === id) return i;
    return 0;
}
function name(id) {
    for (var i = 0; i < list.length; ++i)
        if (list[i].id === id) return list[i].name;
    return id || "Unknown";
}
function names() { return list.map(function (f) { return f.name; }); }
// ISO mount and JNLP import exist only for the AMI and ATEN adapters.
function hasMedia(id) { return id === "ami" || id === "aten"; }
function needsAccount(id) { return id !== "demo"; }

var stages = [
    { id: "logging_in", name: "Logging in", failed: "Login failed" },
    { id: "discovering", name: "Discovering console", failed: "Console discovery failed" },
    { id: "connecting", name: "Connecting", failed: "Console connection failed" }
];
function stageIndex(id) {
    for (var i = 0; i < stages.length; ++i)
        if (stages[i].id === id) return i;
    return id === "connected" ? stages.length : -1;
}
function failedTitle(id) {
    var i = stageIndex(id);
    return i >= 0 && i < stages.length ? stages[i].failed : "Connection failed";
}
// The backend reports "<Stage> failed: <cause>". Drop the stage (it is the
// heading) and add the most likely next step for common causes.
function explainError(message) {
    var m = (message || "").replace(/^[A-Za-z ]+ failed: /, "");
    m = m.charAt(0).toUpperCase() + m.slice(1);
    var hints = [
        [/no such host|server misbehaving/i, "Check the BMC address; it does not resolve in DNS."],
        [/i\/o timeout|deadline exceeded|no route to host|network is unreachable/i, "The BMC did not answer. Check that it is reachable from this computer (VPN, management network)."],
        [/connection refused/i, "The BMC refused the connection. Its web or KVM service may be disabled or on another port."],
        [/certificate|x509|tls:|handshake/i, "TLS failed. Old BMCs often need \"Accept legacy or unverified TLS\" in the connection's advanced options."],
        [/rejected login|login was not accepted|status 401|status 403|token rejected|551/i, "Check the username and password. Some BMCs also lock an account after failed attempts."],
        [/packet 139/i, "Login and the KVM channel work, but OldIPMI cannot decode this BMC's video format yet. Use the BMC's own web console for now."],
        [/unencrypted|plain-console/i, "This BMC offers only an unencrypted console. Enable it in the connection's advanced options if the network is trusted."]
    ];
    for (var i = 0; i < hints.length; ++i)
        if (hints[i][0].test(m)) return hints[i][1] + "\n\n" + m; // next step first, detail below
    return m;
}
function relativeTime(iso) {
    if (!iso) return "never";
    var d = new Date(iso), s = (Date.now() - d.getTime()) / 1000;
    if (isNaN(s)) return "never";
    if (s < 60) return "just now";
    if (s < 3600) return Math.round(s / 60) + " min ago";
    if (s < 86400) return Math.round(s / 3600) + " h ago";
    if (s < 86400 * 30) return Math.round(s / 86400) + " days ago";
    return d.toLocaleDateString();
}
