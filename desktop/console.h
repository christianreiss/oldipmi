#pragma once
#include <QImage>
#include <QQuickPaintedItem>
#include <QColor>
#include <QSet>
class Console : public QQuickPaintedItem {
  Q_OBJECT
  Q_PROPERTY(bool fit READ fit WRITE setFit NOTIFY fitChanged)
  // True while keyboard and pointer go to the remote host.
  Q_PROPERTY(bool captured READ captured NOTIFY capturedChanged)
  // A lone tap of Right Ctrl releases the capture (the tap is still forwarded).
  Q_PROPERTY(bool rightCtrlReleases MEMBER m_rightCtrlReleases NOTIFY capturedChanged)
  Q_PROPERTY(QColor accent MEMBER m_accent NOTIFY capturedChanged)
  Q_PROPERTY(QSize frameSize READ frameSize NOTIFY frameSizeChanged)
public:
  explicit Console(QQuickItem *p = nullptr);
  void paint(QPainter *p) override;
  bool fit() const { return m_fit; }
  bool captured() const { return hasActiveFocus(); }
  QSize frameSize() const { return m_image.size(); }
  Q_INVOKABLE void capture() { forceActiveFocus(Qt::MouseFocusReason); }
  Q_INVOKABLE void release();
  void setFit(bool value) {
    m_fit = value;
    update();
    emit fitChanged();
  }
public slots:
  void setFrame(QImage image);
signals:
  void fitChanged();
  void capturedChanged();
  void frameSizeChanged();
  void keyInput(int usage, bool down);
  void pointerInput(int x, int y, int buttons, int wheel);
  void releaseInput();

protected:
  bool event(QEvent *) override;
  void focusInEvent(QFocusEvent *) override;
  void keyPressEvent(QKeyEvent *) override;
  void keyReleaseEvent(QKeyEvent *) override;
  void mousePressEvent(QMouseEvent *) override;
  void mouseReleaseEvent(QMouseEvent *) override;
  void mouseMoveEvent(QMouseEvent *) override;
  void hoverMoveEvent(QHoverEvent *) override;
  void wheelEvent(QWheelEvent *) override;
  void focusOutEvent(QFocusEvent *) override;

private:
  QImage m_image;
  bool m_fit = true, m_rightCtrlReleases = true, m_lonePress = false, m_swallow = false;
  QColor m_accent{0x3d, 0xae, 0xe9};
  QSet<int> m_pressed;
  QRectF imageRect() const;
  void pointer(QPointF, int, int);
  int usage(QKeyEvent *) const;
};
