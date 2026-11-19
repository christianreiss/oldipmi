#include "console.h"
#include <QHoverEvent>
#include <QKeyEvent>
#include <QMouseEvent>
#include <QPainter>
#include <QQuickWindow>
#include <QWheelEvent>
#include <cmath>
Console::Console(QQuickItem *p) : QQuickPaintedItem(p) {
  setAcceptedMouseButtons(Qt::AllButtons);
  setAcceptHoverEvents(true);
  setFlag(ItemAcceptsInputMethod, false);
  setClip(true);
  setAntialiasing(false);
}
void Console::setFrame(QImage im) {
  bool resized = im.size() != m_image.size();
  m_image = im;
  update();
  if (resized) {
    emit frameSizeChanged();
    emit imageRectChanged();
  }
  if (m_image.isNull() && hasActiveFocus())
    release();
}
void Console::geometryChange(const QRectF &newGeometry, const QRectF &oldGeometry) {
  QQuickPaintedItem::geometryChange(newGeometry, oldGeometry);
  if (newGeometry.size() != oldGeometry.size())
    emit imageRectChanged();
}
void Console::release() {
  if (hasActiveFocus())
    setFocus(false);
  if (auto *w = window(); w && w->activeFocusItem() == this)
    w->contentItem()->forceActiveFocus();
}
bool Console::event(QEvent *e) {
  // While captured, every key belongs to the remote host: application
  // shortcuts (Ctrl+N, F11, Ctrl+Q, ...) must not fire.
  if (e->type() == QEvent::ShortcutOverride && hasActiveFocus() && !m_image.isNull()) {
    e->accept();
    return true;
  }
  return QQuickPaintedItem::event(e);
}
void Console::focusInEvent(QFocusEvent *e) {
  update();
  emit capturedChanged();
  QQuickPaintedItem::focusInEvent(e);
}
QRectF Console::imageRect() const {
  if (m_image.isNull())
    return {};
  QSizeF s = m_image.size();
  if (m_fit)
    s.scale(QSizeF(width(), height()), Qt::KeepAspectRatio);
  // Whole-pixel origin: at actual size every remote pixel lands on one screen pixel.
  return QRectF(std::floor((width() - s.width()) / 2), std::floor((height() - s.height()) / 2),
                s.width(), s.height());
}
void Console::paint(QPainter *p) {
  // Only the remote screen is painted. The letterbox stays transparent so the
  // stage behind it shows, and the capture ring is drawn outside the picture
  // (in QML), so no remote pixel is ever covered.
  if (m_image.isNull())
    return;
  p->setRenderHint(QPainter::SmoothPixmapTransform, m_fit);
  p->drawImage(imageRect(), m_image);
}
int Console::usage(QKeyEvent *e) const {
  // XKB keycodes used by Qt on both xcb and Wayland are evdev keycode + 8.
  static const int hid[] = {
      0,  41, 30, 31, 32, 33, 34, 35, 36, 37,  38, 39,  45,  46, 42,
      43, 20, 26, 8,  21, 23, 28, 24, 12, 18,  19, 47,  48,  40, 224,
      4,  22, 7,  9,  10, 11, 13, 14, 15, 51,  52, 53,  225, 49, 29,
      27, 6,  25, 5,  17, 16, 54, 55, 56, 229, 85, 226, 44,  57, 58,
      59, 60, 61, 62, 63, 64, 65, 66, 67, 83,  71, 95,  96,  97, 86,
      92, 93, 94, 87, 89, 90, 91, 98, 99, 0,   0,  100, 68,  69};
  int scan = int(e->nativeScanCode()) - 8;
  if (scan > 0 && scan < int(sizeof(hid) / sizeof(int)) && hid[scan])
    return hid[scan];
  switch (scan) {
  case 96:
    return 88;
  case 97:
    return 228;
  case 98:
    return 84;
  case 99:
    return 70;
  case 100:
    return 230;
  case 102:
    return 74;
  case 103:
    return 82;
  case 104:
    return 75;
  case 105:
    return 80;
  case 106:
    return 79;
  case 107:
    return 77;
  case 108:
    return 81;
  case 109:
    return 78;
  case 110:
    return 73;
  case 111:
    return 76;
  case 119:
    return 72;
  case 125:
    return 227;
  case 126:
    return 231;
  case 127:
    return 101;
  }
  if (e->key() >= Qt::Key_A && e->key() <= Qt::Key_Z)
    return e->key() - Qt::Key_A + 4;
  if (e->key() >= Qt::Key_1 && e->key() <= Qt::Key_9)
    return e->key() - Qt::Key_1 + 30;
  switch (e->key()) {
  case Qt::Key_0:
    return 39;
  case Qt::Key_Return:
    return 40;
  case Qt::Key_Escape:
    return 41;
  case Qt::Key_Backspace:
    return 42;
  case Qt::Key_Tab:
    return 43;
  case Qt::Key_Space:
    return 44;
  case Qt::Key_Control:
    return 224;
  case Qt::Key_Shift:
    return 225;
  case Qt::Key_Alt:
    return 226;
  case Qt::Key_Meta:
    return 227;
  case Qt::Key_Delete:
    return 76;
  default:
    return 0;
  }
}
void Console::keyPressEvent(QKeyEvent *e) {
  if (e->isAutoRepeat()) {
    e->accept();
    return;
  }
  int u = usage(e);
  m_lonePress = u == 228 && m_pressed.isEmpty();
  if (u) {
    m_pressed.insert(u);
    emit keyInput(u, true);
    e->accept();
  } else
    e->accept(); // unmapped keys must not leak to the application either
}
void Console::keyReleaseEvent(QKeyEvent *e) {
  if (e->isAutoRepeat()) {
    e->accept();
    return;
  }
  int u = usage(e);
  if (u) {
    m_pressed.remove(u);
    emit keyInput(u, false);
  }
  e->accept();
  if (u == 228 && m_lonePress && m_rightCtrlReleases && m_pressed.isEmpty())
    release();
  if (u != 228)
    m_lonePress = false;
}
void Console::focusOutEvent(QFocusEvent *e) {
  m_pressed.clear();
  m_lonePress = false;
  emit releaseInput();
  emit capturedChanged();
  update();
  QQuickPaintedItem::focusOutEvent(e);
}
void Console::pointer(QPointF pos, int buttons, int wheel) {
  if (m_image.isNull())
    return;
  auto r = imageRect();
  if (r.width() <= 0 || r.height() <= 0)
    return;
  int x = qBound(0, int((pos.x() - r.x()) * m_image.width() / r.width()),
                 m_image.width() - 1);
  int y = qBound(0, int((pos.y() - r.y()) * m_image.height() / r.height()),
                 m_image.height() - 1);
  emit pointerInput(x, y, buttons & 7, wheel);
}
void Console::mousePressEvent(QMouseEvent *e) {
  if (m_image.isNull()) {
    e->ignore();
    return;
  }
  if (!hasActiveFocus()) {
    // The first click only captures; it is not sent to the remote host.
    forceActiveFocus(Qt::MouseFocusReason);
    m_swallow = true;
    e->accept();
    return;
  }
  update();
  pointer(e->position(), e->buttons(), 0);
  e->accept();
}
void Console::mouseReleaseEvent(QMouseEvent *e) {
  if (m_swallow || !hasActiveFocus()) {
    m_swallow = false;
    e->accept();
    return;
  }
  pointer(e->position(), e->buttons(), 0);
  e->accept();
}
void Console::mouseMoveEvent(QMouseEvent *e) {
  if (m_swallow || !hasActiveFocus()) {
    e->accept();
    return;
  }
  pointer(e->position(), e->buttons(), 0);
  e->accept();
}
void Console::hoverMoveEvent(QHoverEvent *e) {
  if (hasActiveFocus())
    pointer(e->position(), 0, 0);
  e->accept();
}
void Console::wheelEvent(QWheelEvent *e) {
  if (!hasActiveFocus()) {
    e->ignore();
    return;
  }
  pointer(e->position(), e->buttons(), e->angleDelta().y() > 0 ? 1 : -1);
  e->accept();
}
