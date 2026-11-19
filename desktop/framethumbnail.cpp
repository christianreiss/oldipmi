#include "framethumbnail.h"
#include "controller.h"
#include "session.h"
#include <QPainter>
#include <QPainterPath>
#include <QQuickWindow>

QPointer<Controller> FrameThumbnail::s_controller;

FrameThumbnail::FrameThumbnail(QQuickItem *parent) : QQuickPaintedItem(parent) {
  setAntialiasing(true);
  connect(this, &FrameThumbnail::radiusChanged, this, [this] { update(); });
  m_throttle.setSingleShot(true);
  m_throttle.setInterval(500);
  connect(&m_throttle, &QTimer::timeout, this, &FrameThumbnail::apply);
  // Sessions are created and removed by the controller; follow ours.
  if (s_controller)
    connect(s_controller, &Controller::changed, this, &FrameThumbnail::attach);
}
void FrameThumbnail::setController(Controller *controller) { s_controller = controller; }
void FrameThumbnail::setProfileId(const QString &id) {
  if (id == m_id)
    return;
  m_id = id;
  emit profileIdChanged();
  attach();
}
void FrameThumbnail::attach() {
  Session *s = s_controller && !m_id.isEmpty() ? s_controller->session(m_id) : nullptr;
  // A deleted session leaves m_session null too; drop its picture then.
  if (s == m_session && (s || m_latest.isNull()))
    return;
  if (m_session)
    disconnect(m_session, nullptr, this, nullptr);
  m_session = s;
  m_latest = s ? s->lastFrame : QImage();
  if (s)
    connect(s, &Session::frameReady, this, &FrameThumbnail::take);
  m_throttle.stop();
  apply();
}
void FrameThumbnail::take(const QImage &image) {
  m_latest = image; // implicitly shared, cheap
  // The first picture and the end of the picture show at once; only the
  // stream in between is throttled.
  if (m_frame.isNull() || image.isNull()) {
    m_throttle.stop();
    apply();
  } else if (!m_throttle.isActive()) {
    m_throttle.start();
  }
}
void FrameThumbnail::apply() {
  const bool had = !m_frame.isNull();
  const qreal dpr = window() ? window()->effectiveDevicePixelRatio() : 1.0;
  const QSize target = (QSizeF(width(), height()) * dpr).toSize();
  if (m_latest.isNull() || target.isEmpty())
    m_frame = QImage();
  else // fill the tile; the overhang is cropped in paint()
    m_frame = m_latest.scaled(target, Qt::KeepAspectRatioByExpanding, Qt::SmoothTransformation);
  update();
  if (had != !m_frame.isNull())
    emit hasFrameChanged();
}
void FrameThumbnail::geometryChange(const QRectF &newGeometry, const QRectF &oldGeometry) {
  QQuickPaintedItem::geometryChange(newGeometry, oldGeometry);
  if (newGeometry.size() != oldGeometry.size())
    apply();
}
void FrameThumbnail::paint(QPainter *painter) {
  if (m_frame.isNull() || width() <= 0 || height() <= 0)
    return;
  // Centre crop with the tile's aspect ratio.
  QSizeF crop(width(), height());
  crop.scale(QSizeF(m_frame.size()), Qt::KeepAspectRatio);
  const QRectF source((m_frame.width() - crop.width()) / 2, (m_frame.height() - crop.height()) / 2,
                      crop.width(), crop.height());
  painter->setRenderHint(QPainter::SmoothPixmapTransform);
  painter->setRenderHint(QPainter::Antialiasing);
  if (m_radius > 0) {
    QPainterPath clip;
    clip.addRoundedRect(QRectF(0, 0, width(), height()), m_radius, m_radius);
    painter->setClipPath(clip);
  }
  painter->drawImage(QRectF(0, 0, width(), height()), m_frame, source);
}
