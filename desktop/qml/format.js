.pragma library
// Texts made from values: times, a capital letter, a profile's exceptions.
// How long ago an ISO time was, in words ("5 min ago"); `now` (ms) defaults
// to the current time.
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
// The time from since to now (both ms) as m:ss, or h:mm:ss.
function elapsed(since, now) {
    if (!since) return "";
    const s = Math.max(0, Math.floor((now - since) / 1000));
    const pad = n => (n < 10 ? "0" : "") + n;
    return s >= 3600 ? Math.floor(s / 3600) + ":" + pad(Math.floor(s / 60) % 60) + ":" + pad(s % 60)
                     : Math.floor(s / 60) + ":" + pad(s % 60);
}
function capitalized(s) { return s ? s.charAt(0).toUpperCase() + s.slice(1) : "" }
// Any security exception enabled for a profile, as a short list ("" if none).
function exceptions(p) {
    var out = [];
    if (p.http) out.push("Plain HTTP login");
    if (p.legacyTLS) out.push("Legacy TLS");
    if (p.allowPlainConsole) out.push("Unencrypted console");
    return out.join(", ");
}
