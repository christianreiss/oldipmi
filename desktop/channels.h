#pragma once
#include <QByteArray>
#include <QImage>
#include <QJsonObject>
#include <QString>
#include <QVariantMap>
#include <optional>

// The framing of the two sockets between a Session and its oldipmi-core
// (internal/backend/ipc.go), without the sockets: what they deliver is
// appended, and complete messages are taken out.

// One message of the core on the control socket: an event of the console or,
// with the id of a command, the reply to it.
struct ControlEvent {
  QString type;                    // an ipc::Event*
  int id = 0;                      // the command it answers; 0 for an event of the console
  std::optional<QString> state;    // the console's new state, if the message has one
  QString message;
  QString code;                    // the class of an error (an ipc::Code*), if it has one
  std::optional<QVariantMap> info; // console information, if the message has some
  std::optional<bool> mounted;     // whether an ISO is mounted, if the message says
};

// Control messages, both ways: a 4-byte big-endian length and that many bytes of
// JSON, which has "version": 1.
class ControlChannel {
public:
  static constexpr quint32 MaxMessage = 1024 * 1024;
  enum class Status {
    Event,      // the next message is in the event
    NeedMore,   // no complete message yet
    BadLength,  // a length of 0 or over MaxMessage; the bytes stay, so it repeats
    BadMessage, // a message that is not a JSON object, taken out
    BadVersion, // a message without version 1, taken out
  };
  // A command as the core reads it.
  static QByteArray frame(const QJsonObject &command);
  void append(const QByteArray &bytes) { m_buffer += bytes; }
  Status next(ControlEvent &event);
  void clear() { m_buffer.clear(); }

private:
  QByteArray m_buffer;
};

// Pictures from the core: a 4-byte big-endian length, the width and the height
// (4 bytes each), then the RGBA pixels, at most ipc::MaxFrameWidth by
// ipc::MaxFrameHeight.
class VideoChannel {
public:
  enum class Status {
    Ok,       // latest holds the last complete picture, or is null if none was complete
    BadFrame, // a header did not fit its picture
  };
  void append(const QByteArray &bytes) { m_buffer += bytes; }
  Status take(QImage &latest);
  void clear() { m_buffer.clear(); }

private:
  QByteArray m_buffer;
};
