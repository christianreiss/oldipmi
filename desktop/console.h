#pragma once
#include <QImage>
#include <QPointer>
#include <QQuickPaintedItem>
#include <QSet>
class Console : public QQuickPaintedItem {
  Q_OBJECT
  Q_PROPERTY(bool fit READ fit WRITE setFit NOTIFY fitChanged)
  // True while keyboard and pointer go to the remote host.
  Q_PROPERTY(bool captured READ captured NOTIFY capturedChanged)
  // A lone tap of Right Ctrl releases the capture (the tap is still forwarded);
  // released, the next tap takes control again (see eventFilter()).
  Q_PROPERTY(bool rightCtrlReleases MEMBER m_rightCtrlReleases NOTIFY capturedChanged)
  Q_PROPERTY(QSize frameSize READ frameSize NOTIFY frameSizeChanged)
  // Where the remote screen is drawn, in item coordinates. The letterbox around it
  // stays transparent; QML draws the stage, the border and the capture ring there.
  Q_PROPERTY(QRectF imageRect READ imageRect NOTIFY imageRectChanged)
public:
  explicit Console(QQuickItem *p = nullptr);
  void paint(QPainter *p) override;
  bool fit() const { return m_fit; }
  bool captured() const { return hasActiveFocus(); }
  QSize frameSize() const { return m_image.size(); }
  QRectF imageRect() const;
  Q_INVOKABLE void capture() { forceActiveFocus(Qt::MouseFocusReason); }
  Q_INVOKABLE void release();
  void setFit(bool value) {
    if (m_fit == value)
      return;
    m_fit = value;
    update();
    emit fitChanged();
    emit imageRectChanged();
  }
public slots:
  void setFrame(QImage image);
signals:
  void fitChanged();
  void capturedChanged();
  void frameSizeChanged();
  void imageRectChanged();
  void keyInput(int usage, bool down);
  void pointerInput(int x, int y, int buttons, int wheel);
  void releaseInput();

protected:
  bool event(QEvent *) override;
  bool eventFilter(QObject *, QEvent *) override;
  void geometryChange(const QRectF &newGeometry, const QRectF &oldGeometry) override;
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
  bool m_idleTap = false; // Right Ctrl went down alone while released
  QSet<int> m_pressed;
  QPointer<QQuickItem> m_watched; // the window's content item, which has the focus after release()
  void watch(QQuickWindow *);
  void pointer(QPointF, int, int);
  int usage(QKeyEvent *) const;
};
