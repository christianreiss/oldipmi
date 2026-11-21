#include "channels.h"
#include "ipc.h"
#include <QJsonDocument>
#include <QtEndian>

QByteArray ControlChannel::frame(const QJsonObject &command) {
  QByteArray b = QJsonDocument(command).toJson(QJsonDocument::Compact);
  QByteArray h(4, 0);
  qToBigEndian<quint32>(b.size(), h.data());
  return h + b;
}

ControlChannel::Status ControlChannel::next(ControlEvent &event) {
  if (m_buffer.size() < 4)
    return Status::NeedMore;
  quint32 n = qFromBigEndian<quint32>(m_buffer.constData());
  if (n == 0 || n > MaxMessage)
    return Status::BadLength;
  if (m_buffer.size() < 4 + n)
    return Status::NeedMore;
  QJsonParseError error;
  const auto doc = QJsonDocument::fromJson(m_buffer.mid(4, n), &error);
  m_buffer.remove(0, 4 + n);
  if (error.error != QJsonParseError::NoError || !doc.isObject())
    return Status::BadMessage;
  const auto o = doc.object();
  if (o.value("version").toInt() != 1)
    return Status::BadVersion;
  event = ControlEvent{};
  event.type = o.value("type").toString();
  event.id = o.value("id").toInt();
  if (o.contains("state"))
    event.state = o.value("state").toString();
  event.message = o.value("message").toString();
  event.code = o.value("code").toString();
  if (o.contains("info"))
    event.info = o.value("info").toObject().toVariantMap();
  if (o.contains("mounted"))
    event.mounted = o.value("mounted").toBool();
  return Status::Event;
}

VideoChannel::Status VideoChannel::take(QImage &latest) {
  latest = QImage();
  QImage last;
  while (m_buffer.size() >= 12) {
    quint32 n = qFromBigEndian<quint32>(m_buffer.constData()),
            w = qFromBigEndian<quint32>(m_buffer.constData() + 4),
            h = qFromBigEndian<quint32>(m_buffer.constData() + 8);
    if (w == 0 || h == 0 || w > ipc::MaxFrameWidth || h > ipc::MaxFrameHeight || n != 8 + w * h * 4)
      return Status::BadFrame;
    if (m_buffer.size() < 4 + n)
      break;
    last = QImage(reinterpret_cast<const uchar *>(m_buffer.constData() + 12), w, h, w * 4,
                  QImage::Format_RGBA8888)
               .copy();
    m_buffer.remove(0, 4 + n);
  }
  latest = last;
  return Status::Ok;
}
