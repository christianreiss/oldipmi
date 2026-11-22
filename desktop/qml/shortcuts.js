.pragma library
// The window's keyboard shortcuts, as tooltips, key caps and the settings name
// them. Main.qml binds these texts as the keys, except New, Find and Quit,
// which are the platform's standard keys, the same texts on Linux; Full screen
// is both its standard key and F11.
var newConnection = "Ctrl+N";
var find = "Ctrl+F";
var connectionList = "F9";
var fullScreen = "F11";
var leaveFullScreen = "Esc"; // while the console does not have the keyboard
var copyScreen = "Ctrl+Shift+C";
var disconnect = "Ctrl+D";
var disconnectAll = "Ctrl+Shift+D";
var settings = "Ctrl+,";
var quit = "Ctrl+Q";

// The list of keyboard shortcuts in the settings (GeneralSettings.qml): what
// each does, and its keys. The self-test checks that every shortcut of the
// window is in it.
var listed = [
    ["New connection", newConnection], ["Filter connections", find],
    ["Show or hide the connection list", connectionList], ["Full screen", fullScreen],
    ["Leave full screen", leaveFullScreen], ["Copy screen image", copyScreen],
    ["Disconnect", disconnect], ["Disconnect all", disconnectAll],
    ["Settings", settings], ["Quit", quit]
];
