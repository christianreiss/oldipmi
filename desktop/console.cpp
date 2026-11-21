#include "console.h"
#include "keymap.h"
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
  connect(this, &QQuickItem::windowChanged, this, &Console::watch);
  watch(window());
}
void Console::watch(QQuickWindow *w) {
  if (m_watched)
    m_watched->removeEventFilter(this);
  m_watched = w;
  m_held.clear();
  m_idleTap = false;
  if (m_watched)
    m_watched->installEventFilter(this);
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
// Released, a lone tap of Right Ctrl gives keyboard and mouse to the server
// again, like the host key of a virtual machine. Only while nothing else has the
// keyboard: release() hands the focus to the window's content item, so the tap
// works right after a release, also in full screen where the status strip and its
// button are hidden; dialogs, menus and fields keep the key. The tap itself is not
// forwarded. A tap means Right Ctrl alone: pressed while another key is held
// (whichever went down first) or with another key pressed before its release, it
// is a chord, so typing never moves to the server unnoticed. The filter sits on
// the window, which every key event passes before an item gets it, so a key held
// while the focus moves (to a field or a dialog and back) stays known as held, and
// its release is seen wherever it goes. Only when the window loses the keyboard,
// and releases go to another window, are the held keys forgotten.
bool Console::eventFilter(QObject *o, QEvent *e) {
  const auto type = e->type();
  if (o != m_watched)
    return QQuickPaintedItem::eventFilter(o, e);
  if (type == QEvent::FocusIn || type == QEvent::FocusOut) {
    m_held.clear();
    m_idleTap = false;
    return false;
  }
  if (type != QEvent::KeyPress && type != QEvent::KeyRelease && type != QEvent::ShortcutOverride)
    return false;
  auto *k = static_cast<QKeyEvent *>(e);
  if (k->isAutoRepeat())
    return false;
  if (hidUsage(k->nativeScanCode(), k->key()) != 228) { // another key: a chord, not a tap
    // Held keys by scan code; a ShortcutOverride stands for a press that a
    // shortcut may take. Generated events carry none and are not tracked.
    if (const quint32 scan = k->nativeScanCode()) {
      if (type == QEvent::KeyRelease)
        m_held.remove(scan);
      else
        m_held.insert(scan);
    }
    m_idleTap = false;
    return false;
  }
  if (type == QEvent::KeyPress)
    m_idleTap = m_held.isEmpty() && !hasActiveFocus() &&
                !(k->modifiers() & (Qt::ShiftModifier | Qt::AltModifier | Qt::MetaModifier));
  if (type != QEvent::KeyRelease)
    return false;
  const bool tap = m_idleTap;
  m_idleTap = false;
  // While captured, the release is the console's own (see keyReleaseEvent()).
  if (!tap || hasActiveFocus() || !m_rightCtrlReleases || m_image.isNull() || !isVisible() ||
      m_watched->activeFocusItem() != m_watched->contentItem())
    return false;
  capture();
  return true;
}
void Console::focusInEvent(QFocusEvent *e) {
  m_idleTap = false;
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
void Console::keyPressEvent(QKeyEvent *e) {
  if (e->isAutoRepeat()) {
    e->accept();
    return;
  }
  int u = hidUsage(e->nativeScanCode(), e->key());
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
  int u = hidUsage(e->nativeScanCode(), e->key());
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
  m_idleTap = false;
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
  // The first button of a gesture decides for all of it: a press on the margin
  // around the picture would land on the edge of the remote screen, so neither
  // it nor the rest of its gesture is sent. A drag that started on the picture
  // goes on to its edge, also with another button.
  if (e->buttons() == e->button())
    m_swallow = !imageRect().contains(e->position());
  if (m_swallow) {
    e->accept();
    return;
  }
  update();
  pointer(e->position(), e->buttons(), 0);
  e->accept();
}
void Console::mouseReleaseEvent(QMouseEvent *e) {
  if (m_swallow || !hasActiveFocus()) {
    if (e->buttons() == Qt::NoButton) // the gesture ends with its last button
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
  // Not sent: a turn on the margin (see mousePressEvent()); one while the button
  // of a press that was not sent is still held, since the server would take that
  // button as pressed and never see it released; and one without vertical
  // movement (a sideways scroll), since the wheel step is vertical only.
  const int dy = e->angleDelta().y();
  if (!imageRect().contains(e->position()) || (m_swallow && e->buttons() != Qt::NoButton) || dy == 0) {
    e->accept();
    return;
  }
  pointer(e->position(), e->buttons(), dy > 0 ? 1 : -1);
  e->accept();
}
