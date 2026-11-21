.pragma library
.import OldIPMI 1.0 as App
// Console families understood by oldipmi-core: desktop/families.json, which the
// app registers as the FamilyTable singleton (main.cpp). `id` is stored in the
// profile.
var list = App.FamilyTable.list;
// The family's place in the list, or -1 for one this version does not know.
function index(id) {
    for (var i = 0; i < list.length; ++i)
        if (list[i].id === id) return i;
    return -1;
}
function name(id) {
    for (var i = 0; i < list.length; ++i)
        if (list[i].id === id) return list[i].name;
    return id || "Unknown";
}
// The family's short name ("AMI MegaRAC" rather than the full description); an
// unknown family's id.
function shortName(id) {
    var i = index(id);
    return i >= 0 ? list[i].short : (id || "");
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
// Whether the family offers an advanced option of the connection dialog: a flag
// of its entry (consoleMode, jnlp or clientCertificate).
function offers(id, option) {
    for (var i = 0; i < list.length; ++i)
        if (list[i].id === id) return list[i][option] === true;
    return false;
}
// False for a family without an account (no username or password: the demo
// console); true for any other, known or not.
function needsAccount(id) {
    for (var i = 0; i < list.length; ++i)
        if (list[i].id === id) return list[i].account !== false;
    return true;
}

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
// The most likely next step for each class of error the backend names in an
// error's code (testdata/ipc/names.json, codes), and the words that tell the
// class of an error without one, tried in this order.
var errorHints = [
    ["dns", /no such host|server misbehaving/i, "Check the BMC address; it does not resolve in DNS."],
    ["unreachable", /i\/o timeout|deadline exceeded|no route to host|network is unreachable/i, "The BMC did not answer. Check that it is reachable from this computer (VPN, management network)."],
    ["refused", /connection refused/i, "The BMC refused the connection. Its web or KVM service may be disabled or on another port."],
    // TLS: the destination is checked before it is trusted, and the legacy
    // option is offered only where it is the likely fix.
    ["tls_name", /certificate is valid for|cannot validate certificate for|not valid for any names/i, "The BMC's certificate does not name this address. Check that the address is the right BMC first; if it is (old BMCs often keep a factory certificate), enable \"Accept legacy or unverified TLS\" in the connection's advanced options."],
    ["tls_unverified", /unknown authority|expired or is not yet valid|legacy common name|failed to verify certificate/i, "The BMC's certificate cannot be verified (self-signed or expired, as on most old BMCs). If this is the right BMC, enable \"Accept legacy or unverified TLS\" in the connection's advanced options."],
    // Only when the BMC chose TLS 1.0 or 1.1 (hex 301, 302), which the option
    // allows; a bare alert does not say which side is too old or too new.
    ["tls_old", /selected unsupported protocol version 30[12]\b/i, "The BMC uses an old TLS version (1.0 or 1.1). \"Accept legacy or unverified TLS\" in the connection's advanced options allows it, but also skips checking the BMC's certificate and name: use it only for the right BMC on a trusted network."],
    ["tls", /certificate|x509|tls:|handshake/i, "The secure connection (TLS) to the BMC failed. See the technical details below."],
    ["auth", /rejected login|login was not accepted|status 401|status 403|token rejected|551/i, "Check the username and password. Some BMCs also lock an account after failed attempts."],
    ["text_mode", /text mode/i, "The server's screen is in text mode, which OldIPMI cannot draw for this BMC yet. Use the BMC's own web console until the server switches to graphics."],
    ["plain_console", /unencrypted|plain-console/i, "This BMC offers only an unencrypted console. Enable it in the connection's advanced options if the network is trusted."]
];
// The most likely next step for an error, or "": by the class the backend
// named, else (no code, or one this version does not know) by its words.
function errorHint(message, code) {
    var i, m = errorDetail(message);
    for (i = 0; i < errorHints.length; ++i)
        if (errorHints[i][0] === code) return errorHints[i][2];
    for (i = 0; i < errorHints.length; ++i)
        if (errorHints[i][1].test(m)) return errorHints[i][2];
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
// `now` (ms) defaults to the current time.
function relativeTime(iso, now) {
    if (!iso) return "never";
    var d = new Date(iso), s = ((now === undefined ? Date.now() : now) - d.getTime()) / 1000;
    if (isNaN(s)) return "never";
    if (s < 60) return "just now";
    if (s < 3600) return Math.round(s / 60) + " min ago";
    if (s < 86400) return Math.round(s / 3600) + " h ago";
    if (s < 86400 * 30) return Math.round(s / 86400) + " days ago";
    return d.toLocaleDateString();
}
