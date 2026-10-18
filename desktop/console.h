#pragma once
#include <QImage>
#include <QQuickPaintedItem>
#include <QSet>
class Console : public QQuickPaintedItem {
  Q_OBJECT
  Q_PROPERTY(bool fit READ fit WRITE setFit NOTIFY fitChanged)
public:
  explicit Console(QQuickItem *p = nullptr);
  void paint(QPainter *p) override;
  bool fit() const { return m_fit; }
  void setFit(bool value) {
    m_fit = value;
    update();
    emit fitChanged();
  }
public slots:
  void setFrame(QImage image);
signals:
  void fitChanged();
  void keyInput(int usage, bool down);
  void pointerInput(int x, int y, int buttons, int wheel);
  void releaseInput();

protected:
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
  bool m_fit = true;
  QSet<int> m_pressed;
  QRectF imageRect() const;
  void pointer(QPointF, int, int);
  int usage(QKeyEvent *) const;
};
