#pragma once
#include <QImage>
#include <QPointer>
#include <QQuickPaintedItem>
#include <QTimer>

class Controller;
class Session;

// Live miniature of one session's remote screen, for the connection list. It
// follows the session of `profileId` as sessions come and go, crops the picture
// to fill the tile and repaints at most twice a second, whatever the frame rate.
class FrameThumbnail : public QQuickPaintedItem {
  Q_OBJECT
  Q_PROPERTY(QString profileId READ profileId WRITE setProfileId NOTIFY profileIdChanged)
  Q_PROPERTY(bool hasFrame READ hasFrame NOTIFY hasFrameChanged)
  // Corner radius of the tile; the picture is clipped to it.
  Q_PROPERTY(qreal radius MEMBER m_radius NOTIFY radiusChanged)
public:
  explicit FrameThumbnail(QQuickItem *parent = nullptr);
  // The controller whose sessions all thumbnails show; set once at startup.
  static void setController(Controller *controller);
  QString profileId() const { return m_id; }
  void setProfileId(const QString &id);
  bool hasFrame() const { return !m_frame.isNull(); }
  void paint(QPainter *painter) override;
signals:
  void profileIdChanged();
  void hasFrameChanged();
  void radiusChanged();

protected:
  void geometryChange(const QRectF &newGeometry, const QRectF &oldGeometry) override;

private:
  static QPointer<Controller> s_controller;
  QString m_id;
  QPointer<Session> m_session;
  QImage m_latest, m_frame;
  qreal m_radius = 0;
  QTimer m_throttle;
  void attach();
  void take(const QImage &image);
  void apply();
};
