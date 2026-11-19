.pragma library
// Console families understood by oldipmi-core. `id` is stored in the profile.
var list = [
    { id: "aten", name: "Supermicro (ATEN)", short: "Supermicro",
      hint: "Supermicro X9 and newer, and most X8 boards. Uses the Java iKVM protocol or the HTML5 console, whichever the firmware offers." },
    { id: "ami", name: "AMI MegaRAC", short: "AMI MegaRAC",
      hint: "AMI MegaRAC firmware, including Supermicro X8 boards that shipped with it (e.g. X8DTU), and ASUS, Tyan or Gigabyte BMCs." },
    { id: "avocent", name: "Dell iDRAC (Avocent)", short: "Dell iDRAC",
      hint: "Dell PowerEdge 12th generation and older with an Avocent-based iDRAC 6/7 virtual console." },
    { id: "cisco", name: "Cisco IMC", short: "Cisco IMC",
      hint: "Cisco UCS C-Series Integrated Management Controller (Avocent-based KVM)." },
    { id: "demo", name: "Demo console (no hardware)", short: "Demo",
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
// The family's short name ("AMI MegaRAC" rather than the full description).
function shortName(id) {
    var f = list[index(id)];
    return f ? f.short : "";
}
// Bundled illustration per family (desktop/images, compiled into the binary).
function image(id) {
    for (var i = 0; i < list.length; ++i)
        if (list[i].id === id) return "qrc:/images/family-" + id + ".png";
    return "qrc:/images/family-aten.png";
}
var emptyImage = "qrc:/images/empty-state.png";
var errorImage = "qrc:/images/error-state.png";
function names() { return list.map(function (f) { return f.name; }); }
// ISO mount exists only for the AMI and ATEN adapters.
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
// The backend reports "<Stage> failed: <cause>". The stage is the heading, so
// only the cause is shown as the technical detail.
function errorDetail(message) {
    var m = (message || "").replace(/^[A-Za-z ]+ failed: /, "");
    return m.charAt(0).toUpperCase() + m.slice(1);
}
// The most likely next step for common causes, or "".
function errorHint(message) {
    var m = errorDetail(message);
    var hints = [
        [/no such host|server misbehaving/i, "Check the BMC address; it does not resolve in DNS."],
        [/i\/o timeout|deadline exceeded|no route to host|network is unreachable/i, "The BMC did not answer. Check that it is reachable from this computer (VPN, management network)."],
        [/connection refused/i, "The BMC refused the connection. Its web or KVM service may be disabled or on another port."],
        // TLS: the destination is checked before it is trusted, and the legacy
        // option is offered only where it is the likely fix.
        [/certificate is valid for|cannot validate certificate for|not valid for any names/i, "The BMC's certificate does not name this address. Check that the address is the right BMC first; if it is (old BMCs often keep a factory certificate), enable \"Accept legacy or unverified TLS\" in the connection's advanced options."],
        [/unknown authority|expired or is not yet valid|legacy common name|failed to verify certificate/i, "The BMC's certificate cannot be verified (self-signed or expired, as on most old BMCs). If this is the right BMC, enable \"Accept legacy or unverified TLS\" in the connection's advanced options."],
        [/protocol version|no cipher suite|handshake failure|insufficient security|no supported versions/i, "The BMC offers only old TLS versions or ciphers. Enable \"Accept legacy or unverified TLS\" in the connection's advanced options."],
        [/certificate|x509|tls:|handshake/i, "The secure connection (TLS) to the BMC failed. The technical details below name the cause."],
        [/rejected login|login was not accepted|status 401|status 403|token rejected|551/i, "Check the username and password. Some BMCs also lock an account after failed attempts."],
        [/text mode/i, "The server's screen is in text mode, which OldIPMI cannot draw for this BMC yet. Use the BMC's own web console until the server switches to graphics."],
        [/unencrypted|plain-console/i, "This BMC offers only an unencrypted console. Enable it in the connection's advanced options if the network is trusted."]
    ];
    for (var i = 0; i < hints.length; ++i)
        if (hints[i][0].test(m)) return hints[i][1];
    return "";
}
// Any security exception enabled for a profile, as a short list ("" if none).
function exceptions(p) {
    var out = [];
    if (p.http) out.push("Plain HTTP login");
    if (p.legacyTLS) out.push("Legacy TLS");
    if (p.allowPlainConsole) out.push("Unencrypted console");
    return out.join(", ");
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
