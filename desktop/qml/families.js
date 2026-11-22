.pragma library
.import OldIPMI 1.0 as App
// Console families understood by oldipmi-core: desktop/families.json, which the
// app has as the FamilyTable singleton (app/qmltypes.h). `id` is stored in the
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
