import QtQuick
import QtQuick.Controls as QQC2

// A small upper-case label above a group, a field or a value. The caller
// writes the text in capitals.
QQC2.Label {
    font.pointSize: Theme.tiny
    font.weight: Font.DemiBold
    font.letterSpacing: 0.6
    color: Theme.muted
}
